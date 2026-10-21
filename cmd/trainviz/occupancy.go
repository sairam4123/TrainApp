package main

import (
	"cmp"
	"fmt"
	"slices"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"
)

// The occupancy page: the schematic coloured by how busy each track was,
// or (v) tables of the blocks, stations and their platform tracks.

// metrics are what the occupancy page colours the tracks by. Occupied is
// coloured by the profile's capacity bands; the others, and occupied
// without bands, over an even ramp up to the busiest track.
var metrics = []struct {
	name   string
	of     func(report.TrackUse) float64
	label  func(float64) string
	share  bool // a share of the run, ramped up to 100%
	banded bool
}{
	{"occupied", func(t report.TrackUse) float64 { return t.Occupancy }, pct, true, true},
	{"standing", func(t report.TrackUse) float64 { return t.Standing }, pct, false, false},
	{"trains per hour", func(t report.TrackUse) float64 { return t.TrainsPerHour }, func(v float64) string { return fmt.Sprintf("%.1f/h", v) }, false, false},
}

// rampSteps is how many steps the even ramp has.
const rampSteps = 5

// scale is the figure the even ramp tops out at.
func (s *stats) scale() float64 {
	m := metrics[s.metric]
	if m.share {
		return 1
	}
	top := 0.0
	for _, tu := range s.rep.Utilization.Tracks {
		top = max(top, m.of(tu))
	}
	return top
}

// banded is whether the tracks are coloured by the profile's bands.
func (s *stats) banded() bool {
	return metrics[s.metric].banded && len(s.rep.Utilization.Bands) > 0
}

// trackColor is the colour of a track with figure v.
func (s *stats) trackColor(v, scale float64) string {
	if s.banded() {
		u := s.rep.Utilization
		b := u.Band(v)
		if b < 0 {
			return ""
		}
		return cmp.Or(u.Bands[b].Color, rampColor(b, len(u.Bands)))
	}
	return rampColor(rampStep(v, scale), rampSteps)
}

func rampStep(v, scale float64) int {
	if scale <= 0 {
		return 0
	}
	return min(max(int(v/scale*rampSteps), 0), rampSteps-1)
}

// heat colours every track trains used by the metric.
func (s *stats) heat() map[string]schematic.Heat {
	if s.rep == nil {
		return map[string]schematic.Heat{}
	}
	m := metrics[s.metric]
	scale := s.scale()
	out := map[string]schematic.Heat{}
	for _, tu := range s.rep.Utilization.Tracks {
		id, ok := s.trackID[tu.Track]
		if !ok || tu.Entries == 0 {
			continue
		}
		v := m.of(tu)
		level := v
		if scale > 0 {
			level = v / scale
		}
		out[id] = schematic.Heat{Color: s.trackColor(v, scale), Level: level, Label: m.label(v)}
	}
	return out
}

// keyPanel is the colour key: the profile's bands, or the even ramp, with
// how many blocks are in each.
func (s *stats) keyPanel() panel {
	u := s.rep.Utilization
	m := metrics[s.metric]
	blocksIn := func(in func(report.TrackUse) bool) int {
		n := 0
		for _, tu := range u.Tracks {
			if tu.Kind == report.KindBlock && tu.Entries > 0 && in(tu) {
				n++
			}
		}
		return n
	}
	var lines []string
	title := m.name
	if s.banded() {
		title = "capacity bands"
		for i, b := range u.Bands {
			upTo := "+"
			if i+1 < len(u.Bands) {
				upTo = fmt.Sprintf("–%g", u.Bands[i+1].OverPct)
			}
			n := blocksIn(func(tu report.TrackUse) bool { return u.Band(tu.Occupancy) == i })
			lines = append(lines, colored(cmp.Or(b.Color, rampColor(i, len(u.Bands))), "■ ")+fit(b.Name, 11)+fit(fmt.Sprintf("%g%s%%", b.OverPct, upTo), 9)+blocks(n))
		}
	} else {
		scale := s.scale()
		for i := range rampSteps {
			lo, hi := scale*float64(i)/rampSteps, scale*float64(i+1)/rampSteps
			n := blocksIn(func(tu report.TrackUse) bool { return rampStep(m.of(tu), scale) == i })
			lines = append(lines, colored(rampColor(i, rampSteps), "■ ")+fit(m.label(lo)+"–"+m.label(hi), 20)+blocks(n))
		}
	}
	return panel{title: title, lines: append(lines, dim.Render("─ not used"))}
}

