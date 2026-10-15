package timetable

import (
	"cmp"
	"slices"
	"trainapp/railway/infra"
	"trainapp/railway/sim"
)

// The check run. Rounding moves trains, and moving one can change how it
// meets the others, so the rounded timetable is run again as fully timed
// trains. If every train keeps its times, within a tolerance, the timetable
// can be run, not just generated.

// CheckReport is how the check run went.
type CheckReport struct {
	ToleranceMin float64 `json:"tolerance_min"`
	// Late lists, for each train that ran later than the tolerance, the
	// point where it was latest.
	Late []Lateness `json:"late,omitempty"`
	// Unfinished lists the trains that did not complete their schedule.
	Unfinished []string `json:"unfinished,omitempty"`
	// Err is the check run's own error, if it failed.
	Err string `json:"error,omitempty"`
}

// OK reports whether every train kept its times.
func (r *CheckReport) OK() bool {
	return len(r.Late) == 0 && len(r.Unfinished) == 0 && r.Err == ""
}

// Lateness is how late a train was against the timetable at a point.
type Lateness struct {
	Train   string  `json:"train"`
	Stn     string  `json:"stn"`
	Event   string  `json:"event"` // "arr" or "dep"
	LateMin float64 `json:"late_min"`
}

// NewCheckSim sets up the check run of tt over the world: its timed trains,
// the same priorities, and logs under outputDir. Add an Observer if wanted,
// Run it, then read the result with Report.
func NewCheckSim(world *infra.World, prio *sim.PriorityConfig, tt Timetable, outputDir string) (*sim.Sim, error) {
	s := &sim.Sim{OutputDir: outputDir, Priorities: prio}
	s.SetWorld(world)
	for _, t := range tt.TimedTrains() {
		if err := s.AddTrain(t); err != nil {
			return nil, err
		}
	}
	if err := s.Init(); err != nil {
		return nil, err
	}
	return s, nil
}

// Report compares a finished check run with its timetable.
func Report(s *sim.Sim, runErr error, tolerance float64) *CheckReport {
	r := &CheckReport{ToleranceMin: tolerance}
	if runErr != nil {
		r.Err = runErr.Error()
	}
	for _, st := range s.Status() {
		if st.Phase != "exited" {
			r.Unfinished = append(r.Unfinished, st.Train.Number)
		}
		var worst *Lateness
		for _, sp := range st.Train.Schedule() {
			for _, ev := range []struct {
				name            string
				planned, actual float64
			}{{"arr", sp.ArrTime, sp.ActArrTime}, {"dep", sp.DeptTime, sp.ActDeptTime}} {
				if ev.planned < 0 || ev.actual < 0 {
					continue
				}
				late := ev.actual - ev.planned
				if late > tolerance && (worst == nil || late > worst.LateMin) {
					worst = &Lateness{Train: st.Train.Number, Stn: sp.StnCode.String(), Event: ev.name, LateMin: late}
				}
			}
		}
		if worst != nil {
			r.Late = append(r.Late, *worst)
		}
	}
	slices.SortStableFunc(r.Late, func(a, b Lateness) int { return cmp.Compare(b.LateMin, a.LateMin) })
	return r
}

// Check runs the check run of tt and reports on it.
func Check(world *infra.World, prio *sim.PriorityConfig, tt Timetable, cfg Config, outputDir string) (*CheckReport, error) {
	s, err := NewCheckSim(world, prio, tt, outputDir)
	if err != nil {
		return nil, err
	}
	return Report(s, s.Run(), cfg.CheckToleranceMin), nil
}
