package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"

	"charm.land/lipgloss/v2"
)

// The causes view of the delay and occupancy pages: a Pareto chart of
// where the minutes went, a fishbone diagram of why, and the losses behind
// the picked bone. Both pages feed it the same way, as causeItems, so one
// drawing serves both.

// The causes view's parts, in the order tab moves between them.
const (
	cPareto = iota
	cBones
	cLosses
	cParts
)

// causesView is where a page's causes view is.
type causesView struct {
	focus  int
	sel    [cParts]int
	off    [cParts]int
	rank   int    // into the page's rankings
	tracks bool   // t: tracks rather than stations and sections
	narrow string // the place enter narrowed the fishbone to; "" for all
}

// causeItem is minutes of one cause at one place: a piece of a train's
// delay, or of a track's occupied time.
type causeItem struct {
	place, kind string // the place at the grain in use, and its kind
	cause, rib  string // the bone, and the rib under it
	sub         string // for ranking by cause: the cause and its kind
	detail      string // for the losses table
	train       string // delay: the train that lost it
	track       string // the track it was on, if known
	at          string // delay: the station or section, to show it by
	from, min   float64
	of          float64 // occupancy: the track's occupied minutes
	lost        bool    // occupancy: lost, not needed
}

// bones are the fishbone's bones, in a fixed order before sorting.
var (
	delayBones = []string{report.CauseDisruption, report.CauseHeld, report.CauseSignal, report.CauseStation, report.CauseEntry, report.CauseRunning}
	occBones   = []string{report.CauseFree, report.CauseDwell, report.CauseSlower, report.CauseHeld, report.CauseSignal, report.CauseStation, report.CauseDisruption}
)

// causeColors are the bones' colours: held is blue, never amber.
var causeColors = map[string]string{
	report.CauseDisruption: "203",
	report.CauseHeld:       "33",
	report.CauseSignal:     "141",
	report.CauseStation:    "44",
	report.CauseEntry:      "180",
	report.CauseRunning:    "149",
	report.CauseSlower:     "149",
	report.CauseFree:       "245",
	report.CauseDwell:      "245",
}

// minShown is the fewest minutes a rib or a loss is listed with: less
// shows as 0.0.
const minShown = 0.05

// needed is whether occupied time of the cause is what the timetable needs.
func needed(cause string) bool { return cause == report.CauseFree || cause == report.CauseDwell }

// human writes a kind as words: "late_entry" is "late entry".
func human(kind string) string { return strings.ReplaceAll(kind, "_", " ") }

// view is the page's causes view.
func (s *stats) view(delay bool) *causesView {
	if delay {
		return &s.dc
	}
	return &s.oc
}

// hasCauses is whether the KPI file has causes for the page.
func (s *stats) hasCauses(delay bool) bool {
	if delay {
		for _, t := range s.rep.PerTrain {
			if len(t.Losses) > 0 {
				return true
			}
		}
		return false
	}
	for _, tu := range s.rep.Utilization.Tracks {
		if len(tu.Split) > 0 {
			return true
		}
	}
	return false
}

// trackKinds are the tracks' kinds by name.
func (s *stats) trackKinds() map[string]string {
	out := map[string]string{}
	for _, tu := range s.rep.Utilization.Tracks {
		out[tu.Track] = tu.Kind
	}
	return out
}

// delayItems are the losses of the trains the delay filter keeps.
func (s *stats) delayItems(tracks bool) []causeItem {
	kinds := s.trackKinds()
	f := s.filter()
	var out []causeItem
	for _, t := range s.rep.PerTrain {
		if t.DelayMin == nil || !f.admits(t.DelayMin) {
			continue
		}
		for _, l := range t.Losses {
			it := causeItem{place: l.At, kind: l.Where, cause: l.Cause, train: t.Train, track: l.Track, at: l.At, from: l.FromMin, min: l.Min, sub: l.Cause}
			if l.Where == report.WhereEntry {
				it.place = "entry " + l.At
			}
			if tracks && l.Track != "" {
				it.place, it.kind = l.Track, kinds[l.Track]
			}
			who := l.For
			if who == "" {
				who = "?"
			}
			switch l.Cause {
			case report.CauseDisruption:
				it.rib, it.sub = human(l.Kind)+" "+l.At, l.Cause+" · "+human(l.Kind)
			case report.CauseHeld:
				it.rib, it.sub = human(l.Kind)+" for "+who, l.Cause+" · "+human(l.Kind)
			case report.CauseSignal, report.CauseStation:
				it.rib = "for " + who
			case report.CauseEntry:
				it.rib = "at " + l.At
			case report.CauseRunning:
				it.rib, it.sub = it.place, l.Cause+" · "+l.Kind
				if l.Kind == report.RunDwell {
					it.rib = it.place + " dwell"
				}
			}
			out = append(out, it)
		}
	}
	return out
}

