package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/units"
	"trainapp/visualization/schematic"
	"trainapp/worlds"
	"trainapp/worlds/edit"
	tm "trainapp/worlds/templates"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func press(m *model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft}
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "ctrl+s":
			msg = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
		case "ctrl+u":
			msg = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
		case "ctrl+o":
			msg = tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
		case "ctrl+k":
			msg = tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
		case "alt+up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModAlt}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		m.Update(msg)
	}
}

// typeIn clears the form's current field and types text into it.
func typeIn(m *model, text string) {
	press(m, "ctrl+u")
	for _, r := range text {
		press(m, string(r))
	}
}

// field moves the form to the field with key k.
func toField(t *testing.T, m *model, k string) {
	t.Helper()
	if m.form == nil {
		t.Fatalf("no form open (note: %s)", m.note)
	}
	for range m.form.fields {
		if m.form.fields[m.form.cur].key == k {
			return
		}
		press(m, "down")
	}
	t.Fatalf("form %s has no field %s", m.form.title, k)
}

// setField types a value into a form field.
func setField(t *testing.T, m *model, k, v string) {
	t.Helper()
	toField(t, m, k)
	if f := m.form.fields[m.form.cur]; len(f.choices) > 0 {
		for range f.choices {
			if m.form.fields[m.form.cur].value == v {
				return
			}
			press(m, "right")
		}
		t.Fatalf("field %s has no choice %s", k, v)
	}
	typeIn(m, v)
}

// setCell sets a cell of the open panel by key, with the keys: picking it,
// then choosing or typing its value.
func setCell(t *testing.T, m *model, key, v string) {
	t.Helper()
	p, ok := m.panel.(interface{ cells() *sheet })
	if !ok {
		t.Fatalf("no panel open (note: %s)", m.note)
	}
	sh := p.cells()
	found := false
	// a table's rows share keys: the picked row's cell first
	if rows := sh.section(sh.sec); sh.row < len(rows) {
		for c, f := range rows[sh.row] {
			if f.key == key {
				sh.col, found = c, true
			}
		}
	}
	for sec := range 3 {
		if found {
			break
		}
		for r, cells := range sh.section(sec) {
			for c, f := range cells {
				if f.key == key {
					sh.sec, sh.row, sh.col, found = sec, r, c, true
				}
			}
		}
	}
	if !found {
		t.Fatalf("the panel has no cell %s", key)
	}
	if f := sh.cur(); len(f.choices) > 0 {
		for range f.choices {
			if sh.cur().value == v {
				return
			}
			press(m, "right")
		}
		t.Fatalf("cell %s has no choice %s (%v)", key, v, f.choices)
	}
	typeIn(m, v)
}

// buildStation stamps a station with the builder, from the keys: t, then
// each cell set (tracks typed in as text with :, the first crossover each
// side as wcross and ecross), then enter.
func buildStation(t *testing.T, m *model, kv ...string) {
	t.Helper()
	press(m, "t")
	for i := 0; i < len(kv); i += 2 {
		k, v := kv[i], kv[i+1]
		switch k {
		case "tracks":
			press(m, ":")
			setField(t, m, "tracks", v)
			press(m, "enter")
			if m.form != nil {
				t.Fatalf("tracks %q: %s", v, m.form.err)
			}
			continue
		case "wcross":
			k = "xo:W:0"
		case "ecross":
			k = "xo:E:0"
		}
		setCell(t, m, k, v)
	}
	press(m, "enter")
	if m.panel != nil {
		t.Fatalf("stamping %v: %s", kv, m.note)
	}
}

// copyScenarios copies the repo's scenarios, with their schedules, into a
// temporary directory, the profile alongside, as the repo lays them out.
func copyScenarios(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "scenarios")
	for _, sub := range []string{"", "schedules", "worlds"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(filepath.Join("../../scenarios", sub, "*.json"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(dir, sub, filepath.Base(f)), b, 0o644)
		}
	}
	os.MkdirAll(filepath.Join(root, "profiles"), 0o755)
	b, _ := os.ReadFile("../../profiles/ir.json")
	os.WriteFile(filepath.Join(root, "profiles", "ir.json"), b, 0o644)
	return dir
}

func openModel(t *testing.T, path string) *model {
	t.Helper()
	s, err := openSession(path)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return m
}

