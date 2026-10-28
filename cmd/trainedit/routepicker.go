package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
	"trainapp/worlds/edit"
)

// The route picker: a train's route picked rather than typed. The head has
// the caller's fields (a train's number, a pattern's interval, ...), then
// from, to, when it enters and its dwell; the ways between from and to are
// a choice; and the table is the chosen way's stations, each with whether
// the train stops there, its times, how long, and on which platform.
// Beside it the world's schematic draws the way.

// maxWays is how many ways the picker offers, shortest first.
const maxWays = 6

// stopChoice is what is picked for a station of a way. A station between
// the ends passes unless it is a stop; an end stops unless it is a pass.
type stopChoice struct {
	stop    bool   // a stop, between the ends
	endPass bool   // a pass, at an end: the train enters or leaves running
	dwell   string // minutes, empty for the route's dwell
	pf      string // empty for any
	// arr and dep time a stop, keeping to them, with min its least dwell
	// when late; arr alone times a pass. Empty for untimed.
	arr, dep, min string
}

// stops reports whether the train stops at a station of a way, an end or
// not.
func (c stopChoice) stops(end bool) bool {
	if end {
		return !c.endPass
	}
	return c.stop
}

// pickedRoute is a route as picked: the head's values by key, the way, and
// each station's stop.
type pickedRoute struct {
	head  values
	way   edit.Route
	entry float64
	dwell float64
	stops map[string]stopChoice
	// picked is the entry as picked: a train entering later (a pattern's)
	// keeps its times as much later
	picked float64
}

// points are the route's schedule points: a stop at each end (unless it is
// picked to pass) and wherever picked, a pass elsewhere; the first enters
// at entry. A stop keeps to its arr and dep if it has a dep, else stands
// its dwell (the route's, unless it has its own). out is the way as picked:
// a train running it back takes no platforms or times.
func (r pickedRoute) points(stations []string, out bool) ([]ops.PointSpec, error) {
	var pts []ops.PointSpec
	for i, stn := range stations {
		c := r.stops[stn]
		p := ops.PointSpec{Stn: stn, Kind: ops.SpPass}
		var err error
		at := func(text, what string) *float64 {
			if err != nil || !out {
				return nil
			}
			var t *float64
			if t, err = parseTime(text, what+" at "+stn); t != nil {
				t = ptr(*t + r.entry - r.picked)
			}
			return t
		}
		p.Arr = at(c.arr, "arr")
		if c.stops(i == 0 || i == len(stations)-1) {
			p.Kind = ops.SpStop
			if p.Dep = at(c.dep, "dep"); p.Dep == nil {
				p.Dwell = ptr(r.dwell)
				if c.dwell != "" {
					d, err := parseNum(values{"d": c.dwell}, "d", "the dwell at "+stn)
					if err != nil {
						return nil, err
					}
					p.Dwell = ptr(d)
				}
			} else if c.dwell != "" {
				return nil, fmt.Errorf("%s: a stop keeps to its dep or stands its dwell, not both", stn)
			}
			if out && c.min != "" {
				m, err := parseNum(values{"m": c.min}, "m", "the min dwell at "+stn)
				if err != nil {
					return nil, err
				}
				p.MinDwell = ptr(m)
			}
		}
		if err != nil {
			return nil, err
		}
		if out {
			p.Pf = c.pf
		}
		if i == 0 {
			p.Arr = ptr(r.entry)
		}
		if _, err := p.Point(i == 0); err != nil {
			return nil, fmt.Errorf("%s: %w", stn, err)
		}
		pts = append(pts, p)
	}
	return pts, nil
}

type routePicker struct {
	m     *model
	title string
	sh    sheet

	ways  []edit.Route
	found [2]string             // the from and to the ways are for
	kept  map[string]stopChoice // stops picked, by station, kept as the way changes
	err   string
	// prefer is the stations of the way to start on, if it is one, e.g. a
	// train's own when it is re-routed
	prefer []string

	ok func(r pickedRoute) error
}

// openRoutePicker opens the picker over the tab: head are the caller's
// rows, from and to where the route starts out, entry and dwell its times,
// prefer the stations of the way to start on and kept the stops; ok takes
// the route when enter is pressed, and the picker stays open with its error
// if it fails.
func (m *model) openRoutePicker(title string, head [][]field, from, to string, entry, dwell float64, prefer []string, kept map[string]stopChoice, ok func(pickedRoute) error) *routePicker {
	codes := m.s.stationCodes()
	if from == "" && len(codes) > 0 {
		from = codes[0]
	}
	if to == "" && len(codes) > 0 {
		to = codes[len(codes)-1]
	}
	r := &routePicker{m: m, title: title, kept: kept, prefer: prefer, ok: ok}
	if r.kept == nil {
		r.kept = map[string]stopChoice{}
	}
	r.sh.head = append(head,
		[]field{choice("from", "from", from, codes...), choice("to", "to", to, codes...)},
		[]field{
			field{key: "entry", label: "enters at", value: showTime(ptr(entry))},
			field{key: "dwell", label: "dwell (min)", value: showNum(ptr(dwell))},
		},
		[]field{{key: "way", label: "way", fixed: true}},
	)
	r.sh.cols = []column{{"stop", 5}, {"station", 9}, {"arr", 7}, {"dep", 7}, {"dwell", 6}, {"min", 5}, {"platform", 10}}
	r.refresh()
	r.sh.fix()
	m.panel = r
	return r
}

