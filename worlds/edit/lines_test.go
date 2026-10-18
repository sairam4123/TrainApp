package edit

import (
	"strconv"
	"testing"
	"trainapp/railway/infra"
)

func TestParsePattern(t *testing.T) {
	for in, want := range map[string]string{"E W": "EW", "eewW": "EEWW", "E, B, W": "EBW", "E E E W W W": "EEEWWW"} {
		got, err := ParsePattern(in)
		if err != nil || string(got) != want {
			t.Errorf("ParsePattern(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "E X"} {
		if _, err := ParsePattern(in); err == nil {
			t.Errorf("ParsePattern(%q): no error", in)
		}
	}
	for n, want := range map[int]string{1: "B", 2: "E W", 3: "E B W", 4: "E E W W", 5: "E E B W W", 6: "E E E W W W"} {
		if got := DefaultPattern(n); got != want {
			t.Errorf("DefaultPattern(%d) = %q, want %q", n, got, want)
		}
	}
}

// parallelEnds adds n points to be the west ends of lines and n the east,
// for Lines to join.
func parallelEnds(s *infra.Snapshot, n int) (west, east []string) {
	for range n {
		west, east = append(west, AddPoint(s)), append(east, AddPoint(s))
	}
	return west, east
}

// edges makes the points ends of the world.
func edges(s *infra.Snapshot, pts ...[]string) error {
	for _, ps := range pts {
		for _, p := range ps {
			if err := SetEnd(s, p, EndBoundary); err != nil {
				return err
			}
		}
	}
	return nil
}

func blockPoint(line string, i int) string {
	return pointID(infra.SnapshotPoint{Stn: line, Type: "Bk", Seq: strconv.Itoa(i)}).String()
}

// stepOf is a step over the built world's track from a to b.
func stepOf(t *testing.T, w *infra.World, a, b string) infra.Step {
	t.Helper()
	for _, e := range w.TrackGraph.Edges {
		switch {
		case e.From.Id.String() == a && e.To.Id.String() == b:
			return infra.Step{Edge: e, From: e.From, To: e.To}
		case e.From.Id.String() == b && e.To.Id.String() == a:
			return infra.Step{Edge: e, From: e.To, To: e.From}
		}
	}
	t.Fatalf("no track %s-%s", a, b)
	return infra.Step{}
}

// Lines lays any number of lines, each signalled only for the ways its
// letter lets it run, so the sim runs it only that way; one-way lines are
// labelled by the world's UP.
func TestLines(t *testing.T) {
	for _, pattern := range []string{"B", "E W", "E B W", "E E W W", "E W E W", "E E B W W", "E E E W W W"} {
		t.Run(pattern, func(t *testing.T) {
			ways, _ := ParsePattern(pattern)
			d := NewDoc(Empty(DefaultDefaults()))
			var west, east []string
			do(t, d, func(s *infra.Snapshot) error { west, east = parallelEnds(s, len(ways)); return nil })
			do(t, d, func(s *infra.Snapshot) error { return Lines(s, west, east, "ml", pattern, 12000, 3, 0) })
			do(t, d, func(s *infra.Snapshot) error { return edges(s, west, east) })
			if ps := Problems(d.S); len(ps) > 0 {
				t.Fatalf("problems: %v", ps)
			}
			w, err := d.S.Build()
			if err != nil {
				t.Fatal(err)
			}
			for k, way := range ways {
				line := LineName("ml", k+1, len(ways))
				pts := []string{west[k], blockPoint(line, 1), blockPoint(line, 2), east[k]}
				for i := 0; i+1 < len(pts); i++ {
					eastward, westward := stepOf(t, w, pts[i], pts[i+1]), stepOf(t, w, pts[i+1], pts[i])
					if got, want := w.RunsCorrectWay(eastward), way != WayWest; got != want {
						t.Errorf("line %d block %d: runs east correctly %v, want %v", k+1, i, got, want)
					}
					if got, want := w.RunsCorrectWay(westward), way != WayEast; got != want {
						t.Errorf("line %d block %d: runs west correctly %v, want %v", k+1, i, got, want)
					}
					tr := d.S.Tracks[trackBetween(d.S, pts[i], pts[i+1])]
					if got := Way(d.S, tr.ID); got != way {
						t.Errorf("line %d block %d: Way %q, want %q", k+1, i, got, way)
					}
					// UP runs west by default
					want := map[byte]string{WayEast: "DOWN", WayWest: "UP", WayBoth: ""}[way]
					if tr.Direction != want {
						t.Errorf("line %d block %d: labelled %q, want %q", k+1, i, tr.Direction, want)
					}
				}
			}
		})
	}
}

func TestLinesUpEast(t *testing.T) {
	defaults := DefaultDefaults()
	defaults.Up = "E"
	s := Empty(defaults)
	west, east := parallelEnds(s, 2)
	if err := Lines(s, west, east, "dl", "E W", 5000, 1, 0); err != nil {
		t.Fatal(err)
	}
	for k, want := range []string{"UP", "DOWN"} {
		if got := s.Tracks[trackBetween(s, west[k], east[k])].Direction; got != want {
			t.Errorf("line %d labelled %q, want %q", k+1, got, want)
		}
	}
}

func TestLinesRefuses(t *testing.T) {
	s := Empty(DefaultDefaults())
	west, east := parallelEnds(s, 2)
	for _, c := range []struct {
		west, east []string
		name, pat  string
	}{
		{west, east, "dl", "E W B"},                     // three lines, two ends
		{west, east, "", "E W"},                         // no name
		{[]string{west[0], west[0]}, east, "dl", "E W"}, // one point, two lines
	} {
		if err := Lines(s, c.west, c.east, c.name, c.pat, 1000, 1, 0); err == nil {
			t.Errorf("Lines(%v, %v, %q, %q): no error", c.west, c.east, c.name, c.pat)
		}
	}
	if err := Lines(s, west, east, "dl", "E W", 1000, 1, 0); err != nil {
		t.Fatal(err)
	}
	w2, e2 := parallelEnds(s, 2)
	if err := Lines(s, w2, e2, "dl", "E W", 1000, 1, 0); err == nil {
		t.Error("a name laid already is taken again")
	}
}
