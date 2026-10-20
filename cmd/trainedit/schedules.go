package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// schedTab edits the scenario's trains: a list on the left, the picked
// train's points on the right, checked as they change against the schedule
// file's rules and the world.
type schedTab struct {
	m      *model
	points bool // the arrows move in the points, not the train list
	train  int
	row    int
	top    int // first train shown
	rowTop int // first point shown
	// editing is the point form open, if one is, and the point it edits:
	// the way drawn follows what it has picked
	editing *form
	editK   int
}

func newSchedTab(m *model) *schedTab { return &schedTab{m: m} }

func (t *schedTab) trains() []ops.TrainSpec { return t.m.s.sched.Trains }

func (t *schedTab) cur() (*ops.TrainSpec, bool) {
	ts := t.m.s.sched.Trains
	if t.train < 0 || t.train >= len(ts) {
		return nil, false
	}
	return &ts[t.train], true
}

func (t *schedTab) changed() {
	t.m.s.schedDirty = true
	t.clamp()
}

func (t *schedTab) clamp() {
	n := len(t.trains())
	t.train = max(min(t.train, n-1), 0)
	if tr, ok := t.cur(); ok {
		t.row = max(min(t.row, len(tr.Points)-1), 0)
	} else {
		t.row = 0
	}
}

func (t *schedTab) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if t.points {
		return t.pointKey(k)
	}
	switch k {
	case "up", "k":
		if n := len(t.trains()); n > 0 {
			t.train = (t.train + n - 1) % n
		}
		t.row = 0
	case "down", "j":
		if n := len(t.trains()); n > 0 {
			t.train = (t.train + 1) % n
		}
		t.row = 0
	case "tab", "right", "l":
		if _, ok := t.cur(); ok {
			t.points = true
		}
	case "a":
		t.addForm()
	case "enter", "E", "f":
		t.editForm()
	case "c":
		t.copyForm()
	case "d", "delete":
		if tr, ok := t.cur(); ok {
			n := tr.Number
			t.m.s.sched.Trains = slices.Delete(t.m.s.sched.Trains, t.train, t.train+1)
			t.changed()
			t.m.say("deleted %s", n)
		}
	case "s":
		slices.SortStableFunc(t.m.s.sched.Trains, func(a, b ops.TrainSpec) int {
			return cmp.Or(cmp.Compare(entry(a), entry(b)), strings.Compare(a.Number, b.Number))
		})
		t.changed()
		t.m.say("sorted by entry time")
	case "g":
		t.patternForm()
	case "x":
		t.disruptionForm()
	case "X":
		if tr, ok := t.cur(); ok {
			n := tr.Number
			before := len(t.m.s.sc.Disruptions)
			t.m.s.sc.Disruptions = slices.DeleteFunc(t.m.s.sc.Disruptions, func(d ops.Disruption) bool { return d.Train == n })
			if len(t.m.s.sc.Disruptions) != before {
				t.m.s.scDirty = true
				t.m.say("took %s's disruptions away", n)
			}
		}
	}
	t.clamp()
	return nil
}

func (t *schedTab) pointKey(k string) tea.Cmd {
	tr, ok := t.cur()
	if !ok {
		t.points = false
		return nil
	}
	switch k {
	case "tab", "esc", "left", "h":
		t.points = false
	case "up", "k":
		t.row = max(t.row-1, 0)
	case "down", "j":
		t.row = min(t.row+1, max(len(tr.Points)-1, 0))
	case "enter":
		if len(tr.Points) == 0 {
			t.insert(0)
		} else {
			t.pointForm(t.row, false)
		}
	case "i":
		t.insert(min(t.row+1, len(tr.Points)))
	case "I":
		t.insert(t.row)
	case "d", "delete":
		if len(tr.Points) > 0 {
			tr.Points = slices.Delete(tr.Points, t.row, t.row+1)
			t.changed()
		}
	case "K":
		if t.row > 0 {
			tr.Points[t.row-1], tr.Points[t.row] = tr.Points[t.row], tr.Points[t.row-1]
			t.row--
			t.changed()
		}
	case "J":
		if t.row+1 < len(tr.Points) {
			tr.Points[t.row+1], tr.Points[t.row] = tr.Points[t.row], tr.Points[t.row+1]
			t.row++
			t.changed()
		}
	case "f":
		t.editForm()
	case "x":
		t.disruptionForm()
	}
	return nil
}

