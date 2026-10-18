package edit

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/infra"
	"trainapp/units"
	"trainapp/visualization/schematic"
	tm "trainapp/worlds/templates"
)

// openEnd is the station's point with no track east (or west) that is not an
// end of the world: its throat.
func openEnd(t *testing.T, s *infra.Snapshot, code string, east bool) string {
	t.Helper()
	for _, p := range s.Points {
		if p.Stn == code && !p.DeadEnd && open(s, p.ID, east) {
			return p.ID
		}
	}
	t.Fatalf("%s has no open end %s", code, side(east))
	return ""
}

func do(t *testing.T, d *Doc, edit func(*infra.Snapshot) error) {
	t.Helper()
	if err := d.Do(edit); err != nil {
		t.Fatal(err)
	}
}

// The real single line, stamped station by station and joined by block
// sections, is the world BuildTpjKkdiSingleLineWorld builds.
func TestStampBuildsTheSingleLine(t *testing.T) {
	d := NewDoc(Empty(infra.SnapshotDefaults{SwitchKmph: 30, SwitchLengthM: 100, PlatformKmph: 50, PlatformLengthM: 800, RouteKmph: 110}))
	tpj, err := ParseTracks("1:600 9 2:600 3:600 4:600 5:600* 6:600 7:600 ~St1 ~Sd1< ~Sd2< 8:600")
	if err != nil {
		t.Fatal(err)
	}
	stamp := func(at string, spec StampSpec) {
		t.Helper()
		do(t, d, func(s *infra.Snapshot) error { _, err := Stamp(s, at, spec); return err })
	}
	stamp("", StampSpec{Template: TemplateStation, Code: "TPJ", Name: "Tiruchchirappalli Jn", Edge: "W", TurnoutKmph: 15, Tracks: tpj})
	legs := []struct {
		spec StampSpec
		km   float64
	}{
		{StampSpec{Template: TemplateCrossing, Code: "KRMG", Name: "Kumaramangalam", PfLengthM: 450, LineKmph: 110}, 12.94},
		{StampSpec{Template: TemplateCrossing, Code: "KRUR", Name: "Keeranur", PfLengthM: 450, LineKmph: 110}, 28.37},
		{StampSpec{Template: TemplateCrossing, Code: "VEL", Name: "Vellanur", PfLengthM: 450, LineKmph: 110}, 41.41},
		{StampSpec{Template: TemplateStation, Code: "PDKT", Name: "Pudukkottai", Tracks: []tm.Track{
			{PfNo: "1", Length: 600, Straight: true, Kmph: 110}, tm.Pf("2", 600), tm.Pf("3", 600), {Siding: "Gd1", SidingLength: 700},
		}}, 52.56},
		{StampSpec{Template: TemplateHalt, Code: "NMN", Name: "Namanasamudram", PfLengthM: 400, LineKmph: 110}, 58.57},
		{StampSpec{Template: TemplateCrossing, Code: "TYM", Name: "Tirumayam", PfLengthM: 450, LineKmph: 110}, 68.55},
		{StampSpec{Template: TemplateStation, Code: "CTND", Name: "Chettinad", Tracks: []tm.Track{
			tm.Pf("1", 450), {PfNo: "2", Length: 450, Straight: true, Kmph: 110}, tm.Pf("3", 450), {Siding: "Gd1", SidingLength: 700},
		}}, 77.66},
		{StampSpec{Template: TemplateHalt, Code: "KTYR", Name: "Kottaiyur", PfLengthM: 400, LineKmph: 110}, 83.06},
		{StampSpec{Template: TemplateStation, Code: "KKDI", Name: "Karaikkudi Jn", Edge: "E", TurnoutKmph: 15, Tracks: []tm.Track{
			tm.Pf("1", 600), {PfNo: "6"}, tm.Pf("2", 600), {PfNo: "3", Length: 600, Straight: true}, tm.Pf("4", 600), tm.Pf("5", 600),
		}}, 89.41},
	}
	prev, prevKm := "TPJ", 0.0
	for _, leg := range legs {
		spec := leg.spec
		spec.LinkName = "sl" + title(prev) + title(spec.Code)
		spec.LinkM = float64(units.KM(leg.km-prevKm) - units.M(1000))
		spec.LinkBlocks = 1
		stamp(openEnd(t, d.S, prev, true), spec)
		prev, prevKm = spec.Code, leg.km
	}

	if ps := Problems(d.S); len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}
	w, err := d.S.Build()
	if err != nil {
		t.Fatal(err)
	}
	s, err := w.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.MarshalIndent(s, "", "  ")
	want, err := os.ReadFile("../testdata/tpjkkdi-sl.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		os.WriteFile(t.TempDir()+"/got.json", got, 0o644)
		t.Error("the stamped line is not BuildTpjKkdiSingleLineWorld's")
	}
}

