package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/compare"
	"trainapp/visualization/schematic"
	"trainapp/visualization/traingraph"
	"trainapp/visualization/tui"
)

// The compare occupancy page: the baseline's schematic coloured by how
// much busier or quieter one run left each track than A, or (v) tables of
// the blocks and stations in every shown run. And the graph page: one
// run's time-distance graph over A's.

// Diverging ramps, weakest first: blue for less than A, red for more. No
// amber, which trainviz keeps for held trains.
var (
	lessRamp = []string{"153", "117", "75", "33", "27"}
	moreRamp = []string{"224", "217", "210", "203", "196"}
	sameHeat = "245"
)

// other is the shown run set against A on the schematic and graph.
func (m *cmpModel) other() int { return min(m.oOther, len(m.shown)-1) }

// metricDelta is a track's change in the metric in run k against A, and
// whether either run used it.
func metricDelta(row compare.TrackRow, metric, k int) (float64, bool) {
	a, b := row.Uses[0], row.Uses[k]
	used := func(u *report.TrackUse) bool { return u != nil && u.Entries > 0 }
	if !used(a) && !used(b) {
		return 0, false
	}
	of := metrics[metric].of
	va, vb := 0.0, 0.0
	if a != nil {
		va = of(*a)
	}
	if b != nil {
		vb = of(*b)
	}
	return vb - va, true
}

// deltaLabel writes a change in the metric: points for a share, else
// trains an hour.
func deltaLabel(metric int, d float64) string {
	if metric == 2 {
		return fmt.Sprintf("%+.1f/h", d)
	}
	if math.Round(100*d) == 0 {
		return "±0"
	}
	return fmt.Sprintf("%+.0f", 100*d)
}

// deltaScale is the largest change of any track, which the ramps top out
// at.
func (m *cmpModel) deltaScale() float64 {
	top := 0.0
	for _, row := range m.tracks {
		if d, ok := metricDelta(row, m.oMetric, m.other()); ok {
			top = max(top, abs64(d))
		}
	}
	return top
}

// deltaColor is the colour of a change d on a ramp topping out at scale.
func deltaColor(d, scale float64, metric int) string {
	if scale <= 0 || (metric != 2 && math.Round(100*d) == 0) || (metric == 2 && abs64(d) < 0.05) {
		return sameHeat
	}
	step := min(int(abs64(d)/scale*float64(len(lessRamp))), len(lessRamp)-1)
	if d < 0 {
		return lessRamp[step]
	}
	return moreRamp[step]
}

// deltaHeat colours each track either run used by its change.
func (m *cmpModel) deltaHeat() map[string]schematic.Heat {
	base := m.runs[m.shown[0]].rep
	ids := map[string]string{}
	for id, name := range base.trackName {
		ids[name] = id
	}
	scale := m.deltaScale()
	out := map[string]schematic.Heat{}
	for _, row := range m.tracks {
		id, ok := ids[row.Track]
		d, used := metricDelta(row, m.oMetric, m.other())
		if !ok || !used {
			continue
		}
		level := 0.0
		if scale > 0 {
			level = abs64(d) / scale
		}
		out[id] = schematic.Heat{Color: deltaColor(d, scale, m.oMetric), Level: level, Label: deltaLabel(m.oMetric, d)}
	}
	return out
}

// sizeCam fits the camera to the schematic pane, beside the side pane.
func (m *cmpModel) sizeCam() {
	m.cam.W, m.cam.H = max(m.width-m.occSideWidth()-2, 1), max(m.height-4, 1)
	m.cam.Clamp()
}

func (m *cmpModel) occSideWidth() int {
	if m.width < 100 {
		return 0
	}
	return 40
}

// camera draws the baseline's schematic, made on first use.
func (m *cmpModel) camera() *tui.Camera {
	if m.cam == nil {
		d, _ := m.runs[m.shown[0]].diagram()
		m.cam = tui.NewCamera(func(z tui.ZoomStep) *schematic.Frame {
			return d.Draw(schematic.Options{Level: z.Level, Zoom: z.Zoom, Heat: m.deltaHeat()})
		})
		m.camOf = m.shown[0]
		m.sizeCam()
		m.cam.Fit()
	}
	return m.cam
}

