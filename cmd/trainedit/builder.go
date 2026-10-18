package main

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"trainapp/railway/infra"
	"trainapp/units"
	"trainapp/visualization/schematic"
	"trainapp/worlds/edit"
	"trainapp/worlds/templates"

	tea "charm.land/bubbletea/v2"
)

// The station builder: a station's tracks laid out as a table, a row a
// track, top to bottom, with the station drawn beside it as it stands and
// what is wrong with it said as it is edited. enter stamps it into the
// world, joined as the head says.

// Station starting points (the builder's start).
const (
	startBlank    = "blank"
	startHalt     = "halt"
	startCrossing = "crossing"
	startDouble   = "double line"
)

var starts = []string{startBlank, startHalt, startCrossing, startDouble}

// Track kinds, a builder row's kind.
const (
	kindPlatform = "platform"
	kindThrough  = "through" // a platform track with nowhere to board
	kindSiding   = "siding"
)

var (
	kinds   = []string{kindPlatform, kindThrough, kindSiding}
	ticks   = []string{"·", "■"}
	joinsOf = map[string]string{"auto": "", "above": templates.JoinAbove, "below": templates.JoinBelow, "both": templates.JoinBoth}
	joinsBy = map[string]string{"": "auto", templates.JoinAbove: "above", templates.JoinBelow: "below", templates.JoinBoth: "both"}
)

type builder struct {
	m     *model
	sh    sheet
	at    string // the point it joins, if any
	start string // the starting point the table was filled from

	crossings map[string]string // the crossovers picked, by side and the two mains, e.g. "W 2 3"
	err       string            // why the station cannot be stamped as it stands
	diagram   *schematic.Diagram
}

// openBuilder opens the builder over the world: joined to the selected point,
// if one is, or filling in the selected station if it has no platforms yet.
func (w *worldTab) openBuilder() {
	s := w.m.s.world.S
	at, code, name := "", "", ""
	if w.sel.Kind == schematic.PiecePoint {
		at = w.sel.ID
	}
	if w.sel.Kind == schematic.PieceStation {
		for _, st := range s.Stations {
			if st.Code == w.sel.ID && len(st.Platforms) == 0 {
				code, name = st.Code, st.Name
			}
		}
	}
	b := &builder{m: w.m, at: at, crossings: map[string]string{}}
	b.sh.head = [][]field{
		{choice("start", "start", startCrossing, starts...), text("code", "code", code), text("name", "name", name)},
		{choice("edge", "world's edge", "none", "none", "west", "east"), field{key: "turnout", label: "turnout km/h", value: "0"}},
		{choice("join", "join", edit.JoinSelected, edit.Joins...), number("km", "line km", 10), number("blocks", "blocks", 1)},
		{text("pattern", "lines run", ""), text("link", "line name", "")},
	}
	b.sh.cols = []column{{"kind", 10}, {"no", 5}, {"length", 7}, {"km/h", 5}, {"main W", 7}, {"main E", 7}, {"joins", 9}, {"buffer", 8}}
	b.fill(startCrossing)
	b.update()
	b.sh.pick(0, 0, 1)
	if at == "" && len(s.Points) > 0 {
		w.m.say("no point selected: the station will stand on its own")
	}
	w.m.panel = b
}

func (b *builder) cells() *sheet { return &b.sh }

// row is a table row for a track.
func (b *builder) row(t templates.Track, mainW, mainE bool) []field {
	kind, no, length := kindPlatform, t.PfNo, t.Length
	switch {
	case t.Siding != "":
		kind, no, length = kindSiding, t.Siding, t.SidingLength
	case t.Length == 0:
		kind = kindThrough
	}
	tick := func(on bool) string { return ticks[map[bool]int{false: 0, true: 1}[on]] }
	speed := ""
	if t.Kmph > 0 {
		speed = strconv.FormatFloat(t.Kmph, 'f', -1, 64)
	}
	lengthText := ""
	if length > 0 {
		lengthText = strconv.FormatFloat(float64(length), 'f', -1, 64)
	}
	return []field{
		choice("kind", "", kind, kinds...),
		text("no", "", no),
		text("length", "", lengthText),
		text("kmph", "", speed),
		choice("mainW", "", tick(mainW), ticks...),
		choice("mainE", "", tick(mainE), ticks...),
		choice("joins", "", joinsBy[t.Join], "auto", "above", "below", "both"),
		choice("buffer", "", cmp.Or(t.Buffer, "none"), "none", "W", "E"),
	}
}