// A line drawn track by track: extend, split, join, a switch, a signal, a
// platform, and the ends of the world. It builds, and undo takes it back an
// edit at a time.
func TestDrawALine(t *testing.T) {
	d := NewDoc(Empty(DefaultDefaults()))
	var a, b, c string
	do(t, d, func(s *infra.Snapshot) error { a = AddPoint(s); return nil })
	do(t, d, func(s *infra.Snapshot) (err error) { b, err = Extend(s, a, true, 5000, 0, 0); return })
	do(t, d, func(s *infra.Snapshot) (err error) { c, err = Extend(s, b, true, 800, 50, 0); return })
	if len(Problems(d.S)) == 0 {
		t.Error("open ends are not problems")
	}
	// split the line, then join it again
	var mid string
	do(t, d, func(s *infra.Snapshot) (err error) { mid, err = Split(s, d.S.Tracks[0].ID, 2000); return })
	if n := len(d.S.Tracks); n != 3 {
		t.Fatalf("%d tracks after the split, want 3", n)
	}
	do(t, d, func(s *infra.Snapshot) error { return Join(s, mid) })
	if n, l := len(d.S.Tracks), d.S.Tracks[len(d.S.Tracks)-1].LengthM; n != 2 || l != 5000 {
		t.Fatalf("after the join: %d tracks, the joined one %g m", n, l)
	}

	do(t, d, func(s *infra.Snapshot) error { return SetStation(s, "ABC", "Abc") })
	pf := d.S.Tracks[trackIndex(d.S, d.S.Tracks[trackBetween(d.S, b, c)].ID)].ID
	do(t, d, func(s *infra.Snapshot) error { return SetPlatform(s, pf, "ABC", "1", 600) })
	// the platform's ends are now the station's
	for _, p := range d.S.Points {
		if p.ID != a && p.Stn != "ABC" {
			t.Errorf("point %s is not ABC's", p.ID)
		}
	}
	pfTrack := d.S.Stations[0].Platforms[0].Track
	if trackIndex(d.S, pfTrack) < 0 {
		t.Fatalf("the platform's track %s is not in the world", pfTrack)
	}
	line := d.S.Tracks[slices.IndexFunc(d.S.Tracks, func(t infra.SnapshotTrack) bool { return t.ID != pfTrack })].ID
	do(t, d, func(s *infra.Snapshot) error { return ToggleSignal(s, line, true) })
	do(t, d, func(s *infra.Snapshot) error { return ToggleSignal(s, line, false) })
	if n := len(d.S.Signals); n != 2 {
		t.Errorf("%d signals, want 2", n)
	}
	west := d.S.Tracks[trackIndex(d.S, line)].From
	east := d.S.Tracks[trackIndex(d.S, pfTrack)].To
	do(t, d, func(s *infra.Snapshot) error { return SetEnd(s, west, EndBoundary) })
	do(t, d, func(s *infra.Snapshot) error { return SetEnd(s, east, EndBoundary) })
	if ps := Problems(d.S); len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}
	if _, err := d.S.Build(); err != nil {
		t.Fatal(err)
	}

	n := 0
	for d.Undo() {
		n++
	}
	if len(d.S.Points) != 0 || n == 0 {
		t.Errorf("after %d undos: %d points", n, len(d.S.Points))
	}
	if !d.Redo() || len(d.S.Points) != 1 {
		t.Error("redo does not bring the first point back")
	}
}