func (t *schedTab) insert(at int) {
	tr, _ := t.cur()
	stn := ""
	if codes := t.m.s.stationCodes(); len(codes) > 0 {
		stn = codes[0]
	}
	tr.Points = slices.Insert(tr.Points, at, ops.PointSpec{Stn: stn, Kind: ops.SpPass})
	t.row = at
	t.pointForm(at, true)
}

// stationChoices are the world's stations, with cur first if it is not one.
func (t *schedTab) stationChoices(cur string) []string {
	codes := t.m.s.stationCodes()
	if cur != "" && !slices.Contains(codes, cur) {
		codes = append([]string{cur}, codes...)
	}
	if len(codes) == 0 {
		codes = []string{""}
	}
	return codes
}

func (t *schedTab) classChoices(cur string) []string {
	cs := append([]string{""}, t.m.s.classes()...)
	if !slices.Contains(cs, cur) {
		cs = append(cs, cur)
	}
	return cs
}

// trainHead is the route picker's head for a train: its number, class,
// speed, name and priority.
func (t *schedTab) trainHead(tr ops.TrainSpec) [][]field {
	return [][]field{
		{text("number", "number", tr.Number), choice("class", "class", tr.Class, t.classChoices(tr.Class)...), number("kmph", "max km/h", tr.MaxSpeedKmph)},
		{text("name", "name", tr.Name), field{key: "priority", label: "priority", value: strconv.Itoa(tr.Priority)}},
	}
}

// fromHead is a train as the picker's head (trainHead) and route have it;
// self is its place in the list, -1 for a new train.
func (t *schedTab) fromHead(r pickedRoute, self int) (ops.TrainSpec, error) {
	v := r.head
	if v["number"] == "" {
		return ops.TrainSpec{}, errors.New("a train needs a number")
	}
	if i := t.indexOf(v["number"]); i >= 0 && i != self {
		return ops.TrainSpec{}, fmt.Errorf("there is a train %s already", v["number"])
	}
	pri, err := parseInt(v, "priority", "priority")
	if err != nil {
		return ops.TrainSpec{}, err
	}
	kmph, err := parseNum(v, "kmph", "speed")
	if err != nil {
		return ops.TrainSpec{}, err
	}
	pts, err := r.points(r.way.stations, true)
	if err != nil {
		return ops.TrainSpec{}, err
	}
	return ops.TrainSpec{Number: v["number"], Name: v["name"], Class: v["class"], Priority: pri, MaxSpeedKmph: kmph, Points: pts}, nil
}

func (t *schedTab) indexOf(number string) int {
	return slices.IndexFunc(t.trains(), func(tr ops.TrainSpec) bool { return tr.Number == number })
}

// addForm opens the route picker for a new train.
func (t *schedTab) addForm() {
	if len(t.m.s.stationCodes()) < 2 {
		t.m.fail("the world needs two stations for a train to run between")
		return
	}
	head := t.trainHead(ops.TrainSpec{Number: t.freeNumber(), Class: tui.At(t.classChoices(""), 1), MaxSpeedKmph: 110})
	t.m.openRoutePicker("new train", head, "", "", 10, 2, nil, nil, func(r pickedRoute) error {
		tr, err := t.fromHead(r, -1)
		if err != nil {
			return err
		}
		t.m.s.sched.Trains = append(t.m.s.sched.Trains, tr)
		t.train = len(t.m.s.sched.Trains) - 1
		t.changed()
		t.m.say("added %s, %s: tab goes to its points", tr.Number, strings.Join(r.way.stations, " "))
		return nil
	})
}

// freeNumber is a train number not taken yet.
func (t *schedTab) freeNumber() string {
	for n := len(t.trains()) + 1; ; n++ {
		s := fmt.Sprintf("T%03d", n)
		if t.indexOf(s) < 0 {
			return s
		}
	}
}

