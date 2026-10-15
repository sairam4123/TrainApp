package sim

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/railway/ops"
	"trainapp/units"
)

type Sim struct {
	des   *des.DES[ops.RailwayEvent]
	world *infra.World
	ilk   *interlocking.Interlocking

	dispatcher *Dispatcher
	prec       *precedence // nil when precedence is off

	trainCtrllers map[ids.TrainID]*TrainController
	trains        []*ops.Train // in the order they were added

	// OutputDir is where logs/ and dumps/ are written. Defaults to ".".
	OutputDir string
	// LogFormat is the format of the run log; see LogPath. Defaults to LogText.
	LogFormat LogFormat
	// Observer, if set, watches Run (e.g. to record it).
	Observer Observer
	// Priorities, if set, gives trains their priority by class and adjusts it
	// by delay; see priority.go. Without it a train's priority is its own
	// Priority.
	Priorities *PriorityConfig

	delayBands  []DelayBand // Priorities.DelayBands, smallest OverMin first
	log         *slog.Logger
	initialized bool
	running     bool
	err         error // first fatal error; stops the run
}

func (s *Sim) SetWorld(world *infra.World) *Sim {
	s.world = world
	return s
}

func (s *Sim) World() *infra.World {
	return s.world
}

func (s *Sim) Dispatcher() *Dispatcher {
	return s.dispatcher
}

func (s *Sim) Interlocking() *interlocking.Interlocking {
	return s.ilk
}

// AddTrain registers a train. Call it before Init.
func (s *Sim) AddTrain(train *ops.Train) error {
	if s.initialized {
		return errors.New("sim: AddTrain after Init")
	}
	if err := train.Validate(); err != nil {
		return err
	}
	for _, t := range s.trains {
		if t.ID() == train.ID() {
			return fmt.Errorf("sim: duplicate train number %s", train.Number)
		}
	}
	s.trains = append(s.trains, train)
	return nil
}

func (s *Sim) Init() error {
	if s.world == nil {
		return errors.New("sim: world is nil, did you call SetWorld?")
	}
	if !s.world.IsFinalized() {
		if err := s.world.Finalize(); err != nil {
			return err
		}
	}
	if err := s.initPriorities(); err != nil {
		return err
	}
	if err := s.checkSchedules(); err != nil {
		return err
	}
	ilk, err := interlocking.New(s.world)
	if err != nil {
		return err
	}
	s.ilk = ilk
	s.log = slog.New(slog.DiscardHandler)

	s.trainCtrllers = make(map[ids.TrainID]*TrainController)

	s.des = &des.DES[ops.RailwayEvent]{}
	s.des.Init()

	s.prec = nil
	if s.Priorities != nil && s.Priorities.Precedence != nil {
		s.prec = newPrecedence(s, s.Priorities.Precedence)
	}
	s.dispatcher = newDispatcher(s)

	for _, train := range s.trains {
		s.trainCtrllers[train.ID()] = &TrainController{
			sim:   s,
			train: train,
		}
		first := train.Schedule()[0]
		if err := s.des.Add(first.ArrTime-des.MinDeltaTime, ops.WorldEntered, train, train.Number); err != nil {
			return fmt.Errorf("train %s: %w", train.Number, err)
		}
	}
	s.initialized = true
	return nil
}