// A switch off a long line gets a short straight leg, and a crossover joins
// two lines with a switch at each end.
func TestSwitchAndCrossover(t *testing.T) {
	d := NewDoc(Empty(DefaultDefaults()))
	var a, b, c, e string
	do(t, d, func(s *infra.Snapshot) error { a = AddPoint(s); return nil })
	do(t, d, func(s *infra.Snapshot) (err error) { b, err = Extend(s, a, true, 5000, 0, 0); return })
	var div string
	do(t, d, func(s *infra.Snapshot) (err error) { div, err = AddSwitch(s, a, true, 1); return })
	do(t, d, func(s *infra.Snapshot) (err error) { c, err = Extend(s, div, true, 4900, 0, 0); return })
	if n := len(d.S.Switches); n != 1 {
		t.Fatalf("%d switches, want 1", n)
	}
	for _, leg := range d.S.Switches[0].Tracks {
		if l := d.S.Tracks[trackIndex(d.S, leg)].LengthM; l != 100 {
			t.Errorf("switch leg %s is %g m, want the switch length", leg, l)
		}
	}
	// join the two lines' ends, then lay a crossover between them
	do(t, d, func(s *infra.Snapshot) (err error) { e, err = Extend(s, b, true, 1000, 0, 0); return })
	do(t, d, func(s *infra.Snapshot) error {
		_, err := AddSwitch(s, e, false, 1)
		return err
	})
	tail := d.S.Points[len(d.S.Points)-1].ID // the new switch's diverging end, west of e
	do(t, d, func(s *infra.Snapshot) error { return mergeInto(s, tail, c) })
	dg, err := schematic.FromSnapshot(d.S)
	if err != nil {
		t.Fatal(err)
	}
	var upper, lower string
	for _, tr := range d.S.Tracks {
		if inSwitch(d.S, tr.ID) || tr.Lanes != 0 {
			continue
		}
		if l, _ := dg.Lane(tr.From); l == 0 && upper == "" {
			upper = tr.ID
		} else if l > 0 && lower == "" {
			lower = tr.ID
		}
	}
	do(t, d, func(s *infra.Snapshot) error { return Crossover(s, upper, lower, tm.CrossDown) })
	if n := len(d.S.Switches); n != 4 {
		t.Errorf("%d switches after the crossover, want 4", n)
	}
	for _, end := range []string{a, e} {
		do(t, d, func(s *infra.Snapshot) error { return SetEnd(s, end, EndBoundary) })
	}
	if ps := Problems(d.S); len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}
}

// mergeInto is mergePoint as an edit, for the tests.
func mergeInto(s *infra.Snapshot, from, into string) error {
	mergePoint(s, from, into)
	return nil
}

