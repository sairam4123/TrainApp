package interlocking_test

import (
	"errors"
	"slices"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/worlds"
)

var (
	trainA = ids.TrainID{Number: "A"}
	trainB = ids.TrainID{Number: "B"}
)

func setup(t *testing.T) (*infra.World, *interlocking.Interlocking) {
	t.Helper()
	w := worlds.BuildTpjKkdiWorld()
	ilk, err := interlocking.New(w)
	if err != nil {
		t.Fatal(err)
	}
	return w, ilk
}

// bestPath is the top-ranked path from a boundary platform to a platform.
func bestPath(t *testing.T, w *infra.World, from ids.StnCode, fromPf string, to ids.StnCode, toPf string) *infra.Path {
	t.Helper()
	fs, _ := w.GetStation(from)
	ts, _ := w.GetStation(to)
	start := w.TrackGraph.FindWorldBoundaryPoint(fs.FindStnPlatform(fromPf))
	ranked, err := w.RankedPaths(start, nil, ts.FindStnPlatform(toPf))
	if err != nil || len(ranked) == 0 {
		t.Fatalf("no path %s/%s -> %s/%s: %v", from, fromPf, to, toPf, err)
	}
	return ranked[0].Path
}

// holdings counts the tracks reserved by and switch blocks locked by a train.
func holdings(w *infra.World, ilk *interlocking.Interlocking, train ids.TrainID) (tracks, switches int) {
	for _, trk := range w.TrackGraph.ListTracks() {
		if r, _ := ilk.TrackHolder(trk); r == train {
			tracks++
		}
	}
	for _, sb := range w.ListSwitchBlocks() {
		if _, l := ilk.SwitchState(sb); l == train {
			switches++
		}
	}
	return
}

func TestNewRequiresFinalizedWorld(t *testing.T) {
	w := &infra.World{}
	w.Init(infra.WorldData{})
	if _, err := interlocking.New(w); err == nil {
		t.Fatal("accepted a world that was never finalized")
	}
}

func TestReserveHoldsTracksAndSwitches(t *testing.T) {
	w, ilk := setup(t)
	p := bestPath(t, w, "TPJ", "1", "PDKT", "1")
	route, err := ilk.Reserve(p, trainA)
	if err != nil {
		t.Fatal(err)
	}
	if route.Train() != trainA || route.End() != len(p.Edges) || route.Entered() != 0 {
		t.Fatalf("route = train %v end %d entered %d", route.Train(), route.End(), route.Entered())
	}
	for _, e := range p.Edges {
		if r, o := ilk.TrackHolder(e.Track); r != trainA || !o.IsZero() {
			t.Errorf("%s: reserved by %v occupied by %v", e.Track, r, o)
		}
		for _, sb := range w.SwitchBlocksOn(e.Track) {
			if active, l := ilk.SwitchState(sb); active != e || l != trainA {
				t.Errorf("switch %s: set for %v locked by %v", sb.Id, active.Track, l)
			}
		}
	}
}

func TestConflictIsReportedAndLeavesNothingBehind(t *testing.T) {
	w, ilk := setup(t)
	if _, err := ilk.Reserve(bestPath(t, w, "TPJ", "1", "PDKT", "1"), trainA); err != nil {
		t.Fatal(err)
	}
	beforeA, beforeASw := holdings(w, ilk, trainA)

	// B from another platform shares the line out of TPJ
	_, err := ilk.Reserve(bestPath(t, w, "TPJ", "2", "PDKT", "2"), trainB)
	var conflict *interlocking.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	if conflict.HeldBy != trainA || conflict.Train != trainB {
		t.Errorf("conflict = %+v", conflict)
	}
	if n, s := holdings(w, ilk, trainB); n != 0 || s != 0 {
		t.Errorf("B still holds %d tracks and %d switches after a failed reserve", n, s)
	}
	if n, s := holdings(w, ilk, trainA); n != beforeA || s != beforeASw {
		t.Errorf("A's holdings changed from %d/%d to %d/%d", beforeA, beforeASw, n, s)
	}
}

func TestRetryBySameTrainKeepsItsRoute(t *testing.T) {
	w, ilk := setup(t)
	p := bestPath(t, w, "TPJ", "1", "PDKT", "1")
	if _, err := ilk.Reserve(p, trainA); err != nil {
		t.Fatal(err)
	}
	tracks, switches := holdings(w, ilk, trainA)
	if _, err := ilk.Reserve(p, trainA); err == nil {
		t.Fatal("second reservation of the same path succeeded")
	}
	if n, s := holdings(w, ilk, trainA); n != tracks || s != switches {
		t.Fatalf("retry changed A's holdings from %d/%d to %d/%d", tracks, switches, n, s)
	}
}