// fill lays the table out as a starting point has it.
func (b *builder) fill(start string) {
	line := b.m.s.world.S.Defaults.RouteKmph
	var tracks []templates.Track
	switch start {
	case startBlank:
		tracks = []templates.Track{{PfNo: "1", Length: 600, Straight: true}}
	case startHalt:
		tracks = []templates.Track{{PfNo: "1", Length: 400, Straight: true, Kmph: line}}
	case startCrossing:
		tracks = []templates.Track{templates.Pf("1", 450), {PfNo: "3", Straight: true, Kmph: line}, templates.Pf("2", 450)}
	case startDouble:
		tracks = []templates.Track{templates.Pf("1", 600), {PfNo: "2", Length: 600, Straight: true, Kmph: line}, {PfNo: "3", Length: 600, Straight: true, Kmph: line}, templates.Pf("4", 600)}
		b.crossings["W 2 3"], b.crossings["E 2 3"] = templates.CrossScissors, templates.CrossScissors
	}
	b.setTracks(tracks)
	b.start = start
}

// setTracks lays the table out for tracks, mains both sides where straight.
func (b *builder) setTracks(tracks []templates.Track) {
	b.sh.table = nil
	for _, t := range tracks {
		b.sh.table = append(b.sh.table, b.row(t, t.Straight, t.Straight))
	}
}

// tracks reads the table: the tracks top to bottom, and the mains each side
// by name.
func (b *builder) tracks() (tracks []templates.Track, west, east []string, err error) {
	west, east = []string{}, []string{} // none ticked is no mains, not the straight ones
	for i, cells := range b.sh.table {
		v := map[string]string{}
		for _, f := range cells {
			v[f.key] = strings.TrimSpace(f.value)
		}
		var t templates.Track
		if v["no"] == "" {
			return nil, nil, nil, fmt.Errorf("track %d needs a %s", i+1, map[bool]string{true: "name", false: "number"}[v["kind"] == kindSiding])
		}
		var length float64
		if v["kind"] != kindThrough && v["length"] != "" {
			if length, err = parseNum(values(v), "length", fmt.Sprintf("track %d's length", i+1)); err != nil {
				return nil, nil, nil, err
			}
		}
		if v["kmph"] != "" {
			if t.Kmph, err = parseNum(values(v), "kmph", fmt.Sprintf("track %d's speed", i+1)); err != nil {
				return nil, nil, nil, err
			}
		}
		switch v["kind"] {
		case kindSiding:
			t.Siding, t.SidingLength = v["no"], units.Meters(length)
			if v["buffer"] != "none" {
				t.Buffer = v["buffer"]
			}
		case kindThrough:
			t.PfNo = v["no"]
		default:
			if length == 0 {
				return nil, nil, nil, fmt.Errorf("platform %s needs a length (a through track has none)", v["no"])
			}
			t.PfNo, t.Length = v["no"], units.Meters(length)
		}
		t.Join = joinsOf[v["joins"]]
		t.Straight = v["mainW"] == "■" || v["mainE"] == "■"
		if v["mainW"] == "■" {
			west = append(west, v["no"])
		}
		if v["mainE"] == "■" {
			east = append(east, v["no"])
		}
		tracks = append(tracks, t)
	}
	if len(tracks) == 0 {
		return nil, nil, nil, errors.New("a station needs a track: ctrl+o adds one")
	}
	return tracks, west, east, nil
}

// mainRows are the table rows of a side's mains, top to bottom.
func (b *builder) mainRows(side string) []int {
	var out []int
	for i, cells := range b.sh.table {
		if cells[map[string]int{"W": 4, "E": 5}[side]].value == "■" {
			out = append(out, i)
		}
	}
	return out
}

// edge is the side that is the world's edge, if any.
func (b *builder) edge() string {
	e, _ := cellValue(b.sh.head, "edge")
	return map[string]string{"west": "W", "east": "E"}[e]
}

