package infra_test

import (
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
	"trainapp/worlds"
)

func pathKey(p *infra.Path) string {
	ids := make([]string, len(p.Edges))
	for i, e := range p.Edges {
		ids[i] = e.Track.Id.String()
	}
	return strings.Join(ids, ",")
}

func boundaryOf(t *testing.T, w *infra.World, stn ids.StnCode, pf string) (*infra.TrackPoint, *infra.TrackSegment) {
	t.Helper()
	s, ok := w.GetStation(stn)
	if !ok {
		t.Fatalf("no station %s", stn)
	}
	trk := s.FindStnPlatform(pf)
	if trk == nil {
		t.Fatalf("no platform %s at %s", pf, stn)
	}
	return w.TrackGraph.FindWorldBoundaryPoint(trk), trk
}

func platform(t *testing.T, w *infra.World, stn ids.StnCode, pf string) *infra.TrackSegment {
	t.Helper()
	_, trk := boundaryOf(t, w, stn, pf)
	return trk
}

func TestRankedPathsAreDeterministic(t *testing.T) {
	var want []string
	for i := range 30 {
		w := worlds.BuildTpjKkdiWorld()
		start, _ := boundaryOf(t, w, "TPJ", "1")
		ranked, err := w.RankedPaths(start, nil, platform(t, w, "KKDI", "1"))
		if err != nil {
			t.Fatal(err)
		}
		got := make([]string, len(ranked))
		for j, r := range ranked {
			got[j] = pathKey(r.Path)
		}
		if i == 0 {
			want = got
			continue
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("run %d ranked paths differently:\n%v\nvs\n%v", i, got, want)
		}
	}
}

func TestCandidatesNeverReverseThroughASwitch(t *testing.T) {
	w := worlds.BuildTpjKkdiWorld()
	starts := []string{"TPJ/1", "TPJ/2", "TPJ/3", "TPJ/4", "KKDI/1", "KKDI/2", "KKDI/3", "KKDI/4"}
	for _, s := range starts {
		stn, pf, _ := strings.Cut(s, "/")
		start, _ := boundaryOf(t, w, ids.StnCode(stn), pf)
		for _, target := range w.ListStations() {
			for _, tp := range target.Platforms() {
				paths, err := w.TrackGraph.GenerateCandidatePaths(start, tp.Track)
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range paths {
					if !w.IsLegalPath(p) {
						t.Errorf("%s -> %s pf%s: illegal candidate %s", s, target.Code, tp.PfNo, p)
					}
				}
			}
		}
	}
}

func TestEveryRunningLinePlatformHasASignalCorrectPath(t *testing.T) {
	w := worlds.BuildTpjKkdiWorld()
	cases := []struct {
		from   ids.StnCode
		fromPf string
		to     ids.StnCode
		toPf   string
	}{
		{"TPJ", "1", "PDKT", "1"}, {"TPJ", "1", "PDKT", "2"}, {"TPJ", "3", "PDKT", "1"},
		{"TPJ", "1", "KKDI", "1"}, {"TPJ", "1", "KKDI", "2"},
		{"KKDI", "3", "PDKT", "3"}, {"KKDI", "3", "PDKT", "4"}, {"KKDI", "1", "PDKT", "3"},
		{"KKDI", "3", "TPJ", "3"}, {"KKDI", "3", "TPJ", "1"},
	}
	for _, c := range cases {
		start, _ := boundaryOf(t, w, c.from, c.fromPf)
		ranked, err := w.RankedPaths(start, nil, platform(t, w, c.to, c.toPf))
		if err != nil {
			t.Fatal(err)
		}
		if len(ranked) == 0 || !ranked[0].FollowsSignals {
			t.Errorf("%s pf%s -> %s pf%s: no signal-correct path", c.from, c.fromPf, c.to, c.toPf)
		}
	}
	// PDKT pf3 is on the down line, so reaching it from TPJ means wrong-direction running
	start, _ := boundaryOf(t, w, "TPJ", "1")
	ranked, _ := w.RankedPaths(start, nil, platform(t, w, "PDKT", "3"))
	if len(ranked) == 0 || ranked[0].FollowsSignals {
		t.Errorf("TPJ pf1 -> PDKT pf3: expected only wrong-direction candidates, got %d (first correct=%v)", len(ranked), len(ranked) > 0 && ranked[0].FollowsSignals)
	}
}

