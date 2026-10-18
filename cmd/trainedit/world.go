package main

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/infra"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"
	"trainapp/worlds/edit"
	"trainapp/worlds/templates"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// worldTab edits the world on its schematic: arrows pick pieces, letters
// edit them, the mouse picks, pans, zooms and draws track from open ends.
type worldTab struct {
	m        *model
	cam      *tui.Camera
	diagram  *schematic.Diagram
	drawErr  error
	problems []edit.Problem
	problem  int // the problem [ and ] last went to

	sel schematic.Piece
	// pending is the first piece of an edit that takes two (a crossover's
	// first track, a block section's first point), and the edit
	pending     *schematic.Piece
	pendingEdit string

	drag dragState
}

type dragState struct {
	on     bool
	from   string // the open point a drag draws track from; empty to pan
	x, y   int    // where the mouse was, on screen
	fx, fy int    // the point's frame cell
}

// worldPaneTop is the screen row of the schematic's first line: the
// header, then the box's top border.
const worldPaneTop = 2

// bottomHeight is the inspector and problems boxes' height.
const bottomHeight = 8

func newWorldTab(m *model) *worldTab {
	w := &worldTab{m: m, problem: -1}
	w.rebuild()
	w.cam = tui.NewCamera(w.draw)
	return w
}

// rebuild lays the snapshot out again, from the built world if it builds
// (so main lines show), and lists its problems.
func (w *worldTab) rebuild() {
	s := w.m.s.world.S
	w.problems = edit.Problems(s)
	w.diagram, w.drawErr = nil, nil
	if built, err := s.Build(); err == nil {
		w.diagram, w.drawErr = schematic.FromWorld(built)
	}
	if w.diagram == nil {
		w.diagram, w.drawErr = schematic.FromSnapshot(s)
	}
	if w.diagram == nil { // a file that cannot be drawn: show nothing but say why
		w.diagram, _ = schematic.FromSnapshot(edit.Empty(s.Defaults))
	}
}

// changed is called after every edit, undo and save.
func (w *worldTab) changed() {
	w.rebuild()
	if w.sel.ID != "" && !w.exists(w.sel) {
		w.sel = schematic.Piece{}
	}
	if w.pending != nil && !w.exists(*w.pending) {
		w.pending = nil
	}
	w.cam.Redraw()
}

func (w *worldTab) exists(p schematic.Piece) bool {
	return len(edit.Describe(w.m.s.world.S, p)) > 0
}

func (w *worldTab) draw(z tui.ZoomStep) *schematic.Frame {
	marks := map[string]schematic.Mark{}
	for _, p := range w.problems {
		if p.Piece.ID != "" {
			marks[p.Piece.ID] = schematic.MarkProblem
		}
	}
	if w.pending != nil {
		marks[w.pending.ID] = schematic.MarkSelected
	}
	if w.sel.ID != "" {
		marks[w.sel.ID] = schematic.MarkSelected
	}
	return w.diagram.Draw(schematic.Options{Level: z.Level, Zoom: z.Zoom, Marks: marks})
}

func (w *worldTab) paneSize() (int, int) {
	return max(w.m.width-2, 1), max(w.m.height-3-bottomHeight-2, 1)
}

// resize fits the pane to the window. The first time, the world is drawn at
// the layout level, where every point can be picked, as wide as fits: the
// overview shows stations as boxes, with nothing in them to edit.
func (w *worldTab) resize() {
	first := w.cam.W == 1 && w.cam.H == 1
	w.cam.W, w.cam.H = w.paneSize()
	if first {
		w.cam.Fit()
		w.cam.Step = max(w.cam.Step, 1)
		w.cam.X = 0
	}
	w.cam.Redraw()
}

// do applies an edit, reporting how it went.
func (w *worldTab) do(what string, edit func(*infra.Snapshot) error) bool {
	if err := w.m.s.world.Do(edit); err != nil {
		w.m.fail(what + ": " + err.Error())
		return false
	}
	w.changed()
	w.m.say("%s", what)
	return true
}

func (w *worldTab) selectPiece(p schematic.Piece) {
	w.sel = p
	w.cam.Redraw()
	if x, y, ok := w.cam.Frame.Where(p); ok {
		w.cam.Show(x, y, 8)
	}
}

