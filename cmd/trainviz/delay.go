package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"
)

// The delay page: the run's delay figures, the trains (filtered by how
// late they were), the stations, the calls at the picked station, and the
// waits by place; or (v) the schematic coloured by the delay at each
// station and block (delaystations.go).

// delaySorts are the trains table's orders.
var delaySorts = []struct {
	name string
	key  func(report.Train) *float64
}{
	{"delay", func(t report.Train) *float64 { return t.DelayMin }},
	{"late", func(t report.Train) *float64 { return t.LateMin }},
	{"wait", func(t report.Train) *float64 { return &t.WaitMin }},
	{"train order", nil},
}

// delayFilter picks trains, or calls, by their delay.
type delayFilter struct {
	name string             // e.g. "late", "within 5 min"
	keep func(float64) bool // nil keeps every one
}

// admits is whether the filter keeps a train or call with delay d, which
// may be nil.
func (f delayFilter) admits(d *float64) bool {
	return f.keep == nil || (d != nil && f.keep(*d))
}

// filters are what b cycles through: all, each delay band, then on time
// and late against each punctuality threshold.
func (s *stats) filters() []delayFilter {
	r := s.rep
	out := []delayFilter{{name: "all"}}
	for i, b := range r.DelayBands {
		out = append(out, delayFilter{name: b.Name, keep: func(d float64) bool { return r.DelayBand(d) == i }})
	}
	for _, w := range s.thresholds() {
		out = append(out,
			delayFilter{name: fmt.Sprintf("within %g min", w), keep: func(d float64) bool { return d <= w+1e-9 }},
			delayFilter{name: fmt.Sprintf("over %g min", w), keep: func(d float64) bool { return d > w+1e-9 }})
	}
	return out
}

// filter is the one in use.
func (s *stats) filter() delayFilter {
	f := s.filters()
	return f[min(s.dFilter, len(f)-1)]
}

// thresholds are the punctuality thresholds, in minutes.
func (s *stats) thresholds() []float64 {
	var out []float64
	if s.rep.Delay != nil {
		for _, p := range s.rep.Delay.Punctual {
			out = append(out, p.WithinMin)
		}
	} else if len(s.rep.StationDelays) > 0 {
		for _, p := range s.rep.StationDelays[0].Punctual {
			out = append(out, p.WithinMin)
		}
	}
	return out
}

// delayTrains are the trains the filter keeps, in the table's order: by
// the sort's figure, highest first and trains without it last, or as the
// sim added them.
func (s *stats) delayTrains() []report.Train {
	f := s.filter()
	var out []report.Train
	for _, t := range s.rep.PerTrain {
		if f.admits(t.DelayMin) {
			out = append(out, t)
		}
	}
	key := delaySorts[s.dSort].key
	if key == nil {
		return out
	}
	slices.SortStableFunc(out, func(a, b report.Train) int {
		ka, kb := key(a), key(b)
		switch {
		case ka == nil && kb == nil:
			return 0
		case ka == nil:
			return 1
		case kb == nil:
			return -1
		}
		return cmp.Compare(*kb, *ka)
	})
	return out
}

// selectedTrain is the train picked in the trains table.
func (s *stats) selectedTrain() (report.Train, bool) {
	trains := s.delayTrains()
	if len(trains) == 0 {
		return report.Train{}, false
	}
	return trains[min(s.dSel[dTrains], len(trains)-1)], true
}

// delayColor is the colour of a delay in minutes by the profile's delay
// bands, "" without them.
func (s *stats) delayColor(v float64) string {
	b := s.rep.DelayBand(v)
	if b < 0 {
		return ""
	}
	return cmp.Or(s.rep.DelayBands[b].Color, rampColor(b, len(s.rep.DelayBands)))
}

// delayBandName is the delay band of v, coloured, "" without bands.
func (s *stats) delayBandName(v float64) string {
	if b := s.rep.DelayBand(v); b >= 0 {
		return colored(s.delayColor(v), s.rep.DelayBands[b].Name)
	}
	return ""
}

