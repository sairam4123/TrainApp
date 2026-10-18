package edit

import (
	"slices"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/visualization/schematic"
	"trainapp/worlds"
	tm "trainapp/worlds/templates"
)

func TestParseTracksJoins(t *testing.T) {
	tracks, err := ParseTracks("1:600 2* 3:600^v 4* 5^ 6v 7* ~Sdv ~Gd:700v ~Up^")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tr := range tracks {
		got = append(got, tr.PfNo+tr.Siding+"/"+tr.Join)
	}
	want := []string{"1/", "2/", "3/^v", "4/", "5/^", "6/v", "7/", "Sdv/", "Gd/v", "Up/^"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if again, _ := ParseTracks(FormatTracks(tracks)); !slices.Equal(again, tracks) {
		t.Errorf("FormatTracks does not read back: %q", FormatTracks(tracks))
	}
}

// openEnds is a station's open ends east (or west), top to bottom.
func openEnds(s *infra.Snapshot, code string, east bool) []string {
	var at string
	for _, p := range s.Points {
		if p.Stn == code && !p.DeadEnd && open(s, p.ID, east) && !open(s, p.ID, !east) {
			at = p.ID
		}
	}
	if at == "" {
		return nil
	}
	return OpenBeside(s, at, east)
}

var doubleStation = []tm.Track{tm.Pf("1", 600), {PfNo: "2", Length: 600, Straight: true}, {PfNo: "3", Length: 600, Straight: true}, tm.Pf("4", 600)}

// A double line drawn station by station: all at once, then a line at a
// time by hand.
func TestStampDoubleLine(t *testing.T) {
	d := NewDoc(Empty(DefaultDefaults()))
	stamp := func(at string, spec StampSpec) {
		t.Helper()
		do(t, d, func(s *infra.Snapshot) error { _, err := Stamp(s, at, spec); return err })
	}
	stamp("", StampSpec{Template: TemplateStation, Code: "AAA", Edge: "W", Tracks: doubleStation, EastCrossovers: []string{tm.CrossScissors}})
	ends := openEnds(d.S, "AAA", true)
	if len(ends) != 2 {
		t.Fatalf("AAA has open ends %v, want 2", ends)
	}
	// all at once: both lines, a double line
	stamp(ends[0], StampSpec{Template: TemplateStation, Code: "BBB", Tracks: doubleStation, Join: JoinAll, LinkM: 10000, LinkBlocks: 2,
		WestCrossovers: []string{tm.CrossDown}, EastCrossovers: []string{tm.CrossUp}})
	// by hand: the station joined at the selected point only, the other
	// line drawn after
	ends = openEnds(d.S, "BBB", true)
	stamp(ends[1], StampSpec{Template: TemplateStation, Code: "CCC", Edge: "E", Tracks: doubleStation, LinkM: 8000, LinkPattern: "W", WestCrossovers: []string{tm.CrossScissors}})
	if got := openEnds(d.S, "BBB", true); len(got) != 1 || got[0] != ends[0] {
		t.Fatalf("BBB has open ends %v east, want %s", got, ends[0])
	}
	cw := openEnds(d.S, "CCC", false)
	if len(cw) != 1 {
		t.Fatalf("CCC has open ends %v west, want one", cw)
	}
	do(t, d, func(s *infra.Snapshot) error { return Lines(s, []string{ends[0]}, cw, "upBC", "E", 8000, 2, 0) })

	if ps := Problems(d.S); len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}
	if _, err := d.S.Build(); err != nil {
		t.Fatal(err)
	}
	// the stamped double line runs as tpjkkdi's, upper east, lower west;
	// the line laid with CCC runs west, as asked, from BBB's lower end
	diagram, err := schematic.FromSnapshot(d.S)
	if err != nil {
		t.Fatal(err)
	}
	lane := map[string]int{}
	for line, want := range map[string]byte{"mlAaaBbbL10": WayEast, "mlAaaBbbL20": WayWest, "slBbbCcc0": WayWest, "upBC0": WayEast} {
		i := slices.IndexFunc(d.S.Tracks, func(t infra.SnapshotTrack) bool { return t.Name == line })
		if i < 0 {
			t.Errorf("no track %s", line)
			continue
		}
		tr := d.S.Tracks[i]
		if got := Way(d.S, tr.ID); got != want {
			t.Errorf("%s runs %q, want %q", line, got, want)
		}
		lane[line], _ = diagram.Lane(tr.From)
	}
	if lane["mlAaaBbbL10"] >= lane["mlAaaBbbL20"] || lane["upBC0"] >= lane["slBbbCcc0"] {
		t.Errorf("the eastbound lines are not the upper ones: lanes %v", lane)
	}
}

