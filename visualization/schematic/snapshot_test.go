package schematic

import (
	"slices"
	"strings"
	"testing"
	"trainapp/railway/infra"
	"trainapp/worlds"
)

// A world drawn from its snapshot looks as it does drawn from the built
// world, but for main lines, which only a built world knows.
func TestSnapshotDrawsAsWorld(t *testing.T) {
	all := map[string]func() *infra.World{"TpjKkdiSingleLine": worlds.BuildTpjKkdiSingleLineWorld}
	for name, build := range testWorlds {
		all[name] = build
	}
	for name, build := range all {
		t.Run(name, func(t *testing.T) {
			w := build()
			s, err := w.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			fromWorld, err := FromWorld(w)
			if err != nil {
				t.Fatal(err)
			}
			fromSnap, err := FromSnapshot(s)
			if err != nil {
				t.Fatal(err)
			}
			for _, level := range []Level{Layout, Overview} {
				want := fromWorld.Render(Options{Level: level})
				got := fromSnap.Render(Options{Level: level})
				if !slices.Equal(got, want) {
					t.Errorf("%s: drawn from the snapshot:\n%s\nfrom the world:\n%s", level, strings.Join(got, "\n"), strings.Join(want, "\n"))
				}
			}
		})
	}
}

// A point with no track is laid out and drawn on a lane of its own, so a
// world being built from nothing shows.
func TestLonePoint(t *testing.T) {
	s := &infra.Snapshot{Version: infra.SnapshotVersion, Points: []infra.SnapshotPoint{{Type: "Ed", Seq: "1"}}}
	d, err := FromSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	f := d.Draw(Options{Marks: map[string]Mark{"Ed1": MarkSelected}})
	p, ok := f.At(0, headerRows)
	if !ok || p != (Piece{PiecePoint, "Ed1"}) {
		t.Errorf("at the lone point: %+v, %v", p, ok)
	}
	if !strings.Contains(strings.Join(f.Lines(false), "\n"), "•") {
		t.Errorf("a marked lone point is not drawn:\n%s", strings.Join(f.Lines(false), "\n"))
	}
}

// Every piece drawn can be picked where it is drawn, and At finds a point
// before the tracks that end at it.
func TestPick(t *testing.T) {
	d, err := FromWorld(worlds.BuildTpjKkdiSingleLineWorld())
	if err != nil {
		t.Fatal(err)
	}
	f := d.Draw(Options{Level: Layout, Zoom: 1})
	kinds := map[PieceKind]int{}
	for _, p := range f.Pieces() {
		x, y, ok := f.Where(p)
		if !ok {
			t.Errorf("%+v: not drawn", p)
			continue
		}
		if !slices.Contains(f.PiecesAt(x, y), p) {
			t.Errorf("%+v is drawn at %d,%d but not picked there (%v)", p, x, y, f.PiecesAt(x, y))
		}
		kinds[p.Kind]++
	}
	for _, k := range []PieceKind{PiecePoint, PieceTrack, PieceSignal, PieceStation} {
		if kinds[k] == 0 {
			t.Errorf("no pieces of kind %d", k)
		}
	}
	x, y, _ := f.Where(Piece{PiecePoint, "krmgPf1S"})
	if p, _ := f.At(x, y); p != (Piece{PiecePoint, "krmgPf1S"}) {
		t.Errorf("at krmgPf1S: %+v", p)
	}
}

// A route is drawn in its own colour, under a selection or a problem where
// they share a cell.
func TestMarkRoute(t *testing.T) {
	d, err := FromWorld(worlds.BuildTpjKkdiSingleLineWorld())
	if err != nil {
		t.Fatal(err)
	}
	tr := d.Tracks()[0]
	f := d.Draw(Options{Level: Layout, Zoom: 1, Marks: map[string]Mark{tr.ID: MarkRoute}})
	if !strings.Contains(strings.Join(f.Lines(true), "\n"), "\x1b[1;36m") {
		t.Error("a route is not drawn in its colour")
	}
	for _, c := range []struct {
		m, o Mark
		want bool
	}{{MarkSelected, MarkRoute, true}, {MarkRoute, MarkSelected, false}, {MarkProblem, MarkSelected, true}, {MarkRoute, MarkNone, true}} {
		if got := c.m.over(c.o); got != c.want {
			t.Errorf("%d over %d: %v", c.m, c.o, got)
		}
	}
}
