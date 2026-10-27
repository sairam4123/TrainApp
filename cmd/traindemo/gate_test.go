package main

import (
	"os"
	"strings"
	"testing"
	"time"
	"trainapp/visualization/runs"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// fakeRun writes a run's recording and KPIs, with the mean delay given.
func fakeRun(t *testing.T, p *picker, i int, rule string, meanMin string) {
	t.Helper()
	rec, kpi := runs.Files(p.outDir, p.scenarios[i], rule)
	if err := os.WriteFile(rec, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := `{"run": {"trains": 1, "finished": 1, "holds": 2, "delay": {"trains": 1, "mean_min": ` + meanMin + `, "punctual": [{"within_min": 5, "share": 1}]},
		"per_train": [{"train": "1", "finished": true, "entry_min": 0}]}}`
	if err := os.WriteFile(kpi, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// x and ctrl+x pick runs into the set, marked in the list and on the set
// line; esc clears the set before it quits.
func TestComparePicks(t *testing.T) {
	p := loadPicker(t)
	p.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	p.key("x")
	p.rule = 3 // off
	p.key("x")
	if len(p.set) != 2 || p.setMarks(p.scenarios[0]) != "po" {
		t.Fatalf("set %v, marks %q", p.set, p.setMarks(p.scenarios[0]))
	}
	s := ansi.Strip(p.render())
	if !strings.Contains(s, "compare: ○ "+p.scenarios[0].Name+"·profile, ○ "+p.scenarios[0].Name+"·off") {
		t.Errorf("no set line:\n%s", s)
	}
	if n := len(strings.Split(p.render(), "\n")); n != 40 {
		t.Errorf("%d lines with the set line, want 40", n)
	}
	p.key("x")
	if len(p.set) != 1 {
		t.Error("x does not take a run out")
	}
	p.key("ctrl+x")
	if len(p.set) != len(runs.Rules) {
		t.Errorf("ctrl+x: %d in the set", len(p.set))
	}
	p.key("ctrl+x")
	if len(p.set) != 0 {
		t.Errorf("ctrl+x again leaves %d", len(p.set))
	}
	p.key("x")
	if cmd := p.key("esc"); cmd != nil || len(p.set) != 0 || p.choice == choiceQuit {
		t.Error("esc does not clear the set, or quits")
	}
}

// X opens the comparison only when every run is made; else it asks first.
func TestCompareGate(t *testing.T) {
	p := loadPicker(t)
	p.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	p.key("x")
	if cmd := p.key("X"); cmd != nil || !strings.Contains(p.note, "at least two") {
		t.Errorf("X with one run: note %q", p.note)
	}
	p.choose(1)
	p.key("x")
	p.key("X")
	if p.gate == nil || !p.gate.asking || !strings.Contains(ansi.Strip(p.keyLine()), "2 runs to make first (2 missing)") {
		t.Fatalf("X does not ask about the missing runs: %q", ansi.Strip(p.keyLine()))
	}
	p.key("esc")
	if p.gate != nil || len(p.set) != 2 {
		t.Error("esc does not cancel the question, or clears the set")
	}

	fakeRun(t, p, 0, "", "2")
	fakeRun(t, p, 1, "", "3")
	if cmd := p.key("X"); cmd == nil || p.gate != nil {
		t.Error("X with every run made does not open trainviz compare")
	}
	rec, _ := runs.Files(p.outDir, p.scenarios[1], "")
	old := time.Now().Add(-24 * 365 * time.Hour)
	if err := os.Chtimes(rec, old, old); err != nil {
		t.Fatal(err)
	}
	p.key("X")
	if p.gate == nil || !strings.Contains(p.gate.prompt(), "1 stale") || !strings.Contains(p.gate.prompt(), "s skip") {
		t.Fatalf("a stale run is not asked about")
	}
	if cmd := p.key("s"); cmd == nil || p.gate != nil {
		t.Error("s does not open the comparison with the stale run as it is")
	}
}

// b saves the runs as the baseline, which the run box then shows changes
// against; -against reads one back.
func TestBaseline(t *testing.T) {
	p := loadPicker(t)
	p.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if p.key("b"); !strings.Contains(p.note, "no runs") {
		t.Errorf("b with no runs: %q", p.note)
	}
	fakeRun(t, p, 0, "", "2")
	fakeRun(t, p, 0, "off", "4")
	p.key("b")
	if len(p.baseline) != 2 || !strings.Contains(p.note, "baseline: 2 runs saved") || !runs.IsFile(runs.BaselineFile(p.outDir)) {
		t.Fatalf("b: %d rows, note %q", len(p.baseline), p.note)
	}
	fakeRun(t, p, 0, "", "3")
	p.results = map[string]*runs.Result{}
	title, lines := p.runBox(60)
	text := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(title, "Δ baseline") || !strings.Contains(text, "3.0 min +1.0") {
		t.Errorf("the run box against the baseline: %q\n%s", title, text)
	}
	p.choose(1)
	fakeRun(t, p, 1, "", "3")
	if _, lines := p.runBox(60); !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "no baseline run") {
		t.Error("a run the baseline lacks does not say so")
	}

	q := loadPicker(t)
	if err := q.loadBaseline(runs.BaselineFile(p.outDir)); err != nil || len(q.baseline) != 2 {
		t.Errorf("-against: %d rows, %v", len(q.baseline), err)
	}
}
