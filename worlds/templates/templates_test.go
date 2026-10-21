package templates

import (
	"fmt"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
	"trainapp/visualization/schematic"
)

func newWorld() *infra.World {
	w := &infra.World{}
	w.Init(infra.WorldData{
		DefaultSwMaxSpeed: units.KMPH(30), DefaultSwLength: 100,
		DefaultPfMaxSpeed: units.KMPH(50), DefaultPfLength: 800, DefaultRouteSpeed: units.KMPH(110),
	})
	return w
}

// stationWorld builds a station alone, its line points the world's edge,
// and checks it finalizes and draws.
func stationWorld(t *testing.T, layout Layout, tracks ...Track) (w *infra.World, west, east []*infra.TrackPoint) {
	t.Helper()
	w = newWorld()
	west, east, err := BuildStation(w, "ST", "Station", layout, tracks...)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range append(append([]*infra.TrackPoint{}, west...), east...) {
		p.WithDeadEnd(true).WithSimLimit(true)
	}
	if err := w.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := schematic.FromWorld(w); err != nil {
		t.Fatalf("does not draw: %v", err)
	}
	return w, west, east
}

// reaches reports whether a train at from can run onto platform no.
func reaches(t *testing.T, w *infra.World, from *infra.TrackPoint, no string) bool {
	t.Helper()
	trk, ok := w.TrackGraph.GetTrack(ids.NewTrackID(
		ids.PointID{StnCode: "ST", Type: "Pf", Seq: no, Other: "S"},
		ids.PointID{StnCode: "ST", Type: "Pf", Seq: no, Other: "E"}))
	if !ok {
		t.Fatalf("no platform %s", no)
	}
	paths, err := w.TrackGraph.GenerateCandidatePaths(from, trk)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if w.IsLegalPath(p) {
			return true
		}
	}
	return false
}

func pfs(nos ...string) []Track {
	var out []Track
	for _, no := range nos {
		out = append(out, Pf(no, 600))
	}
	return out
}

// A station on any number of lines has a line point for each main, each
// side; with no crossover a line reaches only the tracks hanging off its
// main.
func TestStationMains(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 6} {
		t.Run(fmt.Sprint(n, " mains"), func(t *testing.T) {
			// a loop above, the mains, a loop below
			tracks := pfs("0")
			for k := 1; k <= n; k++ {
				tracks = append(tracks, Track{PfNo: fmt.Sprint(k), Straight: true})
			}
			tracks = append(tracks, Pf(fmt.Sprint(n+1), 600))
			w, west, east := stationWorld(t, Layout{}, tracks...)
			if len(west) != n || len(east) != n {
				t.Fatalf("%d west and %d east line points, want %d", len(west), len(east), n)
			}
			for k := range n {
				for j := range n {
					if got, want := reaches(t, w, west[k], fmt.Sprint(j+1)), j == k; got != want {
						t.Errorf("line %d reaches main %d: %v, want %v", k+1, j+1, got, want)
					}
				}
			}
			if !reaches(t, w, west[0], "0") || !reaches(t, w, west[n-1], fmt.Sprint(n+1)) {
				t.Error("the outer loops do not hang off the outer mains")
			}
		})
	}
}

// A track between two mains hangs off the one its Join says, or both.
func TestStationJoins(t *testing.T) {
	for join, want := range map[string][2]bool{JoinAbove: {true, false}, JoinBelow: {false, true}, JoinBoth: {true, true}, "": {true, true}} {
		t.Run("join "+join, func(t *testing.T) {
			tracks := []Track{Pf("1", 600), {PfNo: "2", Straight: true}, {PfNo: "3", Length: 600, Join: join}, {PfNo: "4", Straight: true}, Pf("5", 600)}
			w, west, _ := stationWorld(t, Layout{}, tracks...)
			if got := [2]bool{reaches(t, w, west[0], "3"), reaches(t, w, west[1], "3")}; got != want {
				t.Errorf("loop 3 reached from the upper and lower main %v, want %v", got, want)
			}
		})
	}
	// with no Join, tracks between two mains are shared out
	tracks := []Track{{PfNo: "1", Straight: true}, Pf("2", 600), Pf("3", 600), Pf("4", 600), {PfNo: "5", Straight: true}}
	w, west, _ := stationWorld(t, Layout{}, tracks...)
	for no, want := range map[string][2]bool{"2": {true, false}, "3": {true, true}, "4": {false, true}} {
		if got := [2]bool{reaches(t, w, west[0], no), reaches(t, w, west[1], no)}; got != want {
			t.Errorf("loop %s reached from the upper and lower main %v, want %v", no, got, want)
		}
	}
}

func TestStationRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		layout Layout
		tracks []Track
	}{
		"no main":                             {Layout{}, pfs("1", "2")},
		"two both":                            {Layout{}, []Track{{PfNo: "1", Straight: true}, {PfNo: "2", Join: JoinBoth}, {PfNo: "3", Join: JoinBoth}, {PfNo: "4", Straight: true}}},
		"joins cross":                         {Layout{}, []Track{{PfNo: "1", Straight: true}, {PfNo: "2", Join: JoinBelow}, {PfNo: "3", Join: JoinAbove}, {PfNo: "4", Straight: true}}},
		"too many crossovers":                 {Layout{WestCrossovers: []string{CrossDown, CrossDown}}, []Track{{PfNo: "1", Straight: true}, {PfNo: "2", Straight: true}}},
		"main at a buffer":                    {Layout{WestMains: []int{1}}, []Track{{PfNo: "1", Straight: true}, {Siding: "Sd", Buffer: "W"}}},
		"two platform 1s":                     {Layout{}, []Track{{PfNo: "1", Straight: true}, Pf("1", 600)}},
		"two sidings Gd1":                     {Layout{}, []Track{{PfNo: "1", Straight: true}, {Siding: "Gd1"}, {Siding: "Gd1"}}},
		"a siding named as a platform's ends": {Layout{}, []Track{{PfNo: "1", Straight: true}, {Siding: "Pf1"}}},
	} {
		if _, _, err := BuildStation(newWorld(), "ST", "", c.layout, c.tracks...); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A crossover between two mains lets each line reach the other's tracks
// the way it runs: down going east from the upper line, up from the lower,
// a scissors both.
func TestStationCrossovers(t *testing.T) {
	tracks := []Track{Pf("1", 600), {PfNo: "2", Straight: true}, {PfNo: "3", Straight: true}, Pf("4", 600)}
	for kind, want := range map[string][2]bool{CrossNone: {false, false}, CrossDown: {true, false}, CrossUp: {false, true}, CrossScissors: {true, true}} {
		t.Run("crossover "+kind, func(t *testing.T) {
			w, west, _ := stationWorld(t, Layout{WestCrossovers: []string{kind}, EastCrossovers: []string{kind}}, tracks...)
			// trains entering from the west: the upper line to the lower
			// line's loop, and the lower line to the upper's
			if got := [2]bool{reaches(t, w, west[0], "4"), reaches(t, w, west[1], "1")}; got != want {
				t.Errorf("from the west: upper to 4, lower to 1: %v, want %v", got, want)
			}
		})
	}
}

// A junction station: one line west, two lines east, crossed over.
func TestJunctionStation(t *testing.T) {
	tracks := []Track{Pf("1", 600), {PfNo: "2", Length: 600, Straight: true}, Pf("3", 600), {PfNo: "4", Length: 600}}
	w, west, east := stationWorld(t, Layout{WestMains: []int{1}, EastMains: []int{1, 3}, EastCrossovers: []string{CrossScissors}}, tracks...)
	if len(west) != 1 || len(east) != 2 {
		t.Fatalf("%d west and %d east line points, want 1 and 2", len(west), len(east))
	}
	for _, no := range []string{"1", "2", "3", "4"} {
		if !reaches(t, w, west[0], no) {
			t.Errorf("the west line does not reach platform %s", no)
		}
	}
}

// A double line runs east on its upper line and west on its lower, each
// signalled the one way and labelled by the world's Up (west, unset).
func TestDoubleLine(t *testing.T) {
	w := newWorld()
	mains := []Track{{PfNo: "1", Length: 600, Straight: true}, {PfNo: "2", Length: 600, Straight: true}}
	wsW, wsE, err := BuildStation(w, "WS", "West", Layout{Edge: "W"}, mains...)
	if err != nil {
		t.Fatal(err)
	}
	esW, esE, err := BuildStation(w, "ES", "East", Layout{Edge: "E"}, mains...)
	if err != nil {
		t.Fatal(err)
	}
	if len(wsW)+len(esE) != 0 || len(wsE) != 2 || len(esW) != 2 {
		t.Fatalf("line points: %d %d %d %d", len(wsW), len(wsE), len(esW), len(esE))
	}
	DoubleLine(w, "ln", [2]*infra.TrackPoint(wsE), [2]*infra.TrackPoint(esW), 4000, 2)
	if err := w.Finalize(); err != nil {
		t.Fatal(err)
	}

	for _, sig := range w.ListSignals() {
		east := sig.AtPoint.Id.StnCode == "ES" || sig.FacingPoint.Id.StnCode == "WS" ||
			(sig.AtPoint.Id.StnCode == "lnL1" && sig.FacingPoint.Id.StnCode != "ES")
		switch {
		case strings.HasPrefix(sig.Id, "lnL1") && !strings.HasSuffix(sig.Id, "F"),
			strings.HasPrefix(sig.Id, "lnL2") && !strings.HasSuffix(sig.Id, "R"):
			t.Errorf("signal %s faces the wrong way for its line", sig.Id)
		case strings.HasPrefix(sig.Id, "lnL1") && !east:
			t.Errorf("signal %s at %s facing %s is not for trains running east", sig.Id, sig.AtPoint.Id, sig.FacingPoint.Id)
		}
	}
	for name, want := range map[string]infra.TrackDirection{"lnL10": infra.Down, "lnL11": infra.Down, "lnL20": infra.Up, "lnL21": infra.Up} {
		found := false
		for _, e := range w.TrackGraph.Edges {
			if e.Track.Name == name {
				found = true
				if e.Track.Direction != want {
					t.Errorf("%s is %s, want %s", name, e.Track.Direction, want)
				}
			}
		}
		if !found {
			t.Errorf("no track %s", name)
		}
	}

	// legal reports whether a train leaving from's platform can run to
	// platform no of to, the way the signals let it
	legal := func(from ids.StnCode, side string, fromNo string, to ids.StnCode, no string) bool {
		id := ids.PointID{StnCode: from, Type: "Pf", Seq: fromNo, Other: side}
		var start *infra.TrackPoint
		for _, e := range w.TrackGraph.Edges {
			if e.From.Id == id {
				start = e.From
			}
			if e.To.Id == id {
				start = e.To
			}
		}
		trk, ok := w.TrackGraph.GetTrack(ids.NewTrackID(
			ids.PointID{StnCode: to, Type: "Pf", Seq: no, Other: "S"},
			ids.PointID{StnCode: to, Type: "Pf", Seq: no, Other: "E"}))
		if start == nil || !ok {
			t.Fatalf("no platform %s %s or %s %s", from, fromNo, to, no)
		}
		paths, err := w.TrackGraph.GenerateCandidatePaths(start, trk)
		if err != nil {
			return false
		}
		for _, p := range paths {
			if w.IsLegalPath(p) && w.FollowsSignals(p) {
				return true
			}
		}
		return false
	}
	if !legal("WS", "E", "1", "ES", "1") {
		t.Error("the upper line does not run east")
	}
	if !legal("ES", "S", "2", "WS", "2") {
		t.Error("the lower line does not run west")
	}
	if legal("WS", "E", "2", "ES", "2") {
		t.Error("the lower line runs east")
	}
	if legal("ES", "S", "1", "WS", "1") {
		t.Error("the upper line runs west")
	}
}
