// Package timetable turns a finished run into a timetable.
//
// Trains given only stop points (a dwell, no times) and pass points (no time)
// are run by the sim; their actual times are the timetable. Each train's first
// time, its arrival at the first station, is when it enters the simulated
// section and is the only fixed input. The stations are through stations, so
// every train has an arrival at its first station and a departure from its
// last.
//
// Arrival and pass times are when the front reaches the end of the platform
// track, as the sim measures them. A train that stood at a pass point gets a
// halt there, and one that stood at a stop past its dwell an extended dwell;
// both say why (classify.go). Round turns the times into timetable times and
// Check runs the result again to show it can be kept.
package timetable

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/motion"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
)

// Point kinds.
const (
	KindStop = "stop"
	KindPass = "pass"
	KindHalt = "halt" // a pass point where the train stood
)

// Halt reasons.
const (
	ReasonTechnical = "technical" // unscheduled, no other train to blame
	ReasonCross     = "cross"     // waited for a train from the other way
	ReasonOvertake  = "overtake"  // waited for a train behind to pass it
)

// Config is the generator's settings, the "timetable" section of a railway
// profile.
type Config struct {
	// RoundingMin is the step timetable times are rounded up to.
	RoundingMin float64 `json:"rounding_min"`
	// RecoveryPct is added to every running time before rounding.
	RecoveryPct float64 `json:"recovery_pct"`
	// HaltMin is how long a train must stand at a pass point, or past its
	// dwell at a stop for no other train, to count as halted. At a stop, any
	// wait for another train counts (see haltNoiseMin).
	HaltMin float64 `json:"halt_min"`
	// CheckToleranceMin is how late a train may run in the check run.
	CheckToleranceMin float64 `json:"check_tolerance_min"`
	// CheckPrecedence is how the check run decides precedence, if the
	// profile has it on: CheckPlan (the default) or CheckAuto.
	CheckPrecedence string `json:"check_precedence,omitempty"`
}

// How the check run decides precedence (Config.CheckPrecedence).
const (
	// CheckPlan keeps to the timetable's plan: its platforms, and who waits
	// for whom at each crossing and overtake. It shows the plan can be run.
	CheckPlan = "plan"
	// CheckAuto lets precedence decide afresh from the timetable's times,
	// as it would on the day. It shows the dispatching can keep the
	// timetable too.
	CheckAuto = "auto"
)

// DefaultConfig is used when the profile has no timetable section.
func DefaultConfig() Config {
	return Config{RoundingMin: 1, HaltMin: 0.5, CheckToleranceMin: 1, CheckPrecedence: CheckPlan}
}

// LoadConfig reads the settings from a profile's raw "timetable" section;
// fields left out keep their defaults.
func LoadConfig(raw json.RawMessage) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("timetable: %w", err)
		}
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	var errs []error
	if !(c.RoundingMin > 0) {
		errs = append(errs, errors.New("rounding_min must be positive"))
	}
	if c.RecoveryPct < 0 {
		errs = append(errs, errors.New("recovery_pct must not be negative"))
	}
	if c.HaltMin < 0 {
		errs = append(errs, errors.New("halt_min must not be negative"))
	}
	if c.CheckToleranceMin < 0 {
		errs = append(errs, errors.New("check_tolerance_min must not be negative"))
	}
	if c.CheckPrecedence != CheckPlan && c.CheckPrecedence != CheckAuto {
		errs = append(errs, fmt.Errorf("check_precedence %q, want %s or %s", c.CheckPrecedence, CheckPlan, CheckAuto))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("timetable: %w", err)
	}
	return nil
}

// Timetable is every train's times. Times are minutes from 0; -1 means the
// train never got there.
type Timetable struct {
	// RoundingMin is the step the times were rounded to, 0 if they are raw.
	RoundingMin float64      `json:"rounding_min"`
	Trains      []Train      `json:"trains"`
	Check       *CheckReport `json:"check,omitempty"`
}

