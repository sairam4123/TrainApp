package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/compare"
)

// The compare delay page. On one world: the trains, matched by number;
// the picked train's losses in each run, place by place; the stations; and
// the waits by place. On different worlds, only the stations they share.

// cmpSorts order the trains table.
var cmpSorts = []string{"largest change", "ΔB", "ΔC", "train order"}

// cmpFilters pick the trains the table lists.
var cmpFilters = []string{"all", "changed", "missing from a run"}

// changed is the least change in a train's delay the filter counts.
const changed = 0.05

// delayOf is a train's delay in run k, if it has one there.
func delayOf(row compare.TrainRow, k int) (float64, bool) {
	if t := row.Runs[k]; t != nil && t.DelayMin != nil {
		return *t.DelayMin, true
	}
	return 0, false
}

// trainDelta is a train's change in delay in run k against A.
func trainDelta(row compare.TrainRow, k int) (float64, bool) {
	a, okA := delayOf(row, 0)
	b, okB := delayOf(row, k)
	return b - a, okA && okB
}

// largestDelta is a train's largest change in delay among the runs.
func largestDelta(row compare.TrainRow) float64 {
	best := 0.0
	for k := 1; k < len(row.Runs); k++ {
		if d, ok := trainDelta(row, k); ok && abs64(d) > abs64(best) {
			best = d
		}
	}
	return best
}

// cmpTrains are the trains the filter keeps, in the sort's order.
func (m *cmpModel) cmpTrains() []compare.TrainRow {
	var out []compare.TrainRow
	for _, row := range m.trains {
		missing := slices.Contains(row.Runs, nil)
		switch m.dFilter {
		case 1:
			if abs64(largestDelta(row)) < changed {
				continue
			}
		case 2:
			if !missing {
				continue
			}
		}
		out = append(out, row)
	}
	key := func(row compare.TrainRow) float64 {
		switch m.dSort {
		case 0:
			return abs64(largestDelta(row))
		case 1, 2:
			d, _ := trainDelta(row, min(m.dSort, len(m.shown)-1))
			return d
		}
		return 0
	}
	if m.dSort < 3 {
		slices.SortStableFunc(out, func(a, b compare.TrainRow) int { return cmp.Compare(key(b), key(a)) })
	}
	return out
}

// cmpTrainsTable is a row per train: its delay, and lateness if any run is
// timed, in each run, and the Δs.
func (m *cmpModel) cmpTrainsTable(rows []compare.TrainRow) table {
	timed := slices.ContainsFunc(m.reps, func(r *report.Report) bool { return r.Lateness != nil })
	widths, right := []int{10, 14}, []bool{false, false}
	head := []string{" train", "class"}
	for k := range m.shown {
		widths, right = append(widths, 8), append(right, true)
		head = append(head, "delay "+letters[k])
		if k > 0 {
			widths, right = append(widths, 7), append(right, true)
			head = append(head, "Δ"+letters[k])
		}
		if timed {
			widths, right = append(widths, 7), append(right, true)
			head = append(head, "late "+letters[k])
		}
	}
	base := m.runs[m.shown[0]].rep
	t := table{header: cols(widths, right, head...), empty: " no trains"}
	for _, row := range rows {
		name := row.Train
		if slices.Contains(row.Retimed, true) {
			name += " ⟳"
		}
		class := ""
		for _, tr := range row.Runs {
			if tr != nil {
				class = tr.Class
				break
			}
		}
		cells := []string{" " + base.classStyle(row.Train).Render(name), class}
		for k := range m.shown {
			cells = append(cells, optMins(optDelay(row.Runs[k])))
			if k > 0 {
				a, okA := delayOf(row, 0)
				b, okB := delayOf(row, k)
				cells = append(cells, delta(a, b, okA, okB, compare.Minutes, compare.Lower))
			}
			if timed {
				var late *float64
				if row.Runs[k] != nil {
					late = row.Runs[k].LateMin
				}
				cells = append(cells, optMins(late))
			}
		}
		t.rows = append(t.rows, cols(widths, right, cells...))
		t.faint = append(t.faint, slices.Contains(row.Runs, nil))
	}
	return t
}

