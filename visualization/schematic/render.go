package schematic

import (
	"math"
	"slices"
	"strings"
	"trainapp/visualization/vizproto"
	"unicode/utf8"
)

// rowsPerLane leaves an empty row between lanes for diagonals to cross.
const rowsPerLane = 2

// headerRows is the row above the top lane, for station names.
const headerRows = 1

// Level is how much detail is drawn.
type Level int

const (
	Layout   Level = iota // platforms, throats, switches and crossovers
	Overview              // each station collapsed to a box
	Detail                // Layout plus point, track and switch ids
)

func (l Level) String() string {
	switch l {
	case Overview:
		return "overview"
	case Detail:
		return "detail"
	}
	return "layout"
}

// Options controls how a diagram is drawn.
type Options struct {
	Level Level
	Zoom  int  // 1 is the most compact; each step widens every track
	Color bool // ANSI colours, for Render

	// State, if set, colours tracks by who holds them, shows switch
	// positions and signal aspects, and draws trains where they are at Time.
	// Signals are drawn either way; one a train is running past from behind
	// is drawn ≡.
	State    *vizproto.State
	Time     float64
	Selected string // train to highlight

	// Marks picks out pieces of the world, by id: a point, track, signal or
	// station (its code), e.g. an editor's selection and its problems.
	Marks map[string]Mark
}

// Mark is how a piece is picked out (Options.Marks).
type Mark int

const (
	MarkNone     Mark = iota
	MarkSelected      // the piece being worked on
	MarkProblem       // a piece something is wrong with
)

// minWidth is the fewest columns each kind of track takes at zoom 1.
var minWidth = map[Kind]int{Line: 8, Platform: 6, Switch: 2}

// width is how many columns the track spans: enough for its kind, the
// diagonal to its other lane, and its label beside the diagonal.
func (t *Track) width(level Level, zoom int) int {
	if t.Kind == box {
		return t.span
	}
	rise := abs(t.Lanes) * rowsPerLane
	w := max(minWidth[t.Kind]*zoom, rise)
	if label := t.label(level); label != "" {
		need := utf8.RuneCountInString(label) + 2
		if rise > 0 && t.branchWest == t.branchEast {
			need *= 2 // the diagonal is centred, so each level part needs room
		}
		w = max(w, need+2+rise)
	}
	return w
}

// columns places every point as far west as its tracks allow, keeping points
// in the same group (one end of a station's platforms) in one column. If the
// groups cannot all be kept, it places the points without them.
func (d *Diagram) columns(level Level, zoom int) map[string]int {
	for _, grouped := range []bool{true, false} {
		if col, ok := d.placeColumns(level, zoom, grouped); ok {
			return col
		}
	}
	panic("schematic: points could not be placed, but the tracks run west to east")
}

func (d *Diagram) placeColumns(level Level, zoom int, grouped bool) (map[string]int, bool) {
	col := make(map[string]int, len(d.order))
	for _, pt := range d.order {
		col[pt] = 0
	}
	// each round pushes points east; without groups one round settles it, and
	// a group can only need as many rounds as there are points
	for range len(d.order) + 1 {
		changed := false
		for _, pt := range d.order {
			for _, t := range d.tracks {
				if t.From != pt {
					continue
				}
				if need := col[pt] + t.width(level, zoom); col[t.To] < need {
					col[t.To] = need
					changed = true
				}
			}
		}
		if grouped {
			east := make(map[string]int)
			for id, g := range d.group {
				east[g] = max(east[g], col[id])
			}
			for id, g := range d.group {
				if col[id] < east[g] {
					col[id] = east[g]
					changed = true
				}
			}
		}
		if !changed {
			return col, true
		}
	}
	return nil, false
}

// Render draws the diagram, one string per row.
func (d *Diagram) Render(opt Options) []string {
	return d.Draw(opt).Lines(opt.Color)
}