func (t *schedTab) copyForm() {
	tr, ok := t.cur()
	if !ok {
		return
	}
	t.m.open(newForm("copy "+tr.Number, func(v values) error {
		if v["number"] == "" || t.indexOf(v["number"]) >= 0 {
			return fmt.Errorf("train number %q is empty or taken", v["number"])
		}
		shift, err := parseNum(v, "shift", "shift")
		if err != nil {
			return err
		}
		c := *tr
		c.Number, c.Points = v["number"], slices.Clone(tr.Points)
		for i, p := range c.Points {
			if p.Arr != nil {
				c.Points[i].Arr = ptr(*p.Arr + shift)
			}
			if p.Dep != nil {
				c.Points[i].Dep = ptr(*p.Dep + shift)
			}
		}
		t.m.s.sched.Trains = slices.Insert(t.m.s.sched.Trains, t.train+1, c)
		t.train++
		t.changed()
		return nil
	}, text("number", "number", t.freeNumber()), field{key: "shift", label: "later by (min)", value: "30", help: "moves its times"}))
}

// editForm opens the route picker on a train, as it is: its details, its
// route and each station's stop, times and platform.
func (t *schedTab) editForm() {
	tr, ok := t.cur()
	if !ok {
		t.addForm()
		return
	}
	from, to := "", ""
	if n := len(tr.Points); n > 0 {
		from, to = tr.Points[0].Stn, tr.Points[n-1].Stn
	}
	at := 10.0
	if e := entry(*tr); e >= 0 {
		at = e
	}
	var stations []string
	for _, p := range tr.Points {
		stations = append(stations, p.Stn)
	}
	self := t.train
	t.m.openRoutePicker("train "+tr.Number, t.trainHead(*tr), from, to, at, 2, stations, stopsOf(*tr), func(r pickedRoute) error {
		nt, err := t.fromHead(r, self)
		if err != nil {
			return err
		}
		nt.Stock = tr.Stock // the picker does not edit it
		if nt.Number != tr.Number {
			for k := range t.m.s.sc.Disruptions { // its disruptions go with it
				if d := &t.m.s.sc.Disruptions[k]; d.Train == tr.Number {
					d.Train, t.m.s.scDirty = nt.Number, true
				}
			}
		}
		*tr = nt
		t.row = 0
		t.changed()
		t.m.say("%s runs %s: %d points", tr.Number, strings.Join(r.way.stations, " "), len(nt.Points))
		return nil
	})
}

// patternForm opens the route picker for a pattern of trains.
func (t *schedTab) patternForm() {
	if len(t.m.s.stationCodes()) < 2 {
		t.m.fail("the world needs two stations for a train to run between")
		return
	}
	classes := t.m.s.classes()
	head := [][]field{
		{number("interval", "every (min)", 60), number("count", "trains each way", 3), choice("both", "both ways", "yes", "yes", "no")},
		{field{key: "offset", label: "back after (min)", value: "30"}, number("kmph", "max km/h", 110)},
		{field{key: "classes", label: "classes", value: strings.Join(classes[:min(len(classes), 1)], ", ")}},
		{field{key: "up", label: "numbers out", value: "T{n}U"}, text("down", "back", "T{n}D")},
	}
	t.m.openRoutePicker("pattern of trains", head, "", "", 10, 2, nil, nil, func(r pickedRoute) error {
		v := r.head
		pt := pattern{route: r, both: v["both"] == "yes", upNumber: v["up"], downNumber: v["down"]}
		var err error
		for _, f := range []struct {
			key string
			to  *float64
		}{{"interval", &pt.interval}, {"offset", &pt.offset}, {"kmph", &pt.kmph}} {
			if *f.to, err = parseNum(v, f.key, f.key); err != nil {
				return err
			}
		}
		if pt.count, err = parseInt(v, "count", "count"); err != nil {
			return err
		}
		for _, c := range strings.Split(v["classes"], ",") {
			if c = strings.TrimSpace(c); c != "" {
				pt.classes = append(pt.classes, c)
			}
		}
		trains, err := pt.expand()
		if err != nil {
			return err
		}
		for _, tr := range trains {
			if t.indexOf(tr.Number) >= 0 {
				return fmt.Errorf("there is a train %s already", tr.Number)
			}
		}
		t.m.s.sched.Trains = append(t.m.s.sched.Trains, trains...)
		t.changed()
		t.m.say("added %d trains, %s", len(trains), strings.Join(r.way.stations, " "))
		return nil
	})
}