func TestEnterLeaveLifecycle(t *testing.T) {
	w, ilk := setup(t)
	p := bestPath(t, w, "TPJ", "1", "PDKT", "1")
	route, err := ilk.Reserve(p, trainA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ilk.Leave(route); err == nil {
		t.Fatal("left a route that was never entered")
	}

	for i := range route.End() {
		step, err := ilk.Enter(route)
		if err != nil {
			t.Fatalf("enter %d: %v", i, err)
		}
		if _, o := ilk.TrackHolder(step.Edge.Track); o != trainA {
			t.Fatalf("%s not occupied by A after Enter", step.Edge.Track)
		}
		if i > 0 {
			left, err := ilk.Leave(route)
			if err != nil {
				t.Fatalf("leave %d: %v", i-1, err)
			}
			if !ilk.IsTrackFree(left.Edge.Track) {
				t.Fatalf("%s not free after Leave", left.Edge.Track)
			}
			for _, sb := range w.SwitchBlocksOn(left.Edge.Track) {
				if _, l := ilk.SwitchState(sb); !l.IsZero() {
					t.Fatalf("switch %s still locked by %v after its track was left", sb.Id, l)
				}
			}
		}
	}
	if _, err := ilk.Enter(route); err == nil {
		t.Fatal("entered past the end of the route")
	}
	if !route.FullyEntered() || route.Done() {
		t.Fatalf("fully entered %v done %v", route.FullyEntered(), route.Done())
	}
	if _, err := ilk.Leave(route); err != nil {
		t.Fatal(err)
	}
	if !route.Done() {
		t.Fatal("route not done after leaving the last edge")
	}
	if n, s := holdings(w, ilk, trainA); n != 0 || s != 0 {
		t.Fatalf("A still holds %d tracks and %d switches", n, s)
	}
}

func TestReleaseFreesOnlyTheUnusedPart(t *testing.T) {
	w, ilk := setup(t)
	route, err := ilk.Reserve(bestPath(t, w, "TPJ", "1", "PDKT", "1"), trainA)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := ilk.Enter(route)
	freed := ilk.Release(route)
	if len(freed) != route.Len()-1 {
		t.Fatalf("freed %d tracks, want %d", len(freed), route.Len()-1)
	}
	if _, o := ilk.TrackHolder(first.Edge.Track); o != trainA {
		t.Fatal("Release freed the occupied track")
	}
	if n, _ := holdings(w, ilk, trainA); n != 1 {
		t.Fatalf("A holds %d tracks after release, want 1", n)
	}
	if _, err := ilk.Enter(route); err == nil {
		t.Fatal("entered a released edge")
	}
}

func TestOccupiedTrackCannotBeReserved(t *testing.T) {
	w, ilk := setup(t)
	route, _ := ilk.Reserve(bestPath(t, w, "TPJ", "1", "PDKT", "1"), trainA)
	ilk.Enter(route)
	ilk.Release(route) // A now only occupies TPJ pf1

	stn, _ := w.GetStation("TPJ")
	pf := stn.FindStnPlatform("1")
	start := w.TrackGraph.FindWorldBoundaryPoint(pf)
	_, err := ilk.Reserve(&infra.Path{InitPoint: start, Edges: []*infra.GraphEdge{w.TrackGraph.Edges[pf.Id]}}, trainB)
	var conflict *interlocking.ConflictError
	if !errors.As(err, &conflict) || conflict.HeldBy != trainA {
		t.Fatalf("err = %v, want conflict held by A", err)
	}
}

func TestCrossoverConflicts(t *testing.T) {
	w, ilk := setup(t)
	edge := func(a, b string) *infra.GraphEdge {
		pa := ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: a}
		pb := ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: b}
		e, ok := w.TrackGraph.Edges[ids.NewTrackID(pa, pb)]
		if !ok {
			t.Fatalf("no edge Bp%s-Bp%s", a, b)
		}
		return e
	}
	one := func(e *infra.GraphEdge, from *infra.TrackPoint) *infra.Path {
		return &infra.Path{InitPoint: from, Edges: []*infra.GraphEdge{e}}
	}

	diag := edge("0", "3")
	if _, err := ilk.Reserve(one(diag, diag.From), trainA); err != nil {
		t.Fatal(err)
	}
	// the other diagonal crosses at the diamond
	other := edge("1", "2")
	if _, err := ilk.Reserve(one(other, other.From), trainB); err == nil {
		t.Error("both crossover diagonals reserved at once")
	}
	// the straight line from Bp0 shares switch tpjSw0A with the diagonal
	main := edge("0", "2")
	if _, err := ilk.Reserve(one(main, main.From), trainB); err == nil {
		t.Error("straight route through a switch locked for the diagonal was reserved")
	}
	// the parallel line Bp1-Bp3 shares switch tpjSw0B with the diagonal at Bp3
	if _, err := ilk.Reserve(one(edge("1", "3"), edge("1", "3").From), trainB); err == nil {
		t.Error("parallel line through a switch locked for the diagonal was reserved")
	}
}