// selectPoint selects a point by id.
func (w *worldTab) selectPoint(id string) {
	w.selectPiece(schematic.Piece{Kind: schematic.PiecePoint, ID: id})
}

func (w *worldTab) key(msg tea.KeyPressMsg) tea.Cmd {
	s := w.m.s.world.S
	cam := w.cam
	switch msg.String() {
	case "left":
		w.moveSel(-1, 0)
	case "right":
		w.moveSel(1, 0)
	case "up":
		w.moveSel(0, -1)
	case "down":
		w.moveSel(0, 1)
	case "shift+left", "H":
		cam.Pan(-8, 0)
	case "shift+right", "L":
		cam.Pan(8, 0)
	case "shift+up", "K":
		cam.Pan(0, -2)
	case "shift+down", "J":
		cam.Pan(0, 2)
	case "+", "=":
		cam.ZoomTo(cam.Step+1, cam.W/2, cam.H/2)
	case "-", "_":
		cam.ZoomTo(cam.Step-1, cam.W/2, cam.H/2)
	case "0":
		cam.Fit()
	case "tab":
		w.cycle()
	case "esc":
		w.sel, w.pending = schematic.Piece{}, nil
		cam.Redraw()
	case "/":
		w.m.open(newForm("go to a station", func(v values) error {
			code := strings.ToUpper(v["code"])
			x, y, ok := cam.Frame.Station(code)
			if !ok {
				if !slices.ContainsFunc(w.m.s.world.S.Stations, func(st infra.SnapshotStation) bool { return st.Code == code }) {
					return fmt.Errorf("no station %s", code)
				}
				// a station with no platforms is not drawn, but can be picked
				w.sel = schematic.Piece{Kind: schematic.PieceStation, ID: code}
				cam.Redraw()
				w.m.say("%s has no platforms, so it is not drawn: t stamps its tracks, p on a track makes one its platform, d deletes it", code)
				return nil
			}
			cam.CentreOn(x, y)
			w.sel = schematic.Piece{Kind: schematic.PieceStation, ID: code}
			cam.Redraw()
			return nil
		}, text("code", "station code", "")))
	case "]", "[":
		w.nextProblem(msg.String() == "]")
	case "u":
		if w.m.s.world.Undo() {
			w.changed()
			w.m.say("undone")
		} else {
			w.m.say("nothing to undo")
		}
	case "ctrl+r":
		if w.m.s.world.Redo() {
			w.changed()
			w.m.say("redone")
		} else {
			w.m.say("nothing to redo")
		}
	case "e":
		w.extendForm()
	case "c":
		w.splitForm()
	case "j":
		if p, ok := w.point(); ok {
			w.do("joined the tracks at "+p, func(s *infra.Snapshot) error { return edit.Join(s, p) })
		}
	case "s":
		w.switchForm()
	case "x":
		w.twoPieces("crossover", schematic.PieceTrack)
	case "X":
		w.twoPieces("crossing", schematic.PieceTrack)
	case "Y":
		w.junctionForm()
	case "b":
		w.twoPieces("line", schematic.PiecePoint)
	case "g", "G":
		w.signal(msg.String() == "g")
	case "o":
		if p, ok := w.point(); ok {
			kinds := []string{edit.EndOpen, edit.EndBuffer, edit.EndBoundary}
			next := kinds[(slices.Index(kinds, endOf(s, p))+1)%len(kinds)]
			w.do(p+" is now "+next, func(s *infra.Snapshot) error { return edit.SetEnd(s, p, next) })
		}
	case "p":
		w.platformForm()
	case "n":
		w.stationForm()
	case "t":
		w.openBuilder()
	case "a":
		w.attrsForm()
	case "D":
		w.defaultsForm()
	case "d", "delete", "backspace":
		if w.sel.ID == "" {
			w.m.fail("select something to delete first")
			break
		}
		p := w.sel
		if w.do("deleted "+p.ID, func(s *infra.Snapshot) error { return edit.Delete(s, p) }) {
			w.sel = schematic.Piece{}
			w.cam.Redraw()
		}
	}
	return nil
}

// point is the selected point's id, or says to select one.
func (w *worldTab) point() (string, bool) {
	if w.sel.Kind != schematic.PiecePoint {
		w.m.fail("select a point first (the arrows move between pieces)")
		return "", false
	}
	return w.sel.ID, true
}