// occItems are the tracks' occupied minutes by cause.
func (s *stats) occItems(tracks bool) []causeItem {
	var out []causeItem
	for _, tu := range s.rep.Utilization.Tracks {
		if tu.Entries == 0 {
			continue
		}
		place, kind := tu.Stn, "station"
		switch {
		case tracks:
			place, kind = tu.Track, tu.Kind
		case tu.Kind == report.KindBlock:
			place, kind = cmp.Or(tu.Section, tu.Track), "section"
		}
		for _, sh := range tu.Split {
			it := causeItem{place: place, kind: kind, cause: sh.Cause, rib: place, sub: sh.Cause, track: tu.Track, min: sh.Min, of: tu.OccupiedMin, lost: !needed(sh.Cause)}
			if sh.Kind != "" {
				it.rib, it.sub, it.detail = human(sh.Kind), sh.Cause+" · "+human(sh.Kind), human(sh.Kind)
			}
			out = append(out, it)
		}
	}
	return out
}

// ranking is a way of ranking the Pareto chart.
type ranking struct {
	name     string
	byCause  bool // group by cause rather than place
	lostOnly bool // occupancy: lost minutes only
}

var (
	delayRankings = []ranking{{name: "delay by place"}, {name: "delay by cause", byCause: true}}
	occRankings   = []ranking{{name: "occupied by place"}, {name: "lost by place", lostOnly: true}, {name: "lost by cause", byCause: true, lostOnly: true}}
)

func rankings(delay bool) []ranking {
	if delay {
		return delayRankings
	}
	return occRankings
}

// items are the page's items at the view's grain.
func (s *stats) items(delay bool) []causeItem {
	if delay {
		return s.delayItems(s.dc.tracks)
	}
	return s.occItems(s.oc.tracks)
}

// madeUp is the time the trains the delay filter keeps made up: their
// losses of less than none, by station and section.
func (s *stats) madeUp() float64 {
	out := 0.0
	for _, it := range s.delayItems(false) {
		if it.min < 0 {
			out -= it.min
		}
	}
	return out
}

// paretoRow is a row of the Pareto chart.
type paretoRow struct {
	key, kind  string
	min        float64
	share, cum float64
}

// pareto groups the items by the ranking and ranks the groups with minutes
// to them, most first, with each one's share and the running total. Groups
// that come to less than none, where time was made up, are left out.
func pareto(items []causeItem, r ranking) (rows []paretoRow) {
	by := map[string]*paretoRow{}
	var keys []string
	for _, it := range items {
		if r.lostOnly && !it.lost {
			continue
		}
		k, kind := it.place, it.kind
		if r.byCause {
			k, kind = it.sub, ""
		}
		row := by[k]
		if row == nil {
			row = &paretoRow{key: k, kind: kind}
			by[k] = row
			keys = append(keys, k)
		}
		row.min += it.min
	}
	total := 0.0
	for _, k := range keys {
		if row := by[k]; row.min > 1e-6 {
			rows = append(rows, *row)
			total += row.min
		}
	}
	slices.SortStableFunc(rows, func(a, b paretoRow) int { return cmp.Or(cmp.Compare(b.min, a.min), cmp.Compare(a.key, b.key)) })
	cum := 0.0
	for i := range rows {
		cum += rows[i].min
		if total > 0 {
			rows[i].share, rows[i].cum = rows[i].min/total, cum/total
		}
	}
	return rows
}

