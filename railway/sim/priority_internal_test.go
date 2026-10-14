package sim

import (
	"testing"
	"trainapp/units"
)

func TestDelayBandSelection(t *testing.T) {
	s := &Sim{Priorities: &PriorityConfig{DelayBands: []DelayBand{
		{OverMin: 60, Adjust: -5}, // out of order on purpose
		{OverMin: 1, Adjust: 5},
		{OverMin: 15, Adjust: 0},
	}}}
	if err := s.initPriorities(); err != nil {
		t.Fatal(err)
	}
	for delay, want := range map[units.Minutes]int{-3: 0, 0.5: 0, 1: 0, 10: 5, 15: 5, 30: 0, 60: 0, 90: -5} {
		if got := s.delayAdjust(delay); got != want {
			t.Errorf("delayAdjust(%v) = %d, want %d", delay, got, want)
		}
	}
}
