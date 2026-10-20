package motion_test

import (
	"testing"
	"trainapp/railway/infra"
	"trainapp/railway/motion"
	"trainapp/railway/ops"
	"trainapp/units"
)

func step(lengthM, kmph float64) infra.Step {
	trk := &infra.TrackSegment{Length: units.M(lengthM), MaxSpeed: units.KMPH(kmph)}
	return infra.Step{Edge: &infra.GraphEdge{Track: trk}}
}

func TestPlanIsConstantSpeed(t *testing.T) {
	train := &ops.Train{Number: "T", MaxSpeed: units.KMPH(100)}
	steps := []infra.Step{
		step(1000, 130), // the train's limit: 100 km/h
		step(100, 30),   // the track's: 30 km/h
		step(0, 50),
	}
	r := motion.Plan(train, steps, false)
	if len(r.Times) != len(steps) {
		t.Fatalf("%d times for %d steps", len(r.Times), len(steps))
	}
	for i, st := range steps {
		if want := float64(st.Edge.Track.TravelTime(train.MaxSpeed)); r.Times[i] != want {
			t.Errorf("step %d: %v min, want %v", i, r.Times[i], want)
		}
	}
	if r.Times[0] != 0.6 || r.Times[1] != 0.2 {
		t.Errorf("times %v, want 0.6 and 0.2 min for the first two", r.Times)
	}
	if r.ExitSpeed != units.KMPH(50) {
		t.Errorf("exit speed %v, want 50 km/h, the last track's limit", r.ExitSpeed.InKMPH())
	}

	if s := motion.Plan(train, steps, true); s.ExitSpeed != 0 || s.Times[0] != r.Times[0] {
		t.Errorf("stopping: exit speed %v and times %v; want 0 and the same times", s.ExitSpeed, s.Times)
	}
	if e := motion.Plan(train, nil, false); len(e.Times) != 0 || e.ExitSpeed != 0 {
		t.Errorf("no steps: %+v, want an empty run", e)
	}
}