// cut is how many of the rows make pct percent: the vital few.
func cut(rows []paretoRow, pct float64) int {
	for i, r := range rows {
		if 100*r.cum >= pct-1e-9 {
			return i + 1
		}
	}
	return len(rows)
}

// bone is a cause on the fishbone, with its top ribs.
type bone struct {
	cause string
	min   float64
	share float64
	ribs  []paretoRow
}

// bones sums the items by cause, in the page's order of causes, the
// largest first; each with its ribs, largest first.
func bones(items []causeItem, delay bool) []bone {
	causes := occBones
	if delay {
		causes = delayBones
	}
	var out []bone
	total := 0.0
	for _, c := range causes {
		b := bone{cause: c}
		ribs := map[string]float64{}
		var order []string
		for _, it := range items {
			if it.cause != c {
				continue
			}
			b.min += it.min
			if _, ok := ribs[it.rib]; !ok {
				order = append(order, it.rib)
			}
			ribs[it.rib] += it.min
		}
		for _, r := range order {
			if m := ribs[r]; m >= minShown || m <= -minShown {
				b.ribs = append(b.ribs, paretoRow{key: r, min: m})
			}
		}
		slices.SortStableFunc(b.ribs, func(x, y paretoRow) int { return cmp.Compare(y.min, x.min) })
		if b.min > 0 {
			total += b.min
		}
		out = append(out, b)
	}
	if !delay {
		total = 0
		for _, it := range items {
			total += it.min
		}
	}
	for i := range out {
		if total > 0 {
			out[i].share = out[i].min / total
		}
	}
	// largest first; on occupancy the lost causes before the needed
	slices.SortStableFunc(out, func(x, y bone) int {
		if !delay && needed(x.cause) != needed(y.cause) {
			if needed(x.cause) {
				return 1
			}
			return -1
		}
		return cmp.Compare(y.min, x.min)
	})
	return out
}

// narrowed are the items at the place the view is narrowed to.
func narrowed(items []causeItem, place string) []causeItem {
	if place == "" {
		return items
	}
	var out []causeItem
	for _, it := range items {
		if it.place == place {
			out = append(out, it)
		}
	}
	return out
}

// --- the fishbone drawing ---

// cell is a character on the canvas, with its look.
type cell struct {
	r                rune
	color            string
	bold, faint, rev bool
}

// canvas is a grid of cells drawn into lines.
type canvas [][]cell

func newCanvas(w, h int) canvas {
	c := make(canvas, h)
	for y := range c {
		c[y] = make([]cell, w)
		for x := range c[y] {
			c[y][x].r = ' '
		}
	}
	return c
}

// put writes s from x on row y, cut at max cells, in the look of st.
func (c canvas) put(x, y, max int, s string, st cell) {
	if y < 0 || y >= len(c) {
		return
	}
	n := 0
	for _, r := range s {
		if n >= max || x >= len(c[y]) {
			break
		}
		if x >= 0 {
			st.r = r
			c[y][x] = st
		}
		x++
		n++
	}
}

func (c canvas) lines() []string {
	out := make([]string, len(c))
	for y, row := range c {
		var sb strings.Builder
		for i := 0; i < len(row); {
			j := i
			var run strings.Builder
			for j < len(row) && row[j].color == row[i].color && row[j].bold == row[i].bold && row[j].faint == row[i].faint && row[j].rev == row[i].rev {
				run.WriteRune(row[j].r)
				j++
			}
			st := lipgloss.NewStyle().Bold(row[i].bold).Faint(row[i].faint).Reverse(row[i].rev)
			if row[i].color != "" {
				st = st.Foreground(lipgloss.Color(row[i].color))
			}
			if row[i] == (cell{r: row[i].r}) {
				sb.WriteString(run.String())
			} else {
				sb.WriteString(st.Render(run.String()))
			}
			i = j
		}
		out[y] = sb.String()
	}
	return out
}

// fishboneRows is how tall the fishbone is with r ribs a bone.
func fishboneRows(r int) int { return 2*r + 3 }