// occSide is the side pane: the colour key, then the blocks and platforms
// in each shown run.
func (m *cmpModel) occSide(w, h int) []string {
	k := m.other()
	scale := m.deltaScale()
	label := func(d float64) string { return deltaLabel(m.oMetric, d) }
	var less, more, same int
	for _, row := range m.tracks {
		if row.Kind != report.KindBlock {
			continue
		}
		switch d, ok := metricDelta(row, m.oMetric, k); {
		case !ok:
		case deltaColor(d, scale, m.oMetric) == sameHeat:
			same++
		case d < 0:
			less++
		default:
			more++
		}
	}
	ramp := ""
	for i := len(lessRamp) - 1; i >= 0; i-- {
		ramp += colored(lessRamp[i], "■")
	}
	ramp += colored(sameHeat, "■")
	for _, c := range moreRamp {
		ramp += colored(c, "■")
	}
	unit := "points"
	if m.oMetric == 2 {
		unit = "trains/h"
	}
	keyLines := []string{
		fmt.Sprintf("%s less %s more", ramp, dim.Render("·")),
		dim.Render(fmt.Sprintf("%s to %s %s", label(-scale), label(scale), unit)),
		tui.Figure("blocks", fmt.Sprintf("%s fewer · %d same · %s more", colored(lessRamp[3], fmt.Sprint(less)), same, colored(moreRamp[3], fmt.Sprint(more)))),
	}
	sum := func(title string, of func(*report.Report) report.Summary) panel {
		var lines []string
		for k, r := range m.reps {
			s := of(r)
			line := fmt.Sprintf("%s mean %s  max %s  stand %s", bold.Render(letters[k]), pct(s.MeanOccupancy), pct(s.MaxOccupancy), mins(s.StandingMin))
			lines = append(lines, line)
		}
		return panel{title: title, lines: lines}
	}
	panels := []panel{
		{title: fmt.Sprintf("Δ %s · %s − A", metrics[m.oMetric].name, letters[k]), lines: keyLines},
		sum("blocks", func(r *report.Report) report.Summary { return r.Utilization.Blocks }),
		sum("platform tracks", func(r *report.Report) report.Summary { return r.Utilization.Platforms }),
	}
	var out []string
	for _, p := range panels {
		out = append(out, p.draw(w, len(p.lines)+2)...)
	}
	for h > 0 && len(out) < h {
		out = append(out, "")
	}
	return out
}

func (m *cmpModel) occPage(w, h int) []string {
	if m.oTable {
		return m.occTablePage(w, h)
	}
	c := m.camera()
	sw := m.occSideWidth()
	z := tui.ZoomSteps[c.Step]
	title := fmt.Sprintf("occupancy · Δ %s · %s − A · %s, zoom %d", metrics[m.oMetric].name, letters[m.other()], z.Level, z.Zoom)
	lines := box(title, c.Lines(true), w-sw, h)
	if sw > 0 {
		side := m.occSide(sw, h)
		for i := range lines {
			lines[i] += fit(tui.At(side, i), sw)
		}
	}
	return lines
}

// occCmpSorts order the occupancy tables.
var occCmpSorts = []string{"line order", "largest change occupied", "largest change standing"}

// occTracks are the tracks of a kind, in the sort's order.
func (m *cmpModel) occTracks(kind string) []compare.TrackRow {
	var out []compare.TrackRow
	for _, row := range m.tracks {
		if row.Kind == kind {
			out = append(out, row)
		}
	}
	if m.oSort > 0 {
		metric := m.oSort - 1
		largest := func(row compare.TrackRow) float64 {
			best := 0.0
			for k := 1; k < len(m.shown); k++ {
				if d, ok := metricDelta(row, metric, k); ok {
					best = max(best, abs64(d))
				}
			}
			return best
		}
		slices.SortStableFunc(out, func(a, b compare.TrackRow) int { return cmp.Compare(largest(b), largest(a)) })
	}
	return out
}