// delayPage draws the delay page's tables w by h, or as tall as they need
// if h <= 0: the run's figures across the top; the trains and the stations
// on the left; on the right the picked train, or the calls at the picked
// station, over the waits by place. A narrow screen stacks them.
func (s *stats) delayPage(w, h int, rep *replay) []string {
	top := boxRows(w, h/3, s.delayFigures(rep))
	trains, stations, places := s.trainsTable(rep), s.delayStationsTable(), s.placesTable()
	f := s.filter()
	trainsTitle := "trains · by " + delaySorts[s.dSort].name
	if f.keep != nil {
		trainsTitle = fmt.Sprintf("trains · %s · %d of %d · by %s", f.name, len(trains.rows), len(s.rep.PerTrain), delaySorts[s.dSort].name)
	}
	stationsTitle := "stations · by " + stationSorts[s.dStnSort].name

	// the box over the waits: the calls at the picked station while the
	// stations or calls have the focus, else the picked train
	stationSide := s.dFocus == dStations || s.dFocus == dCalls
	detailTitle, detail := s.trainDetail(rep)
	calls := s.callsTable(rep)
	detailNeed := len(detail) + 2
	if stationSide {
		detailNeed = len(calls.rows) + 3
	}
	drawDetail := func(w, h int) []string {
		if stationSide {
			return tableBox(s.callsTitle(), calls, s.sel(s.dSel[dCalls]), s.dFocus == dCalls, &s.dOff[dCalls], w, h)
		}
		return panel{title: detailTitle, lines: detail}.draw(w, h)
	}
	box := func(tbl int, title string, t table, w, h int) []string {
		return tableBox(title, t, s.sel(s.dSel[tbl]), s.dFocus == tbl, &s.dOff[tbl], w, h)
	}

	trainsH, stationsH, placesH := len(trains.rows)+3, len(stations.rows)+3, len(places.rows)+3
	wide := w >= 150
	rw := min(max(w*38/100, 50), 72)
	if !wide {
		rw = w / 2
	}
	body := h - len(top)
	if h <= 0 {
		if wide {
			body = max(trainsH+stationsH, detailNeed+placesH)
		} else {
			body = trainsH + stationsH + max(detailNeed, placesH)
		}
	}
	body = max(body, 12)

	if wide {
		leftTop := body - min(stationsH, body*2/5)
		if h <= 0 {
			leftTop = trainsH
		}
		left := append(box(dTrains, trainsTitle, trains, w-rw, leftTop), box(dStations, stationsTitle, stations, w-rw, body-leftTop)...)
		rightTop := min(detailNeed, body*3/5)
		if h <= 0 {
			rightTop = detailNeed
		}
		right := append(drawDetail(rw, rightTop), box(dPlaces, "waits by place", places, rw, body-rightTop)...)
		return append(top, beside(left, right, w-rw, rw)...)
	}
	lowH := max(detailNeed, placesH)
	tH, sH := trainsH, stationsH
	if h > 0 {
		lowH = max(body*2/5, 6)
		sH = min(stationsH, (body-lowH)/2)
		tH = body - lowH - sH
	}
	out := append(top, box(dTrains, trainsTitle, trains, w, tH)...)
	out = append(out, box(dStations, stationsTitle, stations, w, sH)...)
	return append(out, beside(drawDetail(w-rw, lowH), box(dPlaces, "waits by place", places, rw, lowH), w-rw, rw)...)
}