func TestStampRefuses(t *testing.T) {
	s := Empty(DefaultDefaults())
	if _, err := Stamp(s, "", StampSpec{Template: TemplateStation, Code: "AAA", Edge: "W", Tracks: doubleStation}); err != nil {
		t.Fatal(err)
	}
	ends := openEnds(s, "AAA", true)
	// three lines, two open ends
	triple := []tm.Track{{PfNo: "1", Straight: true}, {PfNo: "2", Straight: true}, {PfNo: "3", Straight: true}}
	if _, err := Stamp(s, ends[0], StampSpec{Template: TemplateStation, Code: "BBB", Tracks: triple, Join: JoinAll, LinkM: 1000}); err == nil {
		t.Error("three lines join two open ends")
	}
	if _, err := Stamp(s, ends[0], StampSpec{Template: TemplateStation, Code: "BBB", Tracks: triple, WestMains: []string{"9"}}); err == nil {
		t.Error("a main that is not a track")
	}
	// none leaves the station on its own
	if _, err := Stamp(s, ends[0], StampSpec{Template: TemplateStation, Code: "BBB", Tracks: triple, Join: JoinNone, LinkM: 1000}); err != nil {
		t.Fatal(err)
	}
	if got := openEnds(s, "AAA", true); len(got) != 2 {
		t.Errorf("join none joined AAA: open ends %v", got)
	}
}

// A scissors crossover between two lines lets a train over from either to
// the other, through a diamond.
func TestScissorsCrossover(t *testing.T) {
	d := NewDoc(Empty(DefaultDefaults()))
	var west, east []string
	do(t, d, func(s *infra.Snapshot) error { west, east = parallelEnds(s, 2); return nil })
	do(t, d, func(s *infra.Snapshot) error { return Lines(s, west, east, "dl", "B B", 3000, 1, 0) })
	do(t, d, func(s *infra.Snapshot) error { return edges(s, west, east) })
	upper, lower := d.S.Tracks[0].ID, d.S.Tracks[1].ID
	do(t, d, func(s *infra.Snapshot) error { return Crossover(s, upper, lower, tm.CrossScissors) })
	if n := len(d.S.Switches); n != 5 {
		t.Errorf("%d switch blocks, want 4 switches and a diamond", n)
	}
	if ps := Problems(d.S); len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}
	w, err := d.S.Build()
	if err != nil {
		t.Fatal(err)
	}
	// from each line's west end to the other's last track
	for k, from := range west {
		other := east[1-k]
		var last *infra.TrackSegment
		for _, e := range w.TrackGraph.Edges {
			if e.To.Id.String() == other {
				last = e.Track
			}
		}
		start, _ := w.TrackGraph.GetPoint(pointIDOf(d.S, from))
		paths, err := w.TrackGraph.GenerateCandidatePaths(start, last)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(paths, w.IsLegalPath) {
			t.Errorf("no way over from line %d", k+1)
		}
	}
}