func (t *schedTab) pointForm(k int, inserted bool) {
	tr, _ := t.cur()
	p := tr.Points[k]
	pfChoices := append([]string{"any"}, t.m.s.platforms(p.Stn)...)
	if p.Pf != "" && !slices.Contains(pfChoices, p.Pf) {
		pfChoices = append(pfChoices, p.Pf)
	}
	f := newForm(fmt.Sprintf("%s point %d", tr.Number, k+1), func(v values) error {
		pf := v["pf"]
		if pf == "any" {
			pf = ""
		}
		np := ops.PointSpec{Stn: v["stn"], Kind: ops.SchedulePointKind(v["kind"]), Pf: pf}
		var err error
		for _, f := range []struct {
			key, what string
			to        **float64
		}{{"arr", "arr", &np.Arr}, {"dep", "dep", &np.Dep}} {
			if *f.to, err = parseTime(v[f.key], f.what); err != nil {
				return err
			}
		}
		for _, f := range []struct {
			key string
			to  **float64
		}{{"dwell", &np.Dwell}, {"min", &np.MinDwell}} {
			if v[f.key] == "" {
				continue
			}
			n, err := parseNum(v, f.key, f.key)
			if err != nil {
				return err
			}
			*f.to = ptr(n)
		}
		if _, err := np.Point(k == 0); err != nil {
			return err
		}
		tr.Points[k] = np
		t.changed()
		return nil
	},
		choice("stn", "station", p.Stn, t.stationChoices(p.Stn)...),
		choice("kind", "kind", string(p.Kind), string(ops.SpStop), string(ops.SpPass)),
		field{key: "pf", label: "platform", value: tui.ValueOr(p.Pf, "any"), choices: pfChoices, help: "any: the dispatcher picks"},
		field{key: "arr", label: "arr", value: showTime(p.Arr), help: "HH:MM or minutes; the first point's is the entry"},
		field{key: "dep", label: "dep", value: showTime(p.Dep), help: "a stop to keep to times: arr and dep"},
		field{key: "dwell", label: "dwell (min)", value: showNum(p.Dwell), help: "a stop that leaves once it has stood this long"},
		field{key: "min", label: "min dwell (min)", value: showNum(p.MinDwell), help: "with arr and dep: how long it stands even when late"},
	)
	f.notes = []string{"a stop gives a dwell, or an arr and a dep; a pass only an arr, if it is timed"}
	f.top = true // the way stays in sight under it
	t.editing, t.editK = f, k
	if inserted {
		// backing out of a new point takes it away again
		f.cancel = func() {
			tr.Points = slices.Delete(tr.Points, k, k+1)
			t.clamp()
		}
	}
	t.m.open(f)
}

func (t *schedTab) disruptionForm() {
	tr, ok := t.cur()
	if !ok {
		return
	}
	var stns []string
	for _, p := range tr.Points {
		if !slices.Contains(stns, p.Stn) {
			stns = append(stns, p.Stn)
		}
	}
	if len(stns) == 0 {
		stns = []string{""}
	}
	t.m.open(newForm("disrupt "+tr.Number, func(v values) error {
		mins, err := parseNum(v, "min", "minutes")
		if err != nil {
			return err
		}
		d := ops.Disruption{Train: tr.Number, Kind: v["kind"], Min: mins}
		if d.Kind == ops.DisruptStand {
			d.Stn = ids.StnCode(v["stn"])
		}
		train, err := tr.Train()
		if err != nil {
			return fmt.Errorf("put the train right first: %w", err)
		}
		if err := ops.ApplyDisruptions([]*ops.Train{train}, []ops.Disruption{d}); err != nil {
			return err
		}
		t.m.s.sc.Disruptions = append(t.m.s.sc.Disruptions, d)
		t.m.s.scDirty = true
		t.m.say("%s: %s", tr.Number, describeDisruption(d))
		return nil
	},
		choice("kind", "kind", ops.DisruptStand, ops.DisruptStand, ops.DisruptLateEntry),
		choice("stn", "where (a stand)", stns[0], stns...),
		number("min", "minutes", 15),
	))
}