// blocksPanel and platformsPanel sum up the blocks and the platform tracks.
func (s *stats) blocksPanel() panel {
	b := s.rep.Utilization.Blocks
	return panel{title: "blocks", lines: []string{
		tui.Figure("tracks", fmt.Sprint(b.Tracks)),
		tui.Figure("mean", pct(b.MeanOccupancy)+" occupied"),
		tui.Figure("range", pct(b.MinOccupancy)+" – "+pct(b.MaxOccupancy)),
		tui.Figure("standing", mins(b.StandingMin)+" min in section"),
	}}
}

func (s *stats) platformsPanel() panel {
	p := s.rep.Utilization.Platforms
	return panel{title: "platform tracks", lines: []string{
		tui.Figure("tracks", fmt.Sprint(p.Tracks)),
		tui.Figure("mean", pct(p.MeanOccupancy)+" occupied"),
		tui.Figure("range", pct(p.MinOccupancy)+" – "+pct(p.MaxOccupancy)),
		tui.Figure("standing", mins(p.StandingMin)+" min"),
	}}
}

// sideLines are the boxes beside the occupancy schematic, w wide and h
// tall, or as tall as they need if h <= 0: the colour key, the blocks and
// platform tracks, and the stations.
func (s *stats) sideLines(w, h int) []string {
	var out []string
	for _, p := range []panel{s.keyPanel(), s.blocksPanel(), s.platformsPanel()} {
		out = append(out, p.draw(w, len(p.lines)+2)...)
	}
	widths := []int{9, 4, 6, 10}
	right := []bool{false, true, true, true}
	t := table{header: cols(widths, right, " station", "pf", "occ", "standing"), empty: " no stations"}
	for _, st := range s.stations() {
		t.rows = append(t.rows, cols(widths, right, " "+st.Stn, fmt.Sprint(st.Platforms), colored(s.occColor(st.Occupancy), pct(st.Occupancy)), fmt.Sprintf("%.0f min", st.StandingMin)))
	}
	off := 0
	rest := h - len(out)
	if h <= 0 {
		rest = 0
	} else if rest < 4 {
		return out[:h]
	}
	return append(out, tableBox("stations", t, -1, false, &off, w, rest)...)
}

func blocks(n int) string {
	if n == 1 {
		return "1 block"
	}
	return fmt.Sprintf("%d blocks", n)
}

// occSorts are the occupancy tables' orders.
var occSorts = []struct {
	name    string
	key     func(report.TrackUse) float64
	station func(report.StationUse) float64
}{
	{"line order", nil, nil},
	{"occupied", func(t report.TrackUse) float64 { return t.Occupancy }, func(s report.StationUse) float64 { return s.Occupancy }},
	{"standing", func(t report.TrackUse) float64 { return t.Standing }, func(s report.StationUse) float64 { return s.StandingMin }},
}

// tracksOf are the tracks of the kind, at the station if it is set, in the
// sort's order.
func (s *stats) tracksOf(kind, stn string) []report.TrackUse {
	var out []report.TrackUse
	for _, tu := range s.rep.Utilization.Tracks {
		if tu.Kind == kind && (stn == "" || tu.Stn == stn) {
			out = append(out, tu)
		}
	}
	if key := occSorts[s.oSort].key; key != nil {
		slices.SortStableFunc(out, func(a, b report.TrackUse) int { return cmp.Compare(key(b), key(a)) })
	}
	return out
}

// stations are the stations in the sort's order.
func (s *stats) stations() []report.StationUse {
	out := slices.Clone(s.rep.Utilization.Stations)
	if key := occSorts[s.oSort].station; key != nil {
		slices.SortStableFunc(out, func(a, b report.StationUse) int { return cmp.Compare(key(b), key(a)) })
	}
	return out
}

// selectedStation is the station picked in the stations table, "" if none.
func (s *stats) selectedStation() string {
	st := s.stations()
	if len(st) == 0 {
		return ""
	}
	return st[min(s.oSel[tableStations], len(st)-1)].Stn
}

// occTracks are the tracks a table of the occupancy view lists.
func (s *stats) occTracks(tbl int) []report.TrackUse {
	switch tbl {
	case tableBlocks:
		return s.tracksOf(report.KindBlock, "")
	case tablePlatforms:
		return s.tracksOf(report.KindPlatform, s.selectedStation())
	case tableThroats:
		return s.tracksOf(report.KindThroat, "")
	}
	return nil
}