// occTracksTable is a row per track: occupied and standing in each run,
// with the Δs, and trains an hour.
func (m *cmpModel) occTracksTable(rows []compare.TrackRow) table {
	widths, right := []int{18}, []bool{false}
	head := []string{" track"}
	for k := range m.shown {
		widths, right = append(widths, 7), append(right, true)
		head = append(head, "occ "+letters[k])
		if k > 0 {
			widths, right = append(widths, 6), append(right, true)
			head = append(head, "Δ")
		}
		widths, right = append(widths, 7), append(right, true)
		head = append(head, "stand")
		if k > 0 {
			widths, right = append(widths, 6), append(right, true)
			head = append(head, "Δ")
		}
		widths, right = append(widths, 6), append(right, true)
		head = append(head, "/h")
	}
	t := table{header: cols(widths, right, head...), empty: " none"}
	for _, row := range rows {
		name := row.Track
		if row.Pf != "" {
			name = row.Stn + " PF " + row.Pf
		}
		cells := []string{" " + name}
		unused := true
		for k := range m.shown {
			u := row.Uses[k]
			if u == nil {
				cells = append(cells, "—")
			} else {
				cells = append(cells, pct(u.Occupancy))
				unused = unused && u.Entries == 0
			}
			if k > 0 {
				cells = append(cells, m.trackDelta(row, 0, k))
			}
			if u == nil {
				cells = append(cells, "—")
			} else {
				cells = append(cells, pct(u.Standing))
			}
			if k > 0 {
				cells = append(cells, m.trackDelta(row, 1, k))
			}
			if u == nil {
				cells = append(cells, "—")
			} else {
				cells = append(cells, fmt.Sprintf("%.1f", u.TrainsPerHour))
			}
		}
		t.rows = append(t.rows, cols(widths, right, cells...))
		t.faint = append(t.faint, unused)
	}
	return t
}

// trackDelta is a track's change in a metric, coloured as on the map.
func (m *cmpModel) trackDelta(row compare.TrackRow, metric, k int) string {
	d, ok := metricDelta(row, metric, k)
	if !ok {
		return dim.Render("—")
	}
	if metric != 2 && math.Round(100*d) == 0 {
		return dim.Render("±0")
	}
	return colored(deltaColor(d, max(abs64(d), 1e-9), metric), deltaLabel(metric, d))
}

// occStationsTable is a row per station: its platforms' occupancy and
// minutes standing in each run, with the Δs.
func (m *cmpModel) occStationsTable() table {
	widths, right := []int{9, 4}, []bool{false, true}
	head := []string{" station", "pfs"}
	for k := range m.shown {
		widths, right = append(widths, 7), append(right, true)
		head = append(head, "occ "+letters[k])
		if k > 0 {
			widths, right = append(widths, 7), append(right, true)
			head = append(head, "Δ")
		}
		widths, right = append(widths, 8), append(right, true)
		head = append(head, "stand")
		if k > 0 {
			widths, right = append(widths, 7), append(right, true)
			head = append(head, "Δ")
		}
	}
	t := table{header: cols(widths, right, head...), empty: " no stations"}
	for _, s := range m.stations {
		pfs := "—"
		if s.Use[0] != nil {
			pfs = fmt.Sprint(s.Use[0].Platforms)
		}
		cells := []string{" " + s.Stn, pfs}
		for k := range m.shown {
			u := s.Use[k]
			occ, stand := "—", "—"
			if u != nil {
				occ, stand = pct(u.Occupancy), mins(u.StandingMin)
			}
			cells = append(cells, occ)
			a, b := s.Use[0], u
			get := func(u *report.StationUse, f func(*report.StationUse) float64) float64 {
				if u == nil {
					return 0
				}
				return f(u)
			}
			if k > 0 {
				cells = append(cells, delta(get(a, func(u *report.StationUse) float64 { return u.Occupancy }), get(b, func(u *report.StationUse) float64 { return u.Occupancy }), a != nil, b != nil, compare.Share, compare.Neither))
			}
			cells = append(cells, stand)
			if k > 0 {
				cells = append(cells, delta(get(a, func(u *report.StationUse) float64 { return u.StandingMin }), get(b, func(u *report.StationUse) float64 { return u.StandingMin }), a != nil, b != nil, compare.Minutes, compare.Neither))
			}
		}
		t.rows = append(t.rows, cols(widths, right, cells...))
	}
	return t
}