// boneLabel fits a bone's name and figures ("41.5 · 31%") into w cells:
// whole, or with the minutes alone, or the name cut short before them.
func boneLabel(name, figures string, w int) string {
	if s := name + " " + figures; len([]rune(s)) <= w {
		return s
	}
	minutes, _, _ := strings.Cut(figures, " · ")
	if s := name + " " + minutes; len([]rune(s)) <= w {
		return s
	}
	room := max(w-len(minutes)-1, 1)
	return string([]rune(name)[:min(room, len([]rune(name)))]) + " " + minutes
}

// fishbone draws the bones w wide, with up to r ribs each, around a spine
// ending at the effect, head: the bones in order nearest it outwards,
// alternately above and below. sel is the bone picked out, -1 for none;
// focused reverses it. label gives a bone's name, figures, and whether it
// is drawn faint.
func fishbone(w, r int, head string, bs []bone, sel int, focused bool, label func(bone) (string, string, bool)) []string {
	c := newCanvas(w, fishboneRows(r))
	head = "▶ " + head
	spine := max(w-len([]rune(head)), 4)
	mid := r + 1
	c.put(0, mid, spine, strings.Repeat("━", spine), cell{bold: true})
	c.put(spine, mid, w-spine, head, cell{bold: true})
	if len(bs) == 0 {
		return c.lines()
	}
	cols := (len(bs) + 1) / 2
	colW := spine / cols
	for i, b := range bs {
		end := spine - (i/2)*colW // the column's right edge
		x0 := end - colW
		name, figures, faint := label(b)
		color := causeColors[b.cause]
		look := cell{color: color, faint: faint, bold: i == sel, rev: i == sel && focused}
		diag := func(row int) int { return end - 2 - row } // row 0 touches the spine
		if i%2 == 0 {
			// above: the label on top, the ribs under it, the bone sloping
			// down to the spine
			for y := 0; y <= r; y++ {
				c.put(diag(r-y), y, 1, "╲", cell{color: color, faint: faint})
			}
			c.put(x0, 0, diag(r)-x0-1, boneLabel(name, figures, diag(r)-x0-1), look)
			for k := 0; k < r && k < len(b.ribs); k++ {
				y := k + 1
				c.put(x0+1, y, diag(r-y)-x0-2, ribLabel(b.ribs[k], diag(r-y)-x0-2), cell{faint: true})
			}
		} else {
			for d := 1; d <= r+1; d++ {
				c.put(diag(d-1), mid+d, 1, "╱", cell{color: color, faint: faint})
			}
			c.put(x0, mid+r+1, diag(r)-x0-1, boneLabel(name, figures, diag(r)-x0-1), look)
			for k := 0; k < r && k < len(b.ribs); k++ {
				d := k + 1
				c.put(x0+1, mid+d, diag(d-1)-x0-2, ribLabel(b.ribs[k], diag(d-1)-x0-2), cell{faint: true})
			}
		}
	}
	return c.lines()
}

// ribLabel is a rib's name and minutes in w cells, the name cut to fit.
func ribLabel(rib paretoRow, w int) string {
	m := mins(rib.min)
	name := rib.key
	if room := w - len(m) - 1; room < len([]rune(name)) {
		name = string([]rune(name)[:max(room, 0)])
	}
	return name + strings.Repeat(" ", max(w-len([]rune(name))-len(m), 1)) + m
}

// --- the page ---

