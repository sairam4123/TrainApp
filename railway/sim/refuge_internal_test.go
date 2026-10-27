package sim

import (
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/units"
	"trainapp/worlds"
)

// refugePrecedence is a precedence over a sim that has not run, on the single
// line with a through track at LPA (lpaPf1) and one loop (lpaPf2), with
// through_track_max_stay_min at maxStay. L runs through LPA; H calls there, at
// the loop.
func refugePrecedence(t *testing.T, maxStay float64) (p *precedence, l, h *ops.Train) {
	t.Helper()
	cfg := &PriorityConfig{Precedence: &PrecedenceConfig{HorizonMin: 60, HeadwayMin: 1, MaxHoldMin: 30, MainLineMaxStayMin: 5}}
	cfg.Precedence.ThroughTrackMaxStayMin = units.Minutes(maxStay)
	s := &Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 1, BlocksPerLine: 2, ThroughTrack: true}))
	l = &ops.Train{Name: "L", Number: "L", MaxSpeed: 60, Priority: 20}
	l.AddSchedule(ops.NewStopPoint("WST", 2, "1").SetEntryTime(1))
	l.AddSchedule(ops.NewPassPoint("LPA", -1, "1"))
	l.AddSchedule(ops.NewPassPoint("HLT", -1, "1"))
	l.AddSchedule(ops.NewStopPoint("EST", 2, "1"))
	h = &ops.Train{Name: "H", Number: "H", MaxSpeed: 100, Priority: 50}
	h.AddSchedule(ops.NewStopPoint("WST", 2, "2").SetEntryTime(5))
	h.AddSchedule(ops.NewStopPoint("LPA", 2, "2"))
	h.AddSchedule(ops.NewPassPoint("HLT", -1, "1"))
	h.AddSchedule(ops.NewStopPoint("EST", 2, "2"))
	for _, tr := range []*ops.Train{l, h} {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	return s.prec, l, h
}

func TestStandable(t *testing.T) {
	p, l, h := refugePrecedence(t, 20)
	lpa, _ := p.sim.world.GetStation("LPA")
	through, loop := lpa.Platforms()[0], lpa.Platforms()[1]
	if lpa.TrackClass(through.Id) != infra.Through {
		t.Fatalf("lpaPf1 is %s, want a through track", lpa.TrackClass(through.Id))
	}
	pass, call := l.Schedule()[1], h.Schedule()[1]
	for _, c := range []struct {
		name string
		sp   *ops.SchedulePoint
		pf   *infra.Platform
		stay float64
		want string
	}{
		{"a pass on the through track", pass, through, 10, ""},
		{"a pass past the cap", pass, through, 21, "through_track_limit"},
		{"a call on the through track", call, through, 1, "through_track"},
		{"an unknown call", nil, through, 1, "through_track"},
		{"a long stand in the loop", pass, loop, 60, ""},
	} {
		if got := p.standable(c.sp, lpa, c.pf, c.stay); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}

	off, l, _ := refugePrecedence(t, 0)
	if got := off.standable(l.Schedule()[1], lpa, through, 1); got != "through_track" {
		t.Errorf("without the cap, a pass on the through track: %q, want through_track", got)
	}
}

// H calls at LPA's only loop, so L can only let it by standing on the
// through track: a refuge for trailing only with the cap set.
func TestTheThroughTrackIsARefugeOnlyWithTheCap(t *testing.T) {
	for _, c := range []struct {
		cap  float64
		want bool
	}{{20, true}, {0, false}} {
		p, l, h := refugePrecedence(t, c.cap)
		lpa, _ := p.sim.world.GetStation("LPA")
		v, ok := p.project(l).visitAt(lpa)
		if !ok {
			t.Fatal("L's projection does not get to LPA")
		}
		if got := p.refugeTrackAt(l, v, []*projection{p.project(h)}, 5); got != c.want {
			t.Errorf("cap %.0f: refuge at LPA %v, want %v", c.cap, got, c.want)
		}
	}
}

// calling is a train that calls at LPA, its plan there from from to to on
// the tracks ranked, best first.
func calling(pl *stationPlan, number string, from, to float64, ranked ...*infra.Platform) *ops.Train {
	t := &ops.Train{Name: number, Number: number, MaxSpeed: 100}
	t.AddSchedule(ops.NewStopPoint("LPA", 2, ""))
	a := &assignment{from: from, to: to}
	for _, pf := range ranked {
		a.ranked = append(a.ranked, candidate{pf: pf})
	}
	pl.assign[t.ID()] = a
	return t
}

func TestAStandingTrainKeepsOffTheLoopACallNeeds(t *testing.T) {
	p, l, _ := refugePrecedence(t, 20)
	lpa, _ := p.sim.world.GetStation("LPA")
	through, loop := lpa.Platforms()[0], lpa.Platforms()[1]
	// X runs through on the through track; L would stand in the loop from
	// 10 to 20
	pl := &stationPlan{stn: lpa, assign: map[ids.TrainID]*assignment{}}
	pl.bookings = []booking{{ids.TrainID{Number: "X"}, through, 12, 14}}
	a := &assignment{stop: 1, from: 10, to: 20}

	during := calling(pl, "C", 15, 19, loop) // a call never has the through track
	if got := p.blockedCalls(pl, l, a, loop, []*ops.Train{during}); len(got) != 1 || got[0] != during {
		t.Errorf("a call during the stand: blocked %v, want C", got)
	}
	after := calling(pl, "D", 21, 25, loop)
	if got := p.blockedCalls(pl, l, a, loop, []*ops.Train{after}); len(got) != 0 {
		t.Errorf("a call after the stand: blocked %v, want none", got)
	}
	// a call that has no track anyway is not L's doing
	pl.bookings = append(pl.bookings, booking{ids.TrainID{Number: "Y"}, loop, 14, 22})
	if got := p.blockedCalls(pl, l, a, loop, []*ops.Train{during}); len(got) != 0 {
		t.Errorf("a call with no track anyway: blocked %v, want none", got)
	}
}

func TestKeepBackWeighsAsTheHoldRule(t *testing.T) {
	held := Waiter{Prio: 50, Wait: 3}
	others := []Waiter{{Prio: 20, Wait: 2}, {Prio: 20, Wait: 2}}
	// plain minutes: 3 against 2+2
	if !(thresholdRule{}).KeepBack(held, others) {
		t.Error("thresholds: 3 min against 4 not kept back")
	}
	if (thresholdRule{}).KeepBack(held, others[:1]) {
		t.Error("thresholds: 3 min against 2 kept back")
	}
	// weighted: 150 against 40+40
	if (weightedRule{}).KeepBack(held, others) {
		t.Error("weighted: 150 against 80 kept back")
	}
	if !(weightedRule{}).KeepBack(Waiter{Prio: 20, Wait: 3}, []Waiter{{Prio: 50, Wait: 2}}) {
		t.Error("weighted: 60 against 100 not kept back")
	}
}