// track is the selected track's id, or says to select one.
func (w *worldTab) track() (string, bool) {
	if w.sel.Kind != schematic.PieceTrack {
		w.m.fail("select a track first (the arrows move between pieces)")
		return "", false
	}
	return w.sel.ID, true
}

// moveSel selects the nearest piece in a direction from the selected one
// (or the middle of the pane).
func (w *worldTab) moveSel(dx, dy int) {
	f := w.cam.Frame
	x0, y0 := w.cam.X+w.cam.W/2, w.cam.Y+w.cam.H/2
	if x, y, ok := f.Where(w.sel); ok {
		x0, y0 = x, y
	}
	var best schematic.Piece
	bestScore := math.MaxInt
	for _, p := range f.Pieces() {
		if p == w.sel {
			continue
		}
		x, y, ok := f.Where(p)
		if !ok {
			continue
		}
		ddx, ddy := x-x0, y-y0
		var score int
		switch {
		case dx != 0 && ddx*dx > 0:
			score = ddx*dx + 4*abs(ddy)
		case dy != 0 && ddy*dy > 0:
			score = 2*ddy*dy + abs(ddx)
		default:
			continue
		}
		if score < bestScore {
			best, bestScore = p, score
		}
	}
	if best.ID != "" {
		w.selectPiece(best)
	}
}

// cycle steps through the pieces drawn in the selected piece's cell.
func (w *worldTab) cycle() {
	f := w.cam.Frame
	x, y, ok := f.Where(w.sel)
	if !ok {
		w.moveSel(1, 0)
		return
	}
	ps := f.PiecesAt(x, y)
	if len(ps) < 2 {
		return
	}
	w.selectPiece(ps[(slices.Index(ps, w.sel)+1)%len(ps)])
}

func (w *worldTab) nextProblem(forward bool) {
	var ps []edit.Problem
	for _, p := range w.problems {
		if p.Piece.ID != "" {
			ps = append(ps, p)
		}
	}
	if len(ps) == 0 {
		w.m.say("no problems to go to")
		return
	}
	if forward {
		w.problem = (w.problem + 1) % len(ps)
	} else {
		w.problem = (w.problem - 1 + len(ps)) % len(ps)
	}
	p := ps[w.problem]
	w.selectPiece(p.Piece)
	w.m.fail(p.Text)
}

// twoPieces starts or finishes an edit that takes two pieces of a kind.
func (w *worldTab) twoPieces(what string, kind schematic.PieceKind) {
	if w.sel.Kind != kind {
		w.m.fail(fmt.Sprintf("a %s needs two %ss: select the first", what, kindName(kind)))
		return
	}
	if w.pending == nil || w.pendingEdit != what {
		p := w.sel
		w.pending, w.pendingEdit = &p, what
		w.m.say("now select the other %s and press %s again (esc to cancel)", kindName(kind), map[string]string{"crossover": "x", "crossing": "X", "line": "b"}[what])
		w.cam.Redraw()
		return
	}
	first, second := *w.pending, w.sel
	if first == second {
		w.m.fail("pick another " + kindName(kind))
		return
	}
	done := func() {
		w.pending = nil
		w.cam.Redraw()
	}
	if what == "crossover" {
		kinds := map[string]string{"down going east": templates.CrossDown, "up going east": templates.CrossUp, "scissors": templates.CrossScissors}
		w.m.open(newForm("crossover", func(v values) error {
			if !w.do("laid a crossover", func(s *infra.Snapshot) error { return edit.Crossover(s, first.ID, second.ID, kinds[v["way"]]) }) {
				return errors.New(w.m.note)
			}
			done()
			return nil
		}, choice("way", "diagonal", "down going east", "down going east", "up going east", "scissors")))
		return
	}
	if what == "crossing" {
		w.m.open(newForm("crossing", func(v values) error {
			if !w.do("laid a "+v["kind"], func(s *infra.Snapshot) error { return edit.Crossing(s, first.ID, second.ID, v["kind"]) }) {
				return errors.New(w.m.note)
			}
			done()
			return nil
		}, field{key: "kind", label: "kind", value: edit.CrossingDiamond, choices: edit.Crossings,
			help: "diamond: the lines cross and swap lanes; a slip also keeps the upper, lower or both straight roads"}))
		return
	}
	s := w.m.s.world.S
	name := "sl" + titled(codeOf(s, first.ID)) + titled(codeOf(s, second.ID))
	ways := map[string]byte{"both ways": edit.WayBoth, "east": edit.WayEast, "west": edit.WayWest}
	f := newForm("line", func(v values) error {
		km, err := parseNum(v, "km", "length")
		if err != nil {
			return err
		}
		blocks, err := parseInt(v, "blocks", "blocks")
		if err != nil {
			return err
		}
		kmph, err := parseNum(v, "kmph", "speed")
		if err != nil {
			return err
		}
		if !w.do("laid line "+v["name"], func(s *infra.Snapshot) error {
			if v["join"] == "all open ends beside them" {
				return edit.LinesBeside(s, first.ID, second.ID, v["name"], v["pattern"], km*1000, max(blocks, 1), kmph)
			}
			return edit.Line(s, first.ID, second.ID, v["name"], ways[v["runs"]], km*1000, max(blocks, 1), kmph)
		}) {
			return errors.New(w.m.note)
		}
		done()
		return nil
	},
		text("name", "name", name),
		number("km", "length (km)", 10),
		number("blocks", "blocks", 1),
		number("kmph", "speed (km/h)", s.Defaults.RouteKmph),
		field{key: "runs", label: "runs", value: "both ways", choices: []string{"both ways", "east", "west"}, help: "the way trains run on it: signals stand for that way only"},
		field{key: "join", label: "join", value: "these two points", choices: []string{"these two points", "all open ends beside them"}, help: "all: a line from each open end beside one to each beside the other"},
		field{key: "pattern", label: "lines run", value: "", help: "with all: E, W or B a line, top to bottom; empty for the usual (E W for two)"},
	)
	w.m.open(f)
}