// causesPage draws the page's causes view w by h, or as tall as it needs
// if h <= 0: the figures across the top, the fishbone under them, and the
// Pareto chart beside the losses of the picked bone.
func (s *stats) causesPage(delay bool, w, h int, rep *replay) []string {
	if !s.hasCauses(delay) {
		return box("causes", []string{"", " no causes in this KPI file: rerun trainapp -kpi"}, w, max(h, 4))
	}
	v := s.view(delay)
	r := rankings(delay)[v.rank]
	items := s.items(delay)
	rows := pareto(items, r)
	pct := s.rep.Pareto()
	vital := cut(rows, pct)
	shown := narrowed(items, v.narrow)
	bs := bones(shown, delay)
	sel := s.sel(min(v.sel[cBones], max(len(bs)-1, 0)))

	top := boxRows(w, h/4, s.causeFigures(delay, items, rows, vital))

	// the fishbone, as tall as there is room for
	ribs := 3
	if h > 0 {
		for ribs > 0 && len(top)+fishboneRows(ribs)+2+8 > h {
			ribs--
		}
	}
	where := cmp.Or(v.narrow, "all")
	if delay && s.filter().keep != nil {
		where += " · " + s.filter().name
	}
	fb := fishbone(max(w-4, 10), ribs, s.causeHead(delay, shown), bs, sel, v.focus == cBones, func(b bone) (string, string, bool) {
		if !delay && needed(b.cause) {
			return b.cause, mins(b.min) + " · needed", true
		}
		return b.cause, mins(b.min) + " · " + pct0(b.share), b.min < minShown
	})
	fishH := len(fb) + 2
	out := append(top, panel{title: "causes · " + where, lines: fb, focused: v.focus == cBones}.draw(w, fishH)...)

	title := fmt.Sprintf("pareto · %s · %d of %d make %g%%", r.name, vital, len(rows), pct)
	pt := s.paretoTable(rows, vital, pct)
	var picked bone
	if len(bs) > 0 {
		picked = bs[min(v.sel[cBones], len(bs)-1)]
	}
	lt := s.lossesTable(delay, shown, picked.cause, rep)
	lossTitle := "losses · " + picked.cause
	if v.narrow != "" {
		lossTitle += " · " + v.narrow
	}
	body := h - len(out)
	if h <= 0 {
		body = max(len(pt.rows), len(lt.rows)) + 3
	}
	body = max(body, 6)
	draw := func(part int, title string, t table, w, h int) []string {
		return tableBox(title, t, s.sel(v.sel[part]), v.focus == part, &v.off[part], w, h)
	}
	if w >= 150 {
		rw := w * 45 / 100
		return append(out, beside(draw(cPareto, title, pt, w-rw, body), draw(cLosses, lossTitle, lt, rw, body), w-rw, rw)...)
	}
	if h <= 0 {
		out = append(out, draw(cPareto, title, pt, w, len(pt.rows)+3)...)
		return append(out, draw(cLosses, lossTitle, lt, w, len(lt.rows)+3)...)
	}
	ph := max(body/2, 4)
	out = append(out, draw(cPareto, title, pt, w, ph)...)
	return append(out, draw(cLosses, lossTitle, lt, w, body-ph)...)
}

func pct0(share float64) string { return fmt.Sprintf("%.0f%%", 100*share) }

// causeHead is the fishbone's effect: the minutes the items come to.
func (s *stats) causeHead(delay bool, items []causeItem) string {
	total, lostMin := 0.0, 0.0
	for _, it := range items {
		total += it.min
		if it.lost {
			lostMin += it.min
		}
	}
	name := "delay"
	if v := s.view(delay); v.narrow != "" {
		name = v.narrow
	}
	if delay {
		return fmt.Sprintf("%s %s min", name, mins(total))
	}
	if name == "delay" {
		name = "occupied"
	}
	share := 0.0
	if total > 0 {
		share = lostMin / total
	}
	return fmt.Sprintf("%s %s min · %s lost", name, mins(total), pct0(share))
}

