package sim

import (
	"errors"
	"fmt"
	"io"
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

	trainCtrllers map[ids.TrainID]*TrainController
	trains        []*ops.Train // in the order they were added

	// OutputDir is where logs/ and dumps/ are written. Defaults to ".".
	OutputDir string

	logger      io.Writer
	initialized bool
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
	ilk, err := interlocking.New(s.world)
	if err != nil {
		return err
	}
	s.ilk = ilk
	s.logger = io.Discard

	s.trainCtrllers = make(map[ids.TrainID]*TrainController)

	s.des = &des.DES[ops.RailwayEvent]{}
	s.des.Init()

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

func (s *Sim) logf(format string, args ...any) {
	fmt.Fprintf(s.logger, "[%.2f] "+format+"\n", append([]any{s.CurTime()}, args...)...)
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
	return os.Create(s.outputPath("logs", "logs.log"))
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
	s.logger = logger
	defer func() { s.logger = io.Discard }()

	if err := s.clearDumps(); err != nil {
		return err
	}

	for s.err == nil {
		ev, ok := s.NextEvent()
		if !ok {
			break
		}
		s.logEvent(ev)

		if ev.TrainID == "" {
			continue
		}
		tc, ok := s.trainCtrllers[ids.TrainID{Number: ev.TrainID}]
		if !ok {
			s.fail(fmt.Errorf("event %s for unknown train %q", ev.Type, ev.TrainID))
			break
		}
		tc.OnEvent(ev.Type, ev.Data)
	}

	for _, st := range s.Status() {
		if st.Phase != phaseExited.String() {
			s.logf("STUCK - %s (%s)%s", st.Train.Name, st.Phase, st.waitingSuffix())
		}
	}
	if err := s.DumpSim(); err != nil && s.err == nil {
		return err
	}
	return s.err
}

func (s *Sim) logEvent(ev des.Event[ops.RailwayEvent]) {
	switch data := ev.Data.(type) {
	case *ops.Train:
		if trk := data.CurrentTrack(); trk != nil {
			fmt.Fprintf(s.logger, "[%.2f] %s - %s  (Track %s - %dm)\n", ev.Time, ev.Type, data.Name, trk, int(trk.Length))
		} else {
			fmt.Fprintf(s.logger, "[%.2f] %s - %s \n", ev.Time, ev.Type, data.Name)
		}
	case *infra.TrackSegment:
		fmt.Fprintf(s.logger, "[%.2f] %s - %s\n", ev.Time, ev.Type, data)
	case *ops.MovementAuthority:
		fmt.Fprintf(s.logger, "[%.2f] %s - %s (up to %s)\n", ev.Time, ev.Type, ev.TrainID, data.Route().Step(data.Limit()).Edge.Track)
	case *interlocking.Route:
		fmt.Fprintf(s.logger, "[%.2f] %s - %s (%s)\n", ev.Time, ev.Type, ev.TrainID, data.Path())
	default:
		fmt.Fprintf(s.logger, "[%.2f] %s - %s\n", ev.Time, ev.Type, ev.TrainID)
	}
}

// TrainStatus is where a train ended up.
type TrainStatus struct {
	Train   *ops.Train
	Phase   string
	Waiting *RouteRequest // set if the train is queued for a route
}

func (st TrainStatus) waitingSuffix() string {
	if st.Waiting == nil {
		return ""
	}
	return fmt.Sprintf(" waiting since %.2f for %s pf%s: %v", st.Waiting.Since, st.Waiting.Station.Code, st.Waiting.PrefPfNo, st.Waiting.LastErr)
}

// Status reports every train's phase, in the order the trains were added.
func (s *Sim) Status() []TrainStatus {
	out := make([]TrainStatus, 0, len(s.trains))
	for _, t := range s.trains {
		st := TrainStatus{Train: t, Phase: phaseOutside.String()}
		if tc, ok := s.trainCtrllers[t.ID()]; ok {
			st.Phase = tc.phase.String()
		}
		if s.dispatcher != nil {
			st.Waiting = s.dispatcher.waitingFor(t.ID())
		}
		out = append(out, st)
	}
	return out
}