func (m *cmpModel) occTablePage(w, h int) []string {
	sel := func(tbl int) int {
		if m.plain {
			return -1
		}
		return m.oSel[tbl]
	}
	box := func(tbl int, title string, t table, w, h int) []string {
		return tableBox(title, t, sel(tbl), m.oFocus == tbl, &m.oOff[tbl], w, h)
	}
	blocks, stations := m.occTracksTable(m.occTracks(report.KindBlock)), m.occStationsTable()
	by := " · by " + occCmpSorts[m.oSort]
	parts := []struct {
		tbl   int
		title string
		t     table
	}{{coBlocks, "blocks · Δ against A" + by, blocks}, {coStations, "stations · Δ against A", stations}}
	if m.throats {
		parts = append(parts, struct {
			tbl   int
			title string
			t     table
		}{coThroats, "throats" + by, m.occTracksTable(m.occTracks(report.KindThroat))})
	}
	var out []string
	for i, p := range parts {
		ph := len(p.t.rows) + 3
		if h > 0 {
			left := h - len(out)
			ph = min(ph, max(left/(len(parts)-i), 4))
			if i == len(parts)-1 {
				ph = left
			}
		}
		out = append(out, box(p.tbl, p.title, p.t, w, ph)...)
	}
	return out
}

func (m *cmpModel) occKey(key string) {
	if m.oTable {
		m.occTableKey(key)
		return
	}
	c := m.camera()
	redraw := func() { c.Redraw() }
	switch key {
	case "+", "=":
		c.ZoomTo(c.Step+1, c.W/2, c.H/2)
	case "-", "_":
		c.ZoomTo(c.Step-1, c.W/2, c.H/2)
	case "0":
		c.Fit()
	case "h", "left":
		c.Pan(-8, 0)
	case "l", "right":
		c.Pan(8, 0)
	case "k", "up":
		c.Pan(0, -3)
	case "j", "down":
		c.Pan(0, 3)
	case "m":
		m.oMetric = (m.oMetric + 1) % len(metrics)
		redraw()
	case "n":
		m.nextOther()
		redraw()
	case "v":
		m.oTable = true
	}
}

// nextOther sets the next shown run against A.
func (m *cmpModel) nextOther() {
	if len(m.shown) < 3 {
		m.note = "one run to set against A: show a third on the summary to switch"
		return
	}
	m.oOther = m.oOther%(len(m.shown)-1) + 1
	m.note = fmt.Sprintf("%s against A", m.label(m.shown[m.oOther]))
}

func (m *cmpModel) occTableKey(key string) {
	tables := coThroats
	if m.throats {
		tables = coTables
	}
	rows := map[int]int{coBlocks: len(m.occTracks(report.KindBlock)), coStations: len(m.stations), coThroats: len(m.occTracks(report.KindThroat))}
	switch key {
	case "tab":
		m.oFocus = (m.oFocus + 1) % tables
	case "shift+tab":
		m.oFocus = (m.oFocus + tables - 1) % tables
	case "c":
		m.oSort = (m.oSort + 1) % len(occCmpSorts)
		m.note = "by " + occCmpSorts[m.oSort]
	case "t":
		m.throats = !m.throats
		if !m.throats && m.oFocus == coThroats {
			m.oFocus = coBlocks
		}
	case "v", "esc":
		m.oTable = false
	case "enter":
		m.showTrack()
	default:
		tableMove(key, &m.oSel[m.oFocus], rows[m.oFocus], m.pageRows())
	}
}

// showTrack goes to the schematic, centred on the picked block or throat.
func (m *cmpModel) showTrack() {
	kind := map[int]string{coBlocks: report.KindBlock, coThroats: report.KindThroat}[m.oFocus]
	if kind == "" {
		return
	}
	rows := m.occTracks(kind)
	if len(rows) == 0 {
		return
	}
	name := rows[min(m.oSel[m.oFocus], len(rows)-1)].Track
	m.oTable = false
	c := m.camera()
	for id, n := range m.runs[m.shown[0]].rep.trackName {
		if n != name {
			continue
		}
		if x, y, ok := c.Frame.Where(schematic.Piece{Kind: schematic.PieceTrack, ID: id}); ok {
			c.CentreOn(x, y)
			return
		}
	}
	m.note = name + " is not drawn at this zoom: + zooms in"
}

// The graph page's views.
var graphViews = []string{"overlay", "A alone", "run alone"}