// causeFigures are the boxes across the top of the causes view.
func (s *stats) causeFigures(delay bool, items []causeItem, rows []paretoRow, vital int) []panel {
	total, lostMin := 0.0, 0.0
	trains := map[string]bool{}
	for _, it := range items {
		total += it.min
		if it.lost {
			lostMin += it.min
		}
		if it.train != "" {
			trains[it.train] = true
		}
	}
	var few []string
	for i, r := range rows[:min(3, len(rows))] {
		few = append(few, fmt.Sprintf("%d %s", i+1, fit(r.key, 18))+fmt.Sprintf("%7s min", mins(r.min)))
	}
	if len(few) == 0 {
		few = []string{dim.Render("nothing lost")}
	}
	vitalPanel := panel{title: "vital few", lines: append([]string{tui.Figure("rows", fmt.Sprintf("%d of %d make %g%%", vital, len(rows), s.rep.Pareto()))}, few...)}
	bs := bones(items, delay)
	if delay {
		first := []string{dim.Render("no delay")}
		if len(bs) > 0 && bs[0].min > 0 {
			b := bs[0]
			first = []string{colored(causeColors[b.cause], b.cause), tui.Figure("minutes", mins(b.min)+" min"), tui.Figure("share", pct0(b.share))}
		}
		return []panel{
			{title: "delay", lines: []string{
				tui.Figure("in all", mins(total)+" min"),
				tui.Figure("trains", fmt.Sprint(len(trains))),
				tui.Figure("made up", mins(s.madeUp())+" min"),
			}},
			vitalPanel,
			{title: "biggest cause", lines: first},
		}
	}
	share := 0.0
	if total > 0 {
		share = lostMin / total
	}
	lostRows := pareto(items, ranking{lostOnly: true})
	most := []string{dim.Render("nothing lost")}
	if len(lostRows) > 0 {
		most = []string{lostRows[0].key, tui.Figure("lost", mins(lostRows[0].min)+" min")}
	}
	return []panel{
		{title: "occupied", lines: []string{
			tui.Figure("in all", mins(total)+" min"),
			tui.Figure("needed", mins(total-lostMin)+" min"),
			tui.Figure("lost", bar(share, 14, "203")+" "+pct0(share)),
		}},
		vitalPanel,
		{title: "most lost", lines: most},
	}
}

// paretoTable is a row per Pareto row; those past the vital few faint, the
// last of them marked.
func (s *stats) paretoTable(rows []paretoRow, vital int, pct float64) table {
	widths := []int{4, 19, 9, 9, 7, 6}
	right := []bool{true, false, false, true, true, true}
	t := table{header: cols(widths, right, "#", " place", "kind", "min", "share", "cum") + " ", empty: " nothing lost"}
	top := 0.0
	if len(rows) > 0 {
		top = rows[0].min
	}
	for i, r := range rows {
		share := 0.0
		if top > 0 {
			share = r.min / top
		}
		line := cols(widths, right, fmt.Sprint(i+1), " "+r.key, r.kind, mins(r.min), pct0(r.share), pct0(r.cum)) + " " + bar(share, 16, "39")
		if i == vital-1 {
			line += bold.Render(fmt.Sprintf(" ◂ %g%%", pct))
		}
		t.rows = append(t.rows, line)
		t.faint = append(t.faint, i >= vital)
	}
	return t
}

// lossItems are the items of the cause, most minutes first.
func lossItems(items []causeItem, cause string) []causeItem {
	var out []causeItem
	for _, it := range items {
		if it.cause == cause && (it.min >= minShown || it.min <= -minShown) {
			out = append(out, it)
		}
	}
	slices.SortStableFunc(out, func(a, b causeItem) int { return cmp.Compare(b.min, a.min) })
	return out
}

// lossesTable is a row per item of the picked cause: the trains' losses, or
// the tracks' minutes.
func (s *stats) lossesTable(delay bool, items []causeItem, cause string, rep *replay) table {
	if delay {
		widths := []int{8, 19, 24, 7, 7}
		right := []bool{false, false, false, true, true}
		t := table{header: cols(widths, right, " train", "place", "detail", "from", "min"), empty: " none"}
		for _, it := range lossItems(items, cause) {
			t.rows = append(t.rows, cols(widths, right, " "+rep.classStyle(it.train).Render(it.train), it.place, it.rib, clock(it.from), mins(it.min)))
		}
		return t
	}
	widths := []int{20, 12, 16, 9, 9}
	right := []bool{false, false, false, true, true}
	t := table{header: cols(widths, right, " track", "place", "detail", "min", "of track"), empty: " none"}
	for _, it := range lossItems(items, cause) {
		share := 0.0
		if it.of > 0 {
			share = it.min / it.of
		}
		t.rows = append(t.rows, cols(widths, right, " "+it.track, it.place, it.detail, mins(it.min), pct0(share)))
	}
	return t
}