func kindName(k schematic.PieceKind) string {
	return map[schematic.PieceKind]string{schematic.PiecePoint: "point", schematic.PieceTrack: "track", schematic.PieceSignal: "signal", schematic.PieceStation: "station"}[k]
}

func (w *worldTab) extendForm() {
	s := w.m.s.world.S
	if len(s.Points) == 0 {
		var start string
		w.do("started the world", func(s *infra.Snapshot) error { start = edit.AddPoint(s); return nil })
		w.selectPoint(start)
	}
	p, ok := w.point()
	if !ok {
		return
	}
	dir := "east"
	if !openSide(s, p, true) && openSide(s, p, false) {
		dir = "west"
	}
	w.extendFormAt(p, dir, 0)
}

func (w *worldTab) extendFormAt(p, dir string, lanes int) {
	s := w.m.s.world.S
	w.m.open(newForm("extend from "+p, func(v values) error {
		m, err := parseNum(v, "m", "length")
		if err != nil {
			return err
		}
		kmph, err := parseNum(v, "kmph", "speed")
		if err != nil {
			return err
		}
		lanes, err := parseInt(v, "lanes", "lanes")
		if err != nil {
			return err
		}
		var np string
		if !w.do("extended "+p, func(s *infra.Snapshot) (err error) {
			np, err = edit.Extend(s, p, v["dir"] == "east", m, kmph, lanes)
			return err
		}) {
			return errors.New(w.m.note)
		}
		w.selectPoint(np)
		return nil
	},
		choice("dir", "direction", dir, "east", "west"),
		number("m", "length (m)", 1000),
		number("kmph", "speed (km/h)", s.Defaults.RouteKmph),
		field{key: "lanes", label: "lanes", value: strconv.Itoa(lanes), help: "how many lanes the new point is below (negative: above)"},
	))
}

func (w *worldTab) splitForm() {
	t, ok := w.track()
	if !ok {
		return
	}
	length := trackLength(w.m.s.world.S, t)
	w.m.open(newForm("split "+t, func(v values) error {
		m, err := parseNum(v, "m", "distance")
		if err != nil {
			return err
		}
		var np string
		if !w.do("split the track", func(s *infra.Snapshot) (err error) { np, err = edit.Split(s, t, m); return }) {
			return errors.New(w.m.note)
		}
		w.selectPoint(np)
		return nil
	}, number("m", fmt.Sprintf("from the west end (m, of %g)", length), length/2)))
}

