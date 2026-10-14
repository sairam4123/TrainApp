package ops_test

import (
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/railway/ops"
	"trainapp/worlds"
)

// reservedRoute reserves the whole best path TPJ pf1 -> PDKT pf1, which
// crosses three signal sections: TPJ pf1, out to the PDKT home signal, and
// into PDKT pf1.
func reservedRoute(t *testing.T, train ids.TrainID) (*infra.World, *interlocking.Interlocking, *interlocking.Route) {
	t.Helper()
	w := worlds.BuildTpjKkdiWorld()
	ilk, err := interlocking.New(w)
	if err != nil {
		t.Fatal(err)
	}
	tpj, _ := w.GetStation("TPJ")
	pdkt, _ := w.GetStation("PDKT")
	start := w.TrackGraph.FindWorldBoundaryPoint(tpj.FindStnPlatform("1"))
	ranked, err := w.RankedPaths(start, nil, pdkt.FindStnPlatform("1"))
	if err != nil {
		t.Fatal(err)
	}
	route, err := ilk.Reserve(ranked[0].Path, train)
	if err != nil {
		t.Fatal(err)
	}
	return w, ilk, route
}

func TestAuthorityStopsAtEachSignal(t *testing.T) {
	w, ilk, route := reservedRoute(t, ids.TrainID{Number: "A"})

	// sections end where a signal or platform end is met
	var ends []int
	for i := range route.End() {
		if w.EndsAtSignal(route.Step(i)) {
			ends = append(ends, i)
		}
	}
	if len(ends) != 3 || ends[2] != route.End()-1 {
		t.Fatalf("signal ends at %v in a route of %d edges, want 3 ending with the last", ends, route.End())
	}

	ma, err := ops.NewMovementAuthority(w, route)
	if err != nil {
		t.Fatal(err)
	}
	for _, end := range ends {
		if ma.Limit() != end {
			t.Fatalf("limit %d, want %d", ma.Limit(), end)
		}
		for ma.PermitsNext() {
			if _, err := ilk.Enter(route); err != nil {
				t.Fatal(err)
			}
		}
		if !ma.Exhausted() {
			t.Fatal("authority not exhausted at its limit")
		}
		if ma.CanExtend() {
			if err := ma.Extend(w); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ma.CanExtend() || ma.Extend(w) == nil {
		t.Fatal("extended past the end of the route")
	}
}

func TestAuthorityNeedsEdgesAhead(t *testing.T) {
	w, ilk, route := reservedRoute(t, ids.TrainID{Number: "A"})
	for range route.End() {
		ilk.Enter(route)
	}
	if _, err := ops.NewMovementAuthority(w, route); err == nil {
		t.Fatal("authority over a fully entered route")
	}
	if _, err := ops.NewMovementAuthority(w, nil); err == nil {
		t.Fatal("authority without a route")
	}
}

func TestTrainAcceptsOnlyItsOwnAuthority(t *testing.T) {
	w, _, route := reservedRoute(t, ids.TrainID{Number: "A"})
	ma, err := ops.NewMovementAuthority(w, route)
	if err != nil {
		t.Fatal(err)
	}
	other := &ops.Train{Number: "B"}
	if err := other.Authorize(ma); err == nil {
		t.Fatal("train B accepted A's authority")
	}
	own := &ops.Train{Number: "A"}
	if err := own.Authorize(ma); err != nil || own.Route() != route {
		t.Fatalf("Authorize = %v, route set %v", err, own.Route() == route)
	}
}
