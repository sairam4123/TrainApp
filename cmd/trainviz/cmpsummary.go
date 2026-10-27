package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"trainapp/visualization/compare"
	"trainapp/visualization/tui"
)

// The compare summary: the runs list, picking which show; the main figures
// in boxes; then every whole-run figure, and the delay by cause.

// summaryBoxes are the figure boxes across the top: a title, then each
// figure's key and label. "on_time" is the widest punctuality threshold.
var summaryBoxes = []struct {
	title string
	keys  [][2]string
}{
	{"delay", [][2]string{{"delay_mean", "mean"}, {"delay_weighted", "weighted"}, {"on_time", "on time"}}},
	{"disruption", [][2]string{{"primary", "primary"}, {"reactionary", "knock-on"}, {"reactionary_share", "knock-on %"}}},
	{"dispatching", [][2]string{{"holds", "holds"}, {"held_min", "held min"}, {"platform_changes", "pf changes"}, {"wait_per_train", "wait/train"}}},
	{"line", [][2]string{{"block_occ_mean", "blocks"}, {"platform_occ_mean", "platforms"}, {"standing", "in section"}}},
}

// figure finds a figure by key; "on_time" is the widest threshold's.
func (m *cmpModel) figure(key string) (compare.Figure, bool) {
	if key != "on_time" {
		return compare.Find(m.figs, key)
	}
	var last compare.Figure
	found := false
	for _, f := range m.figs {
		if strings.HasPrefix(f.Key, "on_time_") {
			last, found = f, true
		}
	}
	return last, found
}

// summaryPanels are the figure boxes, each figure as A, then each other
// run with its Δ.
func (m *cmpModel) summaryPanels() []panel {
	var out []panel
	for _, b := range summaryBoxes {
		var lines []string
		for _, k := range b.keys {
			f, ok := m.figure(k[0])
			if !ok {
				continue
			}
			v := valueCell(f, 0)
			for i := 1; i < len(m.shown); i++ {
				v += dim.Render(" · ") + valueCell(f, i) + " " + deltaCell(f, i)
			}
			lines = append(lines, tui.Figure(k[1], v))
		}
		if len(lines) > 0 {
			out = append(out, panel{title: b.title, lines: lines})
		}
	}
	return out
}

// runsTable is a row per run on the command line.
func (m *cmpModel) runsTable() table {
	widths := []int{3, 26, 6, 8, 9, 18}
	right := []bool{false, false, false, true, true, false}
	t := table{header: cols(widths, right, " ", "run", "world", "trains", "finished", "against A")}
	for i, r := range m.runs {
		name := r.name
		if m.letterOf(i) != "" {
			name = bold.Render(name)
		}
		t.rows = append(t.rows, cols(widths, right, " "+bold.Render(m.letterOf(i)), name, fmt.Sprintf("W%d", r.group),
			fmt.Sprint(r.kpis.Trains), fmt.Sprint(r.kpis.Finished), m.kindOf(i)))
		t.faint = append(t.faint, m.letterOf(i) == "")
	}
	return t
}

// figuresTable is a row per whole-run figure: each run's, and each other
// run's Δ.
func (m *cmpModel) figuresTable() table {
	widths, right := []int{22}, []bool{false}
	head := []string{" figure"}
	for k := range m.shown {
		widths, right = append(widths, 16), append(right, true)
		head = append(head, m.runHead(k, 16))
		if k > 0 {
			widths, right = append(widths, 9), append(right, true)
			head = append(head, "Δ"+letters[k])
		}
	}
	t := table{header: cols(widths, right, head...), empty: " no figures"}
	for _, f := range m.figs {
		cells := []string{" " + f.Name}
		for k := range m.shown {
			cells = append(cells, valueCell(f, k))
			if k > 0 {
				cells = append(cells, deltaCell(f, k))
			}
		}
		t.rows = append(t.rows, cols(widths, right, cells...))
	}
	return t
}