func optDelay(t *report.Train) *float64 {
	if t == nil {
		return nil
	}
	return t.DelayMin
}

// pickedTrain is the train picked on the trains table.
func (m *cmpModel) pickedTrain() (compare.TrainRow, bool) {
	rows := m.cmpTrains()
	if len(rows) == 0 {
		return compare.TrainRow{}, false
	}
	return rows[min(m.dSel[cdTrains], len(rows)-1)], true
}

// trainLosses is the picked train's box: its delay and waits in each run,
// then its losses place by place, the biggest cause at each.
func (m *cmpModel) trainLosses() (string, []string) {
	row, ok := m.pickedTrain()
	if !ok {
		return "train", []string{dim.Render("no trains")}
	}
	var lines []string
	for k := range m.shown {
		t := row.Runs[k]
		if t == nil {
			lines = append(lines, bold.Render(letters[k])+dim.Render(" not in this run"))
			continue
		}
		s := fmt.Sprintf("%s delay %s · %d waits, %s min", bold.Render(letters[k]), optMins(t.DelayMin), len(t.Waits), mins(t.WaitMin))
		if row.Retimed[k] {
			s += dim.Render(" · ⟳ retimed")
		}
		lines = append(lines, s)
	}
	type place struct {
		at    string
		cause []string
		min   []float64
		big   []float64
	}
	var places []*place
	byAt := map[string]*place{}
	for k := range m.shown {
		if row.Runs[k] == nil {
			continue
		}
		for _, l := range row.Runs[k].Losses {
			p := byAt[l.At]
			if p == nil {
				p = &place{at: l.At, cause: make([]string, len(m.shown)), min: make([]float64, len(m.shown)), big: make([]float64, len(m.shown))}
				byAt[l.At] = p
				places = append(places, p)
			}
			p.min[k] += l.Min
			if abs64(l.Min) > abs64(p.big[k]) {
				p.big[k], p.cause[k] = l.Min, l.Cause
			}
		}
	}
	if len(places) == 0 {
		return "train · " + row.Train, append(lines, "", dim.Render("no losses: no free-running times, or no delay"))
	}
	widths, right := []int{16}, []bool{false}
	head := []string{"place"}
	for k := range m.shown {
		widths, right = append(widths, 13, 6), append(right, false, true)
		head = append(head, letters[k]+" cause", "min")
	}
	lines = append(lines, "", dim.Render(cols(widths, right, head...)))
	for _, p := range places {
		if slices.ContainsFunc(p.min, func(v float64) bool { return abs64(v) >= minShown }) {
			cells := []string{p.at}
			for k := range m.shown {
				if abs64(p.min[k]) < minShown {
					cells = append(cells, dim.Render("—"), "")
					continue
				}
				cells = append(cells, p.cause[k], mins(p.min[k]))
			}
			lines = append(lines, cols(widths, right, cells...))
		}
	}
	return "train · " + row.Train, lines
}

