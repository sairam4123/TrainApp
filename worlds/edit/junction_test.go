package edit

import (
	"testing"
	"trainapp/railway/infra"
)

// followsTo reports whether a train at from can run to the track ending at
// to (the end of a line), and whether it can the right way.
func followsTo(t *testing.T, s *infra.Snapshot, w *infra.World, from, to string) (reach, right bool) {
	t.Helper()
	var last *infra.TrackSegment
	for _, e := range w.TrackGraph.Edges {
		if e.To.Id.String() == to || e.From.Id.String() == to {
			last = e.Track
		}
	}
	start, _ := w.TrackGraph.GetPoint(pointIDOf(s, from))
	paths, err := w.TrackGraph.GenerateCandidatePaths(start, last)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if w.IsLegalPath(p) {
			reach = true
			right = right || w.FollowsSignals(p)
		}
	}
	return reach, right
}

// junctionWorld lays a line of the pattern, leaves it for a branch, and
// makes every open end the world's edge.
func junctionWorld(t *testing.T, pattern string, spec JunctionSpec) (d *Doc, west, east, branch []string) {
	t.Helper()
	ways, _ := ParsePattern(pattern)
	d = NewDoc(Empty(DefaultDefaults()))
	do(t, d, func(s *infra.Snapshot) error { west, east = parallelEnds(s, len(ways)); return nil })
	do(t, d, func(s *infra.Snapshot) error { return Lines(s, west, east, "ml", pattern, 10000, 1, 0) })
	for _, tr := range d.S.Tracks {
		spec.Mains = append(spec.Mains, tr.ID)
	}
	do(t, d, func(s *infra.Snapshot) (err error) { branch, err = Junction(s, spec); return })
	do(t, d, func(s *infra.Snapshot) error { return edges(s, west, east, branch) })
	if ps := Problems(d.S); len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}
	return d, west, east, branch
}

// A single line's junction is a switch, as BuildJunctionWorld's: trains run
// on along the line or off onto the branch, and back.
func TestJunctionSingleLine(t *testing.T) {
	d, west, east, branch := junctionWorld(t, "B", JunctionSpec{Below: true, East: true, Lanes: 1, Code: "JCT", Pattern: "B", LengthM: 7000})
	w, err := d.S.Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{{west[0], east[0]}, {west[0], branch[0]}, {branch[0], west[0]}, {east[0], west[0]}} {
		if reach, right := followsTo(t, d.S, w, c[0], c[1]); !reach || !right {
			t.Errorf("%s to %s: reached %v, the right way %v", c[0], c[1], reach, right)
		}
	}
	if reach, _ := followsTo(t, d.S, w, east[0], branch[0]); reach {
		t.Error("a train runs from the line to the branch without reversing")
	}
}

// A double line's junction is flat: the branch's line one way leaves the
// main running that way and crosses the other main at a diamond.
func TestJunctionDoubleLine(t *testing.T) {
	for _, below := range []bool{true, false} {
		d, west, east, branch := junctionWorld(t, "E W", JunctionSpec{Below: below, East: true, Lanes: 1, Code: "JCT", Pattern: "E W", LengthM: 7000})
		var sw, dp int
		for _, b := range d.S.Switches {
			switch b.ID[:len("jctJnSw")] {
			case "jctJnSw":
				sw++
			case "jctJnDp":
				dp++
			}
		}
		if sw != 2 || dp != 1 {
			t.Errorf("below %v: %d switches and %d diamonds, want 2 and 1", below, sw, dp)
		}
		w, err := d.S.Build()
		if err != nil {
			t.Fatal(err)
		}
		// the right way: east on the upper lines, west on the lower
		for _, c := range [][2]string{{west[0], branch[0]}, {west[0], east[0]}, {branch[1], west[1]}, {east[1], west[1]}} {
			if reach, right := followsTo(t, d.S, w, c[0], c[1]); !reach || !right {
				t.Errorf("below %v: %s to %s: reached %v, the right way %v", below, c[0], c[1], reach, right)
			}
		}
	}
}

// Any number of tracks: a double-line branch off four tracks pairs by way.
func TestJunctionFourTracks(t *testing.T) {
	d, west, _, branch := junctionWorld(t, "E E W W", JunctionSpec{Below: true, East: true, Lanes: 2, Code: "JCT", Pattern: "E W", LengthM: 5000})
	w, err := d.S.Build()
	if err != nil {
		t.Fatal(err)
	}
	if reach, right := followsTo(t, d.S, w, west[0], branch[0]); !reach || !right {
		t.Errorf("the top main to the branch's east line: reached %v, the right way %v", reach, right)
	}
	if reach, right := followsTo(t, d.S, w, branch[1], west[2]); !reach || !right {
		t.Errorf("the branch's west line to the third main: reached %v, the right way %v", reach, right)
	}
}

func TestJunctionRefuses(t *testing.T) {
	s := Empty(DefaultDefaults())
	ws, es := parallelEnds(s, 2)
	if err := Lines(s, ws, es, "ml", "E W", 10000, 1, 0); err != nil {
		t.Fatal(err)
	}
	mains := []string{s.Tracks[0].ID, s.Tracks[1].ID}
	for name, spec := range map[string]JunctionSpec{
		"crossing legs": {Mains: mains, Below: true, East: true, Lanes: 1, Pattern: "E W", Pairs: []int{1, 0}, LengthM: 1000},
		"no lanes":      {Mains: mains, Below: true, East: true, Pattern: "B", LengthM: 1000},
		"a bad main":    {Mains: []string{"nope"}, Below: true, East: true, Lanes: 1, Pattern: "B", LengthM: 1000},
		"no way to go":  {Mains: mains, Below: true, East: true, Lanes: 1, Pattern: "W E", LengthM: 1000},
	} {
		if _, err := Junction(Clone(s), spec); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