// Draw lays the diagram out on a grid of cells.
func (d *Diagram) Draw(opt Options) *Frame {
	opt.Zoom = max(opt.Zoom, 1)
	if opt.Level != Overview {
		f := d.draw(opt)
		d.drawState(f, opt)
		d.drawMarks(f, opt.Marks)
		return f
	}

	f := d.overview.draw(opt)
	for _, b := range d.overview.boxes {
		if b.code != "" {
			f.stations[b.code] = f.points[b.west].add(b.span/2, 0)
		}
	}
	for id := range d.points {
		if b := d.inBox[id]; b != nil {
			f.points[id] = f.points[b.west].add(b.span/2, 0)
		}
	}
	for id := range f.points {
		if _, ok := d.points[id]; !ok {
			delete(f.points, id) // box sides exist only in the overview
		}
	}
	d.drawState(f, opt)
	d.drawMarks(f, opt.Marks)
	return f
}

// draw lays out the tracks and their labels, without state.
func (d *Diagram) draw(opt Options) *Frame {
	col := d.columns(opt.Level, opt.Zoom)
	row := func(id string) int { return headerRows + d.lane[id]*rowsPerLane }

	width := 0
	for _, c := range col {
		width = max(width, c+1)
	}
	height := headerRows + (d.lanes-1)*rowsPerLane + 1
	if opt.Level != Layout {
		height++ // box bottoms, or point ids under the bottom lane
	}
	f := &Frame{
		cv:       newCanvas(width, height),
		points:   make(map[string]pos),
		stations: make(map[string]pos),
		labels:   make(map[string]span),
		trains:   make(map[string]pos),
		paths:    make(map[string][]pos),
		signals:  make(map[string]pos),
	}
	cv := f.cv
	for _, pt := range d.order {
		f.points[pt] = pos{col[pt], row(pt)}
	}

	for _, t := range d.tracks {
		if t.Kind != box {
			f.paths[t.ID] = cv.track(t, opt.Level, d.look(t, opt.State), col[t.From], row(t.From), col[t.To], row(t.To))
		}
	}
	trackWest := make(map[string]bool) // a track ends at the point from the west
	trackEast := make(map[string]bool)
	for _, t := range d.tracks {
		trackWest[t.To] = true
		trackEast[t.From] = true
	}
	for _, id := range d.order {
		if pt := d.points[id]; pt.DeadEnd || pt.Boundary {
			cv.set(col[id], row(id), endGlyph(pt.DeadEnd, trackWest[id], trackEast[id]), styleEnd)
		}
	}

	for _, s := range d.stations {
		if len(s.platforms) == 0 {
			continue
		}
		west, east, top := width, 0, d.lanes
		for _, t := range s.platforms {
			west, east = min(west, col[t.From]), max(east, col[t.To])
			top = min(top, d.lane[t.From])
		}
		label := s.code
		if full := label + " " + s.name; utf8.RuneCountInString(full) <= east-west+1 {
			label = full
		}
		f.stations[s.code] = pos{(west + east) / 2, headerRows + top*rowsPerLane}
		f.labels[s.code] = cv.label(label, west, east, headerRows+top*rowsPerLane-1)
	}
	for _, b := range d.boxes {
		d.drawBox(f, b, opt.State)
	}
	if opt.Level == Detail {
		d.drawIDs(f)
	}
	return f
}

// look is how a track is drawn for the state: by who holds it, faint if a
// switch is set against it, bright if a switch is locked for it.
func (d *Diagram) look(t *Track, st *vizproto.State) look {
	if st == nil {
		return look{}
	}
	l := look{state: free}
	switch r, o := st.TrackHolder(t.ID); {
	case o != "":
		l.state = occupied
	case r != "":
		l.state = reserved
	}
	for _, sb := range d.switches {
		if !slices.Contains(sb.Tracks, t.ID) {
			continue
		}
		s, ok := st.Switches[sb.ID]
		switch {
		case !ok:
		case s.SetFor != t.ID:
			l.faint = true
		case s.LockedBy != "":
			l.locked = true
		}
	}
	return l
}