// cmpStationsTable is a row per station: its mean delay, on time and
// platform occupancy in each run, with the Δs.
func (m *cmpModel) cmpStationsTable() table {
	widths, right := []int{9}, []bool{false}
	head := []string{" station"}
	for k := range m.shown {
		widths, right = append(widths, 8), append(right, true)
		head = append(head, "mean "+letters[k])
		if k > 0 {
			widths, right = append(widths, 7), append(right, true)
			head = append(head, "Δ"+letters[k])
		}
		widths, right = append(widths, 8, 7), append(right, true, true)
		head = append(head, "on time", "pf occ")
	}
	t := table{header: cols(widths, right, head...), empty: " no stations to compare"}
	widest := 0.0
	for _, r := range m.reps {
		if r.Delay != nil && len(r.Delay.Punctual) > 0 {
			widest = max(widest, r.Delay.Punctual[len(r.Delay.Punctual)-1].WithinMin)
		}
	}
	for _, s := range m.stations {
		cells := []string{" " + s.Stn}
		for k := range m.shown {
			sd, su := s.Delay[k], s.Use[k]
			mean, onTime, occ := "—", "—", "—"
			if sd != nil {
				mean = mins(sd.MeanMin)
				for _, p := range sd.Punctual {
					if p.WithinMin == widest {
						onTime = pct(p.Share)
					}
				}
			}
			if su != nil {
				occ = pct(su.Occupancy)
			}
			cells = append(cells, mean)
			if k > 0 {
				var a, b float64
				if s.Delay[0] != nil {
					a = s.Delay[0].MeanMin
				}
				if sd != nil {
					b = sd.MeanMin
				}
				cells = append(cells, delta(a, b, s.Delay[0] != nil, sd != nil, compare.Minutes, compare.Lower))
			}
			cells = append(cells, onTime, occ)
		}
		t.rows = append(t.rows, cols(widths, right, cells...))
	}
	return t
}

// cmpPlacesTable is a row per place the trains waited at, of each kind:
// each run's minutes there, and the Δs.
func (m *cmpModel) cmpPlacesTable() table {
	widths, right := []int{15, 9}, []bool{false, false}
	head := []string{" place", "kind"}
	for k := range m.shown {
		widths, right = append(widths, 7), append(right, true)
		head = append(head, letters[k])
		if k > 0 {
			widths, right = append(widths, 7), append(right, true)
			head = append(head, "Δ"+letters[k])
		}
	}
	t := table{header: cols(widths, right, head...), empty: " no waits"}
	for _, p := range m.places {
		cells := []string{" " + p.At, p.Kind}
		for k := range m.shown {
			cells = append(cells, mins(p.Min[k]))
			if k > 0 {
				cells = append(cells, delta(p.Min[0], p.Min[k], true, true, compare.Minutes, compare.Lower))
			}
		}
		t.rows = append(t.rows, cols(widths, right, cells...))
	}
	return t
}

// leftOut lists each run's stations the comparison leaves out, on
// different worlds.
func (m *cmpModel) leftOut() []string {
	var out []string
	for k, r := range m.shownRuns() {
		var codes []string
		for _, s := range r.hello.World.Stations {
			if !slices.Contains(m.shared, s.Code) {
				codes = append(codes, s.Code)
			}
		}
		if len(codes) > 0 {
			out = append(out, fmt.Sprintf(" only in %s: %s", letters[k], strings.Join(codes, ", ")))
		}
	}
	return out
}