// checkSchedules checks every train has a schedule point at every station it
// runs through: it must be able to run from each stop to the next without
// running onto another station's platform tracks. A station it does not stop
// at needs a pass point. Unknown stations are left for the run to report.
func (s *Sim) checkSchedules() error {
	var errs []error
	for _, t := range s.trains {
		sched := t.Schedule()
		for i := 1; i < len(sched); i++ {
			from, okFrom := s.world.GetStation(sched[i-1].StnCode)
			to, okTo := s.world.GetStation(sched[i].StnCode)
			if !okFrom || !okTo || from == to {
				continue
			}
			var starts []infra.Position
			for _, pf := range from.Platforms() {
				edge, _ := s.world.TrackGraph.Edge(pf.Track)
				starts = append(starts, infra.Position{At: edge.To, Via: edge}, infra.Position{At: edge.From, Via: edge})
			}
			other := func(step infra.Step) bool {
				stn, ok := s.world.StationOfPlatform(step.Edge.Track)
				return ok && stn != from && stn != to
			}
			ends, err := s.world.VisitEnds(starts, []ids.StnCode{to.Code}, other)
			if err != nil {
				errs = append(errs, fmt.Errorf("train %s: %w", t.Number, err))
			} else if len(ends) == 0 {
				errs = append(errs, fmt.Errorf("train %s: cannot run from %s to %s without passing another station; give it a pass point there", t.Number, from.Code, to.Code))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Sim) NextEvent() (des.Event[ops.RailwayEvent], bool) {
	return s.des.NextEvent()
}

// ScheduleEventAfter schedules an event delta minutes from now. Deltas below
// des.MinDeltaTime are raised to it, so nothing is ever scheduled in the past.
func (s *Sim) ScheduleEventAfter(delta units.Minutes, evtype ops.RailwayEvent, data any, trainId ids.TrainID) {
	s.ScheduleEventAt(s.CurTime()+max(float64(delta), des.MinDeltaTime), evtype, data, trainId)
}

func (s *Sim) ScheduleEventNext(evtype ops.RailwayEvent, data any, trainId ids.TrainID) {
	s.ScheduleEventAfter(des.MinDeltaTime, evtype, data, trainId)
}

func (s *Sim) ScheduleEventAt(time float64, evType ops.RailwayEvent, data any, trainId ids.TrainID) {
	if err := s.des.Add(time, evType, data, trainId.Number); err != nil {
		s.fail(err)
	}
}

func (s *Sim) CurTime() float64 {
	return s.des.CurTime
}

// fail records the first fatal error; Run stops after the current event.
func (s *Sim) fail(err error) {
	if s.err == nil {
		s.err = fmt.Errorf("[%.2f] %w", s.CurTime(), err)
	}
}

func (s *Sim) outputPath(parts ...string) string {
	dir := s.OutputDir
	if dir == "" {
		dir = "."
	}
	return filepath.Join(append([]string{dir}, parts...)...)
}

func (s *Sim) DumpSim() error {
	if err := os.MkdirAll(s.outputPath("dumps"), 0755); err != nil {
		return err
	}
	dump, err := os.Create(s.outputPath("dumps", fmt.Sprintf("dump%d.txt", int(s.des.CurTime*100))))
	if err != nil {
		return err
	}
	defer dump.Close()

	for _, st := range s.Status() {
		fmt.Fprintf(dump, "Train %s - %s", st.Train, st.Phase)
		if trk := st.Train.CurrentTrack(); trk != nil {
			fmt.Fprintf(dump, " - Track %s", trk)
		}
		fmt.Fprintln(dump)
	}
	fmt.Fprintln(dump)

	for _, track := range s.world.TrackGraph.ListTracks() {
		r, o := s.ilk.TrackHolder(track)
		fmt.Fprintf(dump, "Track %s - Reserved %s - Occupied %s\n", track, r, o)
	}
	fmt.Fprintln(dump)
	return nil
}

func (s *Sim) InitLogger() (*os.File, error) {
	if err := os.MkdirAll(s.outputPath("logs"), 0755); err != nil {
		return nil, err
	}
	return os.Create(s.LogPath())
}

// clearDumps removes dump files from earlier runs, and nothing else.
func (s *Sim) clearDumps() error {
	old, err := filepath.Glob(s.outputPath("dumps", "dump*.txt"))
	if err != nil {
		return err
	}
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}

// Run processes events until none are left or a fatal error occurs.
func (s *Sim) Run() error {
	if !s.initialized {
		return errors.New("sim: Run before Init")
	}
	logger, err := s.InitLogger()
	if err != nil {
		return err
	}
	defer logger.Close()
	s.log = s.newLogger(logger)
	s.running = true
	defer func() {
		s.log = slog.New(slog.DiscardHandler)
		s.running = false
	}()
	if s.prec != nil {
		for _, o := range s.prec.early {
			s.logOverride(o, "applied", "")
		}
		s.prec.early = nil
	}

	if err := s.clearDumps(); err != nil {
		return err
	}

	for s.err == nil {
		ev, ok := s.NextEvent()
		if !ok {
			break
		}
		s.handle(ev)
		if s.Observer != nil {
			s.Observer.AfterEvent(ev)
		}
	}

	for _, st := range s.Status() {
		if st.Phase != phaseExited.String() {
			s.logStuck(st)
		}
	}
	if s.err != nil {
		s.log.Error("SIM_ERROR", "err", s.err.Error())
	}
	if err := s.DumpSim(); err != nil && s.err == nil {
		return err
	}
	return s.err
}

func (s *Sim) handle(ev des.Event[ops.RailwayEvent]) {
	s.logEvent(ev)
	if s.prec != nil {
		s.prec.invalidate()
	}
	if ev.Type == ops.PrecedenceOverride {
		s.applyOverride(ev.Data.(Override))
		return
	}
	if ev.TrainID == "" {
		return
	}
	tc, ok := s.trainCtrllers[ids.TrainID{Number: ev.TrainID}]
	if !ok {
		s.fail(fmt.Errorf("event %s for unknown train %q", ev.Type, ev.TrainID))
		return
	}
	tc.OnEvent(ev.Type, ev.Data)
}

// TrainStatus is where a train ended up.
type TrainStatus struct {
	Train    *ops.Train
	Phase    string
	Waiting  *RouteRequest // set if the train is queued for a route
	Priority int           // priority now, see Sim.Priority
	Delay    units.Minutes // how late it is running now
	// HeldFor lists the trains it is held back for (see precedence.go).
	HeldFor []ids.TrainID
}

// Status reports every train's phase, in the order the trains were added.
func (s *Sim) Status() []TrainStatus {
	out := make([]TrainStatus, 0, len(s.trains))
	for _, t := range s.trains {
		st := TrainStatus{Train: t, Phase: phaseOutside.String(), Priority: t.Priority}
		if tc, ok := s.trainCtrllers[t.ID()]; ok {
			st.Phase = tc.phase.String()
			st.Priority = s.Priority(t.ID())
			st.Delay = t.Delay(s.CurTime())
		}
		if s.dispatcher != nil {
			st.Waiting = s.dispatcher.waitingFor(t.ID())
		}
		if s.prec != nil {
			for _, d := range s.prec.holds[t.ID()] {
				st.HeldFor = append(st.HeldFor, d.For)
			}
		}
		out = append(out, st)
	}
	return out
}

// Decisions returns every precedence decision made so far, in the order they
// were made: active ones and those that have ended. Empty when precedence is
// off.
func (s *Sim) Decisions() []Decision {
	if s.prec == nil {
		return nil
	}
	return s.prec.decisions()
}