func TestIllegalPathsAreRefused(t *testing.T) {
	w, ilk := setup(t)
	stn, _ := w.GetStation("TPJ")
	pf1 := w.TrackGraph.Edges[stn.FindStnPlatform("1").Id]
	pf2 := w.TrackGraph.Edges[stn.FindStnPlatform("2").Id]
	bp0, _ := w.TrackGraph.GetPoint(ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: "0"})
	pf1E, _ := w.TrackGraph.GetPoint(ids.PointID{StnCode: "TPJ", Type: "Pf", Seq: "1", Other: "E"})
	pf2E, _ := w.TrackGraph.GetPoint(ids.PointID{StnCode: "TPJ", Type: "Pf", Seq: "2", Other: "E"})
	leg1, _ := w.TrackGraph.TrackBetween(pf1E, bp0)
	leg2, _ := w.TrackGraph.TrackBetween(bp0, pf2E)

	// pf1 -> Bp0 -> pf2 swings from one switch leg straight onto the other
	toeReversal := &infra.Path{InitPoint: pf1.Other(pf1E), Edges: []*infra.GraphEdge{pf1, leg1, leg2, pf2}}
	if _, err := ilk.Reserve(toeReversal, trainA); !errors.Is(err, interlocking.ErrIllegalPath) {
		t.Errorf("toe reversal: err = %v, want ErrIllegalPath", err)
	}
	broken := &infra.Path{InitPoint: pf1E, Edges: []*infra.GraphEdge{pf2}}
	if _, err := ilk.Reserve(broken, trainA); !errors.Is(err, interlocking.ErrIllegalPath) {
		t.Errorf("broken path: err = %v, want ErrIllegalPath", err)
	}
	if _, err := ilk.Reserve(&infra.Path{InitPoint: pf1E}, trainA); !errors.Is(err, interlocking.ErrIllegalPath) {
		t.Errorf("empty path: err = %v, want ErrIllegalPath", err)
	}
	if n, s := holdings(w, ilk, trainA); n != 0 || s != 0 {
		t.Errorf("A holds %d/%d after refused paths", n, s)
	}
}

func TestReserveFirstFallsBack(t *testing.T) {
	w, ilk := setup(t)
	if _, err := ilk.Reserve(bestPath(t, w, "TPJ", "1", "PDKT", "1"), trainA); err != nil {
		t.Fatal(err)
	}
	stn, _ := w.GetStation("TPJ")
	start := w.TrackGraph.FindWorldBoundaryPoint(stn.FindStnPlatform("3"))
	pdkt, _ := w.GetStation("PDKT")
	ranked, _ := w.RankedPaths(start, nil, pdkt.FindStnPlatform("1"))
	paths := make([]*infra.Path, len(ranked))
	for i, r := range ranked {
		paths[i] = r.Path
	}
	// every way into PDKT pf1 from TPJ crosses A's route
	_, err := ilk.ReserveFirst(paths, trainB)
	var conflict *interlocking.ConflictError
	if !errors.As(err, &conflict) || conflict.HeldBy != trainA {
		t.Fatalf("err = %v, want conflict with A", err)
	}
	if _, err := ilk.ReserveFirst(nil, trainB); err == nil {
		t.Fatal("ReserveFirst with no paths succeeded")
	}
}

func TestHoldingsFollowReservations(t *testing.T) {
	w, ilk := setup(t)
	route, err := ilk.Reserve(bestPath(t, w, "TPJ", "1", "PDKT", "1"), trainA)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ilk.Holdings(trainA)); got != route.End() {
		t.Fatalf("holdings %d, want %d", got, route.End())
	}
	ilk.Enter(route)
	ilk.Enter(route)
	ilk.Leave(route)
	ilk.Release(route)
	if got := ilk.Holdings(trainA); len(got) != 1 || got[0] != route.Step(1).Edge.Track {
		t.Fatalf("holdings after moving on and releasing = %v, want just %s", got, route.Step(1).Edge.Track)
	}
	if len(ilk.Holdings(trainB)) != 0 {
		t.Fatal("B holds tracks it never reserved")
	}
}

func TestConflictBlockers(t *testing.T) {
	w, ilk := setup(t)
	edge := func(a, b string) *infra.GraphEdge {
		e := w.TrackGraph.Edges[ids.NewTrackID(ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: a}, ids.PointID{StnCode: "TPJ", Type: "Bp", Seq: b})]
		return e
	}
	diag := edge("0", "3")
	ilk.Reserve(&infra.Path{InitPoint: diag.From, Edges: []*infra.GraphEdge{diag}}, trainA)

	// same track: the track itself blocks
	_, err := ilk.Reserve(&infra.Path{InitPoint: diag.From, Edges: []*infra.GraphEdge{diag}}, trainB)
	var conflict *interlocking.ConflictError
	if !errors.As(err, &conflict) || conflict.Switch != nil {
		t.Fatalf("err = %v, want a track conflict", err)
	}
	if b := conflict.Blockers(); len(b) != 1 || b[0] != diag.Track {
		t.Fatalf("blockers %v, want the diagonal", b)
	}

	// other track through a locked switch: every track of that switch blocks
	main := edge("0", "2")
	_, err = ilk.Reserve(&infra.Path{InitPoint: main.From, Edges: []*infra.GraphEdge{main}}, trainB)
	if !errors.As(err, &conflict) || conflict.Switch == nil {
		t.Fatalf("err = %v, want a switch conflict", err)
	}
	blockers := conflict.Blockers()
	if !slices.Contains(blockers, diag.Track) {
		t.Fatalf("blockers %v do not include the track holding the switch", blockers)
	}
}