// refresh finds the ways again if from or to changed, and lays the way's
// stations out as the table, keeping what was picked at each.
func (r *routePicker) refresh() {
	from, _ := cellValue(r.sh.head, "from")
	to, _ := cellValue(r.sh.head, "to")
	wayRow := len(r.sh.head) - 1
	if r.found != [2]string{from, to} {
		r.found = [2]string{from, to}
		r.ways, r.err = nil, ""
		if _, err := edit.FindRoute(r.m.s.world.S, from, to, ""); err != nil {
			r.err = err.Error()
		} else {
			r.ways = edit.Routes(r.m.s.world.S, from, to, maxWays)
		}
		f := field{key: "way", label: "way", fixed: true, value: r.err}
		if len(r.ways) > 0 {
			var labels []string
			for _, w := range r.ways {
				labels = append(labels, w.Label())
			}
			pick := labels[0]
			for _, w := range r.ways {
				if slices.Equal(w.Stations, r.prefer) {
					pick = w.Label()
				}
			}
			f = choice("way", "way", pick, labels...)
			if len(labels) == 1 {
				f.fixed = true
			} else {
				f.label = fmt.Sprintf("way (%d)", len(labels))
			}
		}
		r.sh.head[wayRow] = []field{f}
	}
	r.keep()
	w, ok := r.way()
	r.sh.table = nil
	if !ok {
		return
	}
	for i, stn := range w.Stations {
		c := r.kept[stn]
		end := i == 0 || i == len(w.Stations)-1
		stop := choice("stop:"+stn, "", "·", "·", "■")
		if c.stops(end) {
			stop.value = "■"
		}
		stops := stop.value == "■"
		pfs := append([]string{"any"}, r.m.s.platforms(stn)...)
		r.sh.table = append(r.sh.table, []field{
			stop,
			{key: "stn:" + stn, value: stn, fixed: true},
			{key: "arr:" + stn, value: c.arr, off: i == 0}, // the first's is the entry
			{key: "dep:" + stn, value: c.dep, off: !stops},
			{key: "dwell:" + stn, value: c.dwell, off: !stops},
			{key: "min:" + stn, value: c.min, off: !stops},
			choice("pf:"+stn, "", tui.ValueOr(c.pf, "any"), pfs...),
		})
	}
}

// rowValue is the value of a table row's cell whose key starts with kind
// (e.g. "pf:"), trimmed.
func rowValue(cells []field, kind string) string {
	for _, f := range cells {
		if strings.HasPrefix(f.key, kind) {
			return strings.TrimSpace(f.value)
		}
	}
	return ""
}

// keep notes what the table has picked at each station.
func (r *routePicker) keep() {
	for i, cells := range r.sh.table {
		stn := strings.TrimPrefix(cells[0].key, "stop:")
		c := r.kept[stn] // what it is in the middle, or at an end, kept for when it moves
		c.dwell, c.pf, c.dep, c.min = rowValue(cells, "dwell:"), rowValue(cells, "pf:"), rowValue(cells, "dep:"), rowValue(cells, "min:")
		if i > 0 {
			c.arr = rowValue(cells, "arr:")
		}
		if c.pf == "any" {
			c.pf = ""
		}
		if i == 0 || i == len(r.sh.table)-1 {
			c.endPass = cells[0].value == "·"
		} else {
			c.stop = cells[0].value == "■"
		}
		r.kept[stn] = c
	}
}

// way is the way chosen, if there is one.
func (r *routePicker) way() (edit.Route, bool) {
	label, _ := cellValue(r.sh.head, "way")
	for _, w := range r.ways {
		if w.Label() == label {
			return w, true
		}
	}
	return edit.Route{}, false
}

func (r *routePicker) cells() *sheet { return &r.sh }