// Every tab fills the window exactly, whatever its size, with a form or a
// prompt open or not.
func TestRenderFillsWindow(t *testing.T) {
	m := openModel(t, filepath.Join(copyScenarios(t), "sl-12-disrupt.json"))
	for _, size := range [][2]int{{160, 45}, {100, 30}, {80, 24}, {50, 14}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, tb := range []string{"1", "2", "3"} {
			press(m, tb)
			for _, open := range []bool{false, true} {
				if open {
					m.open(newForm("a form", func(values) error { return nil }, text("a", "a field", "x")))
				}
				lines := strings.Split(m.render(), "\n")
				if len(lines) != size[1] {
					t.Errorf("%dx%d tab %s: %d lines", size[0], size[1], tb, len(lines))
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w > size[0] {
						t.Errorf("%dx%d tab %s: line %d is %d wide", size[0], size[1], tb, i, w)
					}
				}
				m.form = nil
			}
		}
	}
}

// The repo's scenarios open with their worlds drawable and without
// problems, and their trains check out against them.
func TestOpenRepoScenarios(t *testing.T) {
	dir := copyScenarios(t)
	scs, err := scenario.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range scs {
		m := openModel(t, sc.Path)
		if ps := m.world.problems; len(ps) > 0 {
			t.Errorf("%s: problems %v", sc.Name, ps)
		}
		if len(m.s.sched.Trains) == 0 {
			t.Errorf("%s: no trains", sc.Name)
		}
		for _, tr := range m.s.sched.Trains {
			if ps := checkTrain(m.s.world.S, tr); len(ps) > 0 {
				t.Errorf("%s %s: %v", sc.Name, tr.Number, ps)
			}
		}
		if m.s.dirty() {
			t.Errorf("%s is changed just by opening it", sc.Name)
		}
	}
}

// A new scenario built from nothing with the keys: two stations stamped and
// joined, a train added, saved; it loads back as a scenario that can be run.
func TestNewScenarioFromNothing(t *testing.T) {
	dir := copyScenarios(t)
	s, err := newSession(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})

	// a terminus at the west edge, on its own
	buildStation(t, m, "code", "AAA", "name", "Aaa", "tracks", "1:600 2:600*", "edge", "west")
	// a crossing station 12 km east, then a terminus at the east edge
	buildStation(t, m, "start", "crossing", "code", "BBB", "km", "12")
	buildStation(t, m, "code", "CCC", "tracks", "1:600* 2:600", "edge", "east", "km", "8")
	if ps := m.world.problems; len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}

	// a pattern of trains both ways
	press(m, "2", "g")
	setCell(t, m, "from", "AAA")
	setCell(t, m, "to", "CCC")
	setCell(t, m, "stop:BBB", "■")
	setCell(t, m, "count", "2")
	setCell(t, m, "classes", "Passenger")
	press(m, "enter")
	if m.panel != nil {
		t.Fatalf("the pattern: %s", m.note)
	}
	if n := len(m.s.sched.Trains); n != 4 {
		t.Fatalf("%d trains, want 4", n)
	}
	for _, tr := range m.s.sched.Trains {
		if ps := checkTrain(m.s.world.S, tr); len(ps) > 0 {
			t.Errorf("%s: %v", tr.Number, ps)
		}
		if got := []string{tr.Points[0].Stn, tr.Points[1].Stn, tr.Points[2].Stn}; !slices.Equal(got, []string{"AAA", "BBB", "CCC"}) && !slices.Equal(got, []string{"CCC", "BBB", "AAA"}) {
			t.Errorf("%s runs %v", tr.Number, got)
		}
	}

	// name it and save
	press(m, "3")
	m.run.field = 0
	press(m, "e")
	typeIn(m, "abc")
	press(m, "enter", "ctrl+s")
	if m.bad || m.s.dirty() {
		t.Fatalf("not saved: %s", m.note)
	}
	sc, err := scenario.Load(filepath.Join(dir, "abc.json"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := worlds.Load(sc.WorldName())
	if err != nil {
		t.Fatal(err)
	}
	if len(w.ListStations()) != 3 {
		t.Errorf("%d stations saved", len(w.ListStations()))
	}
	trains, err := ops.LoadTrains(sc.SchedulePath())
	if err != nil || len(trains) != 4 {
		t.Errorf("%d trains saved: %v", len(trains), err)
	}
}

// Saving a schedule another scenario uses asks first, and c saves a copy for
// this scenario only, leaving the other as it was.
func TestSharedScheduleCopy(t *testing.T) {
	dir := copyScenarios(t)
	m := openModel(t, filepath.Join(dir, "sl-12-disrupt.json"))
	before, _ := os.ReadFile(filepath.Join(dir, "schedules", "sl-12.json"))
	press(m, "2", "d") // delete the first train
	press(m, "ctrl+s")
	if m.prompt == nil {
		t.Fatalf("no question about the shared schedule (note: %s)", m.note)
	}
	if !strings.Contains(strings.Join(m.prompt.lines, " "), "sl-12") {
		t.Errorf("the question does not name sl-12: %v", m.prompt.lines)
	}
	press(m, "c")
	if m.s.dirty() {
		t.Fatalf("not saved: %s", m.note)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "schedules", "sl-12.json"))
	if string(after) != string(before) {
		t.Error("the shared schedule changed")
	}
	sc, _ := scenario.Load(filepath.Join(dir, "sl-12-disrupt.json"))
	if sc.Schedule == "schedules/sl-12.json" {
		t.Error("the scenario still names the shared schedule")
	}
	trains, err := ops.LoadTrains(sc.SchedulePath())
	if err != nil || len(trains) != 11 {
		t.Errorf("its own schedule: %d trains, %v", len(trains), err)
	}
}