// drawState adds signals, with their aspects when there is state, and
// trains.
func (d *Diagram) drawState(f *Frame, opt Options) {
	for _, sig := range d.signals {
		t := d.between(sig.Facing, sig.At)
		if t == nil || len(f.paths[t.ID]) < 3 {
			continue
		}
		path := f.paths[t.ID]
		p, glyph := path[1], '◀'
		if t.To == sig.At {
			p, glyph = path[len(path)-2], '▶'
		}
		f.signals[sig.ID] = p
		s := styleSignal
		if opt.State != nil {
			switch {
			case d.against(sig, t, opt.State):
				// coloured as the track it stands on
				f.cv.reglyph(p.X, p.Y, '≡')
				continue
			case d.clear(sig, t, opt.State):
				s = styleSignalClear
			default:
				s = styleSignalStop
			}
		}
		f.cv.set(p.X, p.Y, glyph, s)
	}
	if opt.State == nil {
		return
	}

	numbers := make([]string, 0, len(opt.State.Trains))
	for n := range opt.State.Trains {
		numbers = append(numbers, n)
	}
	slices.Sort(numbers)
	if i := slices.Index(numbers, opt.Selected); i >= 0 {
		numbers = append(slices.Delete(numbers, i, i+1), opt.Selected) // drawn last, on top
	}
	for _, n := range numbers {
		tr := opt.State.Trains[n]
		t, path := d.track[tr.Track], f.paths[tr.Track]
		if t == nil || len(path) == 0 {
			continue
		}
		east := tr.To == t.To
		if !east {
			path = slices.Clone(path)
			slices.Reverse(path)
		}
		progress := tr.Fraction(opt.Time)
		front := path[int(math.Round(progress*float64(len(path)-1)))]
		label := n
		x, y := front.X, front.Y
		if opt.Level != Overview {
			if east {
				label += "▶"
			} else {
				label = "◀" + label
			}
		}
		held := tr.Waiting.Held()
		if held {
			label += "⏸"
		}
		size := utf8.RuneCountInString(label)
		if east {
			x -= size - 1
		}
		// keep clear of the track's ends: buffer stops, toes, box sides
		lo, hi := path[0].X, path[0].X
		for _, p := range path {
			lo, hi = min(lo, p.X), max(hi, p.X)
		}
		if hi-lo-1 >= size {
			// The label travels the room inside the track in step with the
			// train. Lining it up with the front instead would pin it to the
			// start of the track until the front had run a label's width,
			// which on a long block section is much of the run.
			first, last := lo+1, hi-size
			if !east {
				first, last = last, first
			}
			x = first + int(math.Round(progress*float64(last-first)))
			y = rowAt(path, x+size/2)
		}
		s := styleTrain
		switch {
		case n == opt.Selected:
			s = styleSelected
		case held:
			s = styleHeld
		}
		f.cv.unlabel(x, y, size)
		for i, r := range []rune(label) {
			f.cv.set(x+i, y, r, s)
		}
		f.trains[n] = front
	}
}

// rowAt is the row of the cell of the path nearest to column x.
func rowAt(path []pos, x int) int {
	best := path[0]
	for _, p := range path[1:] {
		if abs(p.X-x) < abs(best.X-x) {
			best = p
		}
	}
	return best.Y
}

// between is the track joining two points, nil if none.
func (d *Diagram) between(a, b string) *Track {
	for _, t := range d.tracks {
		if (t.From == a && t.To == b) || (t.From == b && t.To == a) {
			return t
		}
	}
	return nil
}

// clear stands in for a signal aspect: the train holding the track up to
// the signal has the track beyond it reserved and not yet entered.
func (d *Diagram) clear(sig vizproto.Signal, approach *Track, st *vizproto.State) bool {
	train, _ := st.TrackHolder(approach.ID)
	if train == "" {
		return false
	}
	for _, t := range d.tracks {
		if t == approach || (t.From != sig.At && t.To != sig.At) {
			continue
		}
		if r, o := st.TrackHolder(t.ID); r == train && o != train {
			return true
		}
	}
	return false
}

// against reports whether a train is running past the signal from behind: the
// train holding the approach track has its route onto it from the signal's
// end. The signal does not govern that movement, so it has no aspect.
func (d *Diagram) against(sig vizproto.Signal, approach *Track, st *vizproto.State) bool {
	train, _ := st.TrackHolder(approach.ID)
	if train == "" {
		return false
	}
	tr, ok := st.Trains[train]
	if !ok || tr.Track == "" {
		return false
	}
	if tr.Track == approach.ID {
		return tr.To == sig.Facing
	}
	// follow the train's route on from its front until it reaches the
	// approach track
	seen := map[string]bool{tr.Track: true}
	for at := []string{tr.To}; len(at) > 0; at = at[1:] {
		p := at[0]
		for _, t := range d.tracks {
			if seen[t.ID] || (t.From != p && t.To != p) {
				continue
			}
			if r, _ := st.TrackHolder(t.ID); r != train {
				continue
			}
			if t == approach {
				return p == sig.At
			}
			seen[t.ID] = true
			if t.From == p {
				at = append(at, t.To)
			} else {
				at = append(at, t.From)
			}
		}
	}
	return false
}