// delayFigures are the boxes across the top of the delay page.
func (s *stats) delayFigures(rep *replay) []panel {
	r := s.rep
	done := fmt.Sprintf("%d/%d finished", r.Finished, r.Trains)
	if r.Finished == r.Trains {
		done = tui.Good.Render(done)
	} else {
		done = tui.Failed.Render(done)
	}
	run := []string{tui.Figure("trains", done)}
	if len(r.Unfinished) > 0 {
		run = append(run, tui.Figure("unfinished", strings.Join(r.Unfinished, ", ")))
	}
	stats := func(st *report.Stats) []string {
		out := []string{
			tui.Figure("in all", mins(st.TotalMin)+" min"),
			tui.Figure("a train", mins(st.MeanMin)+" min"),
			tui.Figure("weighted", mins(st.WeightedMeanMin)+" min"),
		}
		if st.Worst != "" {
			out = append(out, tui.Figure("worst", rep.classStyle(st.Worst).Render(st.Worst)+" "+colored(s.delayColor(st.WorstMin), mins(st.WorstMin)+" min")))
		}
		return out
	}
	punctual := func(st *report.Stats) []string {
		var out []string
		for _, p := range st.Punctual {
			out = append(out, fmt.Sprintf("%-14s", fmt.Sprintf("within %g min", p.WithinMin))+bar(p.Share, 14, "42")+fmt.Sprintf(" %4s", pct(p.Share)))
		}
		return out
	}

	panels := []panel{}
	if r.Delay != nil {
		panels = append(panels, panel{title: "delay", lines: append(run, stats(r.Delay)...)},
			panel{title: "punctuality", lines: punctual(r.Delay)})
	} else {
		panels = append(panels, panel{title: "delay", lines: append(run, dim.Render("no free-running times to measure it by"))})
	}
	if r.Lateness != nil {
		panels = append(panels, panel{title: "timetable lateness", lines: append(stats(r.Lateness), punctual(r.Lateness)...)})
	}
	if bands := r.DelayBands; len(bands) > 0 && r.Delay != nil {
		count := make([]int, len(bands))
		for _, t := range r.PerTrain {
			if t.DelayMin != nil && t.Finished {
				if b := r.DelayBand(*t.DelayMin); b >= 0 {
					count[b]++
				}
			}
		}
		var lines []string
		for i, b := range bands {
			lines = append(lines, colored(cmp.Or(b.Color, rampColor(i, len(bands))), "■ ")+fit(b.Name, 14)+fit(fmt.Sprintf("≥%g min", b.OverMin), 9)+fmt.Sprintf("%3d", count[i]))
		}
		panels = append(panels, panel{title: "delay bands", lines: lines})
	}
	if d := r.Disruption; d != nil {
		share := 0.0
		if total := d.PrimaryMin + d.ReactionaryMin; total > 0 {
			share = d.PrimaryMin / total
		}
		panels = append(panels, panel{title: "disruptions", lines: []string{
			tui.Figure("primary", mins(d.PrimaryMin)+" min"),
			tui.Figure("knock-on", mins(d.ReactionaryMin)+" min"),
			tui.Figure("share", bar(share, 14, "203")+" "+pct(share)+" primary"),
		}})
	}
	byKind := map[string]float64{}
	for _, t := range r.PerTrain {
		for _, w := range t.Waits {
			byKind[w.Kind] += w.Min
		}
	}
	waits := []string{tui.Figure("waits", mins(r.WaitMin)+" min")}
	for _, k := range []string{report.WaitEntry, report.WaitSignal, report.WaitStation} {
		waits = append(waits, dim.Render(tui.Fit("  "+k, 11))+mins(byKind[k])+" min")
	}
	if lw := r.LongestWait; lw != nil {
		waits = append(waits, tui.Figure("longest", fmt.Sprintf("%s %s min at %s", rep.classStyle(lw.Train).Render(lw.Train), mins(lw.Min), lw.At)))
	}
	waits = append(waits,
		tui.Figure("holds", fmt.Sprintf("%d · %s min held", r.Holds, mins(r.HeldMin))),
		tui.Figure("pf changes", fmt.Sprint(r.PlatformChanges)))
	return append(panels, panel{title: "waits and holds", lines: waits})
}

// trainsTable is a row per train in the sort's order.
func (s *stats) trainsTable(rep *replay) table {
	r := s.rep
	timed := r.Lateness != nil
	widths := []int{8, 19, 5, 7, 7, 8, 7, 7, 7, 6}
	right := []bool{false, false, true, true, true, true, true, true, true, true}
	head := []string{" train", "class", "prio", "entry", "exit", "journey", "free", "delay", "wait", "waits"}
	if timed {
		widths = slices.Insert(widths, 8, 7)
		right = slices.Insert(right, 8, true)
		head = slices.Insert(head, 8, "late")
	}
	worst := 0.0
	if r.Delay != nil {
		worst = r.Delay.WorstMin
	}
	t := table{header: cols(widths, right, head...) + "delay", empty: " no trains"}
	for _, tr := range s.delayTrains() {
		exit, journey := "—", "—"
		if tr.Finished {
			exit, journey = clock(tr.ExitMin), mins(tr.JourneyMin)
		}
		delay, b := optMins(tr.DelayMin), ""
		if tr.DelayMin != nil {
			c := s.delayColor(*tr.DelayMin)
			delay = colored(c, delay)
			if worst > 0 {
				b = bar(*tr.DelayMin/worst, 20, c)
			}
		}
		cells := []string{" " + rep.classStyle(tr.Train).Render(tr.Train), tr.Class, fmt.Sprint(tr.Priority), clock(tr.EntryMin), exit, journey,
			optMins(tr.FreeRunMin), delay, mins(tr.WaitMin), fmt.Sprint(len(tr.Waits))}
		if timed {
			late := optMins(tr.LateMin)
			if tr.LateMin != nil {
				late = colored(s.delayColor(*tr.LateMin), late)
			}
			cells = slices.Insert(cells, 8, late)
		}
		t.rows = append(t.rows, cols(widths, right, cells...)+b)
		t.faint = append(t.faint, !tr.Finished)
	}
	return t
}