// update follows an edit: a new starting point fills the table again, cells
// that do not apply are off, the crossover rows follow the mains, and the
// station is built again to draw it and say what is wrong.
func (b *builder) update() {
	if start, _ := cellValue(b.sh.head, "start"); start != b.start {
		b.fill(start)
	}
	b.keepCrossings()
	// a track between two mains, either side, says which it hangs off
	between := map[int]bool{}
	for _, side := range []string{"W", "E"} {
		ms := b.mainRows(side)
		for k := 0; k+1 < len(ms); k++ {
			for i := ms[k] + 1; i < ms[k+1]; i++ {
				between[i] = true
			}
		}
	}
	for i, cells := range b.sh.table {
		kind := cells[0].value
		cells[2].off = kind == kindThrough
		cells[6].off = !between[i]
		cells[7].off = kind != kindSiding
	}
	b.sh.foot = nil
	for _, side := range []string{"W", "E"} {
		if side == b.edge() {
			continue
		}
		ms := b.mainRows(side)
		for k := 0; k+1 < len(ms); k++ {
			upper, lower := b.sh.table[ms[k]][1].value, b.sh.table[ms[k+1]][1].value
			label := fmt.Sprintf("%s crossover %s–%s", map[string]string{"W": "west", "E": "east"}[side], upper, lower)
			f := choice("xo:"+side+":"+strconv.Itoa(k), label, cmp.Or(b.crossings[side+" "+upper+" "+lower], "none"), templates.Crossings...)
			b.sh.foot = append(b.sh.foot, []field{f})
		}
	}
	b.sh.fix()
	b.build()
}

// keepCrossings notes the crossovers picked, by the mains they lie between,
// so they stay as rows come and go.
func (b *builder) keepCrossings() {
	for _, row := range b.sh.foot {
		f := row[0]
		parts := strings.SplitN(f.key, ":", 3)
		side := parts[1]
		k, _ := strconv.Atoi(parts[2])
		ms := b.mainRows(side)
		if k+1 < len(ms) {
			b.crossings[side+" "+b.sh.table[ms[k]][1].value+" "+b.sh.table[ms[k+1]][1].value] = f.value
		}
	}
}

// spec is the station as the builder has it.
func (b *builder) spec() (edit.StampSpec, error) {
	v := values{}
	for _, row := range b.sh.head {
		for _, f := range row {
			v[f.key] = strings.TrimSpace(f.value)
		}
	}
	spec := edit.StampSpec{Template: edit.TemplateStation, Code: strings.ToUpper(v["code"]), Name: v["name"], Edge: b.edge(),
		Join: v["join"], LinkPattern: v["pattern"], LinkName: v["link"]}
	var err error
	if spec.Tracks, spec.WestMains, spec.EastMains, err = b.tracks(); err != nil {
		return spec, err
	}
	for _, row := range b.sh.foot {
		side := strings.SplitN(row[0].key, ":", 3)[1]
		if side == "W" {
			spec.WestCrossovers = append(spec.WestCrossovers, row[0].value)
		} else {
			spec.EastCrossovers = append(spec.EastCrossovers, row[0].value)
		}
	}
	if spec.TurnoutKmph, err = parseNum(v, "turnout", "turnout speed"); err != nil {
		return spec, err
	}
	km, err := parseNum(v, "km", "line length")
	if err != nil {
		return spec, err
	}
	spec.LinkM = km * 1000
	if spec.LinkBlocks, err = parseInt(v, "blocks", "blocks"); err != nil {
		return spec, err
	}
	return spec, nil
}

// build stamps the station on its own into an empty world, to draw it and
// to find what is wrong with it.
func (b *builder) build() {
	b.err, b.diagram = "", nil
	spec, err := b.spec()
	if err == nil {
		scratch := edit.Empty(b.m.s.world.S.Defaults)
		spec.Code = cmp.Or(spec.Code, "NEW")
		if _, err = edit.Stamp(scratch, "", spec); err == nil {
			b.diagram, err = schematic.FromSnapshot(scratch)
		}
	}
	if err != nil {
		b.err = err.Error()
	}
}

