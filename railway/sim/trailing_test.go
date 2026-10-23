package sim_test

import (
	"slices"
	"strings"
	"testing"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/worlds"
)

// dlPair is dl-20's 16607, an express that passes half the stations, and the
// passenger 56605 that calls everywhere, entering at TPJ 12 minutes before
// it: 8 minutes later than timetabled, as it got away in dl-20 behind 56603.
func dlPair(t *testing.T) []*ops.Train {
	t.Helper()
	all, err := ops.LoadTrains("../../scenarios/schedules/dl-20.json")
	if err != nil {
		t.Fatal(err)
	}
	pair := slices.DeleteFunc(all, func(tr *ops.Train) bool { return tr.Number != "56605" && tr.Number != "16607" })
	for _, tr := range pair {
		if tr.Number == "56605" {
			tr.Schedule()[0].SetEntryTime(93)
		}
	}
	return pair
}

func irConfig(t *testing.T, followMin float64) *sim.PriorityConfig {
	t.Helper()
	cfg, err := sim.LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Precedence.FollowHorizonMin = units.Minutes(followMin)
	return cfg
}

// exitTime is when the train gets to its last stop. Every train enters at a
// fixed time, so it compares runs fairly, holds at the first stop included.
func exitTime(tr *ops.Train) float64 {
	sched := tr.Schedule()
	return sched[len(sched)-1].ActArrTime
}

func byNumber(trains []*ops.Train, n string) *ops.Train {
	return trains[slices.IndexFunc(trains, func(tr *ops.Train) bool { return tr.Number == n })]
}

func TestAnExpressIsLetPastNotLeftTrailing(t *testing.T) {
	off := dlPair(t)
	runWith(t, worlds.BuildTpjKkdiDoubleLineWorld(), irConfig(t, 0), nil, off...)
	on := dlPair(t)
	_, log := runWith(t, worlds.BuildTpjKkdiDoubleLineWorld(), irConfig(t, 60), nil, on...)
	for _, e := range log.entries {
		if e.event != "TRACK_ENTER" {
			t.Logf("%s %v", e.event, e.attrs)
		}
	}
	xOff, xOn := exitTime(byNumber(off, "16607")), exitTime(byNumber(on, "16607"))
	pOff, pOn := exitTime(byNumber(off, "56605")), exitTime(byNumber(on, "56605"))
	t.Logf("in at: 16607 %.1f -> %.1f, 56605 %.1f -> %.1f", xOff, xOn, pOff, pOn)

	holds := log.find("PRECEDENCE_HOLD", map[string]string{"train": "56605", "for": "16607"})
	if len(holds) == 0 {
		t.Fatal("56605 was never held for 16607")
	}
	if holds[0].attrs["trail"] == "" {
		t.Errorf("the hold carries no trail loss: %v", holds[0].attrs)
	}
	// let go at TPJ, where it would wait longest, to be held where 16607
	// runs through; and held there when it gets there
	defers := log.find("PRECEDENCE_DEFERRED", map[string]string{"train": "56605", "for": "16607", "at": "TPJ"})
	if len(defers) == 0 {
		t.Fatal("56605 was not let go at TPJ to be held further on")
	}
	if to, at := defers[0].attrs["to"], holds[0].attrs["at"]; to != at {
		t.Errorf("let go at TPJ to be held at %s, but held at %s", to, at)
	}
	if got := log.find("PRECEDENCE_RELEASE", map[string]string{"decision": defers[0].attrs["decision"], "status": "done"}); len(got) == 0 {
		t.Errorf("the deferral did not end done")
	}
	if xOn > xOff-2 {
		t.Errorf("16607 got in at %.1f, %.1f without trailing: want at least 2 min sooner", xOn, xOff)
	}
	if skipped := log.find("PRECEDENCE_SKIPPED", map[string]string{"train": "56605", "reason": "main_line_limit"}); len(skipped) > 0 {
		t.Errorf("a hold was skipped for the main line limit: %v", skipped[0].attrs)
	}
}

func TestAVetoedRefugeMovesTheHoldOn(t *testing.T) {
	_, first := runWith(t, worlds.BuildTpjKkdiDoubleLineWorld(), irConfig(t, 60), nil, dlPair(t)...)
	holds := first.find("PRECEDENCE_HOLD", map[string]string{"train": "56605", "for": "16607"})
	if len(holds) == 0 {
		t.Fatal("56605 was never held for 16607")
	}
	veto := holds[0].attrs["decision"]
	_, log := runWith(t, worlds.BuildTpjKkdiDoubleLineWorld(), irConfig(t, 60), func(s *sim.Sim) {
		if err := s.Override(sim.Override{Action: sim.OverrideRelease, Decision: veto}); err != nil {
			t.Fatal(err)
		}
	}, dlPair(t)...)
	if got := log.find("PRECEDENCE_HOLD", map[string]string{"decision": veto}); len(got) > 0 {
		t.Fatalf("%s was placed though vetoed", veto)
	}
	again := log.find("PRECEDENCE_HOLD", map[string]string{"train": "56605", "for": "16607"})
	if len(again) == 0 {
		t.Fatalf("with %s vetoed, 56605 was not held anywhere else for 16607", veto)
	}
	t.Logf("vetoed %s, held at %s instead", veto, again[0].attrs["at"])
}

// dlEastbound is dl-20's eastbound trains of its second hour, as they enter: the
// passenger 56603 at 55, the superfast 22603 at 61, the express 16605 at 75,
// the passenger 56605 at 85 and the express 16607 at 105.
func dlEastbound(t *testing.T) []*ops.Train {
	t.Helper()
	all, err := ops.LoadTrains("../../scenarios/schedules/dl-20.json")
	if err != nil {
		t.Fatal(err)
	}
	keep := []string{"56603", "22603", "16605", "56605", "16607"}
	return slices.DeleteFunc(all, func(tr *ops.Train) bool { return !slices.Contains(keep, tr.Number) })
}

func TestTrainsBehindAPassengerAreLetPastTogether(t *testing.T) {
	off := dlEastbound(t)
	runWith(t, worlds.BuildTpjKkdiDoubleLineWorld(), irConfig(t, 0), nil, off...)
	on := dlEastbound(t)
	_, log := runWith(t, worlds.BuildTpjKkdiDoubleLineWorld(), irConfig(t, 60), nil, on...)
	for _, e := range log.entries {
		if strings.HasPrefix(e.event, "PRECEDENCE") && e.event != "PRECEDENCE_REVIEW" {
			t.Logf("%s %v", e.event, e.attrs)
		}
	}
	weighted := 0.0
	for _, tr := range on {
		was, is := exitTime(byNumber(off, tr.Number)), exitTime(tr)
		weighted += float64(tr.Priority) * (is - was)
		t.Logf("%s (%d) in at %.1f -> %.1f", tr.Number, tr.Priority, was, is)
		if tr.Priority > 20 && is > was+0.1 {
			t.Errorf("%s got in at %.1f, later than %.1f without trailing", tr.Number, is, was)
		}
	}
	t.Logf("priority-weighted change %.1f", weighted)
	// held once at TPJ, for both trains catching it up there
	for _, h := range []string{"22603", "16605"} {
		if got := log.find("PRECEDENCE_HOLD", map[string]string{"train": "56603", "for": h, "at": "TPJ"}); len(got) == 0 {
			t.Errorf("56603 was not held at TPJ for %s", h)
		}
	}
	if weighted > 0 {
		t.Errorf("priority-weighted change %.1f, want no worse than without trailing", weighted)
	}
}