func (m *cmpModel) delayPage(w, h int) []string {
	sel := func(tbl int) int {
		if m.plain {
			return -1
		}
		return m.dSel[tbl]
	}
	box := func(tbl int, title string, t table, w, h int) []string {
		return tableBox(title, t, sel(tbl), m.dFocus == tbl, &m.dOff[tbl], w, h)
	}
	stations := m.cmpStationsTable()
	if !m.oneWorld {
		title := "shared stations · Δ against A · runs on different worlds"
		note := m.leftOut()
		sh := len(stations.rows) + 3
		if h > 0 {
			sh = max(h-len(note), 4)
		}
		return append(box(cdStations, title, stations, w, sh), dim.Render(strings.Join(note, " ·")))
	}
	rows := m.cmpTrains()
	trains, places := m.cmpTrainsTable(rows), m.cmpPlacesTable()
	trainsTitle := fmt.Sprintf("trains · %s · %d of %d · by %s", cmpFilters[m.dFilter], len(rows), len(m.trains), cmpSorts[m.dSort])
	detailTitle, detail := m.trainLosses()

	trainsH, stationsH, placesH, detailH := len(trains.rows)+3, len(stations.rows)+3, len(places.rows)+3, len(detail)+2
	wide := w >= 150
	rw := min(max(w*2/5, 56), 84)
	if !wide {
		rw = w / 2
	}
	body := h
	if h <= 0 {
		if wide {
			body = max(trainsH+stationsH, detailH+placesH)
		} else {
			body = trainsH + stationsH + max(detailH, placesH)
		}
	}
	body = max(body, 12)
	if wide {
		leftTop := body - min(stationsH, body*2/5)
		rightTop := min(detailH, body*3/5)
		if h <= 0 {
			leftTop, rightTop = trainsH, detailH
		}
		left := append(box(cdTrains, trainsTitle, trains, w-rw, leftTop), box(cdStations, "stations · Δ against A", stations, w-rw, body-leftTop)...)
		right := append(panel{title: detailTitle, lines: detail}.draw(rw, rightTop), box(cdPlaces, "waits by place", places, rw, body-rightTop)...)
		return beside(left, right, w-rw, rw)
	}
	lowH := max(detailH, placesH)
	tH, sH := trainsH, stationsH
	if h > 0 {
		lowH = max(body*2/5, 6)
		sH = min(stationsH, (body-lowH)/2)
		tH = body - lowH - sH
	}
	out := append(box(cdTrains, trainsTitle, trains, w, tH), box(cdStations, "stations · Δ against A", stations, w, sH)...)
	return append(out, beside(panel{title: detailTitle, lines: detail}.draw(w-rw, lowH), box(cdPlaces, "waits by place", places, rw, lowH), w-rw, rw)...)
}

func (m *cmpModel) delayKey(key string) {
	if !m.oneWorld {
		tableMove(key, &m.dSel[cdStations], len(m.stations), m.pageRows())
		return
	}
	rows := map[int]int{cdTrains: len(m.cmpTrains()), cdStations: len(m.stations), cdPlaces: len(m.places)}
	switch key {
	case "tab":
		m.dFocus = (m.dFocus + 1) % cdTables
	case "shift+tab":
		m.dFocus = (m.dFocus + cdTables - 1) % cdTables
	case "esc":
		m.dFocus = cdTrains
	case "c":
		m.dSort = (m.dSort + 1) % len(cmpSorts)
		if m.dSort == 2 && len(m.shown) < 3 {
			m.dSort = 3
		}
		m.note = "trains by " + cmpSorts[m.dSort]
	case "f":
		m.dFilter = (m.dFilter + 1) % len(cmpFilters)
		m.dSel[cdTrains], m.dOff[cdTrains] = 0, 0
		m.note = cmpFilters[m.dFilter] + " trains"
	case "enter":
		m.askReplay()
	default:
		tableMove(key, &m.dSel[m.dFocus], rows[m.dFocus], m.pageRows())
	}
}

// askReplay asks which run to open the picked train's longest wait, or the
// picked place's first wait, in.
func (m *cmpModel) askReplay() {
	switch m.dFocus {
	case cdTrains:
		row, ok := m.pickedTrain()
		if !ok {
			return
		}
		m.ask = &replayAsk{what: row.Train, train: row.Train, at: func(k int) float64 {
			t := row.Runs[k]
			if t == nil {
				return 0
			}
			at, longest := t.EntryMin, 0.0
			for _, w := range t.Waits {
				if w.Min > longest {
					at, longest = w.FromMin, w.Min
				}
			}
			return at
		}}
	case cdPlaces:
		if len(m.places) == 0 {
			return
		}
		p := m.places[min(m.dSel[cdPlaces], len(m.places)-1)]
		m.ask = &replayAsk{what: "the waits at " + p.At, at: func(k int) float64 {
			first := -1.0
			for _, t := range m.reps[k].PerTrain {
				for _, w := range t.Waits {
					if w.At == p.At && w.Kind == p.Kind && (first < 0 || w.FromMin < first) {
						first = w.FromMin
					}
				}
			}
			return max(first, 0)
		}}
	}
}
