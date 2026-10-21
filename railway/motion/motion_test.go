package motion_test

import (
	"math"
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

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9*max(1, math.Abs(b)) }

// With instant acceleration and braking every step is run at its limit, and
// the times are exactly TravelTime's, stopping or not.
func TestNoDynamicsIsConstantSpeed(t *testing.T) {
	train := &ops.Train{Number: "T", MaxSpeed: units.KMPH(100)}
	steps := []infra.Step{
		step(1000, 130), // the train's limit: 100 km/h
		step(100, 30),   // the track's: 30 km/h
		step(0, 50),
		step(777.7, 110),
	}
	for _, stop := range []bool{false, true} {
		c := motion.Plan(train, motion.Way{Steps: steps, Stop: stop})
		for i, st := range steps {
			want := float64(st.Edge.Track.TravelTime(train.MaxSpeed))
			if got := c.Time(i); got != want {
				t.Errorf("stop=%v step %d: %v min, want %v", stop, i, got, want)
			}
			for _, d := range []float64{0.1, 13, float64(st.Edge.Track.Length) / 3} {
				if d >= float64(st.Edge.Track.Length) {
					continue
				}
				if got, want := c.TimeIn(i, units.M(d)), want*d/float64(st.Edge.Track.Length); got != want {
					t.Errorf("stop=%v step %d at %v m: %v, want %v", stop, i, d, got, want)
				}
			}
		}
		if bp := c.BrakingPoint(); bp.Step != 3 || float64(bp.Off) != 777.7 {
			t.Errorf("stop=%v: braking point %+v, want the end", stop, bp)
		}
	}
	if c := motion.Plan(train, motion.Way{Steps: steps}); c.ExitSpeed() != units.KMPH(100) {
		t.Errorf("exit speed %v, want 100 km/h", c.ExitSpeed().InKMPH())
	}
	if e := motion.Plan(train, motion.Way{}); e.Steps() != 0 || e.Total() != 0 {
		t.Errorf("no steps: %d steps, %v min", e.Steps(), e.Total())
	}
}

// A lower limit holds until the rear has cleared it, with instant
// acceleration too.
func TestLowerLimitHoldsUntilRearClears(t *testing.T) {
	train := &ops.Train{Number: "T", MaxSpeed: units.KMPH(120), Length: units.M(400)}
	steps := []infra.Step{step(500, 30), step(1000, 120)}
	c := motion.Plan(train, motion.Way{Steps: steps})
	v30, v120 := float64(units.KMPH(30)), float64(units.KMPH(120))
	if got, want := c.Time(1), 400/v30+600/v120; !near(got, want) {
		t.Errorf("second step: %v min, want %v (400 m at 30, then 120)", got, want)
	}
	// a track behind the front counts too
	c = motion.Plan(train, motion.Way{Behind: []infra.Step{step(500, 30)}, Steps: []infra.Step{step(1000, 120)}, From: 100, Speed: units.KMPH(30)})
	if got, want := c.Time(0), 300/v30+600/v120; !near(got, want) {
		t.Errorf("from 100 m with 30 km/h behind: %v min, want %v", got, want)
	}
	// with no length it rises at once
	c = motion.Plan(&ops.Train{Number: "T", MaxSpeed: units.KMPH(120)}, motion.Way{Steps: steps})
	if got, want := c.Time(1), 1000/v120; !near(got, want) {
		t.Errorf("no length: %v, want %v", got, want)
	}
}

func dynTrain() *ops.Train {
	return &ops.Train{Number: "T", MaxSpeed: units.KMPH(108), Accel: units.MS2(0.5), Brake: units.MS2(1)}
}

func TestAccelerateCruiseBrake(t *testing.T) {
	train := dynTrain()
	// 30 m/s: 60 s and 900 m to reach it at 0.5 m/s², 30 s and 450 m to stop
	// at 1 m/s²; 3000 m in all leaves 1650 m at 30 m/s, 55 s
	c := motion.Plan(train, motion.Way{Steps: []infra.Step{step(3000, 200)}, Stop: true})
	if got, want := c.Total(), (60.0+55+30)/60; !near(got, want) {
		t.Errorf("total %v min, want %v", got, want)
	}
	off, v := c.At(0, 60.0/60)
	if !near(float64(off), 900) || !near(v.InKMPH(), 108) {
		t.Errorf("after 60 s: %v m at %v km/h, want 900 m at 108", off, v.InKMPH())
	}
	if got := c.TimeIn(0, 450); !near(got, math.Sqrt(2*450/0.5)/60) {
		t.Errorf("time to 450 m %v min, want %v", got, math.Sqrt(2*450/0.5)/60)
	}
	bp := c.BrakingPoint()
	if !near(float64(bp.Off), 2550) || !near(bp.T, 115.0/60) {
		t.Errorf("braking point %+v, want 2550 m at 115 s", bp)
	}
	if p := c.PointAt(115.0 / 60); !near(float64(p.Off), 2550) {
		t.Errorf("point at 85 s: %+v", p)
	}
	if c.ExitSpeed() != 0 {
		t.Errorf("exit speed %v, want 0", c.ExitSpeed())
	}
	keys := c.Keys(0)
	if len(keys) != 4 || keys[3].Kmph != 0 || !near(keys[1].Kmph, 108) {
		t.Errorf("keys %+v, want start, 108 at 900 m, 108 at 2550 m, 0 at the end", keys)
	}
}

// Too short to reach the limit: up to a peak, then straight down.
func TestShortStepPeaks(t *testing.T) {
	train := dynTrain()
	// from rest to rest over 300 m: peak² = 2·a·b·d/(a+b) = 200, so
	// 14.14 m/s, 28.3 s up and 14.1 s down
	c := motion.Plan(train, motion.Way{Steps: []infra.Step{step(300, 200)}, Stop: true})
	peak := math.Sqrt(200)
	if got, want := c.Total(), (peak/0.5+peak/1)/60; !near(got, want) {
		t.Errorf("total %v min, want %v", got, want)
	}
	if bp := c.BrakingPoint(); !near(float64(bp.Off), 200) {
		t.Errorf("braking point at %v m, want 200", bp.Off)
	}
}

// Starting mid-step at speed, the train brakes from there to stop at the
// end, and a lower limit ahead is met at it.
func TestStartAtSpeedAndLimitAhead(t *testing.T) {
	train := dynTrain()
	v30 := float64(units.KMPH(108)) // 30 m/s
	steps := []infra.Step{step(2000, 200), step(1000, 36)}
	c := motion.Plan(train, motion.Way{Steps: steps, From: 1000, Speed: units.MetersPerMin(v30)})
	// from 30 to 10 m/s at 1 m/s²: 400 m in 20 s, starting 600 m in
	if got, want := c.Time(0), (600.0/30+20)/60; !near(got, want) {
		t.Errorf("first step from 1000 m: %v min, want %v", got, want)
	}
	if _, v := c.At(1, 0); !near(v.InKMPH(), 36) {
		t.Errorf("enters the slow step at %v km/h, want 36", v.InKMPH())
	}
	// a start speed it could not stop from in time is brought down
	c = motion.Plan(train, motion.Way{Steps: []infra.Step{step(200, 200)}, Speed: units.MetersPerMin(v30), Stop: true})
	if got := c.StartSpeed(); !near(float64(got), float64(units.MetersPerMin(math.Sqrt(400)*60))) {
		t.Errorf("start speed %v km/h, want 72 (20 m/s stops in 200 m)", got.InKMPH())
	}
}
