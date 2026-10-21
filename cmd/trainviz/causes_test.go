package main

import (
	"math"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/kpi/report"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCausesIsTheThirdView(t *testing.T) {
	m := newKPIModel(t)
	press(m, "3", "v", "v")
	if !m.st.dCauses || m.st.dMap || !strings.Contains(m.header(), "Delay · causes") {
		t.Fatalf("v v from the delay tables does not show the causes:\n%s", screen(m))
	}
	fits(t, m)
	for _, want := range []string{"pareto · delay by place", "causes · all", "▶ delay", "losses ·", "vital few"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("causes view lacks %q:\n%s", want, screen(m))
		}
	}
	press(m, "v")
	if m.st.dCauses || !strings.Contains(screen(m), "trains · by delay") {
		t.Errorf("v does not go back to the tables:\n%s", screen(m))
	}

	press(m, "4", "v", "v")
	if !m.st.oCauses || m.kpiMap() || !strings.Contains(m.header(), "Occupancy · causes") {
		t.Fatalf("v v from the occupancy schematic does not show the causes:\n%s", screen(m))
	}
	fits(t, m)
	press(m, "v")
	if m.st.oCauses || m.st.table || !m.kpiMap() {
		t.Errorf("v does not go back to the schematic")
	}
}

func TestPareto(t *testing.T) {
	items := []causeItem{
		{place: "A", min: 1}, {place: "B", min: 6}, {place: "C", min: 2}, {place: "B", min: 1}, {place: "D", min: -3}, {place: "E", min: 0},
	}
	rows := pareto(items, ranking{})
	var keys []string
	for _, r := range rows {
		keys = append(keys, r.key)
	}
	if !slices.Equal(keys, []string{"B", "C", "A"}) {
		t.Fatalf("rows %v, want B C A: most first, none made up or empty", keys)
	}
	if rows[0].min != 7 || math.Abs(rows[len(rows)-1].cum-1) > 1e-9 || math.Abs(rows[0].share-0.7) > 1e-9 {
		t.Errorf("rows %+v", rows)
	}
	if n := cut(rows, 80); n != 2 {
		t.Errorf("80%% cut after %d rows, want 2 (70%%, 90%%)", n)
	}
	if n := cut(rows, 70); n != 1 {
		t.Errorf("70%% cut after %d rows, want 1", n)
	}

	m := newKPIModel(t)
	m.st.rep.ParetoPct = 50
	press(m, "3", "v", "v")
	if !strings.Contains(screen(m), "make 50%") || !strings.Contains(screen(m), "◂ 50%") {
		t.Errorf("pareto_pct 50 does not move the cut:\n%s", screen(m))
	}
}

