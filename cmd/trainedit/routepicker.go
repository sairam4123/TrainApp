package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/ops"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
)

// The route picker: a train's route picked rather than typed. The head has
// the caller's fields (a train's number, a pattern's interval, ...), then
// from, to, when it enters and its dwell; the ways between from and to are
// a choice; and the table is the chosen way's stations, each with whether
// the train stops there, how long, and on which platform. Beside it the
// world's schematic draws the way.

// maxWays is how many ways the picker offers, shortest first.
const maxWays = 6

// stopChoice is what is picked for a station of a way.
type stopChoice struct {
	stop  bool
	dwell string // minutes, empty for the route's dwell
	pf    string // empty for any
}

// pickedRoute is a route as picked: the head's values by key, the way, and
// each station's stop.
type pickedRoute struct {
	head  values
	way   way
	entry float64
	dwell float64
	stops map[string]stopChoice
}

// points are the route's schedule points: a stop at each end and wherever
// picked, a pass elsewhere; the first enters at entry.
func (r pickedRoute) points(stations []string, platforms bool) ([]ops.PointSpec, error) {
	var out []ops.PointSpec
	for i, stn := range stations {
		c := r.stops[stn]
		p := ops.PointSpec{Stn: stn, Kind: ops.SpPass}
		if i == 0 || i == len(stations)-1 || c.stop {
			p.Kind, p.Dwell = ops.SpStop, ptr(r.dwell)
			if c.dwell != "" {
				d, err := parseNum(values{"d": c.dwell}, "d", "the dwell at "+stn)
				if err != nil {
					return nil, err
				}
				p.Dwell = ptr(d)
			}
		}
		if platforms {
			p.Pf = c.pf
		}
		if i == 0 {
			p.Arr = ptr(r.entry)
		}
		out = append(out, p)
	}
	return out, nil
}

type routePicker struct {
	m     *model
	title string
	sh    sheet

	ways  []way
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
	r.sh.cols = []column{{"stop", 5}, {"station", 9}, {"dwell", 6}, {"platform", 10}}
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
		if _, err := routeWay(r.m.s.world.S, from, to, ""); err != nil {
			r.err = err.Error()
		} else {
			r.ways = ways(r.m.s.world.S, from, to, maxWays)
		}
		f := field{key: "way", label: "way", fixed: true, value: r.err}
		if len(r.ways) > 0 {
			var labels []string
			for _, w := range r.ways {
				labels = append(labels, w.label())
			}
			pick := labels[0]
			for _, w := range r.ways {
				if slices.Equal(w.stations, r.prefer) {
					pick = w.label()
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
	for i, stn := range w.stations {
		c := r.kept[stn]
		end := i == 0 || i == len(w.stations)-1
		stop := choice("stop:"+stn, "", "·", "·", "■")
		if c.stop || end {
			stop.value = "■"
		}
		stop.fixed = end
		dwell := field{key: "dwell:" + stn, value: c.dwell, off: stop.value != "■"}
		pfs := append([]string{"any"}, r.m.s.platforms(stn)...)
		pf := choice("pf:"+stn, "", tui.ValueOr(c.pf, "any"), pfs...)
		r.sh.table = append(r.sh.table, []field{stop, {key: "stn:" + stn, value: stn, fixed: true}, dwell, pf})
	}
}

// keep notes what the table has picked at each station.
func (r *routePicker) keep() {
	for _, cells := range r.sh.table {
		stn := strings.TrimPrefix(cells[0].key, "stop:")
		pf := cells[3].value
		if pf == "any" {
			pf = ""
		}
		r.kept[stn] = stopChoice{stop: cells[0].value == "■" && !cells[0].fixed, dwell: strings.TrimSpace(cells[2].value), pf: pf}
	}
}

// way is the way chosen, if there is one.
func (r *routePicker) way() (way, bool) {
	label, _ := cellValue(r.sh.head, "way")
	for _, w := range r.ways {
		if w.label() == label {
			return w, true
		}
	}
	return way{}, false
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
	return r.ok(pickedRoute{head: v, way: w, entry: *at, dwell: dwell, stops: r.kept})
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
	marks := map[string]schematic.Mark{}
	way, ok := r.way()
	if ok {
		track := map[[2]string]string{}
		for _, t := range r.m.s.world.S.Tracks {
			track[[2]string{t.From, t.To}], track[[2]string{t.To, t.From}] = t.ID, t.ID
		}
		for i := 0; i+1 < len(way.points); i++ {
			marks[track[[2]string{way.points[i], way.points[i+1]}]] = schematic.MarkRoute
		}
	}
	focus := ""
	if r.sh.sec == 1 && r.sh.row < len(r.sh.table) {
		focus = r.sh.table[r.sh.row][1].value
		marks[focus] = schematic.MarkSelected
	} else if ok {
		focus = way.stations[0]
	}
	f := d.Draw(schematic.Options{Level: schematic.Layout, Zoom: 1, Marks: marks})
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
	for _, p := range tr.Points {
		out[p.Stn] = stopChoice{stop: p.Kind == ops.SpStop, dwell: showNum(p.Dwell), pf: p.Pf}
	}
	return out
}