// Train is one train's timetable, with what it needs to be run again.
type Train struct {
	Number       string  `json:"number"`
	Name         string  `json:"name"`
	Class        string  `json:"class,omitempty"`
	Priority     int     `json:"priority,omitempty"`
	MaxSpeedKmph float64 `json:"max_speed_kmph"`
	// Stock is the train's own rolling stock override, if it has one.
	Stock  *ops.Stock `json:"stock,omitempty"`
	Points []Point    `json:"points"`
	// Warnings are what makes the timetable doubtful for this train: it
	// could not enter on time, or did not finish.
	Warnings []string `json:"warnings,omitempty"`
}

// Point is a station in a train's timetable.
type Point struct {
	Stn  string `json:"stn"`
	Kind string `json:"kind"` // KindStop, KindPass or KindHalt
	Pf   string `json:"pf,omitempty"`
	// Arr at the first point is the entry, Dep at the last the exit.
	Arr float64 `json:"arr"`
	Dep float64 `json:"dep"`
	// Dwell is Dep - Arr; MinDwell the shortest the train had to stand.
	Dwell    float64 `json:"dwell"`
	MinDwell float64 `json:"min_dwell,omitempty"`
	// Run is the running time from the previous point.
	Run float64 `json:"run,omitempty"`
	// Halt is why it stood at a pass point, or past its dwell at a stop.
	Halt *Halt `json:"halt,omitempty"`
	// SignalStops are the stops at signals on the way here.
	SignalStops []SignalStop `json:"signal_stops,omitempty"`
	// Via are the line and throat tracks the train ran over from the
	// previous point. Round keeps the order of trains on each; a timetable
	// without them keeps it on platforms only.
	Via []Via `json:"via,omitempty"`
}

// Via is a track a train ran over between two points, by id: its front
// entered it at In and its rear left it at Out. In a rounded timetable they
// are moved on with the train's departure from the point before.
type Via struct {
	Track string  `json:"track"`
	In    float64 `json:"in"`
	Out   float64 `json:"out"`
}

// Reached reports whether the train got to the point.
func (p Point) Reached() bool { return p.Arr >= 0 }

// Halt is why a train stood longer than it had to.
type Halt struct {
	Reason string  `json:"reason"`        // ReasonTechnical, ReasonCross or ReasonOvertake
	For    string  `json:"for,omitempty"` // the other train, for a crossing or overtake
	Min    float64 `json:"min"`           // how much longer it stood
}

// SignalStop is a stop at a signal between two points, at least
// Config.HaltMin long.
type SignalStop struct {
	Track string  `json:"track"`
	At    float64 `json:"at"`
	Min   float64 `json:"min"`           // -1 if the train never moved on
	For   string  `json:"for,omitempty"` // the train it waited for, if known
}