// A station's west scissors still draws as an X once one of its lines is
// extended a long way, though that pushes the line's end east alone.
func TestScissorsDrawnCrossed(t *testing.T) {
	d := NewDoc(Empty(DefaultDefaults()))
	do(t, d, func(s *infra.Snapshot) error {
		_, err := Stamp(s, "", StampSpec{Template: TemplateStation, Code: "AAA", Edge: "E", Tracks: doubleStation, WestCrossovers: []string{tm.CrossScissors}})
		return err
	})
	do(t, d, func(s *infra.Snapshot) error {
		_, err := Extend(s, openEnds(s, "AAA", false)[0], false, 11000, 0, 0)
		return err
	})
	dg, err := schematic.FromSnapshot(d.S)
	if err != nil {
		t.Fatal(err)
	}
	for zoom := 1; zoom <= 3; zoom++ {
		rows := dg.Render(schematic.Options{Level: schematic.Detail, Zoom: zoom})
		if !slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, "╳") }) {
			t.Errorf("zoom %d: the scissors is not drawn crossed:\n%s", zoom, strings.Join(rows, "\n"))
		}
	}
}

func pointIDOf(s *infra.Snapshot, id string) ids.PointID { return pointID(s.Points[pointIndex(s, id)]) }

func hasProblem(s *infra.Snapshot, text string) bool {
	return slices.ContainsFunc(Problems(s), func(p Problem) bool { return strings.Contains(p.Text, text) })
}