func (w *worldTab) switchForm() {
	p, ok := w.point()
	if !ok {
		return
	}
	s := w.m.s.world.S
	dir := "east"
	if wn, en := countSides(s, p); en != 1 && wn == 1 {
		dir = "west"
	}
	w.m.open(newForm("switch at "+p, func(v values) error {
		lanes, err := parseInt(v, "lanes", "lanes")
		if err != nil {
			return err
		}
		var np string
		if !w.do("added a switch at "+p, func(s *infra.Snapshot) (err error) {
			np, err = edit.AddSwitch(s, p, v["dir"] == "east", lanes)
			return err
		}) {
			return errors.New(w.m.note)
		}
		w.selectPoint(np)
		return nil
	},
		choice("dir", "legs leave to the", dir, "east", "west"),
		field{key: "lanes", label: "lanes", value: "1", help: "how many lanes down the diverging leg goes (negative: up)"},
	))
}

// signal toggles a signal on the selected track (at its east end for trains
// running east, else its west end), or at the selected point on its one
// track that way.
func (w *worldTab) signal(east bool) {
	s := w.m.s.world.S
	t := ""
	switch w.sel.Kind {
	case schematic.PieceTrack:
		t = w.sel.ID
	case schematic.PiecePoint:
		// a signal at the point for trains running east stands on the track
		// west of it
		var ts []string
		for _, tr := range s.Tracks {
			if (east && tr.To == w.sel.ID) || (!east && tr.From == w.sel.ID) {
				ts = append(ts, tr.ID)
			}
		}
		if len(ts) != 1 {
			w.m.fail(fmt.Sprintf("%s has %d tracks %s of it: select the track the signal is on", w.sel.ID, len(ts), map[bool]string{true: "west", false: "east"}[east]))
			return
		}
		t = ts[0]
	case schematic.PieceSignal:
		i := slices.IndexFunc(s.Signals, func(sig infra.SnapshotSignal) bool { return sig.ID == w.sel.ID })
		if i >= 0 {
			id := w.sel.ID
			w.do("took away signal "+id, func(s *infra.Snapshot) error {
				return edit.Delete(s, schematic.Piece{Kind: schematic.PieceSignal, ID: id})
			})
		}
		return
	default:
		w.m.fail("select a track or point for the signal")
		return
	}
	way := "east"
	if !east {
		way = "west"
	}
	w.do("toggled the signal for trains running "+way, func(s *infra.Snapshot) error { return edit.ToggleSignal(s, t, east) })
}

func (w *worldTab) platformForm() {
	t, ok := w.track()
	if !ok {
		return
	}
	s := w.m.s.world.S
	codes := w.m.s.stationCodes()
	if len(codes) == 0 {
		w.m.fail("no stations yet: n adds one")
		return
	}
	stn, no, length := codes[0], "", s.Defaults.PlatformLengthM
	for _, st := range s.Stations {
		for _, pf := range st.Platforms {
			if pf.Track == t {
				stn, no, length = st.Code, pf.No, pf.LengthM
			}
		}
	}
	taken := w.m.s.platforms(stn)
	if no == "" {
		no = nextPlatform(taken)
	}
	w.m.open(newForm("platform on "+t, func(v values) error {
		m, err := parseNum(v, "m", "length")
		if err != nil {
			return err
		}
		what := "platform " + v["no"] + " of " + v["stn"]
		if v["no"] == "" {
			what = "took the platform off"
		}
		if !w.do(what, func(s *infra.Snapshot) error { return edit.SetPlatform(s, t, v["stn"], v["no"], m) }) {
			return errors.New(w.m.note)
		}
		return nil
	},
		choice("stn", "station", stn, codes...),
		field{key: "no", label: "platform no", value: no, help: "empty takes it off; " + stn + " has " + tui.ValueOr(strings.Join(taken, " "), "none yet")},
		field{key: "m", label: "length (m)", value: strconv.FormatFloat(length, 'f', -1, 64), help: "0 makes it a through track, with nowhere to board"},
	))
}

func (w *worldTab) stationForm() {
	code, name := "", ""
	if w.sel.Kind == schematic.PieceStation {
		code = w.sel.ID
		for _, st := range w.m.s.world.S.Stations {
			if st.Code == code {
				name = st.Name
			}
		}
	}
	w.m.open(newForm("station", func(v values) error {
		c := strings.ToUpper(v["code"])
		if !w.do("station "+c, func(s *infra.Snapshot) error { return edit.SetStation(s, c, v["name"]) }) {
			return errors.New(w.m.note)
		}
		w.m.say("station %s: select a track and press p to give it a platform", c)
		return nil
	}, text("code", "code", code), text("name", "name", name)))
}