// drawBox draws a collapsed station with a dot per platform: filled when
// held, coloured by who holds it.
func (d *Diagram) drawBox(f *Frame, b *stationBox, st *vizproto.State) {
	x1, x2 := f.points[b.west].X, f.points[b.east].X
	y1 := headerRows + b.lo*rowsPerLane - 1
	y2 := headerRows + b.hi*rowsPerLane + 1
	cv := f.cv
	for x := x1; x <= x2; x++ {
		for y := y1; y <= y2; y++ {
			cv.put(x, y, ' ', styleNone)
		}
	}
	for x := x1 + 1; x < x2; x++ {
		cv.put(x, y1, '─', styleNone)
		cv.put(x, y2, '─', styleNone)
	}
	cv.put(x1, y1, '┌', styleNone)
	cv.put(x2, y1, '┐', styleNone)
	cv.put(x1, y2, '└', styleNone)
	cv.put(x2, y2, '┘', styleNone)
	for y := y1 + 1; y < y2; y++ {
		cv.put(x1, y, '│', styleNone)
		cv.put(x2, y, '│', styleNone)
	}
	for id := range b.sides {
		p := f.points[id]
		if p.X == x1 {
			cv.put(x1, p.Y, '┤', styleNone)
		} else {
			cv.put(x2, p.Y, '├', styleNone)
		}
	}
	code := []rune(b.code)
	start := x1 + 1 + (x2-x1-1-len(code))/2
	for i, r := range code {
		cv.put(start+i, y1, r, styleStation)
	}
	start = x1 + 1 + (x2-x1-1-len(b.platforms))/2
	for i, pf := range b.platforms {
		r, s := '○', styleEnd
		if st != nil {
			switch res, occ := st.TrackHolder(pf.ID); {
			case occ != "":
				r, s = '●', styleOccupied
			case res != "":
				r, s = '●', styleReserved
			}
		}
		cv.put(start+i, y1+1, r, s)
	}
}

// drawIDs writes switch ids over (or under) their toes and point ids under
// their points, wherever there is room.
func (d *Diagram) drawIDs(f *Frame) {
	for _, sb := range d.switches {
		var at pos
		found := false
		counts := make(map[string]int)
		for _, tid := range sb.Tracks {
			if t := d.track[tid]; t != nil {
				counts[t.From]++
				counts[t.To]++
			}
		}
		for _, id := range d.order {
			if counts[id] == len(sb.Tracks) && len(sb.Tracks) > 1 {
				at, found = f.points[id], true
				break
			}
		}
		if !found {
			path := f.paths[sb.Tracks[0]]
			if len(path) == 0 {
				continue
			}
			at = path[len(path)/2]
		}
		f.cv.text(sb.ID, at.X, at.Y-1, at.Y+1)
	}
	for _, id := range d.order {
		p := f.points[id]
		f.cv.text(id, p.X, p.Y+1)
	}
}

// endGlyph marks a buffer stop or a track leaving the simulated world, open
// on the side its track comes from. A buffer stop with track on both sides
// means the world builds track beyond it.
func endGlyph(deadEnd, trackWest, trackEast bool) rune {
	switch {
	case deadEnd && trackWest && trackEast:
		return '┃'
	case deadEnd && trackWest:
		return ']'
	case deadEnd:
		return '['
	case trackWest:
		return '▶'
	default:
		return '◀'
	}
}

// pos is a cell, x across and y down.
type pos struct{ X, Y int }

func (p pos) add(dx, dy int) pos { return pos{p.X + dx, p.Y + dy} }

// span is the cells x1..x2 of row y.
type span struct{ y, x1, x2 int }

// Frame is a drawn diagram.
type Frame struct {
	cv       *canvas
	points   map[string]pos // every point of the world
	stations map[string]pos
	labels   map[string]span // where each station's name is written
	trains   map[string]pos  // front of each train drawn
	paths    map[string][]pos
	signals  map[string]pos
}

