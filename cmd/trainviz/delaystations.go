package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"
)

// The delay at each station and on each block: the stations table, the
// calls at the picked station, and the delay page's schematic view.

// goodBad colours a share from bad to good where the profile sets no
// colours: red for few trains on time, green for all.
var goodBad = []string{"203", "208", "214", "149", "42"}

// shareColor is the colour of the share of calls on time.
func shareColor(share float64) string {
	return goodBad[min(max(int(share*float64(len(goodBad))), 0), len(goodBad)-1)]
}

// minsColor is the colour of a delay in minutes: by the delay bands, or
// over goodBad up to top without them.
func (s *stats) minsColor(v, top float64) string {
	if len(s.rep.DelayBands) > 0 {
		return s.delayColor(v)
	}
	if top <= 0 {
		return goodBad[len(goodBad)-1]
	}
	return goodBad[len(goodBad)-1-min(max(int(v/top*float64(len(goodBad))), 0), len(goodBad)-1)]
}

// headline is the punctuality threshold the stations are judged by: the
// first one covering the on-time band, or the middle one.
func (s *stats) headline() int {
	th := s.thresholds()
	if len(th) == 0 {
		return -1
	}
	if b := s.rep.DelayBands; len(b) > 1 {
		for i, w := range th {
			if w >= b[1].OverMin-1e-9 {
				return i
			}
		}
	}
	return len(th) / 2
}

// onTime is the share of the station's calls on time by the headline
// threshold.
func (s *stats) onTime(sd report.StationDelay) float64 {
	if h := s.headline(); h >= 0 && h < len(sd.Punctual) {
		return sd.Punctual[h].Share
	}
	return 1
}

// stationSorts are the stations table's orders.
var stationSorts = []struct {
	name string
	key  func(*stats, report.StationDelay) float64 // highest first; nil for line order
}{
	{"line order", nil},
	{"fewest on time", func(s *stats, sd report.StationDelay) float64 { return -s.onTime(sd) }},
	{"most gained", func(_ *stats, sd report.StationDelay) float64 { return sd.ApproachMin + sd.StationMin }},
	{"latest", func(_ *stats, sd report.StationDelay) float64 { return sd.MeanMin }},
}

// delayStations are the stations in the sort's order.
func (s *stats) delayStations() []report.StationDelay {
	out := slices.Clone(s.rep.StationDelays)
	if key := stationSorts[s.dStnSort].key; key != nil {
		slices.SortStableFunc(out, func(a, b report.StationDelay) int { return cmp.Compare(key(s, b), key(s, a)) })
	}
	return out
}

// selectedStation is the station picked in the stations table.
func (s *stats) selectedDelayStation() (report.StationDelay, bool) {
	st := s.delayStations()
	if len(st) == 0 {
		return report.StationDelay{}, false
	}
	return st[min(s.dSel[dStations], len(st)-1)], true
}

// delayStationsTable is a row per station: its calls on time by each threshold,
// how late they were and where they gained delay.
func (s *stats) delayStationsTable() table {
	th := s.thresholds()
	widths := []int{9, 6}
	head := []string{" station", "calls"}
	for _, w := range th {
		widths = append(widths, 7)
		head = append(head, fmt.Sprintf("≤%g", w))
	}
	widths = append(widths, 7, 7, 8, 7)
	head = append(head, "mean", "worst", "before", "at")
	right := make([]bool, len(widths))
	for i := 1; i < len(right); i++ {
		right[i] = true
	}
	t := table{header: cols(widths, right, head...) + " on time", empty: " no stations: the run has no free-running times"}
	for _, sd := range s.delayStations() {
		cells := []string{" " + sd.Stn, fmt.Sprint(sd.Calls)}
		for _, p := range sd.Punctual {
			cells = append(cells, colored(shareColor(p.Share), pct(p.Share)))
		}
		cells = append(cells, colored(s.minsColor(sd.MeanMin, 30), mins(sd.MeanMin)), mins(sd.WorstMin), signed(sd.ApproachMin), signed(sd.StationMin))
		share := s.onTime(sd)
		t.rows = append(t.rows, cols(widths, right, cells...)+" "+bar(share, 10, shareColor(share)))
	}
	return t
}

// signed is minutes with their sign, e.g. "+3.2"; nothing to speak of is
// +0.0, not -0.0.
func signed(v float64) string {
	if math.Abs(v) < 0.05 {
		v = 0
	}
	return fmt.Sprintf("%+.1f", v)
}