// Editing a built-in world saves it as a snapshot beside the scenario.
func TestBuiltinWorldSavedAsSnapshot(t *testing.T) {
	dir := copyScenarios(t)
	m := openModel(t, filepath.Join(dir, "sl-6.json"))
	press(m, "/")
	typeIn(m, "PDKT")
	press(m, "enter", "n") // rename the station
	setField(t, m, "name", "Pudukkottai Town")
	press(m, "enter", "ctrl+s")
	if m.s.dirty() || m.bad {
		t.Fatalf("not saved: %s", m.note)
	}
	sc, err := scenario.Load(filepath.Join(dir, "sl-6.json"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(sc.World) != ".json" {
		t.Fatalf("the world is %s, want a snapshot", sc.World)
	}
	w, err := worlds.Load(sc.WorldName())
	if err != nil {
		t.Fatal(err)
	}
	if stn, _ := w.GetStation("PDKT"); stn.Name != "Pudukkottai Town" {
		t.Errorf("PDKT is %q", stn.Name)
	}
}

// The way between two stations lists every station on it, in order.
func TestRoute(t *testing.T) {
	s, err := worlds.BuildTpjKkdiSingleLineWorld().Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	way, err := route(s, "TPJ", "KKDI", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TPJ", "KRMG", "KRUR", "VEL", "PDKT", "NMN", "TYM", "CTND", "KTYR", "KKDI"}
	if !slices.Equal(way, want) {
		t.Errorf("way %v, want %v", way, want)
	}
	back, _ := route(s, "KKDI", "PDKT", "")
	if !slices.Equal(back, []string{"KKDI", "KTYR", "CTND", "TYM", "NMN", "PDKT"}) {
		t.Errorf("back %v", back)
	}
}

// Extending, undoing and redoing from the keys.
func TestDrawTrack(t *testing.T) {
	dir := copyScenarios(t)
	s, _ := newSession(dir, "")
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	press(m, "e") // starts the world at a point
	setField(t, m, "m", "2000")
	press(m, "enter")
	press(m, "e", "enter") // and on from the new point
	if n := len(m.s.world.S.Tracks); n != 2 {
		t.Fatalf("%d tracks, want 2 (%s)", n, m.note)
	}
	press(m, "u")
	if n := len(m.s.world.S.Tracks); n != 1 {
		t.Errorf("after undo, %d tracks", n)
	}
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if n := len(m.s.world.S.Tracks); n != 2 {
		t.Errorf("after redo, %d tracks", n)
	}
}

// openEnds lists a station's open ends east (or west) in the editor's world.
func openEnds(m *model, code string, east bool) []string {
	s := m.s.world.S
	for _, p := range s.Points {
		if p.Stn == code && !p.DeadEnd && openSide(s, p.ID, east) && !openSide(s, p.ID, !east) {
			return edit.OpenBeside(s, p.ID, east)
		}
	}
	return nil
}

// A double line drawn with the keys: stations on two lines stamped, joined
// all at once and a line at a time; trains run it the right way.
func TestDoubleLineByKeys(t *testing.T) {
	dir := copyScenarios(t)
	s, err := newSession(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	stamp := func(fields ...string) { t.Helper(); buildStation(t, m, fields...) }
	stamp("code", "AAA", "tracks", "1:600 2:600* 3:600* 4:600", "edge", "west", "ecross", "scissors")
	stamp("code", "BBB", "tracks", "1:600 2:600* 3:600* 4:600", "join", "all", "km", "12", "blocks", "2",
		"wcross", "down", "ecross", "up")
	stamp("code", "CCC", "tracks", "1:600 2:600* 3:600* 4:600", "edge", "east", "join", "none", "wcross", "scissors")
	if n := len(openEnds(m, "BBB", true)); n != 2 {
		t.Fatalf("BBB has %d open ends east, want 2", n)
	}
	// BBB to CCC with b, all open ends: the usual E W
	m.world.selectPoint(openEnds(m, "BBB", true)[0])
	press(m, "b")
	m.world.selectPoint(openEnds(m, "CCC", false)[1])
	press(m, "b")
	setField(t, m, "km", "9")
	setField(t, m, "join", "all open ends beside them")
	press(m, "enter")
	if m.form != nil {
		t.Fatalf("the lines: %s", m.form.err)
	}
	if ps := m.world.problems; len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}
	east, west := 0, 0
	for _, tr := range m.s.world.S.Tracks {
		switch edit.Way(m.s.world.S, tr.ID) {
		case edit.WayEast:
			east++
			if tr.Direction != "DOWN" {
				t.Errorf("%s runs east, labelled %q", tr.Name, tr.Direction)
			}
		case edit.WayWest:
			west++
		}
	}
	if east != 3 || west != 3 {
		t.Errorf("%d tracks run east and %d west, want 3 and 3", east, west)
	}

	press(m, "2", "g")
	setCell(t, m, "from", "AAA")
	setCell(t, m, "to", "CCC")
	setCell(t, m, "stop:BBB", "■")
	setCell(t, m, "count", "2")
	setCell(t, m, "classes", "Passenger")
	press(m, "enter")
	if m.panel != nil {
		t.Fatalf("the pattern: %s", m.note)
	}
	for _, tr := range m.s.sched.Trains {
		if ps := checkTrain(m.s.world.S, tr); len(ps) > 0 {
			t.Errorf("%s: %v", tr.Number, ps)
		}
	}
}

// A form taller than the screen scrolls to its current field.
func TestFormScrolls(t *testing.T) {
	m := openModel(t, filepath.Join(copyScenarios(t), "sl-6.json"))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	press(m, "1")
	for _, tr := range m.s.world.S.Tracks {
		if edit.Plain(m.s.world.S, tr.ID) {
			m.world.selectPiece(schematic.Piece{Kind: schematic.PieceTrack, ID: tr.ID})
			break
		}
	}
	press(m, "Y")
	toField(t, m, "blocks")
	out := m.render()
	if !strings.Contains(out, "blocks") || !strings.Contains(out, "enter ok") {
		t.Errorf("the last field or the keys are not drawn:\n%s", out)
	}
	if lines := strings.Split(out, "\n"); len(lines) != 14 {
		t.Errorf("%d lines, want 14", len(lines))
	}
}

// A route runs one way all along: a train does not reverse at a junction.
func TestRouteDoesNotReverse(t *testing.T) {
	s, err := worlds.BuildJunctionWorld(2).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ from, to, want string }{
		{"WJN", "NJN", "WJN MID NJN"}, {"NJN", "WJN", "NJN MID WJN"}, {"EJN", "WJN", "EJN MID WJN"},
	} {
		way, err := route(s, c.from, c.to, "")
		if err != nil || strings.Join(way, " ") != c.want {
			t.Errorf("%s to %s: %v, %v, want %s", c.from, c.to, way, err, c.want)
		}
	}
	if _, err := route(s, "EJN", "NJN", ""); err == nil || !strings.Contains(err.Error(), "reverse") {
		t.Errorf("EJN to NJN: %v, want it refused for reversing", err)
	}
}

// Two ways round between two stations: the route takes one, and a via
// picks the other.
func TestRouteOtherWays(t *testing.T) {
	s := twoWaysWorld(t)
	way, err := route(s, "AAA", "DDD", "")
	if err != nil || len(way) != 3 {
		t.Fatalf("AAA to DDD: %v, %v", way, err)
	}
	other := map[string]string{"BBB": "CCC", "CCC": "BBB"}[way[1]]
	if via, err := route(s, "DDD", "AAA", other); err != nil || strings.Join(via, " ") != "DDD "+other+" AAA" {
		t.Errorf("DDD to AAA by %s: %v, %v", other, via, err)
	}
}

// Y leaves the line at the selected track for a branch, from the keys.
func TestJunctionByKeys(t *testing.T) {
	dir := copyScenarios(t)
	s, err := newSession(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	buildStation(t, m, "code", "AAA", "tracks", "1:600* 2:600", "edge", "west")
	buildStation(t, m, "code", "BBB", "tracks", "1:600* 2:600", "edge", "east", "km", "12")
	var line string
	for _, tr := range m.s.world.S.Tracks {
		if strings.HasPrefix(tr.Name, "slAaaBbb") {
			line = tr.ID
		}
	}
	m.world.selectPiece(schematic.Piece{Kind: schematic.PieceTrack, ID: line})
	press(m, "Y")
	setField(t, m, "side", "above")
	setField(t, m, "lanes", "2")
	setField(t, m, "km", "6")
	press(m, "enter")
	if m.form != nil {
		t.Fatalf("the junction: %s", m.form.err)
	}
	if m.world.sel.Kind != schematic.PiecePoint || len(m.world.problems) != 1 {
		t.Errorf("selected %v, problems %v: want the branch's open end selected, and it the one problem", m.world.sel, m.world.problems)
	}
}

// A station added with n has no platforms and is not drawn: / still finds
// it, and t, with it picked, stamps its tracks into it.
func TestEmptyStationByKeys(t *testing.T) {
	s, err := newSession(copyScenarios(t), "")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	press(m, "n")
	setField(t, m, "code", "AAA")
	setField(t, m, "name", "Aaa Town")
	press(m, "enter", "esc")
	press(m, "/")
	setField(t, m, "code", "aaa")
	press(m, "enter")
	if m.form != nil || m.world.sel != (schematic.Piece{Kind: schematic.PieceStation, ID: "AAA"}) {
		t.Fatalf("/ did not pick AAA: form %v, selected %v, note %s", m.form != nil, m.world.sel, m.note)
	}
	press(m, "t")
	if v, _ := cellValue(m.panel.(*builder).sh.head, "code"); v != "AAA" {
		t.Errorf("the builder's code is %q, want AAA", v)
	}
	press(m, "esc")
	m.world.sel = schematic.Piece{Kind: schematic.PieceStation, ID: "AAA"}
	buildStation(t, m, "start", "crossing")
	if st := m.s.world.S.Stations; len(st) != 1 || st[0].Name != "Aaa Town" || len(st[0].Platforms) != 3 {
		t.Errorf("stations %+v, want AAA Aaa Town with three tracks", st)
	}
}

// A form's text has a cursor: the arrows move it, typing goes in at it, and
// backspace and delete take out the letter before it and at it.
func TestFormTextCursor(t *testing.T) {
	f := newForm("f", func(values) error { return nil }, text("a", "a", "1:600 3:600"), choice("c", "c", "x", "x", "y"))
	key := func(keys ...string) {
		for _, k := range keys {
			var msg tea.KeyPressMsg
			switch k {
			case "left":
				msg = tea.KeyPressMsg{Code: tea.KeyLeft}
			case "right":
				msg = tea.KeyPressMsg{Code: tea.KeyRight}
			case "home":
				msg = tea.KeyPressMsg{Code: tea.KeyHome}
			case "end":
				msg = tea.KeyPressMsg{Code: tea.KeyEnd}
			case "backspace":
				msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
			case "delete":
				msg = tea.KeyPressMsg{Code: tea.KeyDelete}
			case "space":
				msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
			case "down":
				msg = tea.KeyPressMsg{Code: tea.KeyDown}
			default:
				msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
			}
			f.key(msg)
		}
	}
	// put platform 2 between 1 and 3
	key("left", "left", "left", "left", "left", "2", ":", "6", "0", "0", "space")
	if got := f.fields[0].value; got != "1:600 2:600 3:600" {
		t.Fatalf("typed in the middle: %q", got)
	}
	lines, _ := f.lines(60)
	if !strings.Contains(ansi.Strip(lines[0]), "1:600 2:600 ▏3:600") {
		t.Errorf("the cursor is not drawn where it is: %q", ansi.Strip(lines[0]))
	}
	key("home", "delete", "9", "end", "backspace", "7")
	if got := f.fields[0].value; got != "9:600 2:600 3:607" {
		t.Errorf("home, delete, end, backspace: %q", got)
	}
	key("right", "right", "x") // past the end stays at the end
	if got := f.fields[0].value; got != "9:600 2:600 3:607x" {
		t.Errorf("right past the end: %q", got)
	}
	key("down", "right")
	if got := f.fields[1].value; got != "y" {
		t.Errorf("a choice's ← → still choose: %q", got)
	}
}

// twoWaysWorld is AAA - (BBB or CCC) - DDD, a halt on each way round.
func twoWaysWorld(t *testing.T) *infra.Snapshot {
	t.Helper()
	w := &infra.World{}
	w.Init(infra.WorldData{DefaultSwMaxSpeed: units.KMPH(30), DefaultSwLength: 100, DefaultPfMaxSpeed: units.KMPH(50), DefaultPfLength: 800, DefaultRouteSpeed: units.KMPH(100)})
	_, aE, _ := tm.Station(w, "AAA", "", "W", 0, tm.Track{PfNo: "1", Length: 600, Straight: true})
	dW, _, _ := tm.Station(w, "DDD", "", "E", 0, tm.Track{PfNo: "1", Length: 600, Straight: true})
	bW, bE, _ := tm.Halt(w, "BBB", "", 400, 100)
	cW, cE, _ := tm.Halt(w, "CCC", "", 400, 100)
	j1 := w.NewTrackPoint(ids.PointID{StnCode: "JCT", Type: "Jn", Seq: "1"})
	j2 := w.NewTrackPoint(ids.PointID{StnCode: "JCT", Type: "Jn", Seq: "2"})
	infra.NewSwitch(w, "jctSw1", j1, bW, cW, infra.SwitchLayout{Legs: infra.East, Lanes: 2})
	infra.NewSwitch(w, "jctSw2", j2, bE, cE, infra.SwitchLayout{Legs: infra.West, Lanes: 2})
	tm.SingleLine(w, "slAJ", aE, j1, units.KM(5), 1)
	tm.SingleLine(w, "slJD", j2, dW, units.KM(5), 1)
	if err := w.Finalize(); err != nil {
		t.Fatal(err)
	}
	s, err := w.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// ways lists each way round, shortest first, over track a train can run.
func TestWays(t *testing.T) {
	s := twoWaysWorld(t)
	ws := ways(s, "AAA", "DDD", 6)
	if len(ws) != 2 {
		t.Fatalf("%d ways, want 2", len(ws))
	}
	var labels []string
	for _, w := range ws {
		labels = append(labels, w.label())
		// the points are a run of track, one way
		for i := 0; i+1 < len(w.points); i++ {
			if !slices.ContainsFunc(s.Tracks, func(tr infra.SnapshotTrack) bool { return tr.From == w.points[i] && tr.To == w.points[i+1] }) {
				t.Errorf("%s: no track east from %s to %s", w.label(), w.points[i], w.points[i+1])
			}
		}
	}
	if !slices.Equal(labels, []string{"by BBB · 11 km", "by CCC · 11 km"}) {
		t.Errorf("ways %v", labels)
	}
	if ws := ways(s, "AAA", "DDD", 1); len(ws) != 1 {
		t.Errorf("max 1: %d ways", len(ws))
	}

	dl, err := worlds.BuildTpjKkdiWorld().Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	ws = ways(dl, "KKDI", "TPJ", 6)
	if len(ws) != 1 || strings.Join(ws[0].stations, " ") != "KKDI PDKT TPJ" || ws[0].m < 96000 {
		t.Errorf("KKDI to TPJ: %+v", ws)
	}
}

// modelOn is an editor on a new scenario in the world s.
func modelOn(t *testing.T, s *infra.Snapshot) *model {
	t.Helper()
	sess, err := newSession(copyScenarios(t), "")
	if err != nil {
		t.Fatal(err)
	}
	sess.world = edit.NewDoc(s)
	m := newModel(sess, config{outDir: t.TempDir()})
	m.world.changed()
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	press(m, "2")
	return m
}

// The route picker lists the ways, lays the chosen one's stations out with
// their stops, dwells and platforms, and draws it.
func TestRoutePicker(t *testing.T) {
	m := modelOn(t, twoWaysWorld(t))
	press(m, "a")
	setCell(t, m, "from", "AAA")
	setCell(t, m, "to", "DDD")
	r := m.panel.(*routePicker)
	if len(r.ways) != 2 || len(r.sh.table) != 3 {
		t.Fatalf("%d ways and %d stations, want 2 and 3", len(r.ways), len(r.sh.table))
	}
	second := r.ways[1].label()
	setCell(t, m, "way", second)
	via := r.ways[1].stations[1]
	setCell(t, m, "stop:"+via, "■")
	setCell(t, m, "dwell:"+via, "5")
	setCell(t, m, "pf:"+via, "1")
	out := m.render()
	if !strings.Contains(out, "\x1b[1;36m") || !strings.Contains(ansi.Strip(out), second) {
		t.Errorf("the way is not drawn, or not offered:\n%s", ansi.Strip(out))
	}
	press(m, "enter")
	if m.panel != nil {
		t.Fatalf("not added: %s", m.note)
	}
	tr := m.s.sched.Trains[len(m.s.sched.Trains)-1]
	p := tr.Points[1]
	if p.Stn != via || p.Kind != ops.SpStop || p.Dwell == nil || *p.Dwell != 5 || p.Pf != "1" {
		t.Errorf("the stop at %s: %+v", via, p)
	}
	if ps := checkTrain(m.s.world.S, tr); len(ps) > 0 {
		t.Errorf("problems: %v", ps)
	}

	// f lays it out again, starting from what it has
	press(m, "f")
	r = m.panel.(*routePicker)
	if v, _ := cellValue(r.sh.table, "stop:"+via); v != "■" {
		t.Errorf("re-routing does not keep the stop at %s", via)
	}
	press(m, "esc")
	if m.panel != nil {
		t.Error("esc does not close the picker")
	}
}

// A route that would need a train to reverse is no way at all.
func TestRoutePickerRefusesReversing(t *testing.T) {
	s, err := worlds.BuildJunctionWorld(2).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	m := modelOn(t, s)
	press(m, "a")
	setCell(t, m, "from", "EJN")
	setCell(t, m, "to", "NJN")
	press(m, "enter")
	if m.panel == nil || !strings.Contains(m.note, "reverse") {
		t.Errorf("EJN to NJN: panel open %v, note %q", m.panel != nil, m.note)
	}
}

// A pattern's trains back stop where the trains out do, on any platform.
func TestPatternBackOnAnyPlatform(t *testing.T) {
	m := modelOn(t, twoWaysWorld(t))
	press(m, "g")
	setCell(t, m, "from", "AAA")
	setCell(t, m, "to", "DDD")
	r := m.panel.(*routePicker)
	via := r.ways[0].stations[1]
	setCell(t, m, "stop:"+via, "■")
	setCell(t, m, "pf:"+via, "1")
	setCell(t, m, "count", "1")
	press(m, "enter")
	if m.panel != nil {
		t.Fatalf("not added: %s", m.note)
	}
	if n := len(m.s.sched.Trains); n != 2 {
		t.Fatalf("%d trains, want 2", n)
	}
	out, back := m.s.sched.Trains[0], m.s.sched.Trains[1]
	if out.Points[1].Pf != "1" || back.Points[1].Pf != "" || back.Points[1].Kind != ops.SpStop || back.Points[0].Stn != "DDD" {
		t.Errorf("out %+v, back %+v", out.Points, back.Points)
	}
}

// pickRow picks a builder's track row by its number or name.
func pickRow(t *testing.T, b *builder, no string) {
	t.Helper()
	for r, cells := range b.sh.table {
		if cells[1].value == no {
			b.sh.pick(1, r, 0)
			return
		}
	}
	t.Fatalf("no track %s", no)
}

// The builder lays a station out from its table, draws it as it goes, and
// says what is wrong as it is edited.
func TestBuilder(t *testing.T) {
	s, err := newSession(copyScenarios(t), "")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	press(m, "t")
	setCell(t, m, "start", "double line")
	setCell(t, m, "code", "PDKT")
	b := m.panel.(*builder)
	if len(b.sh.table) != 4 || len(b.sh.foot) != 2 {
		t.Fatalf("the double line start: %d tracks and %d crossovers, want 4 and 2", len(b.sh.table), len(b.sh.foot))
	}
	setCell(t, m, "xo:E:0", "down")
	// a goods siding under 4, with a buffer stop east
	pickRow(t, b, "4")
	press(m, "ctrl+o")
	setCell(t, m, "kind", "siding")
	setCell(t, m, "no", "Gd1")
	setCell(t, m, "buffer", "E")
	// the outer tracks are not between mains: they say nothing of joins
	pickRow(t, b, "1")
	if !b.sh.table[b.sh.row][6].off {
		t.Error("track 1's joins is not off")
	}
	out := ansi.Strip(m.render())
	for _, want := range []string{"Pf2", "Gd1", "east crossover 2–3"} {
		if !strings.Contains(out, want) {
			t.Errorf("drawn without %q:\n%s", want, out)
		}
	}
	if b.err != "" {
		t.Fatalf("an error: %s", b.err)
	}

	// no main west: an error, live, and enter does not stamp
	for _, no := range []string{"2", "3"} {
		pickRow(t, b, no)
		setCell(t, m, "mainW", "·")
	}
	if !strings.Contains(b.err, "no straight track") {
		t.Errorf("no main west: error %q", b.err)
	}
	press(m, "enter")
	if m.panel == nil {
		t.Fatal("stamped with an error")
	}
	for _, no := range []string{"2", "3"} {
		pickRow(t, b, no)
		setCell(t, m, "mainW", "■")
	}
	press(m, "enter")
	if m.panel != nil {
		t.Fatalf("not stamped: %s", m.note)
	}
	st := m.s.world.S.Stations
	if len(st) != 1 || len(st[0].Platforms) != 4 || !slices.ContainsFunc(m.s.world.S.Tracks, func(tr infra.SnapshotTrack) bool { return tr.Name == "pdktGd1" }) {
		t.Errorf("stations %+v", st)
	}
}

// A junction station: one main west, two east, a crossover between them.
func TestBuilderJunctionStation(t *testing.T) {
	s, err := newSession(copyScenarios(t), "")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	press(m, "t")
	setCell(t, m, "start", "double line")
	setCell(t, m, "code", "JCT")
	b := m.panel.(*builder)
	pickRow(t, b, "3")
	setCell(t, m, "mainW", "·")
	if len(b.sh.foot) != 1 || b.sh.foot[0][0].key != "xo:E:0" {
		t.Fatalf("crossover rows %v, want the east one only", b.sh.foot)
	}
	press(m, "enter")
	if m.panel != nil {
		t.Fatalf("not stamped: %s", m.note)
	}
	west, east := 0, 0
	for _, p := range m.s.world.S.Points {
		if p.Stn == "JCT" && !p.DeadEnd {
			wn, en := countSides(m.s.world.S, p.ID)
			if wn == 0 {
				west++
			}
			if en == 0 {
				east++
			}
		}
	}
	if west != 1 || east != 2 {
		t.Errorf("%d line ends west and %d east, want 1 and 2", west, east)
	}
}

// Two tracks numbered the same are an error, said as it is typed, not a
// crash.
func TestBuilderSameNumberTwice(t *testing.T) {
	s, err := newSession(copyScenarios(t), "")
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(s, config{outDir: t.TempDir()})
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	press(m, "t")
	setCell(t, m, "start", "crossing")
	setCell(t, m, "code", "AAA")
	b := m.panel.(*builder)
	pickRow(t, b, "2")
	setCell(t, m, "no", "1")
	if !strings.Contains(b.err, "both platform 1") {
		t.Errorf("two platform 1s: error %q", b.err)
	}
	m.render()
	press(m, "enter")
	if m.panel == nil || len(m.s.world.S.Stations) != 0 {
		t.Error("stamped with two platform 1s")
	}
}