func pt(name string) ids.PointID { return ids.PointID{Type: name} }

func newWorld() *infra.World {
	w := &infra.World{}
	w.Init(infra.WorldData{
		DefaultSwLength: 100, DefaultSwMaxSpeed: units.KMPH(30),
		DefaultPfLength: 800, DefaultPfMaxSpeed: units.KMPH(50),
		DefaultRouteSpeed: units.KMPH(110),
	})
	return w
}

func TestSlipSwitches(t *testing.T) {
	for _, double := range []bool{false, true} {
		w := newWorld()
		a, ta, b, tb := w.NewTrackPoint(pt("a")), w.NewTrackPoint(pt("ta")), w.NewTrackPoint(pt("b")), w.NewTrackPoint(pt("tb"))
		var sb *infra.SwitchBlock
		var err error
		if double {
			sb, err = infra.NewDoubleSlip(w, "s", a, ta, b, tb)
		} else {
			sb, err = infra.NewSingleSlip(w, "s", a, ta, b, tb, infra.FromAtoB)
		}
		if err != nil {
			t.Fatal(err)
		}
		edges := sb.ListManagedEdges()
		wantEdges := 3
		if double {
			wantEdges = 4
		}
		if len(edges) != wantEdges {
			t.Fatalf("double=%v: %d managed edges, want %d", double, len(edges), wantEdges)
		}
		for _, e := range edges {
			if e == nil {
				t.Fatalf("double=%v: nil managed edge", double)
			}
			if got := w.SwitchBlocksOn(e.Track); len(got) != 1 || got[0] != sb {
				t.Fatalf("SwitchBlocksOn(%s) = %v", e.Track.Id, got)
			}
		}
		if err := w.Finalize(); err != nil {
			t.Fatal(err)
		}
		aToA, _ := w.TrackGraph.TrackBetween(a, ta)
		aToB, _ := w.TrackGraph.TrackBetween(a, tb)
		if w.TrackGraph.TransitionAllowed(a, aToA, aToB) {
			t.Errorf("double=%v: transition between two slip routes out of a should be forbidden", double)
		}
	}
	if _, err := infra.NewSingleSlip(newWorld(), "s", nil, nil, nil, nil, infra.SlipOrientation(5)); err == nil {
		t.Error("bad orientation accepted")
	}
}

func TestCrossoverDiagonalsRunAtSwitchSpeed(t *testing.T) {
	w := worlds.BuildTpjKkdiWorld()
	trk, ok := w.TrackGraph.GetTrack(ids.NewTrackID(
		ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: "3"},
		ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: "0"},
	))
	if !ok {
		t.Fatal("crossover diagonal tpjBp0-tpjBp3 missing")
	}
	if trk.MaxSpeed != units.KMPH(30) {
		t.Errorf("diagonal speed = %v, want %v", trk.MaxSpeed, units.KMPH(30))
	}
	if got := len(w.SwitchBlocksOn(trk)); got != 3 {
		t.Errorf("diagonal is managed by %d switch blocks, want 3 (diamond + two switches)", got)
	}
}

func TestTrackIDsDoNotCollide(t *testing.T) {
	if ids.NewTrackID(pt("a"), pt("bc")) == ids.NewTrackID(pt("ab"), pt("c")) {
		t.Fatal("NewTrackID(a,bc) == NewTrackID(ab,c)")
	}
	if ids.NewTrackID(pt("x"), pt("y")) != ids.NewTrackID(pt("y"), pt("x")) {
		t.Fatal("NewTrackID is not symmetric")
	}
}

func TestAddTrackDuplicates(t *testing.T) {
	w := newWorld()
	a, b, c := w.NewTrackPoint(pt("a")), w.NewTrackPoint(pt("b")), w.NewTrackPoint(pt("c"))
	e1 := w.TrackGraph.AddTrack(a, b, &infra.TrackSegment{Length: 10, MaxSpeed: 100})
	e2 := w.TrackGraph.AddTrack(b, a, &infra.TrackSegment{Length: 10, MaxSpeed: 40})
	if e1 != e2 || e1.Track.MaxSpeed != 40 {
		t.Fatalf("same-endpoint duplicate should reuse the edge with the lower speed, got %v speed %v", e1 == e2, e1.Track.MaxSpeed)
	}

	mustPanic(t, "adding one segment between two pairs of points", func() { w.TrackGraph.AddTrack(a, c, e1.Track) })
	mustPanic(t, "two point objects with one id", func() { w.TrackGraph.AddTrack(w.NewTrackPoint(pt("a")), c, &infra.TrackSegment{}) })
}