func TestFishbone(t *testing.T) {
	bs := []bone{{cause: "big", min: 6}, {cause: "middle", min: 3}, {cause: "small", min: 1}}
	lines := fishbone(100, 2, "delay 10", bs, 0, false, func(b bone) (string, string, bool) { return b.cause, mins(b.min), false })
	if len(lines) != fishboneRows(2) {
		t.Fatalf("%d rows, want %d", len(lines), fishboneRows(2))
	}
	top, bottom := ansi.Strip(lines[0]), ansi.Strip(lines[len(lines)-1])
	if !(strings.Index(top, "big") > strings.Index(top, "small")) || !strings.Contains(bottom, "middle") {
		t.Errorf("want big nearest the head above, middle below, small further out:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(ansi.Strip(lines[3]), "▶ delay 10") {
		t.Errorf("no head on the spine:\n%s", strings.Join(lines, "\n"))
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w != 100 {
			t.Errorf("line %d wide, want 100", w)
		}
	}

	// the bones add up to the head, at every width
	m := newKPIModel(t)
	items := m.st.delayItems(false)
	total, sum := 0.0, 0.0
	for _, it := range items {
		total += it.min
	}
	for _, b := range bones(items, true) {
		sum += b.min
	}
	if math.Abs(total-sum) > 1e-6 || total <= 0 {
		t.Errorf("bones %.3f, items %.3f", sum, total)
	}
	press(m, "3", "v", "v")
	for _, w := range []int{80, 120, 160} {
		m.Update(tea.WindowSizeMsg{Width: w, Height: 40})
		fits(t, m)
	}
}

func TestCausesNarrowAndShow(t *testing.T) {
	m := newKPIModel(t)
	press(m, "3", "v", "v")
	rows := pareto(m.st.delayItems(false), delayRankings[0])
	if len(rows) == 0 {
		t.Fatal("no delay in the fixture")
	}
	press(m, "enter")
	if m.st.dc.narrow != rows[0].key || !strings.Contains(screen(m), "causes · "+rows[0].key) || !strings.Contains(screen(m), "▶ "+rows[0].key) {
		t.Fatalf("enter does not narrow to %s:\n%s", rows[0].key, screen(m))
	}
	press(m, "esc")
	if m.st.dc.narrow != "" {
		t.Errorf("esc does not widen")
	}

	// the losses of the biggest cause; enter shows the first
	press(m, "tab", "tab")
	if m.st.dc.focus != cLosses {
		t.Fatalf("focus %d, want the losses", m.st.dc.focus)
	}
	bs := bones(m.st.delayItems(false), true)
	first := lossItems(m.st.delayItems(false), bs[0].cause)[0]
	press(m, "enter")
	if m.page != pageSchematic || m.selectedTrain() != first.train || m.rep.t != first.from {
		t.Errorf("page %d, train %q at %.2f; want the schematic, %s at %.2f", m.page, m.selectedTrain(), m.rep.t, first.train, first.from)
	}
}

func TestCausesByCauseAndTracks(t *testing.T) {
	m := newKPIModel(t)
	press(m, "3", "v", "v", "c")
	if !strings.Contains(screen(m), "delay by cause") {
		t.Fatalf("c does not rank by cause:\n%s", screen(m))
	}
	press(m, "enter")
	if m.st.dc.focus != cBones {
		t.Errorf("enter on a cause does not pick its bone")
	}
	press(m, "c", "t")
	kinds := map[string]bool{}
	for _, r := range pareto(m.st.items(true), delayRankings[0]) {
		kinds[r.kind] = true
	}
	for k := range kinds {
		if !slices.Contains([]string{report.KindBlock, report.KindThroat, report.KindPlatform, report.WhereEntry}, k) {
			t.Errorf("track grain has a %q row: %v", k, kinds)
		}
	}
	if !kinds[report.KindPlatform] {
		t.Errorf("no platform tracks at the track grain: %v", kinds)
	}
}

func TestOccupancyCauses(t *testing.T) {
	m := newKPIModel(t)
	items := m.st.occItems(false)
	occupied, sum := 0.0, 0.0
	for _, tu := range m.st.rep.Utilization.Tracks {
		occupied += tu.OccupiedMin
	}
	for _, b := range bones(items, false) {
		sum += b.min
	}
	if math.Abs(occupied-sum) > 1e-6 {
		t.Errorf("bones %.3f, occupied %.3f", sum, occupied)
	}
	bs := bones(items, false)
	if needed(bs[0].cause) && !needed(bs[len(bs)-1].cause) {
		t.Errorf("needed bones before the lost: %+v", bs)
	}
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 44})
	press(m, "4", "v", "v")
	fits(t, m)
	for _, want := range []string{"occupied by place", "· needed", "% lost", "most lost"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("occupancy causes lack %q:\n%s", want, screen(m))
		}
	}
	press(m, "c")
	if !strings.Contains(screen(m), "lost by place") {
		t.Errorf("c does not rank by lost minutes")
	}
}

func TestCausesPrint(t *testing.T) {
	m := newKPIModel(t)
	for _, delay := range []bool{true, false} {
		m.st.plain = true
		m.st.view(delay).focus = -1
		lines := m.st.causesPage(delay, 120, 0, m.rep)
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > 120 {
				t.Errorf("line %d is %d wide", i, w)
			}
		}
		if s := ansi.Strip(strings.Join(lines, "\n")); !strings.Contains(s, "pareto ·") || strings.Contains(s, "▸") {
			t.Errorf("printed causes:\n%s", s)
		}
	}
}

func TestCausesOfAnOlderFile(t *testing.T) {
	m := newKPIModel(t)
	for i := range m.st.rep.PerTrain {
		m.st.rep.PerTrain[i].Losses = nil
	}
	for i := range m.st.rep.Utilization.Tracks {
		m.st.rep.Utilization.Tracks[i].Split = nil
	}
	press(m, "3", "v", "v")
	if !strings.Contains(screen(m), "rerun trainapp -kpi") {
		t.Errorf("no word of rerunning:\n%s", screen(m))
	}
	fits(t, m)
	press(m, "v")
	if m.st.dCauses {
		t.Error("v does not leave the empty causes view")
	}
}