// causesTable is the delay by cause in each run, the largest change first,
// with a bar for it: green where it fell, red where it rose.
func (m *cmpModel) causesTable() table {
	widths, right := []int{22}, []bool{false}
	head := []string{" cause"}
	for k := range m.shown {
		widths, right = append(widths, 8), append(right, true)
		head = append(head, letters[k])
		if k > 0 {
			widths, right = append(widths, 8), append(right, true)
			head = append(head, "Δ"+letters[k])
		}
	}
	t := table{header: cols(widths, right, head...), empty: " no delay causes in the KPIs"}
	most := 0.0
	for _, c := range m.causes {
		most = max(most, abs64(maxDelta(c.Min)))
	}
	for _, c := range m.causes {
		name := c.Cause
		if c.Kind != "" {
			name += " · " + human(c.Kind)
		}
		cells := []string{" " + name}
		for k := range m.shown {
			cells = append(cells, mins(c.Min[k]))
			if k > 0 {
				cells = append(cells, delta(c.Min[0], c.Min[k], true, true, compare.Minutes, compare.Lower))
			}
		}
		b := ""
		if d := maxDelta(c.Min); most > 0 && len(m.shown) > 1 {
			b = bar(abs64(d)/most, 12, map[bool]string{true: "42", false: "203"}[d < 0])
		}
		t.rows = append(t.rows, cols(widths, right, cells...)+b)
	}
	return t
}

func (m *cmpModel) summaryPage(w, h int) []string {
	runs, figs, causes := m.runsTable(), m.figuresTable(), m.causesTable()
	box := func(tbl int, title string, t table, w, h int) []string {
		sel := m.sSel[tbl]
		if m.plain {
			sel = -1
		}
		return tableBox(title, t, sel, m.sFocus == tbl, &m.sOff[tbl], w, h)
	}
	runsH := len(runs.rows) + 3
	if h > 0 {
		runsH = min(runsH, max(h/4, 5))
	}
	out := box(sRuns, fmt.Sprintf("runs · %d shown of %d", len(m.shown), len(m.runs)), runs, w, runsH)
	out = append(out, boxRows(w, h/4, m.summaryPanels())...)
	figsTitle, causesTitle := "figures · Δ against A", "delay by cause · min"
	if !m.oneWorld {
		figsTitle += " · runs on different worlds"
	}
	figsH, causesH := len(figs.rows)+3, len(causes.rows)+3
	// the causes beside the figures if both fit: the causes as wide as
	// their columns and bar
	cw := ansi.StringWidth(causes.header) + 12 + 4
	if w-cw >= ansi.StringWidth(figs.header)+4 {
		body := max(figsH, causesH)
		if h > 0 {
			body = max(h-len(out), 6)
		}
		return append(out, beside(box(sFigures, figsTitle, figs, w-cw, body), box(sCauses, causesTitle, causes, cw, body), w-cw, cw)...)
	}
	if h > 0 {
		body := max(h-len(out), 8)
		causesH = min(causesH, body/3)
		figsH = body - causesH
	}
	out = append(out, box(sFigures, figsTitle, figs, w, figsH)...)
	return append(out, box(sCauses, causesTitle, causes, w, causesH)...)
}

func (m *cmpModel) summaryKey(key string) {
	rows := map[int]int{sRuns: len(m.runs), sFigures: len(m.figs), sCauses: len(m.causes)}
	switch key {
	case "tab":
		m.sFocus = (m.sFocus + 1) % sTables
	case "shift+tab":
		m.sFocus = (m.sFocus + sTables - 1) % sTables
	case "esc":
		m.sFocus = sRuns
	case "space":
		if m.sFocus == sRuns {
			m.toggle(m.sSel[sRuns])
			return
		}
		tableMove(key, &m.sSel[m.sFocus], rows[m.sFocus], m.pageRows())
	case "b":
		if m.sFocus == sRuns {
			m.setBaseline(m.sSel[sRuns])
		}
	default:
		tableMove(key, &m.sSel[m.sFocus], rows[m.sFocus], m.pageRows())
	}
}
