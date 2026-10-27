package sim_test

import (
	"testing"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/worlds"
)

// Refuge on a through track (docs/trainapp/through_refuge/plan.md): with
// through_track_max_stay_min set, a train that does not call takes LPA's
// through track, lpaPf1, before its loops, lpaPf2 and lpaPf3, whether it runs
// through or stands there.

func throughLine() *infra.World {
	return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2, ThroughTrack: true})
}

func refugeConfig() *sim.PriorityConfig {
	cfg := precedenceConfig()
	cfg.Precedence.ThroughTrackMaxStayMin = 20
	return cfg
}

// arrivedOn returns the tracks the train arrived on at stations whose tracks
// are named with prefix.
func arrivedOn(log *eventLog, train, prefix string) []string {
	var out []string
	for _, e := range log.find("TRAIN_ARRIVE", map[string]string{"train": train}) {
		if trk := e.attrs["track"]; len(trk) >= len(prefix) && trk[:len(prefix)] == prefix {
			out = append(out, trk)
		}
	}
	return out
}

func TestANotCallingTrainCrossesOnTheThroughTrack(t *testing.T) {
	// P runs through LPA, timetabled on a loop, but stands there for X,
	// which calls at LPA on the way the other way
	trains := []*ops.Train{
		testTrain("P", 20, 60, 1, stop{"WST", "2", 2, false}, stop{"LPA", "2", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
		testTrain("X", 50, 100, 12, stop{"EST", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"LPA", "3", 2, false}, stop{"WST", "2", 2, false}),
	}
	_, log := runWith(t, throughLine(), refugeConfig(), nil, trains...)
	if got := arrivedOn(log, "P", "lpa"); len(got) != 1 || got[0] != "lpaPf1" {
		t.Errorf("P stood at LPA on %v, want lpaPf1", got)
	}
	if got := log.find("PLATFORM_CHANGE", map[string]string{"train": "P", "station": "LPA", "pf_to": "1", "reason": "refuge_through"}); len(got) == 0 {
		t.Errorf("P was not moved to the through track as a refuge: %+v", log.find("PLATFORM_CHANGE", nil))
	}
	if got := arrivedOn(log, "X", "lpa"); len(got) != 1 || got[0] == "lpaPf1" {
		t.Errorf("X called at LPA on %v, want a platform", got)
	}
}

func TestWithoutTheCapAStandingTrainKeepsOffTheThroughTrack(t *testing.T) {
	trains := []*ops.Train{
		testTrain("P", 20, 60, 1, stop{"WST", "2", 2, false}, stop{"LPA", "2", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
		testTrain("X", 50, 100, 12, stop{"EST", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"LPA", "3", 2, false}, stop{"WST", "2", 2, false}),
	}
	_, log := runWith(t, throughLine(), precedenceConfig(), nil, trains...)
	if got := arrivedOn(log, "P", "lpa"); len(got) != 1 || got[0] == "lpaPf1" {
		t.Errorf("P stood at LPA on %v, want a loop", got)
	}
}

func TestAHaltIsPlannedOnTheThroughTrack(t *testing.T) {
	h := testTrain("H", 20, 60, 1, stop{"WST", "2", 2, false}, stop{"LPA", "2", 3, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false})
	h.Schedule()[1].AsHalt()
	_, log := runWith(t, throughLine(), refugeConfig(), nil, h)
	if got := arrivedOn(log, "H", "lpa"); len(got) != 1 || got[0] != "lpaPf1" {
		t.Errorf("H halted at LPA on %v, want lpaPf1", got)
	}
}

func TestANotCallingTrainIsHeldOnTheThroughTrack(t *testing.T) {
	// X catches P up and calls at LPA, on a platform: P, running through,
	// is held on the through track while X gets by
	trains := []*ops.Train{
		testTrain("P", 20, 60, 1, stop{"WST", "2", 2, false}, stop{"LPA", "1", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
		testTrain("X", 50, 100, 12, stop{"WST", "3", 2, false}, stop{"LPA", "2", 2, false}, stop{"HLT", "1", 0, true}, stop{"EST", "2", 2, false}),
	}
	_, log := runWith(t, throughLine(), refugeConfig(), nil, trains...)
	if got := log.find("PRECEDENCE_HOLD", map[string]string{"train": "P", "for": "X", "at": "LPA"}); len(got) == 0 {
		t.Errorf("P was not held at LPA for X: %+v", log.find("PRECEDENCE_SKIPPED", nil))
	}
	if got := log.find("PRECEDENCE_SKIPPED", map[string]string{"train": "P", "reason": "through_track"}); len(got) != 0 {
		t.Errorf("a hold of P was skipped for the through track: %+v", got)
	}
	if got := arrivedOn(log, "P", "lpa"); len(got) != 1 || got[0] != "lpaPf1" {
		t.Errorf("P stood at LPA on %v, want lpaPf1", got)
	}
	if arrivalAt(trains[1], "EST") >= arrivalAt(trains[0], "EST") {
		t.Errorf("X reached EST at %.2f, after P at %.2f", arrivalAt(trains[1], "EST"), arrivalAt(trains[0], "EST"))
	}
}

func TestAHigherTrainRunningThroughKeepsTheThroughTrack(t *testing.T) {
	// X catches P up and both run through LPA: X has the higher priority,
	// so it keeps the through track and P stands in a loop
	trains := []*ops.Train{
		testTrain("P", 20, 60, 1, stop{"WST", "2", 2, false}, stop{"LPA", "1", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
		testTrain("X", 50, 100, 12, stop{"WST", "3", 2, false}, stop{"LPA", "1", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "2", 2, false}),
	}
	_, log := runWith(t, throughLine(), refugeConfig(), nil, trains...)
	if got := log.find("PRECEDENCE_HOLD", map[string]string{"train": "P", "for": "X", "at": "LPA"}); len(got) == 0 {
		t.Fatalf("P was not held at LPA for X: %+v", log.find("PRECEDENCE_SKIPPED", nil))
	}
	if got := arrivedOn(log, "X", "lpa"); len(got) != 1 || got[0] != "lpaPf1" {
		t.Errorf("X ran through LPA on %v, want lpaPf1", got)
	}
	if got := arrivedOn(log, "P", "lpa"); len(got) != 1 || got[0] == "lpaPf1" {
		t.Errorf("P stood at LPA on %v, want a loop", got)
	}
}

func TestAStandingTrainLeavesTheThroughTrackToAnEqualOneRunningThrough(t *testing.T) {
	// P stands at LPA to cross X, which runs through LPA the other way: the
	// same priority, so P keeps to a loop and X runs through on the through
	// track
	trains := []*ops.Train{
		testTrain("P", 50, 60, 1, stop{"WST", "2", 2, false}, stop{"LPA", "2", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
		testTrain("X", 50, 100, 6, stop{"EST", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"LPA", "2", 0, true}, stop{"WST", "2", 2, false}),
	}
	_, log := runWith(t, throughLine(), refugeConfig(), nil, trains...)
	if got := arrivedOn(log, "P", "lpa"); len(got) != 1 || got[0] == "lpaPf1" {
		t.Errorf("P stood at LPA on %v, want a loop", got)
	}
	var x []string
	for _, e := range log.find("TRACK_ENTER", map[string]string{"train": "X"}) {
		if trk := e.attrs["track"]; len(trk) == len("lpaPf1") && trk[:5] == "lpaPf" {
			x = append(x, trk)
		}
	}
	if len(x) != 1 || x[0] != "lpaPf1" {
		t.Errorf("X ran through LPA on %v, want lpaPf1", x)
	}
}