func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", what)
		}
	}()
	f()
}

func TestPathLengthAndSteps(t *testing.T) {
	w := newWorld()
	a, b, c := w.NewTrackPoint(pt("a")), w.NewTrackPoint(pt("b")), w.NewTrackPoint(pt("c"))
	ab := w.TrackGraph.AddTrack(a, b, &infra.TrackSegment{Length: 100})
	cb := w.TrackGraph.AddTrack(c, b, &infra.TrackSegment{Length: 900})

	p := &infra.Path{InitPoint: a, Edges: []*infra.GraphEdge{ab}}
	if p.Length() != 100 {
		t.Fatalf("length %v", p.Length())
	}
	p.ExtendPath(cb)
	if p.Length() != 1000 {
		t.Fatalf("length after extend %v, want 1000", p.Length())
	}
	steps, err := p.Steps()
	if err != nil {
		t.Fatal(err)
	}
	// cb was built c->b but is run b->c here
	if steps[1].From != b || steps[1].To != c {
		t.Fatalf("step 1 runs %s->%s, want b->c", steps[1].From.Id, steps[1].To.Id)
	}
	if p.EndPoint() != c {
		t.Fatalf("end point %v", p.EndPoint())
	}

	broken := &infra.Path{InitPoint: c, Edges: []*infra.GraphEdge{ab}}
	if _, err := broken.Steps(); err == nil {
		t.Fatal("broken path accepted")
	}
}

func TestFinalizeRejectsSignalWithoutTrack(t *testing.T) {
	w := newWorld()
	a, b, c := w.NewTrackPoint(pt("a")), w.NewTrackPoint(pt("b")), w.NewTrackPoint(pt("c"))
	w.TrackGraph.AddTrack(a, b, w.NewTrackSegment("ab", 10))
	w.TrackGraph.AddTrack(b, c, w.NewTrackSegment("bc", 10))
	w.AddSignal(infra.NewSignal("bad", a, c))
	if err := w.Finalize(); err == nil {
		t.Fatal("signal between non-adjacent points accepted")
	}
	if w.IsFinalized() {
		t.Fatal("world marked finalized after failure")
	}
}

func TestCandidatePathsNeedFinalize(t *testing.T) {
	w := newWorld()
	a, b := w.NewTrackPoint(pt("a")), w.NewTrackPoint(pt("b"))
	ab := w.TrackGraph.AddTrack(a, b, w.NewTrackSegment("ab", 10))
	if _, err := w.TrackGraph.GenerateCandidatePaths(a, ab.Track); err == nil {
		t.Fatal("expected error before cache is built")
	}
	w.Finalize()
	paths, err := w.TrackGraph.GenerateCandidatePaths(a, ab.Track)
	if err != nil || len(paths) != 1 {
		t.Fatalf("got %d paths, err %v", len(paths), err)
	}
}

func TestTravelTimePanicsOnZeroSpeed(t *testing.T) {
	trk := &infra.TrackSegment{Name: "x", Length: 1000, MaxSpeed: 0}
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	trk.TravelTime(units.KMPH(100))
}

func stepOn(t *testing.T, w *infra.World, from, to ids.PointID) infra.Step {
	t.Helper()
	a, ok1 := w.TrackGraph.GetPoint(from)
	b, ok2 := w.TrackGraph.GetPoint(to)
	if !ok1 || !ok2 {
		t.Fatalf("missing point %s or %s", from, to)
	}
	e, err := w.TrackGraph.TrackBetween(a, b)
	if err != nil {
		t.Fatal(err)
	}
	return infra.Step{Edge: e, From: a, To: b}
}