// trainDetail is the picked train's box: its figures and its waits.
func (s *stats) trainDetail(rep *replay) (string, []string) {
	t, ok := s.selectedTrain()
	if !ok {
		return "train", []string{dim.Render("no trains")}
	}
	title := t.Train
	if t.Class != "" {
		title += " · " + t.Class
	}
	var out []string
	if t.DelayMin != nil {
		out = append(out, tui.Figure("delay", colored(s.delayColor(*t.DelayMin), mins(*t.DelayMin)+" min")+dim.Render(" · ")+s.delayBandName(*t.DelayMin)))
	}
	if t.LateMin != nil {
		out = append(out, tui.Figure("late", colored(s.delayColor(*t.LateMin), mins(*t.LateMin)+" min")))
	}
	if t.Finished {
		journey := mins(t.JourneyMin) + " min"
		if t.FreeRunMin != nil {
			journey += dim.Render(" · free running " + mins(*t.FreeRunMin))
		}
		out = append(out, tui.Figure("journey", journey), tui.Figure("ran", clock(t.EntryMin)+" – "+clock(t.ExitMin)))
	} else {
		out = append(out, tui.Figure("journey", tui.Failed.Render("did not finish")), tui.Figure("entry", clock(t.EntryMin)))
	}
	out = append(out, tui.Figure("priority", fmt.Sprint(t.Priority)))
	if t.PrimaryMin > 0 {
		out = append(out, tui.Figure("disrupted", mins(t.PrimaryMin)+" min primary"))
	}
	out = append(out, tui.Figure("waits", fmt.Sprintf("%d · %s min", len(t.Waits), mins(t.WaitMin))))
	for _, w := range t.Waits {
		out = append(out, dim.Render(fmt.Sprintf("  %-8s %-14s %s ", w.Kind, w.At, clock(w.FromMin)))+fmt.Sprintf("%5s min", mins(w.Min)))
	}
	if len(t.Calls) > 0 {
		widths := []int{9, 6, 7, 8, 7}
		right := []bool{false, true, true, true, true}
		out = append(out, "", dim.Render(cols(widths, right, "station", "at", "delay", "before", "at stn")))
		for k, c := range t.Calls {
			at := c.ArrMin
			if k == 0 {
				at = c.DepMin
			}
			when, delay := "—", optMins(c.DelayMin)
			if at >= 0 {
				when = clock(at)
			}
			if c.DelayMin != nil {
				delay = colored(s.delayColor(*c.DelayMin), delay)
			}
			out = append(out, cols(widths, right, c.Stn, when, delay, optSigned(c.ApproachMin), optSigned(c.StationMin)))
		}
	}
	return title, out
}

// waitPlace is the waits at one place, of one kind.
type waitPlace struct {
	at, kind string
	waits    int
	trains   map[string]bool
	min      float64
}

// waitPlaces groups every train's waits by place and kind, most minutes
// first.
func waitPlaces(r *report.Report) []*waitPlace {
	by := map[[2]string]*waitPlace{}
	var out []*waitPlace
	for _, t := range r.PerTrain {
		for _, w := range t.Waits {
			k := [2]string{w.At, w.Kind}
			p := by[k]
			if p == nil {
				p = &waitPlace{at: w.At, kind: w.Kind, trains: map[string]bool{}}
				by[k] = p
				out = append(out, p)
			}
			p.waits++
			p.trains[t.Train] = true
			p.min += w.Min
		}
	}
	slices.SortStableFunc(out, func(a, b *waitPlace) int { return cmp.Compare(b.min, a.min) })
	return out
}

