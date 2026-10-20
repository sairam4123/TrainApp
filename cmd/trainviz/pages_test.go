package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"trainapp/railway/kpi"
	"trainapp/visualization/schematic"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// bandedConfig colours the KPI pages as the IR profile does.
func bandedConfig() kpi.Config {
	cfg := kpi.DefaultConfig()
	cfg.CapacityColorBand = []kpi.CapacityBand{{OverPct: 0, Name: "light", Color: "39"}, {OverPct: 40, Name: "moderate"}, {OverPct: 75, Name: "saturated", Color: "203"}}
	cfg.DelayColorBand = []kpi.DelayBand{{OverMin: 0, Name: "on time", Color: "42"}, {OverMin: 5, Name: "late", Color: "203"}}
	return cfg
}

func newKPIModel(t *testing.T) *model {
	t.Helper()
	hello, msgs, r := recordingWithKPIs(t, bandedConfig())
	d, err := schematic.Build(&hello.World)
	if err != nil {
		t.Fatal(err)
	}
	rep := newReplay(hello, msgs)
	m := newModel(d, rep)
	m.st.set(r, rep)
	m.name = "test-run"
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return m
}

// fits checks the screen fills the window and no more.
func fits(t *testing.T, m *model) {
	t.Helper()
	lines := strings.Split(m.render(), "\n")
	if len(lines) != m.height {
		t.Errorf("%d lines, want %d:\n%s", len(lines), m.height, screen(m))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
}

// Every page has the tab bar on top and ends its keys alike.
func TestTabBarAndKeyLine(t *testing.T) {
	m := newKPIModel(t)
	for _, k := range []string{"1", "2", "3", "4", "v"} {
		press(m, k)
		fits(t, m)
		lines := strings.Split(screen(m), "\n")
		if !strings.Contains(lines[0], "1 Schematic") || !strings.Contains(lines[0], "4 Occupancy") || !strings.Contains(lines[0], "trainviz · test-run") {
			t.Errorf("after %s, the top line is not the tab bar: %q", k, lines[0])
		}
		if last := strings.TrimRight(lines[len(lines)-1], " "); !strings.HasSuffix(last, "1-4 tabs  q quit") {
			t.Errorf("after %s, the key line is %q", k, last)
		}
	}
	if !strings.Contains(strings.Split(screen(m), "\n")[0], "Occupancy · tables") {
		t.Error("the tab bar does not say the occupancy tables show")
	}
	// a click on a tab shows its page
	x := strings.Index(ansi.Strip(m.header()), "Delay")
	m.Update(tea.MouseClickMsg{X: x, Y: 0, Button: tea.MouseLeft})
	if m.page != pageDelay {
		t.Errorf("page %d after clicking the delay tab", m.page)
	}
}

func TestKPIPagesWithoutKPIs(t *testing.T) {
	m := newReplayModel(t)
	for _, p := range []string{"3", "4"} {
		press(m, p)
		fits(t, m)
		if !strings.Contains(screen(m), "no KPIs for this recording") {
			t.Errorf("page %s:\n%s", p, screen(m))
		}
	}
	press(m, "1")
	if m.page != pageSchematic {
		t.Errorf("page %d after 1", m.page)
	}
}

func TestReadReport(t *testing.T) {
	_, _, r := recordingWithKPIs(t, bandedConfig())
	dir := t.TempDir()
	rec := filepath.Join(dir, "run.ndjson")
	data, err := json.Marshal(map[string]*kpi.Report{"run": r})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kpiPath(rec), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readReport(filepath.Join(dir, "run.kpi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Trains != r.Trains || len(got.Utilization.Bands) != 3 || len(got.DelayBands) != 2 {
		t.Errorf("read %d trains, %d capacity and %d delay bands", got.Trains, len(got.Utilization.Bands), len(got.DelayBands))
	}
	if _, err := readReport(filepath.Join(dir, "none.kpi.json")); err == nil {
		t.Error("no error for a missing file")
	}
}
