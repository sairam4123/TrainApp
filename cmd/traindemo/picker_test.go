package main

import (
	"strings"
	"testing"
	"trainapp/railway/scenario"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func loadPicker(t *testing.T) *picker {
	t.Helper()
	scenarios, err := scenario.LoadDir("../../scenarios")
	if err != nil {
		t.Fatal(err)
	}
	return newPicker(scenarios, 0, "trainapp", "trainviz", "logs/demo")
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
	p.choose(3) // sl-24
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
	if p.focus != focusList || p.cursor != 4 || p.train != 0 {
		t.Errorf("after esc, down: focus %d, scenario %d, train %d", p.focus, p.cursor, p.train)
	}
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