// FromRun builds the timetable from a finished run: one train per train in
// the run, in the order they were added.
func FromRun(s *sim.Sim, cfg Config) Timetable {
	var runs []*ops.Train
	for _, st := range s.Status() {
		runs = append(runs, st.Train)
	}
	c := newClassifier(runs, s.Decisions())
	tt := Timetable{}
	for _, st := range s.Status() {
		t := st.Train
		tr := Train{
			Number: t.Number, Name: t.Name, Class: t.Class, Priority: t.Priority,
			MaxSpeedKmph: float64(t.MaxSpeed) * 60 / 1000, Stock: t.Stock,
		}
		sched := t.Schedule()
		for k, sp := range sched {
			p := Point{Stn: sp.StnCode.String(), Kind: KindStop, Pf: sp.ActPfNo, Arr: sp.ActArrTime, Dep: sp.ActDeptTime}
			if p.Pf == "" {
				p.Pf = sp.SpPfNo
			}
			if sp.IsPass() {
				p.Kind = KindPass
			} else {
				p.MinDwell = minDwell(sp, s.MinStop())
			}
			for _, v := range sp.Via {
				p.Via = append(p.Via, Via{Track: v.Track, In: v.In, Out: v.Out})
			}
			for _, ss := range sp.SignalStops {
				// most stops are only a signal handing over to the next
				if ss.To >= 0 && ss.To-ss.From <= cfg.HaltMin {
					continue
				}
				stop := SignalStop{Track: ss.Track, At: ss.From, Min: -1, For: ss.WaitedFor.Number}
				if ss.To >= 0 {
					stop.Min = ss.To - ss.From
				}
				p.SignalStops = append(p.SignalStops, stop)
			}
			if p.Reached() && sp.Departed() {
				// when it could have left: its dwell from when it reached the
				// end of the platform, which at entry is a run along it
				due := sp.ActArrTime
				if k == 0 {
					due += entryRun(s.World(), t, sp)
				}
				due += max(float64(sp.ExpDwellTime(due, s.MinStop())), des.MinDeltaTime)
				extra := sp.ActDeptTime - due
				if extra > cfg.HaltMin || (p.Kind == KindStop && extra > haltNoiseMin) {
					h := c.classify(t, k, max(due, sp.ActArrTime), sp.ActDeptTime)
					if isHalt(p.Kind, extra, cfg.HaltMin, h.For, t.Number) {
						h.Min = extra
						p.Halt = &h
						if p.Kind == KindPass {
							p.Kind = KindHalt
						}
					}
				}
			}
			tr.Points = append(tr.Points, p)
		}
		first := sched[0]
		if late := first.ActArrTime - first.ArrTime; first.Arrived() && late <= cfg.HaltMin {
			// on time but for event hops: the entry time is the given one
			tr.Points[0].Arr = first.ArrTime
		}
		switch {
		case !first.Arrived():
			tr.Warnings = append(tr.Warnings, "never entered")
		case first.ActArrTime-first.ArrTime > cfg.HaltMin:
			tr.Warnings = append(tr.Warnings, fmt.Sprintf("entered %.1f min late: no free boundary platform at %s", first.ActArrTime-first.ArrTime, first.StnCode))
		}
		if st.Phase != "exited" && first.Arrived() {
			tr.Warnings = append(tr.Warnings, fmt.Sprintf("did not finish: %s", st.Phase))
		}
		tr.fill()
		tt.Trains = append(tt.Trains, tr)
	}
	return tt
}

// isHalt reports whether a train that stood extra minutes longer than it had
// to at a point of the kind, held up by the train waitedFor (empty if none),
// halted there. At a pass, or for no other train, it must be over haltMin. At
// a stop, any wait for another train is a halt, however short: Round times it
// a full step.
func isHalt(kind string, extra, haltMin float64, waitedFor, self string) bool {
	if extra > haltMin {
		return true
	}
	return kind == KindStop && extra > haltNoiseMin && waitedFor != "" && waitedFor != self
}

// haltNoiseMin is how long past its dwell a train may leave a stop for no
// more than the hops between events (des.MinDeltaTime) that take it off.
const haltNoiseMin = 0.01

// minDwell is the shortest the train has to stand at a stop: its own minimum
// dwell, and at a call (not a halt) the sim's minimum stop.
func minDwell(sp *ops.SchedulePoint, minStop units.Minutes) float64 {
	if sp.IsHalt() {
		return max(sp.DwellTime, 0)
	}
	return max(sp.DwellTime, float64(minStop))
}

// entryRun is the time from entering the world to reaching the end of the
// first platform.
func entryRun(w *infra.World, t *ops.Train, sp *ops.SchedulePoint) float64 {
	stn, ok := w.GetStation(sp.StnCode)
	if !ok {
		return 0
	}
	trk := stn.FindStnPlatform(sp.ActPfNo)
	if trk == nil {
		return 0
	}
	edge, ok := w.TrackGraph.Edge(trk)
	if !ok {
		return 0
	}
	// which way it runs the platform does not matter to the time
	step := infra.Step{Edge: edge, From: edge.From, To: edge.To}
	return motion.Plan(t, []infra.Step{step}, true).Times[0]
}

// fill works out each point's dwell and running time from its times.
func (t *Train) fill() {
	for i := range t.Points {
		p := &t.Points[i]
		p.Dwell, p.Run = 0, 0
		if p.Reached() && p.Dep >= 0 {
			p.Dwell = p.Dep - p.Arr
		}
		if i > 0 && p.Reached() && t.Points[i-1].Dep >= 0 {
			p.Run = p.Arr - t.Points[i-1].Dep
		}
	}
}

