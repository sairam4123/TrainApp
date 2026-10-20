// Package motion is the run-time model: how long a train takes over a
// sequence of tracks. Everything that needs a run time asks it, so that the
// sim, its look-ahead, the timetable and the viewer agree.
//
// Today speed is constant on each track: the lower of the train's and the
// track's limit, t = d/s. Acceleration and braking will come here (see
// docs/trainapp/length_dynamics/plan.md, Phase 3).
package motion

import (
	"math"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/units"
)

// Run is how a train runs a sequence of steps.
type Run struct {
	// Times is the minutes spent on each step, from the front entering it to
	// the front leaving it. Callers add them up step by step.
	Times []float64
	// ExitSpeed is the speed leaving the last step, 0 if the train stops
	// there.
	ExitSpeed units.MetersPerMin
}

// Plan works out how train t runs steps, standing at the end of the last one
// if stop is set.
func Plan(t *ops.Train, steps []infra.Step, stop bool) Run {
	r := Run{Times: make([]float64, len(steps))}
	for i, st := range steps {
		r.Times[i] = float64(st.Edge.Track.TravelTime(t.MaxSpeed))
	}
	if n := len(steps); n > 0 && !stop {
		r.ExitSpeed = units.MetersPerMin(math.Min(float64(t.MaxSpeed), float64(steps[n-1].Edge.Track.MaxSpeed)))
	}
	return r
}
