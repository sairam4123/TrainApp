package sim

import (
	"math"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/units"
	"trainapp/worlds"
)

// trailingPrecedence is a precedence with trailing on, over a sim that has
// not run: its time is 0. Train H calls at LPA, timetabled 16 to 24.
func trailingPrecedence(t *testing.T) (*precedence, *ops.Train) {
	t.Helper()
	cfg := &PriorityConfig{Precedence: &PrecedenceConfig{HorizonMin: 30, HeadwayMin: 1, MaxHoldMin: 30, MainLineMaxStayMin: 5, FollowHorizonMin: 60}}
	s := &Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2}))
	h := &ops.Train{Name: "H", Number: "H", MaxSpeed: 100}
	h.AddSchedule(ops.NewStopPoint("WST", 2, "1").SetEntryTime(1))
	h.AddSchedule(ops.NewArrDepPoint("LPA", 16, 24, "1"))
	h.AddSchedule(ops.NewPassPoint("HLT", -1, "1"))
	h.AddSchedule(ops.NewStopPoint("EST", 2, "1"))
	if err := s.AddTrain(h); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	return s.prec, h
}

// fakeLine is three tracks, t[0] to t[2], each with an end to run from either
// way.
type fakeLine struct {
	t          [3]*infra.TrackSegment
	east, west [3]*infra.TrackPoint
}

func newFakeLine() *fakeLine {
	l := &fakeLine{}
	for i := range l.t {
		l.t[i] = &infra.TrackSegment{Name: string(rune('a' + i))}
		l.east[i], l.west[i] = &infra.TrackPoint{}, &infra.TrackPoint{}
	}
	return l
}

// run is a slot on track i running east (from its west end), entered at
// enter and left, and cleared, at exit.
func (l *fakeLine) run(i int, enter, exit float64) slot {
	return slot{step: infra.Step{Edge: &infra.GraphEdge{Track: l.t[i]}, From: l.west[i], To: l.east[i]}, enter: enter, exit: exit, clear: exit}
}

