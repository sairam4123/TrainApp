package ops_test

import (
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/units"
)

func TestExpDwellTimeTimedStop(t *testing.T) {
	cases := []struct {
		name    string
		minimum float64 // -1 for none
		halt    bool
		minStop units.Minutes
		dep     float64
		now     float64
		want    units.Minutes
	}{
		{"early waits for departure", -1, false, 1, 15, 8, 7},
		{"on time", -1, false, 1, 15, 10, 5},
		{"a little late still leaves on time", -1, false, 1, 15, 12, 3},
		{"late beyond departure stops the minimum stop", -1, false, 1, 15, 20, 1},
		{"minimum dwell kept when nearly due out", 4, false, 1, 15, 13, 4},
		{"minimum dwell kept when late", 4, false, 1, 15, 20, 4},
		{"a minimum dwell of 0 still stops the minimum stop", 0, false, 1, 15, 20, 1},
		{"timetabled for less than the minimum stop", 0, false, 1, 10.5, 10, 1},
		{"the minimum stop is configurable", -1, false, 2, 15, 20, 2},
		{"a late halt goes at once", 0, true, 1, 15, 20, 0},
		{"an on-time halt keeps its time", 0, true, 1, 10.5, 10, 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := ops.NewArrDepPoint("A", 10, c.dep, "1")
			if c.minimum >= 0 {
				sp.WithMinDwell(c.minimum)
			}
			if c.halt {
				sp.AsHalt()
			}
			if got := sp.ExpDwellTime(c.now, c.minStop); got != c.want {
				t.Errorf("ExpDwellTime(%v, %v) = %v, want %v", c.now, c.minStop, got, c.want)
			}
		})
	}
}

func TestExpDwellTimePassNeverStops(t *testing.T) {
	sp := ops.NewPassPoint("A", 10, "1")
	if got := sp.ExpDwellTime(20, 5); got > 0.01 {
		t.Errorf("ExpDwellTime of a pass = %v, want no stop", got)
	}
}

func TestSignalStops(t *testing.T) {
	sp := ops.NewStopPoint("A", 2, "1")
	other := ids.TrainID{Number: "B"}
	sp.EndSignalStop(1, other) // nothing to end
	sp.BeginSignalStop(3, "t1")
	sp.EndSignalStop(5, other)
	sp.BeginSignalStop(7, "t2")
	want := []ops.SignalStop{{Track: "t1", From: 3, To: 5, WaitedFor: other}, {Track: "t2", From: 7, To: -1}}
	if len(sp.SignalStops) != len(want) {
		t.Fatalf("SignalStops = %v, want %v", sp.SignalStops, want)
	}
	for i := range want {
		if sp.SignalStops[i] != want[i] {
			t.Errorf("SignalStops[%d] = %v, want %v", i, sp.SignalStops[i], want[i])
		}
	}
}