// graphTrains are the trains of A and the run set against it, A's first.
func (m *cmpModel) graphTrains() []string {
	var out []string
	for _, k := range []int{0, m.other()} {
		if k >= len(m.shown) {
			continue
		}
		for _, t := range m.runs[m.shown[k]].hello.World.Trains {
			if !slices.Contains(out, t.Number) {
				out = append(out, t.Number)
			}
		}
	}
	return out
}

// graphPane is the graph's size inside its box.
func (m *cmpModel) graphPane() (int, int) { return max(m.width-2, 1), max(m.height-4, 1) }

// graphDrawn are the graph on top and the one under it, as the view has
// them.
func (m *cmpModel) graphDrawn() (top, under *traingraph.Graph) {
	a, b := m.runs[m.shown[0]].trainGraph(), m.runs[m.shown[m.other()]].trainGraph()
	switch m.gView {
	case 1:
		return a, nil
	case 2:
		return b, nil
	}
	return b, a
}

func (m *cmpModel) graphOptions() traingraph.Options {
	pw, ph := m.graphPane()
	top, under := m.graphDrawn()
	sel := ""
	if m.gTrain >= 0 {
		sel = m.graphTrains()[m.gTrain]
	}
	return traingraph.Options{Width: pw, Height: ph, From: m.gFrom, To: m.gFrom + m.gPerCol*float64(max(pw-top.Gutter()-1, 1)),
		Now: -1, Selected: sel, Color: true, Under: under}
}

// fitGraph shows both runs whole.
func (m *cmpModel) fitGraph() {
	if m.width == 0 || len(m.shown) < 2 || !m.oneWorld {
		return
	}
	a, b := m.runs[m.shown[0]].trainGraph(), m.runs[m.shown[m.other()]].trainGraph()
	if a == nil || b == nil {
		return
	}
	s1, e1 := a.Span()
	s2, e2 := b.Span()
	start, end := min(s1, s2), max(e1, e2)
	pw, _ := m.graphPane()
	m.gFrom = start
	m.gPerCol = max((end-start)/float64(max(pw-a.Gutter()-1, 1)), graphScales[0])
	m.gFitted = true
}

func (m *cmpModel) zoomGraph(in bool) {
	next := m.gPerCol
	if in {
		for i := len(graphScales) - 1; i >= 0; i-- {
			if graphScales[i] < m.gPerCol-1e-9 {
				next = graphScales[i]
				break
			}
		}
	} else {
		for _, s := range graphScales {
			if s > m.gPerCol+1e-9 {
				next = s
				break
			}
		}
	}
	pw, _ := m.graphPane()
	mid := m.gFrom + m.gPerCol*float64(pw)/2
	m.gFrom = mid - next*float64(pw)/2
	m.gPerCol = next
}

func (m *cmpModel) graphPage(w, h int) []string {
	top, under := m.graphDrawn()
	if top == nil {
		return box("time-distance graph", []string{" " + dim.Render("no time-distance graph for this run")}, w, h)
	}
	o := m.graphOptions()
	o.Height = h - 2
	who := map[int]string{0: fmt.Sprintf("A faint, %s over it", letters[m.other()]), 1: "A alone", 2: letters[m.other()] + " alone"}[m.gView]
	title := fmt.Sprintf("time-distance graph · %s · %.3g min a column", who, m.gPerCol)
	if o.Selected != "" {
		title += " · " + o.Selected
		if under != nil {
			title += " (A dotted)"
		}
	}
	return box(title, top.Draw(o), w, h)
}

func (m *cmpModel) graphKey(key string) {
	n := len(m.graphTrains())
	switch key {
	case "+", "=":
		m.zoomGraph(true)
	case "-", "_":
		m.zoomGraph(false)
	case "0":
		m.fitGraph()
	case "h", "left":
		m.gFrom -= 8 * m.gPerCol
	case "l", "right":
		m.gFrom += 8 * m.gPerCol
	case "tab":
		if n > 0 {
			m.gTrain = (m.gTrain+2)%(n+1) - 1
		}
	case "shift+tab":
		if n > 0 {
			m.gTrain = (m.gTrain+n+1)%(n+1) - 1
		}
	case "esc":
		m.gTrain = -1
	case "n":
		m.nextOther()
		m.fitGraph()
	case "o":
		m.gView = (m.gView + 1) % len(graphViews)
	}
}