func describeDisruption(d ops.Disruption) string {
	if d.Kind == ops.DisruptStand {
		return fmt.Sprintf("stands %g min longer at %s", d.Min, d.Stn)
	}
	return fmt.Sprintf("enters %g min late", d.Min)
}

func (t *schedTab) render(width, h int) []string {
	s := t.m.s
	lw := min(max(width*2/5, 30), 52)
	rw := width - lw
	var banner []string
	if s.sc.Timetable != "" {
		text := fmt.Sprintf("this scenario runs the timetable %s", s.sc.Timetable)
		if s.tt != nil {
			text += fmt.Sprintf(" (%d timed trains)", len(s.tt.Trains))
		}
		text += ": the trains here are its draft schedule; the Run tab's t exports a new timetable from them, U goes back to running them"
		for _, l := range tui.Wrap(text, width-2, 2) {
			banner = append(banner, tui.Accent.Render(" "+l))
		}
	}
	way := t.wayPane(width, h-len(banner))
	bh := h - len(banner) - len(way)

	// the train list
	trains := t.trains()
	rows := max(bh-3, 1)
	t.top = min(max(t.top, t.train-rows+1), t.train)
	t.top = max(min(t.top, len(trains)-rows), 0)
	list := []string{tui.Dim.Render(tui.Fit(" train", 9) + tui.Fit("class", 14) + tui.Fit("entry", 7) + "route")}
	for i := t.top; i < min(t.top+rows, len(trains)); i++ {
		tr := trains[i]
		route := ""
		if n := len(tr.Points); n > 0 {
			route = tr.Points[0].Stn + "→" + tr.Points[n-1].Stn
		}
		e := "—"
		if at := entry(tr); at >= 0 {
			e = tui.Clock(at)
		}
		mark := " "
		if len(checkTrain(s.world.S, tr)) > 0 {
			mark = tui.Failed.Render("✗")
		} else if slices.ContainsFunc(s.sc.Disruptions, func(d ops.Disruption) bool { return d.Train == tr.Number }) {
			mark = tui.Amber.Render("⚠")
		}
		line := " " + tui.Fit(tr.Number, 8) + tui.Fit(tui.ValueOr(tr.Class, "—"), 13) + " " + tui.Fit(e, 7) + route
		if i == t.train {
			line = tui.Bold.Reverse(true).Faint(t.points).Render(tui.Fit(line, lw-4))
		}
		list = append(list, tui.Fit(line, lw-4)+mark)
	}
	if len(trains) == 0 {
		list = append(list, tui.Dim.Render(" no trains: a adds one, g a pattern of them"))
	}
	title := fmt.Sprintf("trains (%d)", len(trains))
	if s.schedDirty {
		title += " · unsaved"
	}
	left := tui.Box(title, !t.points, list, lw, bh)

	// the picked train
	var body []string
	rtitle := "train"
	if tr, ok := t.cur(); ok {
		rtitle = "train " + tr.Number
		body = append(body, fmt.Sprintf(" %s · %s · prio %+d · %g km/h", tui.ValueOr(tr.Name, tui.Dim.Render("no name")), tui.ValueOr(tr.Class, tui.Dim.Render("no class")), tr.Priority, tr.MaxSpeedKmph), "")
		cols := func(n, stn, kind, pf, arr, dep, dwell, min string) string {
			return " " + tui.Fit(n, 4) + tui.Fit(stn, 7) + tui.Fit(kind, 6) + tui.Fit(pf, 4) + tui.Fit(arr, 7) + tui.Fit(dep, 7) + tui.Fit(dwell, 7) + min
		}
		body = append(body, tui.Dim.Render(cols("#", "stn", "kind", "pf", "arr", "dep", "dwell", "min")))
		probs := checkTrain(s.world.S, *tr)
		prows := max(bh-2-len(body)-min(len(probs), 4)-3, 1)
		t.rowTop = min(max(t.rowTop, t.row-prows+1), t.row)
		t.rowTop = max(min(t.rowTop, len(tr.Points)-prows), 0)
		for i := t.rowTop; i < min(t.rowTop+prows, len(tr.Points)); i++ {
			p := tr.Points[i]
			dash := func(s string) string { return tui.ValueOr(s, "—") }
			line := cols(strconv.Itoa(i+1), p.Stn, string(p.Kind), dash(p.Pf), dash(showTime(p.Arr)), dash(showTime(p.Dep)), dash(showNum(p.Dwell)), showNum(p.MinDwell))
			if slices.ContainsFunc(probs, func(pr trainProblem) bool { return pr.at == i }) {
				line = tui.Failed.Render(line)
			}
			if t.points && i == t.row {
				line = tui.Bold.Reverse(true).Render(tui.Fit(line, rw-2))
			}
			body = append(body, line)
		}
		if len(tr.Points) == 0 {
			body = append(body, tui.Dim.Render(" no points: f fills them from a route, or tab then enter adds one"))
		}
		body = append(body, "")
		for i, pr := range probs {
			if i == 4 {
				body = append(body, tui.Failed.Render(fmt.Sprintf(" … %d more", len(probs)-i)))
				break
			}
			body = append(body, tui.Failed.Render(" ✗ "+ansi.Truncate(pr.text, rw-6, "…")))
		}
		for _, d := range s.sc.Disruptions {
			if d.Train == tr.Number {
				body = append(body, tui.Amber.Render(" ⚠ "+describeDisruption(d)))
			}
		}
	}
	right := tui.Box(rtitle, t.points, body, rw, bh)
	return slices.Concat(banner, tui.Side([]int{lw, rw}, left, right), way)
}