func TestSignalSections(t *testing.T) {
	w := worlds.BuildTpjKkdiWorld()
	tpjBp2 := ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: "2"}
	pdktBp0 := ids.PointID{StnCode: "PDKT", Type: "Bp", Seq: "0"}
	pf1S := ids.PointID{StnCode: "PDKT", Type: "Pf", Seq: "1", Other: "S"}
	pf1E := ids.PointID{StnCode: "PDKT", Type: "Pf", Seq: "1", Other: "E"}

	if !w.EndsAtSignal(stepOn(t, w, tpjBp2, pdktBp0)) {
		t.Error("running into PDKT should end at its home signal")
	}
	if w.EndsAtSignal(stepOn(t, w, pdktBp0, tpjBp2)) {
		t.Error("the PDKT home signal does not face trains leaving PDKT")
	}
	// platforms have an invisible signal at each end
	if !w.EndsAtSignal(stepOn(t, w, pf1S, pf1E)) || !w.EndsAtSignal(stepOn(t, w, pf1E, pf1S)) {
		t.Error("platform ends should act as signals both ways")
	}
	if w.EndsAtSignal(stepOn(t, w, pdktBp0, pf1S)) {
		t.Error("a plain switch track has no signal")
	}

	steps := []infra.Step{stepOn(t, w, tpjBp2, pdktBp0), stepOn(t, w, pdktBp0, pf1S), stepOn(t, w, pf1S, pf1E)}
	if got := w.SectionEnd(steps, 0); got != 1 {
		t.Errorf("first section ends at %d, want 1", got)
	}
	if got := w.SectionEnd(steps, 1); got != 3 {
		t.Errorf("second section ends at %d, want 3", got)
	}
	if got := w.SectionEnd(steps[1:2], 0); got != 1 {
		t.Errorf("unsignalled tail ends at %d, want its length 1", got)
	}
}

func TestCanVisit(t *testing.T) {
	w := worlds.BuildTpjKkdiWorld()
	start, _ := boundaryOf(t, w, "TPJ", "1")
	pos := infra.Position{At: start}

	ok, err := w.CanVisit(pos, []ids.StnCode{"TPJ", "PDKT", "KKDI"}, nil)
	if err != nil || !ok {
		t.Fatalf("TPJ -> PDKT -> KKDI: %v %v", ok, err)
	}
	// an up train must not be told it can reach KKDI by running back down the down line
	leg, _ := w.TrackGraph.GetTrack(ids.NewTrackID(ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: "2"}, ids.PointID{StnCode: "PDKT", Type: "Bp", Seq: "0"}))
	blockUpLeg := func(s infra.Step) bool { return s.Edge.Track == leg }
	if ok, _ := w.CanVisit(pos, []ids.StnCode{"TPJ", "PDKT"}, blockUpLeg); ok {
		t.Error("PDKT reachable with the up line blocked: wrong-direction running was allowed")
	}
	// a train at the boundary calls at the platform in front of it
	if ok, _ := w.CanVisit(pos, []ids.StnCode{"TPJ"}, nil); !ok {
		t.Error("could not call at the platform it stands next to")
	}
	if _, err := w.CanVisit(pos, []ids.StnCode{"NOPE"}, nil); err == nil {
		t.Error("unknown station accepted")
	}
}

func TestReachStopsAtTargetsAndHonoursAllow(t *testing.T) {
	w := worlds.BuildTpjKkdiWorld()
	start, _ := boundaryOf(t, w, "TPJ", "1")
	kkdi, _ := w.GetStation("KKDI")
	atKkdi := func(s infra.Step) bool { return kkdi.IsPlatform(s.Edge.Track.Id) }

	got, err := w.TrackGraph.Reach([]infra.Position{{At: start}}, atKkdi, nil)
	if err != nil || len(got) == 0 {
		t.Fatalf("Reach found %d positions, err %v", len(got), err)
	}
	for _, p := range got {
		if !kkdi.IsPlatform(p.Via.Track.Id) {
			t.Errorf("result %s via %s is not a KKDI platform", p.At.Id, p.Via.Track)
		}
	}
	none, _ := w.TrackGraph.Reach([]infra.Position{{At: start}}, atKkdi, func(infra.Step) bool { return false })
	if len(none) != 0 {
		t.Errorf("reached %d positions with every step disallowed", len(none))
	}
}
