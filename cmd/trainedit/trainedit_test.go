package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/worlds"

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

// copyScenarios copies the repo's scenarios, with their schedules, into a
// temporary directory, the profile alongside, as the repo lays them out.
func copyScenarios(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "scenarios")
	for _, sub := range []string{"", "schedules"} {
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
	press(m, "t")
	setField(t, m, "template", "station")
	setField(t, m, "code", "AAA")
	setField(t, m, "name", "Aaa")
	setField(t, m, "tracks", "1:600 2:600*")
	setField(t, m, "edge", "west")
	press(m, "enter")
	if m.form != nil {
		t.Fatalf("stamping AAA: %s", m.form.err)
	}
	// a crossing station 12 km east, then a terminus at the east edge
	press(m, "t")
	setField(t, m, "template", "crossing")
	setField(t, m, "code", "BBB")
	setField(t, m, "km", "12")
	press(m, "enter")
	if m.form != nil {
		t.Fatalf("stamping BBB: %s", m.form.err)
	}
	press(m, "t")
	setField(t, m, "template", "station")
	setField(t, m, "code", "CCC")
	setField(t, m, "tracks", "1:600* 2:600")
	setField(t, m, "edge", "east")
	setField(t, m, "km", "8")
	press(m, "enter")
	if m.form != nil {
		t.Fatalf("stamping CCC: %s", m.form.err)
	}
	if ps := m.world.problems; len(ps) > 0 {
		t.Fatalf("problems: %v", ps)
	}

	// a pattern of trains both ways
	press(m, "2", "g")
	setField(t, m, "from", "AAA")
	setField(t, m, "to", "CCC")
	setField(t, m, "stops", "BBB")
	setField(t, m, "count", "2")
	setField(t, m, "classes", "Passenger")
	press(m, "enter")
	if m.form != nil {
		t.Fatalf("the pattern: %s", m.form.err)
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