// occRows is how many rows a table of the occupancy view has.
func (s *stats) occRows(tbl int) int {
	if tbl == tableStations {
		return len(s.rep.Utilization.Stations)
	}
	return len(s.occTracks(tbl))
}

// occTables are the tables tab moves between.
func (s *stats) occTables() int {
	if s.throats {
		return 4
	}
	return 3
}

// tracksTable is a row per track; platform tracks are named by their
// platform, throats by their station.
func (s *stats) tracksTable(tbl int) table {
	widths := []int{20, 15, 6, 8, 9, 8, 9, 8}
	right := []bool{false, false, true, true, true, true, true, true}
	name := map[int]string{tableBlocks: " block", tablePlatforms: " platform", tableThroats: " throat"}[tbl]
	t := table{header: cols(widths, right, name, "occupied", "", "running", "standing", "entries", "trains/h", "min") + "band", empty: " none"}
	for _, tu := range s.occTracks(tbl) {
		label := tu.Track
		switch tbl {
		case tablePlatforms:
			label = "pf " + tu.Pf + "  " + dim.Render(tu.Track)
		case tableThroats:
			label = tu.Stn + "  " + dim.Render(tu.Track)
		}
		t.rows = append(t.rows, cols(widths, right, " "+label, bar(tu.Occupancy, 13, s.occColor(tu.Occupancy)), pct(tu.Occupancy), pct(tu.Running),
			fmt.Sprintf("%.1f%%", 100*tu.Standing), fmt.Sprint(tu.Entries), fmt.Sprintf("%.1f", tu.TrainsPerHour), mins(tu.OccupiedMin))+s.bandName(tu.Occupancy))
		t.faint = append(t.faint, tu.Entries == 0)
	}
	return t
}

// stationsTable is a row per station: its platform tracks' occupancy and
// standing.
func (s *stats) stationsTable() table {
	widths := []int{9, 4, 15, 6, 10}
	right := []bool{false, true, false, true, true}
	t := table{header: cols(widths, right, " station", "pf", "occupied", "", "standing") + "band", empty: " no stations"}
	for _, st := range s.stations() {
		t.rows = append(t.rows, cols(widths, right, " "+st.Stn, fmt.Sprint(st.Platforms), bar(st.Occupancy, 13, s.occColor(st.Occupancy)), pct(st.Occupancy),
			fmt.Sprintf("%.0f min", st.StandingMin))+s.bandName(st.Occupancy))
	}
	return t
}

// occFigures are the boxes across the top of the occupancy tables.
func (s *stats) occFigures() []panel {
	u := s.rep.Utilization
	panels := []panel{
		{title: "measured over", lines: []string{
			tui.Figure("from", clock(u.FromMin)),
			tui.Figure("to", clock(u.ToMin)),
			tui.Figure("window", mins(u.ToMin-u.FromMin)+" min"),
		}},
		s.blocksPanel(),
		s.platformsPanel(),
	}
	if len(u.Bands) > 0 {
		var lines []string
		for i, band := range u.Bands {
			upTo := "+"
			if i+1 < len(u.Bands) {
				upTo = fmt.Sprintf("–%g", u.Bands[i+1].OverPct)
			}
			n := 0
			for _, tu := range u.Tracks {
				if tu.Kind == report.KindBlock && tu.Entries > 0 && u.Band(tu.Occupancy) == i {
					n++
				}
			}
			lines = append(lines, colored(cmp.Or(band.Color, rampColor(i, len(u.Bands))), "■ ")+fit(band.Name, 11)+fit(fmt.Sprintf("%g%s%%", band.OverPct, upTo), 9)+blocks(n))
		}
		panels = append(panels, panel{title: "capacity bands", lines: lines})
	}
	return panels
}