func (b *builder) key(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "esc":
		return true
	case "enter":
		return b.accept()
	case ":":
		b.paste()
		return false
	case "ctrl+o":
		b.addRow()
	case "ctrl+k":
		if b.sh.sec == 1 && len(b.sh.table) > 0 {
			b.sh.table = append(b.sh.table[:b.sh.row], b.sh.table[b.sh.row+1:]...)
		}
	case "alt+up", "ctrl+up":
		b.moveRow(-1)
	case "alt+down", "ctrl+down":
		b.moveRow(1)
	default:
		if !b.sh.key(msg) {
			return false
		}
	}
	b.update()
	return false
}

// addRow adds a platform under the picked track (or at the top), numbered
// one on from the highest.
func (b *builder) addRow() {
	at := 0
	if b.sh.sec == 1 {
		at = b.sh.row + 1
	}
	next := 1
	for _, cells := range b.sh.table {
		if n, err := strconv.Atoi(cells[1].value); err == nil && cells[0].value != kindSiding {
			next = max(next, n+1)
		}
	}
	row := b.row(templates.Pf(strconv.Itoa(next), 600), false, false)
	b.sh.table = append(b.sh.table[:at], append([][]field{row}, b.sh.table[at:]...)...)
	b.sh.sec, b.sh.row = 1, at
}

// moveRow moves the picked track up or down a place.
func (b *builder) moveRow(d int) {
	r := b.sh.row
	if b.sh.sec != 1 || r+d < 0 || r+d >= len(b.sh.table) {
		return
	}
	b.sh.table[r], b.sh.table[r+d] = b.sh.table[r+d], b.sh.table[r]
	b.sh.row += d
}

// paste opens the tracks as text, as the tracks line reads them, to type or
// paste; ok lays the table out from it.
func (b *builder) paste() {
	line := ""
	if tracks, _, _, err := b.tracks(); err == nil {
		line = edit.FormatTracks(tracks)
	}
	f := newForm("tracks as text", func(v values) error {
		tracks, err := edit.ParseTracks(v["tracks"])
		if err != nil {
			return err
		}
		b.setTracks(tracks)
		b.update()
		return nil
	}, field{key: "tracks", label: "tracks", value: line, help: "top to bottom; the mains are both sides"})
	f.notes = []string{"NO:LENGTH a platform (no length: a through track), ~NAME:LENGTH a siding;",
		"after it * a main, @KMPH its speed, < or > a siding's buffer stop W or E,",
		"^ v or ^v: a track between two mains hangs off the one above, below, or both"}
	b.m.open(f)
}

func (b *builder) accept() bool {
	if b.err != "" {
		b.m.fail(b.err)
		return false
	}
	spec, err := b.spec()
	if err == nil && spec.Code == "" {
		err = errors.New("a station needs a code")
	}
	if err != nil {
		b.m.fail(err.Error())
		return false
	}
	w, at := b.m.world, b.at
	if !w.do("stamped "+spec.Code, func(s *infra.Snapshot) error { _, err := edit.Stamp(s, at, spec); return err }) {
		return false
	}
	w.selectPiece(schematic.Piece{Kind: schematic.PieceStation, ID: spec.Code})
	// leave the cursor on its far throat, to stamp the next from there
	for _, p := range b.m.s.world.S.Points {
		if p.Stn == spec.Code && !p.DeadEnd && openSide(b.m.s.world.S, p.ID, true) {
			w.selectPoint(p.ID)
		}
	}
	return true
}

func (b *builder) render(w, h int) []string {
	left := b.sh.render(w, h-4)
	if b.err != "" {
		left = append(left, "", " ⚠ "+b.err)
	}
	title := "station"
	if b.at != "" {
		title += " joined to " + b.at
	}
	return sidePanes(title, left, "the station", b.preview, w, h)
}

func (b *builder) preview(w, h int) []string {
	if b.diagram == nil {
		return nil
	}
	f := b.diagram.Draw(schematic.Options{Level: schematic.Layout, Zoom: 1})
	return f.View(0, 0, w, min(h, f.Height()), true)
}

func (b *builder) keyLine() string {
	return " ↑↓ row  tab cell  ←→ change  ctrl+o add track  ctrl+k remove  alt+↑↓ move  : as text  enter stamp  esc cancel"
}