// junctionForm leaves the line at the selected track for a branch. The line's
// tracks are the plain ones drawn in the selected track's column (or just
// the selected one).
func (w *worldTab) junctionForm() {
	t, ok := w.track()
	if !ok {
		return
	}
	s := w.m.s.world.S
	column := []string{t}
	if x, _, ok := w.cam.Frame.Where(w.sel); ok {
		column = nil
		for y := range w.cam.Frame.Height() {
			for _, p := range w.cam.Frame.PiecesAt(x, y) {
				if p.Kind == schematic.PieceTrack && edit.Plain(s, p.ID) && !slices.Contains(column, p.ID) {
					column = append(column, p.ID)
				}
			}
		}
		if !slices.Contains(column, t) {
			column = append(column, t)
		}
	}
	var ways []byte
	for _, tr := range column {
		ways = append(ways, cmp.Or(edit.Way(s, tr), edit.WayBoth))
	}
	all := fmt.Sprintf("the %d tracks in its column", len(column))
	w.m.open(newForm("junction at "+t, func(v values) error {
		spec := edit.JunctionSpec{Mains: []string{t}, Below: v["side"] == "below", East: v["leaves"] == "east",
			Code: strings.ToUpper(v["code"]), Pattern: v["pattern"], Name: v["name"]}
		if v["mains"] == all {
			spec.Mains = column
		}
		var err error
		if spec.Lanes, err = parseInt(v, "lanes", "lanes"); err != nil {
			return err
		}
		if spec.Pairs, err = edit.ParsePairs(v["pairs"]); err != nil {
			return err
		}
		km, err := parseNum(v, "km", "length")
		if err != nil {
			return err
		}
		spec.LengthM = km * 1000
		if spec.Blocks, err = parseInt(v, "blocks", "blocks"); err != nil {
			return err
		}
		var ends []string
		if !w.do("laid a junction", func(s *infra.Snapshot) (err error) { ends, err = edit.Junction(s, spec); return }) {
			return errors.New(w.m.note)
		}
		w.selectPoint(ends[0])
		return nil
	},
		choice("mains", "the line", all, all, "the selected track only"),
		choice("side", "branch", "below", "below", "above"),
		choice("leaves", "leaves going", "east", "east", "west"),
		field{key: "lanes", label: "lanes", value: "1", help: "how many lanes beyond the line the branch's nearest line runs"},
		text("code", "junction code", "JCT"),
		field{key: "pattern", label: "branch lines", value: edit.FormatPattern(ways), help: "E, W or B a line, top to bottom"},
		field{key: "pairs", label: "leave mains", value: "", help: "the main (1 the top) each branch line leaves; empty: by the way they run"},
		field{key: "name", label: "branch name", value: "", help: "empty: made from the junction code"},
		number("km", "first section (km)", 5),
		number("blocks", "blocks", 1),
	))
}

func (w *worldTab) attrsForm() {
	t, ok := w.track()
	if !ok {
		return
	}
	s := w.m.s.world.S
	i := slices.IndexFunc(s.Tracks, func(tr infra.SnapshotTrack) bool { return tr.ID == t })
	tr := s.Tracks[i]
	w.m.open(newForm("track "+cmp.Or(tr.Name, tr.ID), func(v values) error {
		a := edit.TrackAttrs{Name: v["name"], Direction: tr.Direction}
		var err error
		if a.LengthM, err = parseNum(v, "m", "length"); err != nil {
			return err
		}
		if a.MaxKmph, err = parseNum(v, "kmph", "speed"); err != nil {
			return err
		}
		if a.Lanes, err = parseInt(v, "lanes", "lanes"); err != nil {
			return err
		}
		if !w.do("set the track", func(s *infra.Snapshot) error { return edit.SetTrack(s, t, a) }) {
			return errors.New(w.m.note)
		}
		return nil
	},
		text("name", "name", tr.Name),
		number("m", "length (m)", tr.LengthM),
		number("kmph", "speed (km/h)", tr.MaxKmph),
		field{key: "lanes", label: "lanes", value: strconv.Itoa(tr.Lanes), help: "how many lanes it moves going east (down is positive)"},
	))
}