// occTablePage draws the occupancy tables w by h, or as tall as they need
// if h <= 0: the figures across the top, the blocks (and throats) on the
// left, the stations and the picked one's platform tracks on the right.
func (s *stats) occTablePage(w, h int) []string {
	top := boxRows(w, h/3, s.occFigures())
	wide := w >= 150
	rw := w / 2
	if wide {
		rw = min(max(w*45/100, 70), 90)
	}
	lw := w - rw
	tables := map[int]table{tableBlocks: s.tracksTable(tableBlocks), tableStations: s.stationsTable(), tablePlatforms: s.tracksTable(tablePlatforms)}
	if s.throats {
		tables[tableThroats] = s.tracksTable(tableThroats)
	}
	need := func(tbl int) int { return len(tables[tbl].rows) + 3 }
	body := h - len(top)
	if h <= 0 {
		body = max(need(tableBlocks)+map[bool]int{true: need(tableThroats)}[s.throats], need(tableStations)+need(tablePlatforms))
	}
	body = max(body, 8)

	draw := func(tbl int, title string, w, h int) []string {
		return tableBox(title, tables[tbl], s.sel(s.oSel[tbl]), s.oFocus == tbl, &s.oOff[tbl], w, h)
	}
	sortBy := " · by " + occSorts[s.oSort].name
	stations := func(h int) []string {
		stationsH := min(need(tableStations), h/2)
		return append(draw(tableStations, "stations"+sortBy, rw, stationsH),
			draw(tablePlatforms, "platform tracks · "+s.selectedStation(), rw, h-stationsH)...)
	}
	if !wide {
		// the blocks across the screen, the rest below them
		blocksH := min(need(tableBlocks), body/2)
		out := append(top, draw(tableBlocks, "blocks"+sortBy, w, blocksH)...)
		lowH := body - blocksH
		if !s.throats {
			return append(out, beside(draw(tableStations, "stations"+sortBy, lw, lowH), draw(tablePlatforms, "platform tracks · "+s.selectedStation(), rw, lowH), lw, rw)...)
		}
		return append(out, beside(draw(tableThroats, "throats"+sortBy, lw, lowH), stations(lowH), lw, rw)...)
	}
	left := draw(tableBlocks, "blocks"+sortBy, lw, body)
	if s.throats {
		blocksH := min(need(tableBlocks), body/2)
		left = append(draw(tableBlocks, "blocks"+sortBy, lw, blocksH), draw(tableThroats, "throats"+sortBy, lw, body-blocksH)...)
	}
	return append(top, beside(left, stations(body), lw, rw)...)
}

// occColor is the colour of an occupancy by the profile's capacity bands,
// or over the even ramp without them.
func (s *stats) occColor(occ float64) string {
	u := s.rep.Utilization
	if len(u.Bands) == 0 {
		return rampColor(rampStep(occ, 1), rampSteps)
	}
	if b := u.Band(occ); b >= 0 {
		return cmp.Or(u.Bands[b].Color, rampColor(b, len(u.Bands)))
	}
	return ""
}

// bandName is the capacity band an occupancy is in, "" without bands.
func (s *stats) bandName(occ float64) string {
	u := s.rep.Utilization
	if b := u.Band(occ); b >= 0 {
		return colored(cmp.Or(u.Bands[b].Color, rampColor(b, len(u.Bands))), u.Bands[b].Name)
	}
	return ""
}

func (m *model) occTableKey(key string) bool {
	s := m.st
	if m.tableKey(key) {
		return true
	}
	switch key {
	case "tab":
		s.oFocus = (s.oFocus + 1) % s.occTables()
	case "shift+tab":
		s.oFocus = (s.oFocus + s.occTables() - 1) % s.occTables()
	case "c":
		s.oSort = (s.oSort + 1) % len(occSorts)
		s.oSel, s.oOff = [4]int{}, [4]int{}
	case "t":
		s.throats = !s.throats
		if !s.throats && s.oFocus == tableThroats {
			s.oFocus = tableBlocks
		}
	case "v":
		s.table, s.oCauses = false, true
	case "esc":
		s.table = false
		m.redraw()
	case "enter":
		if s.oFocus == tableStations {
			s.oFocus = tablePlatforms
			return true
		}
		if tracks := s.occTracks(s.oFocus); len(tracks) > 0 {
			m.showTrack(s.trackID[tracks[min(s.oSel[s.oFocus], len(tracks)-1)].Track])
		}
	default:
		return false
	}
	return true
}

// showTrack goes to the occupancy schematic, centred on the track and
// picking it out.
func (m *model) showTrack(id string) {
	s := m.st
	s.table, s.focus = false, id
	m.redraw()
	if x, y, ok := m.frame.Where(schematic.Piece{Kind: schematic.PieceTrack, ID: id}); ok {
		m.centreOn(x, y)
	} else {
		m.note = "the track is not drawn at this zoom: + zooms in"
	}
}