// placesTable is a row per place the trains waited at.
func (s *stats) placesTable() table {
	widths := []int{15, 9, 6, 7, 8}
	right := []bool{false, false, true, true, true}
	t := table{header: cols(widths, right, " place", "kind", "waits", "trains", "min"), empty: " no waits"}
	places := waitPlaces(s.rep)
	most := 0.0
	for _, p := range places {
		most = max(most, p.min)
	}
	for _, p := range places {
		share := 0.0
		if most > 0 {
			share = p.min / most
		}
		t.rows = append(t.rows, cols(widths, right, " "+p.at, p.kind, fmt.Sprint(p.waits), fmt.Sprint(len(p.trains)), mins(p.min))+bar(share, 12, ""))
	}
	return t
}

func (m *model) delayKey(key string) bool {
	s := m.st
	if s.dMap {
		return m.delayMapKey(key)
	}
	if m.tableKey(key) {
		return true
	}
	switch key {
	case "tab":
		s.dFocus = (s.dFocus + 1) % dTables
	case "shift+tab":
		s.dFocus = (s.dFocus + dTables - 1) % dTables
	case "esc":
		switch {
		case s.dFocus == dCalls:
			s.dFocus = dStations
		case s.dFilter != 0:
			s.dFilter, s.dSel[dTrains], s.dSel[dCalls] = 0, 0, 0
		default:
			s.dFocus = dTrains
		}
	case "b":
		s.dFilter = (s.dFilter + 1) % len(s.filters())
		s.dSel[dTrains], s.dOff[dTrains], s.dSel[dCalls], s.dOff[dCalls] = 0, 0, 0, 0
	case "c":
		if s.dFocus == dStations {
			s.dStnSort = (s.dStnSort + 1) % len(stationSorts)
		} else {
			s.dSort = (s.dSort + 1) % len(delaySorts)
		}
	case "v":
		s.dMap = true
		m.redraw()
	case "enter":
		switch s.dFocus {
		case dPlaces:
			if places := waitPlaces(s.rep); len(places) > 0 {
				m.showPlace(places[min(s.dSel[dPlaces], len(places)-1)])
			}
		case dStations:
			s.dFocus = dCalls
		case dCalls:
			if calls := s.stationCalls(); len(calls) > 0 {
				m.showCall(calls[min(s.dSel[dCalls], len(calls)-1)])
			}
		default:
			if t, ok := s.selectedTrain(); ok {
				m.showWait(t)
			}
		}
	default:
		return false
	}
	return true
}

// showCall goes to the schematic with the train selected as it got to the
// station.
func (m *model) showCall(c stationCall) {
	at := c.at()
	if at < 0 {
		m.note = c.train.Train + " never got to " + c.call.Stn
		return
	}
	if i := slices.Index(m.rep.trains, c.train.Train); i >= 0 {
		m.selected = i
	}
	m.rep.seek(at)
	m.setPage(pageSchematic)
	if x, y, ok := m.frame.Station(c.call.Stn); ok {
		m.centreOn(x, y)
	}
	m.note = fmt.Sprintf("%s at %s, %s min late", c.train.Train, c.call.Stn, optMins(c.call.DelayMin))
}

// showWait goes to the schematic with the train selected, at the start of
// its longest wait, or its entry if it had none.
func (m *model) showWait(t report.Train) {
	at := t.EntryMin
	longest := -1.0
	for _, w := range t.Waits {
		if w.Min > longest {
			at, longest = w.FromMin, w.Min
		}
	}
	if i := slices.Index(m.rep.trains, t.Train); i >= 0 {
		m.selected = i
	}
	m.rep.seek(at)
	m.setPage(pageSchematic)
	if longest >= 0 {
		m.note = fmt.Sprintf("%s's longest wait: %s min", t.Train, mins(longest))
	} else {
		m.note = t.Train + " had no waits: its entry"
	}
}

// showPlace goes to the schematic, centred on where the trains waited: a
// station, or for a signal the track they stood on.
func (m *model) showPlace(p *waitPlace) {
	m.setPage(pageSchematic)
	x, y, ok := m.frame.Station(p.at)
	if !ok {
		x, y, ok = m.frame.Where(schematic.Piece{Kind: schematic.PieceTrack, ID: m.st.trackID[p.at]})
	}
	if !ok {
		m.note = p.at + " is not drawn at this zoom: + zooms in"
		return
	}
	m.centreOn(x, y)
	m.note = fmt.Sprintf("%s: %d %s waits, %s min", p.at, p.waits, p.kind, mins(p.min))
}
