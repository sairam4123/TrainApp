package kpi

import (
	"errors"
	"fmt"
	"os"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
)

// FreeRun is how a train runs with the line to itself: from its planned
// entry to when it leaves the world.
type FreeRun struct {
	EntryMin   float64 `json:"entry_min"`
	ExitMin    float64 `json:"exit_min"`
	JourneyMin float64 `json:"journey_min"`
}

// FreeRunning runs each train alone over the world, with the same profile,
// to find its free-running time: the dispatcher's own way, platforms and
// dwells, with no other train in the way. Pass the trains as planned, before
// any disruption is applied; they are copied, not run. Their logs are thrown
// away.
func FreeRunning(world *infra.World, prio *sim.PriorityConfig, trains []*ops.Train) (map[string]FreeRun, error) {
	dir, err := os.MkdirTemp("", "trainapp-freerun-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	out := make(map[string]FreeRun, len(trains))
	var errs []error
	for _, t := range trains {
		alone := t.Clone()
		s := &sim.Sim{OutputDir: dir, Priorities: prio}
		s.SetWorld(world)
		if err := s.AddTrain(alone); err != nil {
			return nil, err
		}
		if err := s.Init(); err != nil {
			return nil, err
		}
		if err := s.Run(); err != nil {
			errs = append(errs, fmt.Errorf("train %s alone: %w", t.Number, err))
			continue
		}
		if st := s.Status()[0]; st.Phase != "exited" {
			errs = append(errs, fmt.Errorf("train %s does not finish even alone: %s", t.Number, st.Phase))
			continue
		}
		sched := alone.Schedule()
		entry, exit := sched[0].ArrTime, sched[len(sched)-1].ActDeptTime
		out[t.Number] = FreeRun{EntryMin: entry, ExitMin: exit, JourneyMin: exit - entry}
	}
	return out, errors.Join(errs...)
}