func (f *Frame) Width() int  { return f.cv.width }
func (f *Frame) Height() int { return len(f.cv.cells) }

// Station is where a station is drawn.
func (f *Frame) Station(code string) (x, y int, ok bool) {
	p, ok := f.stations[strings.ToUpper(code)]
	return p.X, p.Y, ok
}

// Train is where a train's front is drawn.
func (f *Frame) Train(number string) (x, y int, ok bool) {
	p, ok := f.trains[number]
	return p.X, p.Y, ok
}

// Anchor is a column given by the points either side of it, so the same
// place can be found in a frame drawn at another level or zoom.
type Anchor struct {
	west, east string
	frac       float64
	off        int // from the only point found, if there is just one
}

func (f *Frame) AnchorAt(x int) Anchor {
	var a Anchor
	wx, ex := math.MinInt, math.MaxInt
	for id, p := range f.points {
		switch {
		case p.X <= x && (p.X > wx || p.X == wx && id < a.west):
			a.west, wx = id, p.X
		case p.X > x && (p.X < ex || p.X == ex && id < a.east):
			a.east, ex = id, p.X
		}
	}
	switch {
	case a.west != "" && a.east != "":
		a.frac = float64(x-wx) / float64(ex-wx)
	case a.west != "":
		a.off = x - wx
	case a.east != "":
		a.off = x - ex
	}
	return a
}

// Column is where the anchor is in this frame.
func (f *Frame) Column(a Anchor) int {
	w, wok := f.points[a.west]
	e, eok := f.points[a.east]
	switch {
	case wok && eok:
		return w.X + int(math.Round(a.frac*float64(e.X-w.X)))
	case wok:
		return w.X + a.off
	case eok:
		return e.X + a.off
	}
	return 0
}

// Lines returns the rows with trailing space trimmed, with ANSI colours if
// asked.
func (f *Frame) Lines(color bool) []string {
	out := make([]string, f.Height())
	for y := range out {
		out[y] = strings.TrimRight(f.cv.row(y, 0, f.Width(), color), " ")
	}
	return out
}

// View returns the h rows of w cells starting at (x, y), padded with blanks
// outside the frame.
func (f *Frame) View(x, y, w, h int, color bool) []string {
	out := make([]string, h)
	for i := range out {
		out[i] = f.cv.row(y+i, x, w, color)
	}
	return out
}

type style int

const (
	styleNone style = iota
	styleTrack
	stylePlatform
	stylePlatformLabel
	styleMainLabel // a main line's platform label, underlined
	styleLength
	styleEnd
	styleStation
	styleID
	styleTrain
	styleSelected
	styleHeld   // a train precedence holds back, in amber
	styleSignal // a signal drawn without state, so without an aspect
	styleSignalClear
	styleSignalStop
	styleReserved
	styleOccupied
)

var sgr = map[style]string{
	stylePlatform:      "1",
	stylePlatformLabel: "1;33",
	styleMainLabel:     "1;33;4",
	styleLength:        "2",
	styleEnd:           "2",
	styleStation:       "1;36",
	styleID:            "2",
	styleTrain:         "1;7",
	styleSelected:      "1;30;43",
	styleHeld:          "1;30;48;5;214",
	styleSignal:        "1",
	styleSignalClear:   "1;32",
	styleSignalStop:    "1;31",
	styleReserved:      "32",
	styleOccupied:      "31",
}

type holdState int

const (
	none holdState = iota // drawn without state
	free
	reserved
	occupied
)

type look struct {
	state         holdState
	faint, locked bool
}

// rank orders looks for a cell two tracks share: occupied beats reserved
// beats free, and a leg a switch is set for beats one it is not.
func (l look) rank() int {
	r := int(l.state) * 4
	if !l.faint {
		r += 2
	}
	if l.locked {
		r++
	}
	return r
}

type cell struct {
	r     rune
	style style
	track bool // a track glyph, coloured by look
	look  look
	mark  Mark
}

