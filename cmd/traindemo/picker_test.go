package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"trainapp/visualization/runs"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func loadPicker(t *testing.T) *picker {
	t.Helper()
	p, err := newPicker("../../scenarios", 0, bins{trainapp: "trainapp", trainviz: "trainviz"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The screen is always exactly the window, whatever its size, with each
// scenario's previews, with or without a train picked.
func TestRenderFillsWindow(t *testing.T) {
	p := loadPicker(t)
	for _, size := range [][2]int{{160, 45}, {110, 34}, {80, 24}, {40, 10}, {10, 3}} {
		p.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for i := range 2 * len(p.scenarios) {
			p.focus = focus(i % 2) // with a train picked, its timetable too
			i /= 2
			p.choose(i)
			p.clamp()
			lines := strings.Split(p.render(), "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d, %s: %d lines", size[0], size[1], p.scenarios[i].Name, len(lines))
			}
			for n, line := range lines {
				if w := ansi.StringWidth(line); w > size[0] {
					t.Errorf("%dx%d, %s: line %d is %d wide", size[0], size[1], p.scenarios[i].Name, n, w)
				}
			}
		}
	}
}

// Every scenario shows its schematic and a row per train.
func TestPreviews(t *testing.T) {
	p := loadPicker(t)
	for _, sc := range p.scenarios {
		if d := p.previews.diagram(sc, p.level); d.err != nil || d.width == 0 {
			t.Errorf("%s: no schematic: %v", sc.Name, d.err)
		}
		if s := p.previews.schedule(sc); s.err != nil || len(s.trains) == 0 {
			t.Errorf("%s: no schedule: %v", sc.Name, s.err)
		}
	}
}

// tab moves the arrow keys to the trains, keeping the picked one in view,
// and esc moves them back; a new scenario starts at its first train.
func TestPickTrain(t *testing.T) {
	p := loadPicker(t)
	p.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	sl24 := -1
	for i, sc := range p.scenarios {
		if sc.Name == "sl-24" {
			sl24 = i
		}
	}
	if sl24 < 0 {
		t.Fatal("no sl-24 scenario")
	}
	p.choose(sl24)
	p.key("tab")
	for range 20 {
		p.key("down")
	}
	l := p.layout()
	if p.train != 20 || p.train < p.schedTop || p.train >= p.schedTop+l.rows {
		t.Errorf("train %d, rows %d-%d shown", p.train, p.schedTop, p.schedTop+l.rows-1)
	}
	if title, _ := p.timetableBox(p.scenarios[p.cursor], l); !strings.Contains(title, p.previews.schedule(p.scenarios[p.cursor]).trains[20].number) {
		t.Errorf("timetable %q is not the picked train's", title)
	}
	p.key("esc")
	p.key("down")
	if next := (sl24 + 1) % len(p.scenarios); p.focus != focusList || p.cursor != next || p.train != 0 {
		t.Errorf("after esc, down: focus %d, scenario %d (want %d), train %d", p.focus, p.cursor, next, p.train)
	}
	p.choose(len(p.scenarios) - 1)
	p.key("down") // past the last scenario, round to the first
	if p.cursor != 0 {
		t.Errorf("down from the last scenario: scenario %d, want 0", p.cursor)
	}
}

// ↑ and ↓ wrap round from one end of the trains to the other; home, end and
// the page keys stop at the ends.
func TestPickTrainWraps(t *testing.T) {
	p := loadPicker(t)
	p.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	p.key("tab")
	n := len(p.previews.schedule(p.scenarios[p.cursor]).trains)
	for _, k := range []struct {
		key  string
		want int
	}{{"up", n - 1}, {"down", 0}, {"pgup", 0}, {"home", 0}, {"end", n - 1}, {"pgdown", n - 1}, {"down", 0}, {"up", n - 1}} {
		p.key(k.key)
		p.render()
		if p.train != k.want {
			t.Errorf("%s: train %d, want %d", k.key, p.train, k.want)
		}
	}
}

// A scenario that does not read is listed with its error, the others as
// ever; reading the directory again picks up a new scenario and selects it.
func TestUnreadableAndReload(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	abs, _ := filepath.Abs("../../scenarios/schedules/sl-6.json")
	write("a.json", `{"world": "tpjkkdi-sl", "schedule": "`+abs+`", "profile": ""}`)
	write("b.json", `{"world": "tpjkkdi-sl", "trains": 3}`)
	p, err := newPicker(dir, 0, bins{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if len(p.scenarios) != 2 || p.scenarios[0].Err != nil || p.scenarios[1].Err == nil {
		t.Fatalf("scenarios: %d, errors %v, %v", len(p.scenarios), p.scenarios[0].Err, p.scenarios[1].Err)
	}
	p.choose(1)
	screen := ansi.Strip(p.render())
	if !strings.Contains(screen, "does not read") {
		t.Errorf("the unreadable scenario does not say so:\n%s", screen)
	}
	if cmd := p.start(); cmd != nil || !strings.Contains(p.note, "does not read") {
		t.Errorf("an unreadable scenario starts a run (note %q)", p.note)
	}
	c := write("c.json", `{"world": "tpjkkdi-sl", "schedule": "`+abs+`", "profile": ""}`)
	p.Update(editDoneMsg{saved: c})
	if len(p.scenarios) != 3 || p.scenarios[p.cursor].Path != c {
		t.Errorf("after the edit: %d scenarios, %s selected, want %s", len(p.scenarios), p.scenarios[p.cursor].Path, c)
	}
}

// A run older than its scenario's files is stale.
func TestStaleRun(t *testing.T) {
	p := loadPicker(t)
	sc := p.scenarios[0]
	rec, _ := runs.Files(p.outDir, sc, "")
	if err := os.WriteFile(rec, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * 365 * time.Hour)
	if err := os.Chtimes(rec, old, old); err != nil {
		t.Fatal(err)
	}
	if res := p.result(); res == nil || !res.Stale {
		t.Errorf("a run from a year ago is not stale: %+v", res)
	}
}