// A line's signals that do not add up are problems: one whose way changes
// along it, and a label they do not bear out.
func TestLineProblems(t *testing.T) {
	// two blocks, each a line of its own, east then west
	s := Empty(DefaultDefaults())
	a, b, c := AddPoint(s), AddPoint(s), AddPoint(s)
	if err := Line(s, a, b, "one", WayEast, 1000, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := Line(s, b, c, "two", WayWest, 1000, 1, 0); err != nil {
		t.Fatal(err)
	}
	if !hasProblem(s, "runs east west of it and west east of it") {
		t.Errorf("a line that flips is not a problem: %v", Problems(s))
	}

	// a line both ways in three blocks, a signal in the middle taken away
	s = Empty(DefaultDefaults())
	ws, es := parallelEnds(s, 1)
	if err := Lines(s, ws, es, "ln", "B", 3000, 3, 0); err != nil {
		t.Fatal(err)
	}
	if hasProblem(s, "the line runs") {
		t.Fatalf("a line both ways is a problem: %v", Problems(s))
	}
	s.Signals = slices.DeleteFunc(s.Signals, func(sig infra.SnapshotSignal) bool { return sig.ID == "lnSig1F" })
	if !hasProblem(s, "runs both ways west of it and west east of it") {
		t.Errorf("a signal missing mid-line is not a problem: %v", Problems(s))
	}

	// a line labelled UP (west) but signalled east
	s = Empty(DefaultDefaults())
	ws, es = parallelEnds(s, 1)
	if err := Lines(s, ws, es, "ln", "E", 1000, 1, 0); err != nil {
		t.Fatal(err)
	}
	s.Tracks[0].Direction = "UP"
	if !hasProblem(s, "labelled UP, which runs west, but signalled east") {
		t.Errorf("a label against the signals is not a problem: %v", Problems(s))
	}
}

// The built-in worlds have none of these problems.
func TestBuiltInWorldsHaveNoLineProblems(t *testing.T) {
	for name, build := range map[string]func() *infra.World{
		"tpjkkdi": worlds.BuildTpjKkdiWorld, "tpjkkdi-sl": worlds.BuildTpjKkdiSingleLineWorld,
		"junction": func() *infra.World { return worlds.BuildJunctionWorld(2) },
	} {
		s, err := build().Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if ps := lineProblems(s); len(ps) > 0 {
			t.Errorf("%s: %v", name, ps)
		}
	}
}

// reachesEnd reports whether a train at the point from can run to the
// track ending at the point to.
func reachesEnd(t *testing.T, s *infra.Snapshot, w *infra.World, from, to string) bool {
	t.Helper()
	var last *infra.TrackSegment
	for _, e := range w.TrackGraph.Edges {
		if e.To.Id.String() == to {
			last = e.Track
		}
	}
	start, _ := w.TrackGraph.GetPoint(pointIDOf(s, from))
	paths, err := w.TrackGraph.GenerateCandidatePaths(start, last)
	if err != nil {
		t.Fatal(err)
	}
	return slices.ContainsFunc(paths, w.IsLegalPath)
}

// Two lines crossing at a diamond swap lanes; a slip keeps a straight road
// too, a double slip both.
func TestCrossings(t *testing.T) {
	for kind, want := range map[string][4]bool{ // upper to upper, upper to lower, lower to upper, lower to lower
		CrossingDiamond:    {false, true, true, false},
		CrossingSlipUpper:  {true, true, true, false},
		CrossingSlipLower:  {false, true, true, true},
		CrossingDoubleSlip: {true, true, true, true},
	} {
		t.Run(kind, func(t *testing.T) {
			d := NewDoc(Empty(DefaultDefaults()))
			var west, east []string
			do(t, d, func(s *infra.Snapshot) error { west, east = parallelEnds(s, 2); return nil })
			do(t, d, func(s *infra.Snapshot) error { return Lines(s, west, east, "dl", "B B", 3000, 1, 0) })
			do(t, d, func(s *infra.Snapshot) error { return edges(s, west, east) })
			upper, lower := d.S.Tracks[0].ID, d.S.Tracks[1].ID
			do(t, d, func(s *infra.Snapshot) error { return Crossing(s, upper, lower, kind) })
			if ps := Problems(d.S); len(ps) > 0 {
				t.Fatalf("problems: %v", ps)
			}
			w, err := d.S.Build()
			if err != nil {
				t.Fatal(err)
			}
			got := [4]bool{
				reachesEnd(t, d.S, w, west[0], east[0]), reachesEnd(t, d.S, w, west[0], east[1]),
				reachesEnd(t, d.S, w, west[1], east[0]), reachesEnd(t, d.S, w, west[1], east[1]),
			}
			if got != want {
				t.Errorf("reaches %v, want %v", got, want)
			}
			if len(w.ListSwitchBlocks()) != 1 {
				t.Errorf("%d switch blocks, want 1", len(w.ListSwitchBlocks()))
			}
		})
	}
}

// A signalled track split in two runs as it did, each half signalled at the
// new point for the ways it runs.
func TestSplitKeepsTheWay(t *testing.T) {
	for _, way := range []byte{WayBoth, WayEast, WayWest} {
		s := Empty(DefaultDefaults())
		ws, es := parallelEnds(s, 1)
		if err := Lines(s, ws, es, "ln", string(way), 3000, 1, 0); err != nil {
			t.Fatal(err)
		}
		np, err := Split(s, s.Tracks[0].ID, 1000)
		if err != nil {
			t.Fatal(err)
		}
		for _, half := range []int{trackBetween(s, ws[0], np), trackBetween(s, np, es[0])} {
			if got := Way(s, s.Tracks[half].ID); got != way {
				t.Errorf("a line running %c split: a half runs %c", way, got)
			}
		}
		if hasProblem(s, "the line runs") {
			t.Errorf("a line running %c split: %v", way, Problems(s))
		}
	}
}

// A station added with its code alone, no platforms yet, is filled in by
// stamping its code; one with platforms is not stamped over.
func TestStampFillsAnEmptyStation(t *testing.T) {
	s := Empty(DefaultDefaults())
	if err := SetStation(s, "AAA", "Aaa Town"); err != nil {
		t.Fatal(err)
	}
	if _, err := Stamp(s, "", StampSpec{Template: TemplateCrossing, Code: "AAA"}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Stations); n != 1 || s.Stations[0].Name != "Aaa Town" || len(s.Stations[0].Platforms) != 3 {
		t.Errorf("stations %+v, want AAA Aaa Town with its three tracks", s.Stations)
	}
	if _, err := Stamp(s, "", StampSpec{Template: TemplateHalt, Code: "AAA"}); err == nil {
		t.Error("a station with platforms is stamped over")
	}
}