func (c cell) sgr() string {
	switch c.mark {
	case MarkSelected:
		return "1;30;46"
	case MarkProblem:
		return "1;97;41"
	}
	codes := []string{}
	if s := sgr[c.style]; s != "" {
		codes = append(codes, s)
	}
	if c.track {
		switch c.look.state {
		case free:
			codes = append(codes, "90")
		case reserved:
			codes = append(codes, "32")
		case occupied:
			codes = append(codes, "31")
		}
		if c.look.faint {
			codes = append(codes, "2")
		}
		if c.look.locked {
			codes = append(codes, "1")
		}
	}
	return strings.Join(codes, ";")
}

type canvas struct {
	cells [][]cell
	width int
}

func newCanvas(width, height int) *canvas {
	cv := &canvas{cells: make([][]cell, height), width: width}
	for y := range cv.cells {
		cv.cells[y] = make([]cell, width)
		for x := range cv.cells[y] {
			cv.cells[y][x] = cell{r: ' '}
		}
	}
	return cv
}

func (cv *canvas) in(x, y int) bool {
	return y >= 0 && y < len(cv.cells) && x >= 0 && x < cv.width
}

// set draws a glyph. Two diagonals in one cell make a crossing, and a
// diagonal is not overwritten by a horizontal.
func (cv *canvas) set(x, y int, r rune, s style) {
	cv.draw(x, y, r, s, false, look{})
}

func (cv *canvas) setTrack(x, y int, r rune, s style, l look) {
	cv.draw(x, y, r, s, true, l)
}

func (cv *canvas) draw(x, y int, r rune, s style, track bool, l look) {
	if !cv.in(x, y) {
		return
	}
	c := &cv.cells[y][x]
	if track && c.track && c.look.rank() > l.rank() {
		l = c.look
	}
	switch {
	case (c.r == '╱' && r == '╲') || (c.r == '╲' && r == '╱'):
		r = '╳'
	case isDiagonal(c.r) && (r == '─' || r == '═'):
		if track && c.track {
			c.look = l
		}
		return
	}
	*c = cell{r: r, style: s, track: track, look: l}
}

// put draws a glyph over whatever is there.
// reglyph changes a cell's glyph and keeps its colours.
func (cv *canvas) reglyph(x, y int, r rune) {
	if cv.in(x, y) {
		cv.cells[y][x].r = r
	}
}

func (cv *canvas) put(x, y int, r rune, s style) {
	if cv.in(x, y) {
		cv.cells[y][x] = cell{r: r, style: s}
	}
}

func isDiagonal(r rune) bool { return r == '╱' || r == '╲' || r == '╳' }

// track draws a track from (x1, y1) to (x2, y2), x1 < x2, and returns the
// cells it runs through, west to east. A track that changes lanes runs level,
// crosses diagonally and runs level again. The diagonal leaves right where
// the track branches off others (e.g. at a switch toe), or is centred when it
// branches at both ends or neither (a crossover, where the two legs meet in
// an X).
func (cv *canvas) track(t *Track, level Level, l look, x1, y1, x2, y2 int) []pos {
	// a through track has no platform, so it is drawn as plain track
	glyph, s := '─', styleTrack
	if t.Kind == Platform && t.Class != "through" {
		glyph, s = '═', stylePlatform
	}

	rise := abs(y2 - y1)
	step, diag := 1, '╲'
	if y2 < y1 {
		step, diag = -1, '╱'
	}
	var path []pos
	bend, resume := t.bend(x1, x2, rise)
	for x := x1; x <= bend; x++ {
		cv.setTrack(x, y1, glyph, s, l)
		path = append(path, pos{x, y1})
	}
	for k := 1; k < rise; k++ {
		cv.setTrack(bend+k, y1+k*step, diag, s, l)
		path = append(path, pos{bend + k, y1 + k*step})
	}
	for x := resume; x <= x2; x++ {
		cv.setTrack(x, y2, glyph, s, l)
		path = append(path, pos{x, y2})
	}

	text := t.label(level)
	if text == "" {
		return path
	}
	ls := styleLength
	switch {
	case t.Kind == Platform && t.Class == "main":
		ls = styleMainLabel
	case t.Kind == Platform:
		ls = stylePlatformLabel
	}
	// on the longer level part, if it fits with a track glyph either side
	lx1, lx2, ly := x1, bend, y1
	switch {
	case rise == 0:
		lx2 = x2
	case x2-resume > bend-x1:
		lx1, lx2, ly = resume, x2, y2
	}
	label := []rune(text)
	if len(label)+2 > lx2-lx1+1 {
		return path
	}
	start := lx1 + (lx2-lx1+1-len(label))/2
	for i, r := range label {
		cv.set(start+i, ly, r, ls)
	}
	return path
}

