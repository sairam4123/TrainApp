package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/kpi"
	"trainapp/visualization/vizproto"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// writeRun writes a recording and its KPIs to dir as name.ndjson and
// name.kpi.json, as trainapp -record and -kpi do.
func writeRun(t *testing.T, dir, name string, hello *vizproto.HelloMsg, msgs []vizproto.Message, r *kpi.Report) string {
	t.Helper()
	path := filepath.Join(dir, name+".ndjson")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := vizproto.NewWriter(f)
	for _, m := range append([]vizproto.Message{hello}, msgs...) {
		if err := w.Write(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	data, err := json.Marshal(map[string]*kpi.Report{"run": r})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kpiPath(path), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// copyReport is a deep copy of a report.
func copyReport(t *testing.T, r *kpi.Report) *kpi.Report {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out kpi.Report
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

// compareRuns writes four runs of the KPI fixture: run-a as it ran; run-b
// with every other train 5 min later and the first block busier; run-c as
// run-a; and other-d on a world with a station less.
func compareRuns(t *testing.T) []string {
	t.Helper()
	hello, msgs, r := recordingWithKPIs(t, bandedConfig())
	dir := t.TempDir()
	b := copyReport(t, r)
	for i := range b.PerTrain {
		if d := b.PerTrain[i].DelayMin; d != nil && i%2 == 0 {
			v := *d + 5
			b.PerTrain[i].DelayMin = &v
		}
	}
	b.Delay.MeanMin += 2.5
	for i := range b.Utilization.Tracks {
		if b.Utilization.Tracks[i].Kind == kpi.KindBlock {
			b.Utilization.Tracks[i].Occupancy += 0.2
			break
		}
	}
	var other vizproto.HelloMsg
	data, _ := json.Marshal(hello)
	json.Unmarshal(data, &other)
	other.World.Stations = other.World.Stations[:len(other.World.Stations)-1]
	return []string{
		writeRun(t, dir, "run-a", hello, msgs, r),
		writeRun(t, dir, "run-b", hello, msgs, b),
		writeRun(t, dir, "run-c", hello, msgs, r),
		writeRun(t, dir, "other-d", &other, msgs, r),
	}
}

func newCompareModel(t *testing.T, paths []string, shown ...int) *cmpModel {
	t.Helper()
	var runs []*cmpRun
	for _, p := range paths {
		r, err := loadRun(p)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, r)
	}
	m := newCompare(runs, shown)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return m
}

func cmpScreen(m *cmpModel) string { return ansi.Strip(m.render()) }

// cmpFits checks the screen fills the window and no more, with the tab bar
// on top and the shared keys at the end.
func cmpFits(t *testing.T, m *cmpModel) {
	t.Helper()
	lines := strings.Split(m.render(), "\n")
	if len(lines) != m.height {
		t.Errorf("%d lines, want %d:\n%s", len(lines), m.height, cmpScreen(m))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
	plain := strings.Split(cmpScreen(m), "\n")
	if !strings.Contains(plain[0], "1 Summary") || !strings.Contains(plain[0], "trainviz compare") {
		t.Errorf("the top line is not the tab bar: %q", plain[0])
	}
	if last := strings.TrimRight(plain[len(plain)-1], " "); !strings.HasSuffix(last, "1-4 tabs  q quit") {
		t.Errorf("the key line is %q", last)
	}
}

func TestComparePagesFit(t *testing.T) {
	m := newCompareModel(t, compareRuns(t)[:3], 0, 1, 2)
	for _, k := range []string{"1", "2", "3", "v", "4"} {
		press(m, k)
		cmpFits(t, m)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, k := range []string{"1", "2", "3", "v", "4"} {
		press(m, k)
		cmpFits(t, m)
	}
}

func TestCompareSummary(t *testing.T) {
	m := newCompareModel(t, compareRuns(t)[:2], 0, 1)
	s := cmpScreen(m)
	for _, want := range []string{"runs · 2 shown of 2", "A run-a", "B run-b", "same trains", "figures · Δ against A", "mean delay", "+2.5", "delay by cause"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q on the summary:\n%s", want, s)
		}
	}
	if !strings.Contains(strings.Split(s, "\n")[0], "run · a ⟷ b") {
		t.Errorf("the shared prefix is not shown once: %q", strings.Split(s, "\n")[0])
	}
}

func TestCompareRunsList(t *testing.T) {
	m := newCompareModel(t, compareRuns(t), 0, 1, 2)
	press(m, "down", "down", "down", "space")
	if len(m.shown) != 3 || !strings.Contains(m.note, "3 runs at most") {
		t.Errorf("a fourth run shows: %v, note %q", m.shown, m.note)
	}
	press(m, "up", "space")
	if !slices.Equal(m.shown, []int{0, 1}) {
		t.Errorf("hiding run-c leaves %v", m.shown)
	}
	// other-d as the baseline: the others are on another world
	press(m, "down", "b")
	if !slices.Equal(m.shown, []int{3, 0, 1}) || m.oneWorld {
		t.Errorf("baseline other-d: shown %v, one world %v", m.shown, m.oneWorld)
	}
	if !strings.Contains(cmpScreen(m), "other world") {
		t.Error("the runs list does not say the others are on another world")
	}
	press(m, "3")
	if m.page != cSummary || !strings.Contains(m.note, "different worlds") {
		t.Errorf("the occupancy page opens across worlds: page %d, note %q", m.page, m.note)
	}
	press(m, "2")
	s := cmpScreen(m)
	if !strings.Contains(s, "shared stations") || !strings.Contains(s, "only in B") {
		t.Errorf("the delay page across worlds:\n%s", s)
	}
	cmpFits(t, m)
}

func TestCompareDelay(t *testing.T) {
	m := newCompareModel(t, compareRuns(t)[:2], 0, 1)
	press(m, "2")
	s := cmpScreen(m)
	for _, want := range []string{"trains · all", "by largest change", "delay A", "ΔB", "+5.0", "train · ", "stations · Δ against A", "waits by place"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %q on the delay page:\n%s", want, s)
		}
	}
	all := len(m.cmpTrains())
	press(m, "f")
	if n := len(m.cmpTrains()); n == 0 || n >= all {
		t.Errorf("changed trains: %d of %d", n, all)
	}
	press(m, "enter")
	if m.ask == nil || !strings.Contains(m.keyLine(), "a run-a") {
		t.Fatalf("enter does not ask for the run: %q", ansi.Strip(m.keyLine()))
	}
	press(m, "esc")
	if m.ask != nil {
		t.Error("esc does not cancel the question")
	}
}

func TestCompareOccupancy(t *testing.T) {
	m := newCompareModel(t, compareRuns(t)[:2], 0, 1)
	press(m, "3")
	s := cmpScreen(m)
	if !strings.Contains(s, "occupancy · Δ occupied · B − A") || !strings.Contains(s, "+20") {
		t.Errorf("the occupancy schematic:\n%s", s)
	}
	heat := m.deltaHeat()
	if len(heat) == 0 {
		t.Fatal("no track coloured")
	}
	more := 0
	for _, h := range heat {
		if slices.Contains(moreRamp, h.Color) {
			more++
		}
	}
	if more != 1 {
		t.Errorf("%d tracks coloured busier, want the one block", more)
	}
	press(m, "m")
	if m.oMetric != 1 {
		t.Error("m does not change the metric")
	}
	press(m, "v")
	if !m.oTable || !strings.Contains(strings.Split(cmpScreen(m), "\n")[0], "Occupancy · tables") {
		t.Error("v does not show the tables")
	}
	press(m, "enter")
	if m.oTable {
		t.Error("enter on a block does not go back to the schematic")
	}
}

func TestCompareGraph(t *testing.T) {
	m := newCompareModel(t, compareRuns(t)[:2], 0, 1)
	press(m, "4")
	if m.page != cGraph {
		t.Fatalf("no graph page: %q", m.note)
	}
	if s := cmpScreen(m); !strings.Contains(s, "A faint, B over it") {
		t.Errorf("the graph:\n%s", s)
	}
	press(m, "tab")
	if s := cmpScreen(m); m.gTrain != 0 || !strings.Contains(s, "(A dotted)") {
		t.Errorf("tab does not pick a train: %d", m.gTrain)
	}
	press(m, "o")
	if s := cmpScreen(m); !strings.Contains(s, "A alone") {
		t.Error("o does not show A alone")
	}
}

func TestComparePrint(t *testing.T) {
	m := newCompareModel(t, compareRuns(t)[:3], 0, 1, 2)
	m.plain, m.sFocus, m.dFocus, m.oFocus = true, -1, -1, -1
	for _, lines := range [][]string{m.summaryPage(140, 0), m.delayPage(140, 0), m.occTablePage(140, 0)} {
		if len(lines) < 5 {
			t.Errorf("printed page:\n%s", strings.Join(lines, "\n"))
		}
		for _, l := range lines {
			if strings.Contains(ansi.Strip(l), "▸") {
				t.Error("a row is picked out when printing")
			}
		}
	}
}

func TestParseShowAndPrefix(t *testing.T) {
	if s, err := parseShow("", 5); err != nil || !slices.Equal(s, []int{0, 1, 2}) {
		t.Errorf("default %v %v", s, err)
	}
	if s, err := parseShow("3,1", 4); err != nil || !slices.Equal(s, []int{2, 0}) {
		t.Errorf("3,1: %v %v", s, err)
	}
	for _, bad := range []string{"0", "5", "1,1", "1,2,3,4", "x"} {
		if _, err := parseShow(bad, 4); err == nil {
			t.Errorf("%q is taken", bad)
		}
	}
	for _, c := range []struct {
		names []string
		want  string
	}{{[]string{"sl-6-profile", "sl-6-off"}, "sl-6-"}, {[]string{"sl-6-profile", "dl-12-profile"}, ""}, {[]string{"sl-6", "sl-6-off"}, ""}} {
		if got := commonPrefix(c.names); got != c.want {
			t.Errorf("%v: %q, want %q", c.names, got, c.want)
		}
	}
}