// optSigned is signed for a figure that may be missing.
func optSigned(v *float64) string {
	if v == nil {
		return "—"
	}
	return signed(*v)
}

// stationCall is a train's call at the picked station.
type stationCall struct {
	train report.Train
	call  report.Call
	first bool // the train's first point
}

// at is when the call is measured: on arriving, or on leaving the train's
// first point; -1 if it never got there.
func (c stationCall) at() float64 {
	if c.first {
		return c.call.DepMin
	}
	return c.call.ArrMin
}

// stationCalls are the calls at the picked station the filter keeps, as the
// trains got there.
func (s *stats) stationCalls() []stationCall {
	sd, ok := s.selectedDelayStation()
	if !ok {
		return nil
	}
	f := s.filter()
	var out []stationCall
	for _, t := range s.rep.PerTrain {
		for k, c := range t.Calls {
			if c.Stn == sd.Stn && f.admits(c.DelayMin) {
				out = append(out, stationCall{t, c, k == 0})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b stationCall) int {
		ta, tb := a.at(), b.at()
		switch {
		case ta < 0 && tb < 0:
			return 0
		case ta < 0:
			return 1
		case tb < 0:
			return -1
		}
		return cmp.Compare(ta, tb)
	})
	return out
}

// callsTable is a row per call at the picked station.
func (s *stats) callsTable(rep *replay) table {
	timed := s.rep.Lateness != nil
	widths := []int{8, 5, 6, 6, 7, 7, 6}
	head := []string{" train", "", "arr", "dep", "delay", "before", "at"}
	if timed {
		widths = slices.Insert(widths, 5, 6)
		head = slices.Insert(head, 5, "late")
	}
	right := []bool{false, false, true, true, true, true, true, true}
	t := table{header: cols(widths, right, head...) + " band", empty: " no calls " + s.filter().name}
	time := func(v float64) string {
		if v < 0 {
			return "—"
		}
		return clock(v)
	}
	for _, sc := range s.stationCalls() {
		c := sc.call
		kind := "stop"
		if c.Pass {
			kind = "pass"
		}
		delay, band := optMins(c.DelayMin), ""
		if c.DelayMin != nil {
			delay = colored(s.delayColor(*c.DelayMin), delay)
			band = s.delayBandName(*c.DelayMin)
		}
		cells := []string{" " + rep.classStyle(sc.train.Train).Render(sc.train.Train), dim.Render(kind), time(c.ArrMin), time(c.DepMin), delay, optSigned(c.ApproachMin), optSigned(c.StationMin)}
		if timed {
			cells = slices.Insert(cells, 5, optMins(c.LateMin))
		}
		t.rows = append(t.rows, cols(widths, right, cells...)+" "+band)
		t.faint = append(t.faint, c.DelayMin == nil)
	}
	return t
}

// callsTitle names the calls table: the station, the filter and how many
// of its calls it keeps.
func (s *stats) callsTitle() string {
	sd, ok := s.selectedDelayStation()
	if !ok {
		return "calls"
	}
	title := "calls at " + sd.Stn
	if f := s.filter(); f.keep != nil {
		title += fmt.Sprintf(" · %s · %d of %d", f.name, len(s.stationCalls()), sd.Calls)
	}
	return title
}

// --- the schematic view ---

// delayMetrics are what the delay schematic colours the stations by; the
// blocks show the delay gained on them throughout.
var delayMetrics = []struct {
	name  string
	of    func(*stats, report.StationDelay) float64
	label func(*stats, float64) string
	color func(*stats, float64) string
}{
	{"on time",
		func(s *stats, sd report.StationDelay) float64 { return s.onTime(sd) },
		func(_ *stats, v float64) string { return pct(v) },
		func(_ *stats, v float64) string { return shareColor(v) }},
	{"mean delay",
		func(_ *stats, sd report.StationDelay) float64 { return sd.MeanMin },
		func(_ *stats, v float64) string { return mins(v) + "m" },
		func(s *stats, v float64) string { return s.minsColor(v, 30) }},
	{"delay gained here",
		func(_ *stats, sd report.StationDelay) float64 {
			if sd.Calls == 0 {
				return 0
			}
			return sd.StationMin / float64(sd.Calls)
		},
		func(_ *stats, v float64) string { return signed(v) + "m" },
		func(s *stats, v float64) string { return s.minsColor(v, 10) }},
}

// blockGain is the delay a train gained on the block on average.
func blockGain(bd report.BlockDelay) float64 {
	if bd.Trains == 0 {
		return 0
	}
	return bd.GainedMin / float64(bd.Trains)
}

// delayHeat colours each station's platform tracks by the metric, and each
// block by the delay a train gained on it.
func (s *stats) delayHeat() (map[string]schematic.Heat, map[string]schematic.Heat) {
	heat, notes := map[string]schematic.Heat{}, map[string]schematic.Heat{}
	if s.rep == nil {
		return heat, notes
	}
	m := delayMetrics[s.dMetric]
	byStn := map[string]report.StationDelay{}
	for _, sd := range s.rep.StationDelays {
		v := m.of(s, sd)
		byStn[sd.Stn] = sd
		notes[sd.Stn] = schematic.Heat{Color: m.color(s, v), Label: m.label(s, v)}
	}
	for _, tu := range s.rep.Utilization.Tracks {
		if sd, ok := byStn[tu.Stn]; ok && tu.Kind == report.KindPlatform && tu.Entries > 0 {
			v := m.of(s, sd)
			heat[s.trackID[tu.Track]] = schematic.Heat{Color: m.color(s, v), Level: v}
		}
	}
	for _, bd := range s.rep.BlockDelays {
		g := blockGain(bd)
		heat[s.trackID[bd.Track]] = schematic.Heat{Color: s.minsColor(g, 5), Level: 10 + g, Label: signed(g) + "m"}
	}
	return heat, notes
}

// delaySide are the boxes beside the delay schematic, w wide and h tall,
// or as tall as they need if h <= 0: the stations' colours, the blocks',
// and the stations.
func (s *stats) delaySide(w, h int) []string {
	m := delayMetrics[s.dMetric]
	var key []string
	switch {
	case s.dMetric == 0:
		th := s.thresholds()
		if hl := s.headline(); hl >= 0 {
			key = append(key, dim.Render(fmt.Sprintf("calls within %g min", th[hl])))
		}
		for i := range goodBad {
			lo, hi := float64(i)/float64(len(goodBad)), float64(i+1)/float64(len(goodBad))
			key = append(key, colored(goodBad[i], "■ ")+pct(lo)+"–"+pct(hi))
		}
	case len(s.rep.DelayBands) > 0:
		for i, b := range s.rep.DelayBands {
			key = append(key, colored(cmp.Or(b.Color, rampColor(i, len(s.rep.DelayBands))), "■ ")+fit(b.Name, 14)+fmt.Sprintf("≥%g min", b.OverMin))
		}
	default:
		key = append(key, dim.Render("green on time, red late"))
	}
	blocks := []string{dim.Render("delay a train gained on it")}
	if bd := s.rep.BlockDelays; len(bd) > 0 {
		worst := slices.MaxFunc(bd, func(a, b report.BlockDelay) int { return cmp.Compare(blockGain(a), blockGain(b)) })
		blocks = append(blocks, tui.Figure("most", fmt.Sprintf("%s %sm", worst.Track, signed(blockGain(worst)))))
	} else {
		blocks = append(blocks, dim.Render("no block delays in this run"))
	}
	var out []string
	for _, p := range []panel{{title: "stations · " + m.name, lines: key}, {title: "blocks", lines: blocks}} {
		out = append(out, p.draw(w, len(p.lines)+2)...)
	}
	widths := []int{9, 6, 8, 7}
	right := []bool{false, true, true, true}
	t := table{header: cols(widths, right, " station", "calls", "on time", "mean"), empty: " no stations"}
	for _, sd := range s.rep.StationDelays {
		share := s.onTime(sd)
		t.rows = append(t.rows, cols(widths, right, " "+sd.Stn, fmt.Sprint(sd.Calls), colored(shareColor(share), pct(share)), mins(sd.MeanMin)))
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

// delayMapKey handles the delay schematic's own keys.
func (m *model) delayMapKey(key string) bool {
	s := m.st
	switch key {
	case "m":
		s.dMetric = (s.dMetric + 1) % len(delayMetrics)
	case "v":
		s.dMap, s.dCauses = false, true
	case "esc":
		s.dMap = false
	default:
		return false
	}
	m.redraw()
	return true
}