// bend is the last column of the track's west level part and resume the first
// of its east one, for a track drawn from column x1 to x2 that rises (or
// falls) rise rows.
func (t *Track) bend(x1, x2, rise int) (bend, resume int) {
	bend = x1 + (x2-x1-rise)/2
	switch {
	case rise == 0:
	case t.branchWest && !t.branchEast:
		bend = x1
	case t.branchEast && !t.branchWest:
		bend = x2 - rise
	}
	return bend, bend + max(rise, 1)
}

// label writes a station name centred over west..east on the first row at or
// above row with room for it, or failing that on the top row, moved east past
// whatever is there.
func (cv *canvas) label(label string, west, east, row int) span {
	runes := []rune(label)
	start := max(west+(east-west+1-len(runes))/2, 0)
	for y := min(row, len(cv.cells)-1); y > 0; y-- {
		if cv.free(start-1, start+len(runes), y) {
			cv.write(start, y, runes, styleStation)
			return span{y, start, start + len(runes) - 1}
		}
	}
	for !cv.free(start-1, start+len(runes), 0) && start < cv.width {
		start++
	}
	cv.write(start, 0, runes, styleStation)
	return span{0, start, start + len(runes) - 1}
}

// text writes an id starting at column x on the first of the rows with room
// for it, or not at all.
func (cv *canvas) text(id string, x int, rows ...int) {
	runes := []rune(id)
	for _, y := range rows {
		if y >= 0 && y < len(cv.cells) && x+len(runes) <= cv.width && cv.free(x-1, x+len(runes), y) {
			cv.write(x, y, runes, styleID)
			return
		}
	}
}

// unlabel turns any track label under x..x+n-1 on row y back into track, so
// a train drawn over part of it leaves no pieces behind.
func (cv *canvas) unlabel(x, y, n int) {
	isLabel := func(x int) bool {
		return cv.in(x, y) && (cv.cells[y][x].style == styleLength || cv.cells[y][x].style == stylePlatformLabel)
	}
	for i := x; i < x+n; i++ {
		if !isLabel(i) {
			continue
		}
		a, b := i, i
		for isLabel(a - 1) {
			a--
		}
		for isLabel(b + 1) {
			b++
		}
		fill := cell{r: '─', track: true}
		if cv.in(a-1, y) && cv.cells[y][a-1].track {
			fill = cv.cells[y][a-1]
		} else if cv.in(b+1, y) && cv.cells[y][b+1].track {
			fill = cv.cells[y][b+1]
		}
		if fill.r != '═' {
			fill.r = '─' // the neighbour's colours, not a ≡ signal's glyph
		}
		for k := a; k <= b; k++ {
			cv.cells[y][k] = fill
		}
		i = b
	}
}

// free reports whether row y is blank from x1 to x2 (clipped to the canvas).
func (cv *canvas) free(x1, x2, y int) bool {
	for x := max(x1, 0); x <= min(x2, cv.width-1); x++ {
		if cv.cells[y][x].r != ' ' {
			return false
		}
	}
	return true
}

// write puts text on row y, growing the canvas if it runs off the east edge.
func (cv *canvas) write(x, y int, text []rune, s style) {
	for len(text)+x > cv.width {
		for i := range cv.cells {
			cv.cells[i] = append(cv.cells[i], cell{r: ' '})
		}
		cv.width++
	}
	for i, r := range text {
		cv.set(x+i, y, r, s)
	}
}