// causeRows is how many rows the part of the view has.
func (s *stats) causeRows(delay bool, part int) int {
	v := s.view(delay)
	items := s.items(delay)
	switch part {
	case cPareto:
		rows := pareto(items, rankings(delay)[v.rank])
		return len(rows)
	case cBones:
		return len(bones(nil, delay))
	}
	bs := bones(narrowed(items, v.narrow), delay)
	if len(bs) == 0 {
		return 0
	}
	return len(lossItems(narrowed(items, v.narrow), bs[min(v.sel[cBones], len(bs)-1)].cause))
}

// causesKey handles the causes view's own keys, and reports whether it did.
func (m *model) causesKey(key string) bool {
	s := m.st
	delay := m.page == pageDelay
	v := s.view(delay)
	if !s.hasCauses(delay) && key != "v" {
		return false
	}
	if m.tableKey(key) {
		return true
	}
	resetPareto := func() { v.sel[cPareto], v.off[cPareto] = 0, 0 }
	resetLosses := func() { v.sel[cLosses], v.off[cLosses] = 0, 0 }
	switch key {
	case "tab":
		v.focus = (v.focus + 1) % cParts
	case "shift+tab":
		v.focus = (v.focus + cParts - 1) % cParts
	case "c":
		v.rank = (v.rank + 1) % len(rankings(delay))
		v.narrow = ""
		resetPareto()
		resetLosses()
	case "t":
		v.tracks = !v.tracks
		v.narrow = ""
		resetPareto()
		resetLosses()
	case "b":
		if !delay {
			return false
		}
		s.dFilter = (s.dFilter + 1) % len(s.filters())
		v.narrow = ""
		resetPareto()
		resetLosses()
	case "esc":
		switch {
		case v.narrow != "":
			v.narrow = ""
			resetLosses()
		case delay && s.dFilter != 0:
			s.dFilter = 0
			resetPareto()
			resetLosses()
		default:
			v.focus = cPareto
		}
	case "v":
		if delay {
			s.dCauses = false
		} else {
			s.oCauses, s.table = false, false
		}
		m.redraw()
	case "enter":
		m.causesEnter(delay)
	default:
		return false
	}
	return true
}

// causesEnter narrows the fishbone to the picked Pareto row (or picks the
// row's bone, ranked by cause), or shows the picked loss.
func (m *model) causesEnter(delay bool) {
	s := m.st
	v := s.view(delay)
	items := s.items(delay)
	switch v.focus {
	case cPareto:
		r := rankings(delay)[v.rank]
		rows := pareto(items, r)
		if len(rows) == 0 {
			return
		}
		row := rows[min(v.sel[cPareto], len(rows)-1)]
		if r.byCause {
			cause, _, _ := strings.Cut(row.key, " · ")
			for i, b := range bones(narrowed(items, v.narrow), delay) {
				if b.cause == cause {
					v.sel[cBones], v.focus = i, cBones
				}
			}
			return
		}
		if v.narrow == row.key {
			v.narrow = ""
		} else {
			v.narrow = row.key
		}
		v.sel[cLosses], v.off[cLosses] = 0, 0
	case cBones:
		v.focus = cLosses
	case cLosses:
		shown := narrowed(items, v.narrow)
		bs := bones(shown, delay)
		if len(bs) == 0 {
			return
		}
		losses := lossItems(shown, bs[min(v.sel[cBones], len(bs)-1)].cause)
		if len(losses) == 0 {
			return
		}
		it := losses[min(v.sel[cLosses], len(losses)-1)]
		if delay {
			m.showLoss(it)
		} else {
			s.oCauses = false
			m.showTrack(s.trackID[it.track])
		}
	}
}

// showLoss goes to the schematic with the train selected as the loss began,
// centred on where it happened.
func (m *model) showLoss(it causeItem) {
	if i := slices.Index(m.rep.trains, it.train); i >= 0 {
		m.selected = i
	}
	m.rep.seek(it.from)
	m.setPage(pageSchematic)
	x, y, ok := m.frame.Where(schematic.Piece{Kind: schematic.PieceTrack, ID: m.st.trackID[it.track]})
	if !ok {
		x, y, ok = m.frame.Station(it.at)
	}
	if ok {
		m.centreOn(x, y)
	}
	m.note = fmt.Sprintf("%s: %s, %s min at %s", it.train, it.cause, mins(it.min), it.place)
}