func (w *worldTab) defaultsForm() {
	d := w.m.s.world.S.Defaults
	w.m.open(newForm("world defaults", func(v values) error {
		nd := infra.SnapshotDefaults{Up: map[string]string{"west": "W", "east": "E"}[v["up"]]}
		var err error
		for _, f := range []struct {
			key string
			to  *float64
		}{{"sw", &nd.SwitchKmph}, {"swm", &nd.SwitchLengthM}, {"pf", &nd.PlatformKmph}, {"pfm", &nd.PlatformLengthM}, {"route", &nd.RouteKmph}} {
			if *f.to, err = parseNum(v, f.key, f.key); err != nil {
				return err
			}
		}
		if !w.do("set the defaults", func(s *infra.Snapshot) error { return edit.SetDefaults(s, nd) }) {
			return errors.New(w.m.note)
		}
		return nil
	},
		number("sw", "switch speed (km/h)", d.SwitchKmph),
		number("swm", "switch length (m)", d.SwitchLengthM),
		number("pf", "platform speed (km/h)", d.PlatformKmph),
		number("pfm", "platform length (m)", d.PlatformLengthM),
		number("route", "line speed (km/h)", d.RouteKmph),
		field{key: "up", label: "UP runs", value: upRuns(d.Up), choices: []string{"west", "east"}, help: "labels one-way lines UP or DOWN; the sim goes by their signals"},
	))
}

// mouse: click selects, drag pans, drag from an open point draws track, the
// wheel zooms.
func (w *worldTab) mouse(msg tea.MouseMsg) {
	m := msg.Mouse()
	sx, sy := m.X-1, m.Y-worldPaneTop
	inPane := sx >= 0 && sy >= 0 && sx < w.cam.W && sy < w.cam.H
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if !inPane {
			return
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			w.cam.ZoomTo(w.cam.Step+1, sx, sy)
		case tea.MouseWheelDown:
			w.cam.ZoomTo(w.cam.Step-1, sx, sy)
		}
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || !inPane {
			return
		}
		fx, fy := w.cam.FrameCell(sx, sy)
		w.drag = dragState{on: true, x: m.X, y: m.Y}
		if p, ok := w.cam.Frame.At(fx, fy); ok {
			w.sel = p
			if p.Kind == schematic.PiecePoint && (openSide(w.m.s.world.S, p.ID, true) || openSide(w.m.s.world.S, p.ID, false)) {
				w.drag.from, w.drag.fx, w.drag.fy = p.ID, fx, fy
			}
			w.cam.Redraw()
		}
	case tea.MouseMotionMsg:
		if w.drag.on && w.drag.from == "" {
			w.cam.Pan(w.drag.x-m.X, w.drag.y-m.Y)
			w.drag.x, w.drag.y = m.X, m.Y
		}
	case tea.MouseReleaseMsg:
		d := w.drag
		w.drag = dragState{}
		if !d.on || d.from == "" {
			return
		}
		fx, fy := w.cam.FrameCell(sx, sy)
		dx := fx - d.fx
		if abs(dx) < 2 {
			return // a click, not a drag
		}
		dir := "east"
		if dx < 0 {
			dir = "west"
		}
		lanes := int(math.Round(float64(fy-d.fy) / 2))
		w.extendFormAt(d.from, dir, lanes)
	}
}