// row renders w cells of row y from column x, blank outside the canvas.
func (cv *canvas) row(y, x, w int, color bool) string {
	var sb strings.Builder
	cur := ""
	for i := range w {
		c := cell{r: ' '}
		if cv.in(x+i, y) {
			c = cv.cells[y][x+i]
		}
		if color {
			if s := c.sgr(); s != cur {
				sb.WriteString("\x1b[0m")
				if s != "" {
					sb.WriteString("\x1b[")
					sb.WriteString(s)
					sb.WriteString("m")
				}
				cur = s
			}
		}
		sb.WriteRune(c.r)
	}
	if color && cur != "" {
		sb.WriteString("\x1b[0m")
	}
	return sb.String()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// drawMarks picks out the marked pieces: a track along its path, a point,
// signal or station name where it is drawn. A point drawn as blank gets a
// dot, so it shows.
func (d *Diagram) drawMarks(f *Frame, marks map[string]Mark) {
	cv := f.cv
	mark := func(p pos, m Mark) {
		if cv.in(p.X, p.Y) {
			cv.cells[p.Y][p.X].mark = m
		}
	}
	for id, m := range marks {
		if m == MarkNone {
			continue
		}
		if path, ok := f.paths[id]; ok {
			for _, p := range path {
				mark(p, m)
			}
			continue
		}
		if p, ok := f.signals[id]; ok {
			mark(p, m)
			continue
		}
		if p, ok := f.points[id]; ok {
			if cv.in(p.X, p.Y) && cv.cells[p.Y][p.X].r == ' ' {
				cv.cells[p.Y][p.X].r = '•'
			}
			mark(p, m)
			continue
		}
		if l, ok := f.labels[id]; ok {
			for x := l.x1; x <= l.x2; x++ {
				mark(pos{x, l.y}, m)
			}
		}
	}
}

// PieceKind is what a piece of a drawn world is.
type PieceKind int

const (
	PiecePoint PieceKind = iota + 1
	PieceTrack
	PieceSignal
	PieceStation
)

// Piece is a point, track or signal of the world, by id, or a station, by
// its code.
type Piece struct {
	Kind PieceKind
	ID   string
}

// PiecesAt lists the pieces drawn in cell (x, y), the most specific first:
// a point, then a signal, then the tracks running through it, then a
// station whose name is written there.
func (f *Frame) PiecesAt(x, y int) []Piece {
	var out []Piece
	at := pos{x, y}
	for _, id := range sortedKeys(f.points) {
		if f.points[id] == at {
			out = append(out, Piece{PiecePoint, id})
		}
	}
	for _, id := range sortedKeys(f.signals) {
		if f.signals[id] == at {
			out = append(out, Piece{PieceSignal, id})
		}
	}
	for _, id := range sortedKeys(f.paths) {
		if slices.Contains(f.paths[id], at) {
			out = append(out, Piece{PieceTrack, id})
		}
	}
	for _, code := range sortedKeys(f.labels) {
		if l := f.labels[code]; l.y == y && x >= l.x1 && x <= l.x2 {
			out = append(out, Piece{PieceStation, code})
		}
	}
	return out
}

// At is the most specific piece drawn in cell (x, y) (see PiecesAt).
func (f *Frame) At(x, y int) (Piece, bool) {
	if ps := f.PiecesAt(x, y); len(ps) > 0 {
		return ps[0], true
	}
	return Piece{}, false
}

// Where is the cell a piece is drawn at: a point's or signal's cell, the
// middle of a track's path, the middle of a station's name.
func (f *Frame) Where(p Piece) (x, y int, ok bool) {
	switch p.Kind {
	case PiecePoint:
		q, ok := f.points[p.ID]
		return q.X, q.Y, ok
	case PieceSignal:
		q, ok := f.signals[p.ID]
		return q.X, q.Y, ok
	case PieceTrack:
		path := f.paths[p.ID]
		if len(path) == 0 {
			return 0, 0, false
		}
		q := path[len(path)/2]
		return q.X, q.Y, true
	case PieceStation:
		l, ok := f.labels[p.ID]
		return (l.x1 + l.x2) / 2, l.y, ok
	}
	return 0, 0, false
}

// Pieces lists every piece drawn, points first, then signals, tracks and
// stations, each in id order.
func (f *Frame) Pieces() []Piece {
	var out []Piece
	for _, id := range sortedKeys(f.points) {
		out = append(out, Piece{PiecePoint, id})
	}
	for _, id := range sortedKeys(f.signals) {
		out = append(out, Piece{PieceSignal, id})
	}
	for _, id := range sortedKeys(f.paths) {
		out = append(out, Piece{PieceTrack, id})
	}
	for _, code := range sortedKeys(f.labels) {
		out = append(out, Piece{PieceStation, code})
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