// TimedTrains turns the timetable into fully timed trains for a run: stops and
// halts become timed stops keeping their minimum dwell, passes timed passes.
// Points a train never reached are left out.
func (tt Timetable) TimedTrains() []*ops.Train {
	var out []*ops.Train
	for _, tr := range tt.Trains {
		t := &ops.Train{
			Number: tr.Number, Name: tr.Name, Class: tr.Class, Priority: tr.Priority,
			MaxSpeed: units.KMPH(tr.MaxSpeedKmph), Stock: tr.Stock,
		}
		for _, p := range tr.Points {
			if !p.Reached() || p.Dep < 0 {
				break
			}
			switch p.Kind {
			case KindPass:
				t.AddSchedule(ops.NewPassPoint(ids.StnCode(p.Stn), p.Arr, p.Pf))
			case KindHalt:
				// a pass timed to stand: not a stop, so no minimum stop
				t.AddSchedule(ops.NewArrDepPoint(ids.StnCode(p.Stn), p.Arr, p.Dep, p.Pf).WithMinDwell(p.MinDwell).AsHalt())
			default:
				t.AddSchedule(ops.NewArrDepPoint(ids.StnCode(p.Stn), p.Arr, p.Dep, p.Pf).WithMinDwell(p.MinDwell))
			}
		}
		if len(t.Schedule()) > 0 {
			out = append(out, t)
		}
	}
	return out
}

// PlanOverrides are the run's decisions as precedence overrides, for running
// the timetable again: every train pinned to the track it used at each point,
// and the order of trains on each track between points kept (see orders) by
// holding the second at its stop or halt before the track. One from the other
// way is waited for until it has arrived there; one the same way that
// overtakes it there until it has left. One the same way already gone needs
// no hold: the line ahead keeps the train back. Without them a run of the
// timed trains decides precedence afresh from the new times, and can meet
// trains elsewhere than the timetable has them meet. Points TimedTrains leaves
// out are left out here too.
func (tt Timetable) PlanOverrides() []sim.Override {
	var out []sim.Override
	for _, tr := range tt.Trains {
		for _, p := range tr.Points {
			if !p.Reached() || p.Dep < 0 {
				break // as TimedTrains
			}
			if p.Pf != "" {
				out = append(out, sim.Override{Action: sim.OverridePlatform, Train: tr.Number, Station: p.Stn, Pf: p.Pf})
			}
		}
	}
	seen := map[sim.Override]bool{}
	for _, o := range orders(tt) {
		if !o.track {
			continue
		}
		f, l := o.follow.take, o.lead.free // its departure, the other's arrival
		fp, lps := tt.Trains[f.i].Points[f.k], tt.Trains[l.i].Points
		if fp.Kind == KindPass {
			continue // a pass is not held
		}
		h := sim.Override{Action: sim.OverrideHold, Train: tt.Trains[f.i].Number, For: tt.Trains[l.i].Number, Station: fp.Stn}
		// only for what the timetable has happen while the train stands
		// there, give or take the gap Round keeps: the track may be at the
		// far end of the way, freed long before the other gets to the
		// station, or the other may be there before the train
		while := func(t float64) bool { return t >= fp.Arr && t <= fp.Dep+tt.RoundingMin+platformRunMin }
		switch {
		case lps[l.k].Stn == fp.Stn && while(lps[l.k].Arr):
			// from the other way, to where the train stands
			h.Until = sim.HoldUntilArrived
		case l.k > 0 && lps[l.k-1].Stn == fp.Stn && lps[l.k-1].Arr >= fp.Arr && while(lps[l.k-1].Dep):
			// the same way, overtaking it there
			h.Until = sim.HoldUntilLeft
		default:
			continue
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// Train returns the train with the number.
func (tt Timetable) Train(number string) (Train, bool) {
	i := slices.IndexFunc(tt.Trains, func(t Train) bool { return t.Number == number })
	if i < 0 {
		return Train{}, false
	}
	return tt.Trains[i], true
}
