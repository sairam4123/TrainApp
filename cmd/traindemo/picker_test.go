package main

import (
	"os"
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
	return newPicker(scenarios, 0)
}

// The screen is always exactly the window, whatever its size, with each
// scenario's previews.
func TestRenderFillsWindow(t *testing.T) {
	p := loadPicker(t)
	for _, size := range [][2]int{{160, 45}, {80, 24}, {40, 10}, {10, 3}} {
		p.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for i := range p.scenarios {
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
	if os.Getenv("SHOW") != "" {
		p.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
		p.choose(2)
		t.Log("\n" + ansi.Strip(p.render()))
	}
}

// Every scenario shows its schematic and a row per train.
func TestPreviews(t *testing.T) {
	p := loadPicker(t)
	for _, sc := range p.scenarios {
		if d := p.previews.diagram(sc, p.level); d.width == 0 || strings.Contains(d.lines[0], "no preview") {
			t.Errorf("%s: no schematic: %q", sc.Name, d.lines)
		}
		s := p.previews.schedule(sc)
		if len(s.rows) == 0 || strings.Contains(s.rows[0], "no schedule") {
			t.Errorf("%s: no schedule: %q", sc.Name, s.rows)
		}
	}
}