// back is run, westwards.
func (l *fakeLine) back(i int, enter, exit float64) slot {
	s := l.run(i, enter, exit)
	s.step.From, s.step.To = l.east[i], l.west[i]
	return s
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestFollowWaitsForTheTrainAhead(t *testing.T) {
	p, h := trailingPrecedence(t)
	line := newFakeLine()
	hp := &projection{train: h, slots: []slot{line.run(0, 10, 15), line.run(1, 15, 20), line.run(2, 20, 25)}}
	l := &projection{slots: []slot{line.run(1, 12, 19), line.run(2, 19, 30)}}

	f := p.follow(hp, []trailed{{l, -1}})
	// onto b once l has cleared it plus headway, 5 late; onto c no earlier
	// than 31, now 11 late
	if got := f.slots[1].enter; !near(got, 20) {
		t.Errorf("enters b at %.2f, want 20", got)
	}
	if got := lateAtEnd(hp, f); !near(got, 11) {
		t.Errorf("late at the end by %.2f, want 11", got)
	}
	if got := hp.slots[2].enter; !near(got, 20) {
		t.Errorf("the free projection changed: c entered at %.2f", got)
	}

	// cut at its track b, as if held there: only what is before b counts
	if got := lateAtEnd(hp, p.follow(hp, []trailed{{l, 0}})); !near(got, 0) {
		t.Errorf("cut at b: late by %.2f, want 0", got)
	}
	if got := lateAtEnd(hp, p.follow(hp, []trailed{{l, 1}})); !near(got, 5) {
		t.Errorf("cut at c: late by %.2f, want 5", got)
	}
}

func TestFollowIgnoresTrainsTheOtherWayOrBehind(t *testing.T) {
	p, h := trailingPrecedence(t)
	line := newFakeLine()
	hp := &projection{train: h, slots: []slot{line.run(0, 10, 15), line.run(1, 15, 20), line.run(2, 20, 25)}}
	other := &projection{slots: []slot{line.back(1, 12, 19)}}
	behind := &projection{slots: []slot{line.run(1, 16, 30)}}
	if got := lateAtEnd(hp, p.follow(hp, []trailed{{other, -1}, {behind, -1}})); !near(got, 0) {
		t.Errorf("late by %.2f, want 0", got)
	}
}

func TestTrailLossIsOnlyWhatTheTrainAdds(t *testing.T) {
	p, h := trailingPrecedence(t)
	line := newFakeLine()
	hp := &projection{train: h, slots: []slot{line.run(0, 10, 15), line.run(1, 15, 20), line.run(2, 20, 25)}}
	l := &projection{slots: []slot{line.run(1, 12, 19), line.run(2, 19, 30)}}
	// a slower one further ahead, off c only at 30 too
	l2 := &projection{slots: []slot{line.run(2, 8, 30)}}

	base := lateAtEnd(hp, p.follow(hp, []trailed{{l2, -1}}))
	full := lateAtEnd(hp, p.follow(hp, []trailed{{l2, -1}, {l, -1}}))
	if !near(base, 11) || !near(full-base, 0) {
		t.Errorf("behind l2 alone %.2f late (want 11), l adds %.2f (want 0)", base, full-base)
	}
}

func TestFollowSlackAtAStopTakesUpDelay(t *testing.T) {
	p, h := trailingPrecedence(t)
	line := newFakeLine()
	// at LPA, track b: in at 15, arrives 16, leaves at its timetabled 24
	hp := &projection{
		train:  h,
		slots:  []slot{line.run(0, 10, 15), line.run(1, 15, 24), line.run(2, 24, 29)},
		visits: []visit{{stop: 1, arr: 16, dep: 24, slot: 1}},
	}
	l := &projection{slots: []slot{line.run(1, 12, 19)}}
	f := p.follow(hp, []trailed{{l, -1}})
	if got := f.visits[0].arr; !near(got, 21) {
		t.Errorf("arrives at %.2f, want 21", got)
	}
	if got := f.visits[0].dep; !near(got, 24) {
		t.Errorf("leaves at %.2f, want its timetabled 24", got)
	}
	if got := lateAtEnd(hp, f); !near(got, 0) {
		t.Errorf("late at the end by %.2f, want 0: the slack takes it up", got)
	}
}

// hold is a hold of L (priority 20) for train id at some place.
func hold(id string, prio int, saving, wait float64) *conflict {
	return &conflict{Conflict: Conflict{Kind: KindOvertake, Held: ids.TrainID{Number: "L"}, For: ids.TrainID{Number: id},
		HeldPrio: 20, ForPrio: prio, Saving: units.Min(saving), Wait: units.Min(wait)}, chosen: true}
}

// spotsFor are places named by at, each allowing a stand up to limit.
func spotsFor(limit float64, at ...string) []spot {
	out := make([]spot, len(at))
	for i, a := range at {
		out[i] = spot{here: i == 0, stop: i, at: a, fits: func(_ []conflict, wait float64) bool { return wait <= limit }}
	}
	return out
}

// placesOf names where each train is held: "TPJ:A,B KRMG:C".
func placesOf(a *refugePlan) string {
	if a == nil {
		return ""
	}
	var parts []string
	for _, o := range a.holds {
		var trains []string
		for _, c := range o.pass {
			trains = append(trains, c.For.Number)
		}
		parts = append(parts, o.at+":"+strings.Join(trains, ","))
	}
	return strings.Join(parts, " ")
}

func TestAssignHoldsOnceForTwoTrains(t *testing.T) {
	p, _ := trailingPrecedence(t)
	p.rule = thresholdRule{gap: 10, saving: 1, ratio: 4}
	// as 56603 at TPJ in dl-20: a superfast A due by in 14 min, an express
	// B in 28; let go, B trails L to KRMG and gains less there
	items := [][]*conflict{
		{hold("A", 50, 6.4, 14), hold("A", 50, 3, 14)},
		{hold("B", 40, 6.5, 28), hold("B", 40, 4, 14)},
	}
	if got := placesOf(p.assign(spotsFor(30, "TPJ", "KRMG"), items)); got != "TPJ:A,B" {
		t.Errorf("held %q, want TPJ:A,B", got)
	}
	// B's extra 14 min for 6.5 saved is within 4 per minute; at 30 extra
	// minutes it is not, so B is held for at KRMG
	items[1][0] = hold("B", 40, 6.5, 44)
	if got := placesOf(p.assign(spotsFor(60, "TPJ", "KRMG"), items)); got != "TPJ:A KRMG:B" {
		t.Errorf("held %q, want TPJ:A KRMG:B", got)
	}
	// both cannot be had at TPJ within the hold cap
	items[1][0] = hold("B", 40, 6.5, 28)
	if got := placesOf(p.assign(spotsFor(20, "TPJ", "KRMG"), items)); got != "TPJ:A KRMG:B" {
		t.Errorf("held %q with a 20 min cap, want TPJ:A KRMG:B", got)
	}
}

func TestAssignPicksTheCheaperPlace(t *testing.T) {
	p, _ := trailingPrecedence(t)
	p.rule = thresholdRule{gap: 10, saving: 1, ratio: 4}
	// here costs 20 min for 5.8; VEL, where it runs through, 12 for 5
	items := [][]*conflict{{hold("X", 40, 5.8, 20), nil, hold("X", 40, 5, 12)}}
	if got := placesOf(p.assign(spotsFor(30, "TPJ", "KRUR", "VEL"), items)); got != "VEL:X" {
		t.Errorf("held %q, want VEL:X", got)
	}
	// on a tie, the earlier place
	items[0][2] = hold("X", 40, 5.8, 20)
	if got := placesOf(p.assign(spotsFor(30, "TPJ", "KRUR", "VEL"), items)); got != "TPJ:X" {
		t.Errorf("held %q on a tie, want TPJ:X", got)
	}
	// nowhere the rule accepts: a priority gap below 10
	items = [][]*conflict{{hold("Y", 25, 5.8, 12)}}
	if got := p.assign(spotsFor(30, "TPJ"), items); got != nil {
		t.Errorf("held %q for a train 5 above it", placesOf(got))
	}
}