// An edit that would leave a world that cannot be drawn is refused, and the
// world stays as it was.
func TestRefused(t *testing.T) {
	d := NewDoc(Empty(DefaultDefaults()))
	var a, b string
	do(t, d, func(s *infra.Snapshot) error { a = AddPoint(s); return nil })
	do(t, d, func(s *infra.Snapshot) (err error) { b, err = Extend(s, a, true, 1000, 0, 0); return })
	before := Clone(d.S)
	for name, edit := range map[string]func(*infra.Snapshot) error{
		"extend again": func(s *infra.Snapshot) error { _, err := Extend(s, a, true, 1000, 0, 0); return err },
		"track back": func(s *infra.Snapshot) error {
			addTrack(s, infra.SnapshotTrack{From: b, To: a, LengthM: 1, MaxKmph: 1})
			return nil
		},
		"loop round": func(s *infra.Snapshot) error {
			c, _ := Extend(s, b, true, 1000, 0, 1)
			addTrack(s, infra.SnapshotTrack{From: c, To: a, LengthM: 1, MaxKmph: 1})
			return nil
		},
		"split a nothing":  func(s *infra.Snapshot) error { _, err := Split(s, "nope", 1); return err },
		"bad speed":        func(s *infra.Snapshot) error { return SetTrack(s, s.Tracks[0].ID, TrackAttrs{LengthM: 1}) },
		"switch, no lanes": func(s *infra.Snapshot) error { _, err := AddSwitch(s, a, true, 0); return err },
	} {
		if err := d.Do(edit); err == nil {
			t.Errorf("%s: not refused", name)
		}
		if got, _ := json.Marshal(d.S); !bytes.Equal(got, mustJSON(before)) {
			t.Errorf("%s: changed the world", name)
		}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// Deleting a track takes its switch and signals with it, and the end it
// leaves with no track.
func TestDelete(t *testing.T) {
	d := NewDoc(Empty(DefaultDefaults()))
	var a, div string
	do(t, d, func(s *infra.Snapshot) error { a = AddPoint(s); return nil })
	do(t, d, func(s *infra.Snapshot) error { _, err := Extend(s, a, true, 1000, 0, 0); return err })
	do(t, d, func(s *infra.Snapshot) (err error) { div, err = AddSwitch(s, a, true, 1); return })
	leg := d.S.Tracks[trackBetween(d.S, a, div)].ID
	do(t, d, func(s *infra.Snapshot) error { return ToggleSignal(s, leg, true) })
	do(t, d, func(s *infra.Snapshot) error { return Delete(s, schematic.Piece{Kind: schematic.PieceTrack, ID: leg}) })
	if len(d.S.Switches) != 0 || len(d.S.Signals) != 0 || pointIndex(d.S, div) >= 0 {
		t.Errorf("after deleting the leg: %d switches, %d signals, point %s still there: %v", len(d.S.Switches), len(d.S.Signals), div, pointIndex(d.S, div) >= 0)
	}
	do(t, d, func(s *infra.Snapshot) error { return Delete(s, schematic.Piece{Kind: schematic.PiecePoint, ID: a}) })
	if len(d.S.Tracks) != 1 {
		t.Errorf("%d tracks after deleting %s, want 1", len(d.S.Tracks), a)
	}
}

func TestParseTracks(t *testing.T) {
	line := "1:600 9 2:600@50 5:600*@110 ~Gd1:700 ~Sd1< ~Sd2>"
	tracks, err := ParseTracks(line)
	if err != nil {
		t.Fatal(err)
	}
	want := []tm.Track{
		tm.Pf("1", 600), {PfNo: "9"}, {PfNo: "2", Length: 600, Kmph: 50}, {PfNo: "5", Length: 600, Straight: true, Kmph: 110},
		{Siding: "Gd1", SidingLength: 700}, {Siding: "Sd1", Buffer: "W"}, {Siding: "Sd2", Buffer: "E"},
	}
	if !slices.Equal(tracks, want) {
		t.Errorf("got %+v\nwant %+v", tracks, want)
	}
	if got := FormatTracks(tracks); got != line {
		t.Errorf("formatted %q, want %q", got, line)
	}
	for _, bad := range []string{"", "1:x", "~", "1<", ":600", "1@0"} {
		if _, err := ParseTracks(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// Problems name the piece they are about.
func TestProblemsNamePieces(t *testing.T) {
	d := NewDoc(Empty(DefaultDefaults()))
	var a string
	do(t, d, func(s *infra.Snapshot) error { a = AddPoint(s); return nil })
	do(t, d, func(s *infra.Snapshot) error { _, err := Extend(s, a, true, 1000, 0, 0); return err })
	do(t, d, func(s *infra.Snapshot) error { return SetStation(s, "XYZ", "") })
	ps := Problems(d.S)
	var texts []string
	for _, p := range ps {
		texts = append(texts, p.Text)
		if p.Piece.ID == "" {
			t.Errorf("%q names no piece", p.Text)
		}
	}
	if all := strings.Join(texts, "\n"); !strings.Contains(all, "open end") || !strings.Contains(all, "no platform") {
		t.Errorf("problems:\n%s", all)
	}
}