func (r *routePicker) key(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "esc":
		return true
	case "enter":
		if err := r.accept(); err != nil {
			r.m.fail(err.Error())
			return false
		}
		return true
	case "space":
		// a station's stop or pass, whichever of its cells is picked
		if r.sh.sec == 1 && r.sh.row < len(r.sh.table) {
			stop := &r.sh.table[r.sh.row][0]
			stop.value = map[string]string{"■": "·", "·": "■"}[stop.value]
			r.refresh()
			r.sh.fix()
			return false
		}
	}
	if r.sh.key(msg) {
		r.refresh()
		r.sh.fix()
	}
	return false
}

func (r *routePicker) accept() error {
	w, ok := r.way()
	if !ok {
		return errors.New(tui.ValueOr(r.err, "pick a way"))
	}
	r.keep()
	v := values{}
	for _, row := range r.sh.head {
		for _, f := range row {
			v[f.key] = strings.TrimSpace(f.value)
		}
	}
	at, err := parseTime(v["entry"], "entry")
	if err != nil {
		return err
	}
	if at == nil {
		return errors.New("a train needs an entry time")
	}
	dwell, err := parseNum(v, "dwell", "dwell")
	if err != nil {
		return err
	}
	return r.ok(pickedRoute{head: v, way: w, entry: *at, picked: *at, dwell: dwell, stops: r.kept})
}

func (r *routePicker) render(w, h int) []string {
	return sidePanes(r.title, r.sh.render(w, h-2), "the way", r.preview, w, h)
}

// preview is the world's schematic with the way drawn on it, the picked
// station selected, scrolled to show it.
func (r *routePicker) preview(w, h int) []string {
	d := r.m.world.diagram
	if d == nil {
		return nil
	}
	s := r.m.s.world.S
	marks := map[string]schematic.Mark{}
	way, ok := r.way()
	if ok {
		// over the platforms picked, if the track allows
		var pts []ops.PointSpec
		for _, stn := range way.Stations {
			pts = append(pts, ops.PointSpec{Stn: stn, Pf: r.kept[stn].pf})
		}
		path, err := edit.TrainPath(s, pts)
		if err != nil {
			path = way.Points
		}
		markWay(s, path, marks)
	}
	focus := ""
	if r.sh.sec == 1 && r.sh.row < len(r.sh.table) {
		row := r.sh.table[r.sh.row]
		focus = rowValue(row, "stn:")
		marks[focus] = schematic.MarkSelected
		if strings.HasPrefix(row[r.sh.col].key, "pf:") { // the platform being picked
			if t := pfTrack(s, focus, rowValue(row, "pf:")); t != "" {
				marks[t] = schematic.MarkSelected
			}
		}
	} else if ok {
		focus = way.Stations[0]
	}
	return viewAround(d.Draw(schematic.Options{Level: schematic.Layout, Zoom: 1, Marks: marks}), focus, w, h)
}

// markWay marks the tracks between a way's points as a route.
func markWay(s *infra.Snapshot, points []string, marks map[string]schematic.Mark) {
	track := map[[2]string]string{}
	for _, t := range s.Tracks {
		track[[2]string{t.From, t.To}], track[[2]string{t.To, t.From}] = t.ID, t.ID
	}
	for i := 0; i+1 < len(points); i++ {
		marks[track[[2]string{points[i], points[i+1]}]] = schematic.MarkRoute
	}
}

// pfTrack is the track of a station's platform (or through track) no, empty
// if there is none.
func pfTrack(s *infra.Snapshot, stn, no string) string {
	for _, st := range s.Stations {
		for _, pf := range st.Platforms {
			if st.Code == stn && pf.No == no {
				return pf.Track
			}
		}
	}
	return ""
}

// viewAround is w by h of a drawn frame, scrolled to show the station focus.
func viewAround(f *schematic.Frame, focus string, w, h int) []string {
	x, y := 0, 0
	if fx, fy, found := f.Station(focus); found {
		x = min(max(fx-w/2, 0), max(f.Width()-w, 0))
		y = min(max(fy-h/2, 0), max(f.Height()-h, 0))
	}
	return f.View(x, y, w, min(h, f.Height()), true)
}

func (r *routePicker) keyLine() string {
	return " ↑↓ row  tab cell  ←→ change  space stop or pass  enter ok  esc cancel"
}

// stopsOf are the stops a train has, by station, for the picker to start
// from.
func stopsOf(tr ops.TrainSpec) map[string]stopChoice {
	out := map[string]stopChoice{}
	for i, p := range tr.Points {
		end := i == 0 || i == len(tr.Points)-1
		c := stopChoice{stop: p.Kind == ops.SpStop, endPass: end && p.Kind == ops.SpPass, dwell: showNum(p.Dwell), pf: p.Pf,
			dep: showTime(p.Dep), min: showNum(p.MinDwell)}
		if i > 0 { // the first's is the entry
			c.arr = showTime(p.Arr)
		}
		out[p.Stn] = c
	}
	return out
}
