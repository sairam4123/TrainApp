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
		now     float64
		want    units.Minutes
	}{
		{"early waits for departure", -1, 8, 7},
		{"on time", -1, 10, 5},
		{"a little late still leaves on time", -1, 12, 3},
		{"late beyond departure stops a minute", -1, 20, 1},
		{"minimum dwell kept when nearly due out", 4, 13, 4},
		{"minimum dwell kept when late", 4, 20, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := ops.NewArrDepPoint("A", 10, 15, "1")
			if c.minimum >= 0 {
				sp.WithMinDwell(c.minimum)
			}
			if got := sp.ExpDwellTime(c.now); got != c.want {
				t.Errorf("ExpDwellTime(%v) = %v, want %v", c.now, got, c.want)
			}
		})
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