func (w *worldTab) render(width, h int) []string {
	paneH := max(h-bottomHeight, 3)
	cam := w.cam
	cam.W, cam.H = max(width-2, 1), max(paneH-2, 1)
	z := tui.ZoomSteps[cam.Step]
	title := fmt.Sprintf("world · %s · %s ×%d", worldLabel(w.m.s), z.Level, z.Zoom)
	if w.m.s.world.Dirty {
		title += " · unsaved"
	}
	body := cam.Lines(true)
	if len(w.m.s.world.S.Points) == 0 {
		body = []string{"", tui.Dim.Render("  an empty world: t stamps a station (a halt, a crossing station or one of your own tracks),"),
			tui.Dim.Render("  or e starts a line you draw track by track")}
	}
	if w.drawErr != nil {
		body = append([]string{tui.Failed.Render(" cannot draw: " + w.drawErr.Error())}, body...)
	}
	out := tui.Box(title, true, body, width, paneH)

	iw := width / 2
	insp := []string{}
	for _, l := range edit.Describe(w.m.s.world.S, w.sel) {
		insp = append(insp, " "+l)
	}
	if len(insp) == 0 {
		insp = []string{tui.Dim.Render(" nothing selected: the arrows move between pieces, tab through a cell's")}
	}
	if w.pending != nil {
		insp = append(insp, tui.Accent.Render(" "+w.pendingEdit+" from "+w.pending.ID+": pick the other"))
	}
	if w.sel.ID != "" {
		insp = append(insp, "", tui.Dim.Render(" "+w.selKeys()))
	}
	inspTitle := "inspector"
	probs := []string{}
	for i, p := range w.problems {
		if i == bottomHeight-3 && len(w.problems) > bottomHeight-2 {
			probs = append(probs, tui.Dim.Render(fmt.Sprintf(" … and %d more", len(w.problems)-i)))
			break
		}
		probs = append(probs, tui.Amber.Render(" ⚠ ")+ansi.Truncate(p.Text, width-iw-6, "…"))
	}
	probTitle := fmt.Sprintf("problems (%d)", len(w.problems))
	if len(w.problems) == 0 {
		probs = []string{tui.Good.Render(" none: the world can be run")}
		probTitle = "problems"
	}
	bottom := tui.Side([]int{iw, width - iw},
		tui.Box(inspTitle, false, insp, iw, bottomHeight),
		tui.Box(probTitle+" · [ ] go to", false, probs, width-iw, bottomHeight))
	return append(out, bottom...)
}

// selKeys are the edits for the selected piece.
func (w *worldTab) selKeys() string {
	switch w.sel.Kind {
	case schematic.PiecePoint:
		return "e extend  s switch  j join  o end  g/G signal east/west  b line  t stamp here  d delete"
	case schematic.PieceTrack:
		return "c split  a attributes  p platform  x crossover  X diamond/slip  Y junction  g/G signal east/west  d delete"
	case schematic.PieceSignal:
		return "g take it away  d delete"
	case schematic.PieceStation:
		return "n rename  d delete (its tracks stay)"
	}
	return ""
}

func (w *worldTab) keyLine() string {
	return " ←→↑↓ pick  tab cycle  t build station  e extend  n station  D defaults  u undo  ctrl+r redo  [ ] problems  +/- zoom  0 fit  shift+←→↑↓ pan  / station"
}

func worldLabel(s *session) string {
	switch {
	case s.sc.World == "":
		return "new world"
	case s.builtin:
		return s.sc.World + " (built in; saved as a snapshot)"
	}
	return s.sc.World
}

// helpers on the snapshot

func openSide(s *infra.Snapshot, p string, east bool) bool {
	w, e := countSides(s, p)
	if east {
		return e == 0
	}
	return w == 0
}

// countSides counts a point's tracks west and east of it.
func countSides(s *infra.Snapshot, p string) (west, east int) {
	for _, t := range s.Tracks {
		if t.To == p {
			west++
		}
		if t.From == p {
			east++
		}
	}
	return west, east
}

func endOf(s *infra.Snapshot, p string) string {
	for _, pt := range s.Points {
		if pt.ID == p {
			switch {
			case pt.Boundary:
				return edit.EndBoundary
			case pt.DeadEnd:
				return edit.EndBuffer
			}
		}
	}
	return edit.EndOpen
}

func trackLength(s *infra.Snapshot, t string) float64 {
	for _, tr := range s.Tracks {
		if tr.ID == t {
			return tr.LengthM
		}
	}
	return 0
}

func codeOf(s *infra.Snapshot, p string) string {
	for _, pt := range s.Points {
		if pt.ID == p {
			return pt.Stn
		}
	}
	return ""
}

func titled(code string) string {
	if code == "" {
		return "Ln"
	}
	l := strings.ToLower(code)
	return strings.ToUpper(l[:1]) + l[1:]
}

func abs(n int) int { return max(n, -n) }

// nextPlatform is the number after the highest of a station's platforms.
func nextPlatform(taken []string) string {
	next := 1
	for _, no := range taken {
		if n, err := strconv.Atoi(no); err == nil {
			next = max(next, n+1)
		}
	}
	return strconv.Itoa(next)
}

// upRuns is the way the world's UP runs, as the defaults form shows it.
func upRuns(up string) string {
	if up == "E" {
		return "east"
	}
	return "west"
}
