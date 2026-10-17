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

func TestThresholdRuleCapsTheWait(t *testing.T) {
	r := thresholdRule{gap: 10, saving: 1, ratio: 1.5}
	for _, c := range []struct {
		held, forP   int
		saving, wait units.Minutes
		want         bool
	}{
		{20, 40, 4, 6, true},    // 1.5 min a minute saved: at the cap
		{20, 40, 4, 6.5, false}, // just over it
		{20, 40, 3.4, 11.7, false},
		{50, 90, 2, 20, false}, // a Superfast held for a Rajdhani is capped too
		{50, 90, 10, 5, true},
		{40, 45, 10, 1, false}, // gap still applies
	} {
		got, why := r.Hold(Conflict{HeldPrio: c.held, ForPrio: c.forP, Saving: c.saving, Wait: c.wait})
		if got != c.want {
			t.Errorf("prio %d for %d, saving %v, wait %v: hold %v (%s), want %v", c.held, c.forP, c.saving, c.wait, got, why, c.want)
		}
	}
	if ok, _ := (thresholdRule{gap: 10, saving: 1}).Hold(Conflict{HeldPrio: 20, ForPrio: 40, Saving: 1, Wait: 29}); !ok {
		t.Error("ratio 0 should leave the wait unlimited")
	}
}