// wayPane is the picked train's way drawn on the world, under the trains,
// at most half of h: the point picked (or being edited) selected, with its
// platform or through track.
func (t *schedTab) wayPane(width, h int) []string {
	d := t.m.world.diagram
	tr, ok := t.cur()
	if d == nil || !ok || len(tr.Points) == 0 || h < 16 {
		return nil
	}
	s := t.m.s.world.S
	pts := slices.Clone(tr.Points)
	k := -1
	if t.points {
		k = t.row
	}
	if f := t.editing; f != nil && t.m.form == f && t.editK < len(pts) {
		k = t.editK
		pts[k].Stn, pts[k].Pf = f.value("stn"), f.value("pf")
		if pts[k].Pf == "any" {
			pts[k].Pf = ""
		}
	}
	marks := map[string]schematic.Mark{}
	var note []string
	if path, err := trainPath(s, pts); err != nil {
		note = []string{tui.Failed.Render(" ✗ " + err.Error())}
	} else {
		markWay(s, path, marks)
	}
	focus := pts[0].Stn
	if k >= 0 && k < len(pts) {
		focus = pts[k].Stn
		marks[focus] = schematic.MarkSelected
		if pf := pfTrack(s, focus, pts[k].Pf); pf != "" {
			marks[pf] = schematic.MarkSelected
		}
	}
	f := d.Draw(schematic.Options{Level: schematic.Layout, Zoom: 1, Marks: marks})
	ph := min(f.Height()+len(note)+2, h/2)
	body := append(note, viewAround(f, focus, width-2, ph-2-len(note))...)
	return tui.Box("the way · "+tr.Number, false, body, width, ph)
}

func (t *schedTab) keyLine() string {
	if t.points {
		return " ↑↓ point  enter edit  i/I insert after/before  d delete  K/J move  f edit the train  x disrupt  tab trains"
	}
	return " ↑↓ train  tab points  a add  enter/f edit  c copy  d delete  s sort  g pattern  x disrupt  X clear its disruptions"
}
