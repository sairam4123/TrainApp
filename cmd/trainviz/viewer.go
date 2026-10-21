package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"trainapp/visualization/schematic"
	"trainapp/visualization/traingraph"
	"trainapp/visualization/tui"
	"trainapp/visualization/vizproto"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// zoomSteps run from the overview to the most detailed drawing.
var zoomSteps = tui.ZoomSteps

// graphScales are the time-distance graph's minutes per column.
var graphScales = []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20}

// speeds are sim minutes per real minute.
var speeds = []float64{1, 10, 60, 600}

const tickEvery = 50 * time.Millisecond

// followEase is how quickly the view glides after a followed train: it
// closes about two thirds of the way in this time.
const followEase = 0.15 // seconds

var (
	dim  = lipgloss.NewStyle().Faint(true)
	bold = lipgloss.NewStyle().Bold(true)
	held = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // amber, as on the schematic
)

type tickMsg time.Time

// page is what the viewer shows.
type page int

const (
	pageSchematic page = iota
	pageGraph          // the time-distance graph
	pageDelay          // the run's delays, from its KPIs
	pageOccupancy      // how busy each track was, from its KPIs
)

// pageNames name the pages for -page.
var pageNames = map[string]page{
	"schematic": pageSchematic,
	"graph":     pageGraph,
	"delay":     pageDelay,
	"occupancy": pageOccupancy,
}

// bottomPane is what the pane below the schematic shows.
type bottomPane int

const (
	paneEvents    bottomPane = iota
	paneDecisions            // precedence decisions
	paneSchedule             // the selected train's schedule
)

type model struct {
	d   *schematic.Diagram
	rep *replay // nil when only showing the layout

	step   int // index into zoomSteps
	frame  *schematic.Frame
	x, y   int // frame cell at the top left of the schematic pane
	width  int
	height int
	fitted bool

	dragging     bool
	dragX, dragY int
	searching    bool
	query        string
	note         string

	playing  bool
	ticking  bool
	lastTick time.Time
	speed    int
	selected int // index into rep.trains, -1 for none
	follow   bool

	// gliding after the followed train: the view's position, in fractions
	// of a cell, eases towards (toX, toY), where the train is centred
	gliding    bool
	camX, camY float64
	toX, toY   int
	lastGlide  time.Time
	bottom     bottomPane

	page page

	// the time-distance graph, shown in place of the schematic
	graph   *traingraph.Graph // nil without a recording or a single line
	noGraph string            // why there is no graph
	gFrom   float64           // time at the graph's left edge
	gPerCol float64           // minutes per column
	gFitted bool
	planned bool // mark planned times on the graph

	st *stats // the run's KPIs, for the delay and occupancy pages

	name string // the recording's, on the tab bar

	interrupted bool // quit with ctrl+c rather than q
}

func newModel(d *schematic.Diagram, rep *replay) *model {
	m := &model{d: d, rep: rep, step: 1, speed: 2, selected: -1, noGraph: "the graph needs a recording (trainviz replay)",
		st: newStats("no KPIs for this recording: run trainapp with -kpi")}
	if rep == nil {
		m.st.missing = "the KPI pages need a recording (trainviz replay)"
	}
	if rep != nil {
		if cor, err := d.Corridor(); err != nil {
			m.noGraph = "no time-distance graph: " + strings.TrimPrefix(err.Error(), "schematic: ")
		} else {
			m.graph = traingraph.Build(rep.world, cor, rep.msgs)
		}
	}
	m.redraw()
	return m
}

func (m *model) Init() tea.Cmd { return nil }

// layout of the screen: the schematic pane and, for a replay, the trains
// pane beside it and the events pane, timeline and key line below. The
// KPI pages' tables take the screen but for the key line; their schematics
// have their keys beside them.
func (m *model) sideWidth() int {
	switch {
	case m.rep == nil || m.width < 100 || m.fullPage():
		return 0
	case m.kpiMap():
		return 38
	}
	return 34
}

func (m *model) eventsHeight() int {
	if m.rep == nil || m.height < 20 || m.kpiPage() {
		return 0
	}
	return 7
}

func (m *model) bottomLines() int {
	if m.rep == nil || m.kpiPage() {
		return 1
	}
	return 2
}

// kpiPage is whether a page of the run's KPIs shows.
func (m *model) kpiPage() bool { return m.page == pageDelay || m.page == pageOccupancy }

// fullPage is whether the page is text across the whole screen rather than
// a drawing.
func (m *model) fullPage() bool {
	return m.kpiPage() && !m.kpiMap()
}

// kpiMap is whether a KPI page shows its schematic: the occupancy page
// unless it shows its tables, the delay page if it shows its schematic.
func (m *model) kpiMap() bool {
	switch {
	case m.st.rep == nil:
		return false
	case m.page == pageOccupancy:
		return !m.st.table && !m.st.oCauses
	case m.page == pageDelay:
		return m.st.dMap
	}
	return false
}

// pane is the size of the schematic inside its border.
func (m *model) pane() (w, h int) {
	return max(m.width-m.sideWidth()-2, 1), max(m.height-m.headerLines()-m.eventsHeight()-m.bottomLines()-2, 1)
}

// headerLines is the tab bar's: a replay has the pages to choose from.
func (m *model) headerLines() int {
	if m.rep == nil {
		return 0
	}
	return 1
}

func (m *model) selectedTrain() string {
	if m.rep == nil || m.selected < 0 {
		return ""
	}
	return m.rep.trains[m.selected]
}

func (m *model) options() schematic.Options {
	s := zoomSteps[m.step]
	opt := schematic.Options{Level: s.Level, Zoom: s.Zoom, Selected: m.selectedTrain()}
	switch {
	case m.page == pageOccupancy:
		opt.Selected, opt.Heat = "", m.st.heat()
		if m.st.focus != "" {
			opt.Marks = map[string]schematic.Mark{m.st.focus: schematic.MarkSelected}
		}
	case m.page == pageDelay:
		opt.Selected = ""
		opt.Heat, opt.StationNotes = m.st.delayHeat()
	case m.rep != nil:
		opt.State, opt.Time = m.rep.state, m.rep.t
	}
	return opt
}

func (m *model) redraw() {
	m.frame = m.d.Draw(m.options())
	if m.follow {
		if x, y, ok := m.frame.Train(m.selectedTrain()); ok {
			m.followTo(x, y)
		}
	}
	m.clamp()
}

// followTo points the view at the followed train, at frame cell (x, y):
// straight there if the train is off the pane, as after a seek or a zoom,
// else gliding, a tick at a time (see glide).
func (m *model) followTo(x, y int) {
	pw, ph := m.pane()
	m.toX, m.toY = m.clamped(x-pw/2, y-ph/2)
	if x < m.x || x >= m.x+pw || y < m.y || y >= m.y+ph {
		m.x, m.y, m.gliding = m.toX, m.toY, false
		return
	}
	if !m.gliding && (m.toX != m.x || m.toY != m.y) {
		m.gliding, m.camX, m.camY, m.lastGlide = true, float64(m.x), float64(m.y), time.Now()
	}
}

// glide eases the view towards the followed train for the time since the
// last glide, and reports whether it has further to go.
func (m *model) glide(now time.Time) bool {
	if !m.gliding || !m.follow {
		m.gliding = false
		return false
	}
	a := 1 - math.Exp(-now.Sub(m.lastGlide).Seconds()/followEase)
	m.lastGlide = now
	m.camX += (float64(m.toX) - m.camX) * a
	m.camY += (float64(m.toY) - m.camY) * a
	if math.Abs(float64(m.toX)-m.camX) < 0.5 && math.Abs(float64(m.toY)-m.camY) < 0.5 {
		m.camX, m.camY, m.gliding = float64(m.toX), float64(m.toY), false
	}
	m.x, m.y = int(math.Round(m.camX)), int(math.Round(m.camY))
	return m.gliding
}

// clamp keeps the frame on screen, centred if it is smaller than the pane.
func (m *model) clamp() { m.x, m.y = m.clamped(m.x, m.y) }

// clamped is where the view at (x, y) is kept to.
func (m *model) clamped(x, y int) (int, int) {
	pw, ph := m.pane()
	fw, fh := m.frame.Width(), m.frame.Height()
	if fw <= pw {
		x = (fw - pw) / 2
	} else {
		x = min(max(x, 0), fw-pw)
	}
	if fh <= ph {
		y = (fh - ph) / 2
	} else {
		y = min(max(y, 0), fh-ph)
	}
	return x, y
}

// zoomTo changes the zoom step, keeping the place at (sx, sy) in the pane
// where it is.
func (m *model) zoomTo(step, sx, sy int) {
	step = min(max(step, 0), len(zoomSteps)-1)
	if step == m.step {
		return
	}
	a := m.frame.AnchorAt(m.x + sx)
	fy := float64(m.y+sy) / float64(max(m.frame.Height(), 1))
	m.step = step
	m.redraw()
	m.x = m.frame.Column(a) - sx
	m.y = int(fy*float64(m.frame.Height())) - sy
	m.clamp()
}

// fit picks the widest layout step that fits the pane, or the overview.
func (m *model) fit() {
	pw, ph := m.pane()
	m.step = 0
	for i := len(zoomSteps) - 1; i > 0; i-- {
		if zoomSteps[i].Level != schematic.Layout {
			continue
		}
		m.step = i
		m.redraw()
		if m.frame.Width() <= pw && m.frame.Height() <= ph {
			break
		}
		m.step = 0
	}
	m.redraw()
}

func (m *model) centreOn(x, y int) {
	pw, ph := m.pane()
	m.x, m.y = x-pw/2, y-ph/2
	m.clamp()
}

func (m *model) tick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.fitted {
			m.fitted = true
			m.fit()
		}
		if m.page == pageGraph {
			m.fitGraph()
		}
		m.redraw()

	case tickMsg:
		m.ticking = false
		now := time.Time(msg)
		if m.playing && m.rep != nil {
			dt := now.Sub(m.lastTick).Minutes() * speeds[m.speed]
			m.lastTick = now
			if !m.rep.advance(dt) {
				m.playing = false
			}
			m.keepCursor()
			m.redraw()
		}
		if m.glide(now) || m.playing {
			return m, m.tick()
		}

	case tea.KeyPressMsg:
		if m.searching {
			m.searchKey(msg)
			return m, nil
		}
		return m, m.key(msg)

	case tea.MouseWheelMsg:
		sx, sy := msg.X-1, msg.Y-1-m.headerLines()
		if m.fullPage() {
			m.scrollPage(map[bool]int{true: -3, false: 3}[msg.Button == tea.MouseWheelUp])
			break
		}
		if m.page == pageGraph {
			m.zoomGraph(msg.Button == tea.MouseWheelUp, sx)
			break
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			m.zoomTo(m.step+1, sx, sy)
		case tea.MouseWheelDown:
			m.zoomTo(m.step-1, sx, sy)
		}

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			break
		}
		if msg.Y == 0 && m.headerLines() == 1 {
			if p, ok := m.tabAt(msg.X); ok {
				m.setPage(p)
			}
			break
		}
		if m.rep != nil && m.bottomLines() == 2 && msg.Y == m.height-2 {
			m.seekTimeline(msg.X)
			break
		}
		if m.fullPage() {
			break
		}
		if pw, ph := m.pane(); m.page == pageGraph && msg.X >= 1 && msg.X <= pw && msg.Y-m.headerLines() >= 1 && msg.Y-m.headerLines() <= ph {
			m.seekGraph(msg.X - 1)
		}
		m.dragging, m.dragX, m.dragY = true, msg.X, msg.Y

	case tea.MouseMotionMsg:
		if m.dragging && m.page == pageGraph {
			m.gFrom -= float64(msg.X-m.dragX) * m.gPerCol
			m.dragX, m.dragY = msg.X, msg.Y
		} else if m.dragging {
			m.x -= msg.X - m.dragX
			m.y -= msg.Y - m.dragY
			m.dragX, m.dragY = msg.X, msg.Y
			m.clamp()
		}

	case tea.MouseReleaseMsg:
		m.dragging = false
	}
	return m, nil
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	m.note = ""
	pw, ph := m.pane()
	switch msg.String() {
	case "q":
		return tea.Quit
	case "ctrl+c":
		m.interrupted = true
		return tea.Quit
	case "g":
		m.toggleGraph()
		return nil
	case "1", "f1":
		m.setPage(pageSchematic)
		return nil
	case "2", "f2":
		m.setPage(pageGraph)
		return nil
	case "3", "f3":
		m.setPage(pageDelay)
		return nil
	case "4", "f4":
		m.setPage(pageOccupancy)
		return nil
	}
	if m.page == pageGraph && m.graphKey(msg.String()) {
		return nil
	}
	if m.kpiPage() {
		if m.kpiKey(msg.String()) || m.fullPage() {
			return nil
		}
	}
	switch msg.String() {
	case "+", "=":
		m.zoomTo(m.step+1, pw/2, ph/2)
	case "-", "_":
		m.zoomTo(m.step-1, pw/2, ph/2)
	case "0":
		m.fit()
	case "h", "left":
		m.x -= 8
	case "l", "right":
		m.x += 8
	case "k", "up":
		m.y -= 2
	case "j", "down":
		m.y += 2
	case "/":
		m.searching, m.query = true, ""
	}
	if m.rep != nil && !m.kpiPage() {
		m.replayKey(msg.String())
		if m.playing || m.gliding {
			return m.tick()
		}
	}
	m.clamp()
	return nil
}

func (m *model) replayKey(key string) {
	r := m.rep
	was := r.t
	switch key {
	case "space":
		m.playing = !m.playing
		if m.playing && r.t >= r.end {
			r.reset()
		}
		m.lastTick = time.Now()
	case "[":
		m.speed = max(m.speed-1, 0)
	case "]":
		m.speed = min(m.speed+1, len(speeds)-1)
	case ".":
		m.playing = false
		r.step(true)
	case ",":
		m.playing = false
		r.step(false)
	case "shift+right", "shift+left":
		if train := m.selectedTrain(); train != "" {
			m.playing = false
			r.jumpTrain(train, key == "shift+right")
		} else {
			m.note = "select a train with tab first"
		}
	case "tab":
		m.selected = (m.selected+2)%(len(r.trains)+1) - 1
	case "shift+tab":
		m.selected = (m.selected+len(r.trains)+1)%(len(r.trains)+1) - 1
	case "esc":
		m.selected, m.follow = -1, false
	case "f":
		m.follow = !m.follow && m.selected >= 0
		if m.selected < 0 {
			m.note = "select a train with tab first"
		}
	case "d":
		m.bottom = toggle(m.bottom, paneDecisions)
	case "s":
		m.bottom = toggle(m.bottom, paneSchedule)
	case "enter":
		if m.bottom != paneDecisions {
			return
		}
		m.playing = false
		d, ok := r.nextDecision(r.t)
		if !ok {
			m.note = "no more decisions"
			return
		}
		r.seek(d.Since)
		if i := slices.Index(r.trains, d.Train); i >= 0 {
			m.selected = i
		}
	case "home":
		m.playing = false
		r.reset()
	case "end":
		m.playing = false
		r.seek(r.end)
	default:
		return
	}
	if r.t != was {
		m.keepCursor()
	}
	m.redraw()
}

func (m *model) searchKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.searching = false
	case "enter":
		m.searching = false
		if x, y, ok := m.frame.Station(m.query); ok {
			m.centreOn(x, y)
		} else {
			m.note = "no station " + strings.ToUpper(m.query)
		}
	case "backspace":
		if q := []rune(m.query); len(q) > 0 {
			m.query = string(q[:len(q)-1])
		}
	default:
		if msg.Text != "" {
			m.query += strings.ToUpper(msg.Text)
		}
	}
}

// timelineBar is where the bar sits on the timeline line.
func (m *model) timelineBar() (start, width int) {
	return 26, max(m.width-26-9, 10)
}

func (m *model) seekTimeline(x int) {
	start, w := m.timelineBar()
	if x < start || x >= start+w {
		return
	}
	m.playing = false
	m.rep.seek(float64(x-start) / float64(w-1) * m.rep.end)
	m.keepCursor()
	m.redraw()
}

func (m *model) toggleGraph() {
	if m.page == pageGraph {
		m.setPage(pageSchematic)
		return
	}
	m.setPage(pageGraph)
}

// setPage shows page p. Playback stops on the KPI pages, which are of the
// whole run.
func (m *model) setPage(p page) {
	if p == pageGraph && m.graph == nil {
		m.note = m.noGraph
		return
	}
	m.page = p
	switch p {
	case pageGraph:
		if !m.gFitted {
			m.fitGraph()
		}
	case pageDelay, pageOccupancy:
		m.playing = false
	}
	m.redraw()
}

// graphKey handles the keys that work differently on the graph, and reports
// whether it did.
func (m *model) graphKey(key string) bool {
	pw, _ := m.pane()
	switch key {
	case "+", "=":
		m.zoomGraph(true, pw/2)
	case "-", "_":
		m.zoomGraph(false, pw/2)
	case "0":
		m.fitGraph()
	case "h", "left":
		m.gFrom -= 8 * m.gPerCol
	case "l", "right":
		m.gFrom += 8 * m.gPerCol
	case "p":
		m.planned = !m.planned
	case "k", "up", "j", "down":
	case "/":
		m.note = "station search is on the schematic (g)"
	default:
		return false
	}
	return true
}

// graphOptions draws the graph to fill the pane.
func (m *model) graphOptions() traingraph.Options {
	pw, ph := m.pane()
	return traingraph.Options{
		Width: pw, Height: ph,
		From: m.gFrom, To: m.gFrom + m.gPerCol*float64(max(pw-m.graph.Gutter()-1, 1)),
		Now: m.rep.t, Selected: m.selectedTrain(), Planned: m.planned, Color: true,
	}
}

// fitGraph shows the whole run.
func (m *model) fitGraph() {
	if m.graph == nil || m.width == 0 {
		return
	}
	pw, _ := m.pane()
	start, end := m.graph.Span()
	m.gFrom = start
	m.gPerCol = max((end-start)/float64(max(pw-m.graph.Gutter()-1, 1)), graphScales[0])
	m.gFitted = true
}

// zoomGraph moves to the next time scale, keeping the time at pane column
// col where it is.
func (m *model) zoomGraph(in bool, col int) {
	at, ok := m.graph.TimeAt(m.graphOptions(), col)
	if !ok {
		at = m.gFrom
	}
	next := m.gPerCol
	if in {
		for i := len(graphScales) - 1; i >= 0; i-- {
			if graphScales[i] < m.gPerCol-1e-9 {
				next = graphScales[i]
				break
			}
		}
	} else {
		for _, s := range graphScales {
			if s > m.gPerCol+1e-9 {
				next = s
				break
			}
		}
	}
	m.gFrom = at - (at-m.gFrom)*next/m.gPerCol
	m.gPerCol = next
}

// seekGraph moves playback to the time at pane column col of the graph.
func (m *model) seekGraph(col int) {
	if t, ok := m.graph.TimeAt(m.graphOptions(), col); ok {
		m.playing = false
		m.rep.seek(t)
		m.redraw()
	}
}

// keepCursor scrolls the graph to keep the replay time in view, but not to
// before the run starts.
func (m *model) keepCursor() {
	if m.page != pageGraph || m.graph == nil {
		return
	}
	o := m.graphOptions()
	if t := m.rep.t; t < o.From || t > o.To {
		start, _ := m.graph.Span()
		m.gFrom = max(t-(o.To-o.From)/4, start)
	}
}

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *model) render() string {
	if m.width == 0 {
		return ""
	}
	var head []string
	if m.headerLines() == 1 {
		head = []string{m.header()}
	}
	if m.fullPage() {
		return strings.Join(append(append(head, m.pageBody()...), m.keyLine()), "\n")
	}
	pw, ph := m.pane()
	s := zoomSteps[m.step]
	title := fmt.Sprintf("schematic · %s, zoom %d", s.Level, s.Zoom)
	body := m.frame.View(m.x, m.y, pw, ph, true)
	switch m.page {
	case pageOccupancy:
		title = fmt.Sprintf("occupancy · %s · %s, zoom %d", metrics[m.st.metric].name, s.Level, s.Zoom)
	case pageDelay:
		title = fmt.Sprintf("delay · stations by %s · blocks by delay gained · %s, zoom %d", delayMetrics[m.st.dMetric].name, s.Level, s.Zoom)
	}
	if m.page == pageGraph {
		title = fmt.Sprintf("time-distance graph · %.3g min a column", m.gPerCol)
		if m.planned {
			title += " · ◆ planned"
		}
		body = m.graph.Draw(m.graphOptions())
	}
	lines := box(title, body, pw+2, ph+2)
	if sw := m.sideWidth(); sw > 0 && m.kpiMap() {
		side := m.st.sideLines(sw, ph+2)
		if m.page == pageDelay {
			side = m.st.delaySide(sw, ph+2)
		}
		for i := range lines {
			lines[i] += side[i]
		}
	} else if sw > 0 {
		side := box("trains", m.trainLines(sw-2, ph), sw, ph+2)
		for i := range lines {
			lines[i] += side[i]
		}
	}
	if eh := m.eventsHeight(); eh > 0 {
		title, body := "events", m.eventLines(eh-2)
		switch {
		case m.bottom == paneDecisions:
			title, body = "decisions", m.decisionLines(eh-2)
		case m.bottom == paneSchedule:
			title, body = "schedule", m.scheduleLines(m.width-2, eh-2)
			if t := m.selectedTrain(); t != "" {
				title += " · " + t
			}
		default:
			if t := m.selectedTrain(); t != "" {
				title += " · " + t
			}
		}
		lines = append(lines, box(title, body, m.width, eh)...)
	}
	if m.rep != nil && m.bottomLines() == 2 {
		lines = append(lines, m.timeline())
	}
	lines = append(lines, m.keyLine())
	return strings.Join(append(head, lines...), "\n")
}

func (m *model) trainLines(w, h int) []string {
	r := m.rep
	var out []string
	at := 0
	for i, n := range r.trains {
		if i == m.selected {
			at = len(out)
		}
		tr, ok := r.state.Trains[n]
		text := "  " + tr.Phase
		switch {
		case !ok:
			text = ""
		case tr.Waiting != nil:
			text = fmt.Sprintf("  waiting %s %s", tr.Waiting.Station, tr.Waiting.Pf)
		case tr.Track != "":
			if v, ok := tr.Speed(r.t); ok && tr.Phase == "running" {
				// the speed in a column of its own, ahead of the track's
				// name, so a narrow pane cuts the name, not the speed
				text = fmt.Sprintf(" %4.0f km/h", v)
			}
			text += " " + r.trackName[tr.Track]
		}
		// the number in its class's colour, the rest by what the train does
		style, number := lipgloss.NewStyle(), r.classStyle(n)
		switch {
		case i == m.selected:
			style, number = bold.Reverse(true), number.Reverse(true)
		case tr.Phase == "exited" || tr.Phase == "outside":
			style = dim
		}
		if ok && tr.Waiting.Held() && i != m.selected {
			style = held
		}
		rest := max(w-ansi.StringWidth(n), 0)
		out = append(out, fit(number.Render(n)+style.Render(fit(text, rest)), w))
		for _, d := range r.state.ActiveDecisions(n) {
			out = append(out, subLines(decisionSummary(d), w)...)
		}
		if ok && tr.Waiting != nil && tr.Waiting.BlockedBy != "" && !tr.Waiting.Held() {
			out = append(out, subLines("blocked by "+tr.Waiting.BlockedBy, w)...)
		}
	}
	// a key to the class colours, highest priority first, if there is room
	if keys := r.classes; len(keys) > 0 && len(out)+1+len(keys) <= h {
		out = append(out, "")
		for _, k := range slices.Backward(keys) {
			line := k.name
			if k.priority != 0 {
				line += fmt.Sprintf(" (%d)", k.priority)
			}
			mark := lipgloss.NewStyle().Foreground(lipgloss.Color(k.color)).Render("■ ")
			out = append(out, mark+dim.Render(fit(line, max(w-2, 0))))
		}
	}
	if at >= h {
		out = out[at-h+1:]
	}
	return out
}

// subLines are text under a train in the trains pane, w wide: indented,
// dim, and wrapped onto a second line rather than cut.
func subLines(text string, w int) []string {
	const indent = "   "
	lines := tui.Wrap(text, max(w-len(indent), 1), 2)
	for i, l := range lines {
		lines[i] = dim.Render(fit(indent+l, w))
	}
	return lines
}

// decisionSummary says what a decision does, e.g. "held at PDKT for 0451U
// (overtake, +6.2m)" or "pf 3→4 at PDKT (main_line_stay)".
func decisionSummary(d vizproto.DecisionMsg) string {
	if d.Action == "platform" {
		text := fmt.Sprintf("pf %s→%s at %s (%s", d.PfFrom, d.PfTo, d.At, d.Reason)
		if d.For != "" {
			text += " for " + d.For
		}
		return text + ")"
	}
	text := fmt.Sprintf("held at %s for %s (%s", d.At, d.For, d.Kind)
	if d.Gain > 0 {
		text += fmt.Sprintf(", +%.1fm", d.Gain)
	}
	if d.Source == "manual" {
		text += ", manual"
	}
	return text + ")"
}

// decisionLines lists the precedence decisions as they stand now.
func (m *model) decisionLines(n int) []string {
	var out []string
	for _, d := range m.rep.decisions() {
		if len(out) == n {
			break
		}
		line := fmt.Sprintf("t=%.2f %-10s %s %s", d.Since, d.Status, d.Train, decisionSummary(d))
		if d.Reason != "" && d.Action == "hold" {
			line += "  " + d.Reason
		}
		if d.Active() {
			line = held.Render(line)
		} else {
			line = dim.Render(line)
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		out = append(out, dim.Render("no precedence decisions yet"))
	}
	return out
}

// toggle shows pane in the bottom pane, or the events if it already shows.
func toggle(cur, pane bottomPane) bottomPane {
	if cur == pane {
		return paneEvents
	}
	return pane
}

// scheduleLines is the selected train's schedule as a table, a column per
// station: whether it stops or passes, and on which platform and when it
// arrived and left. Times still to come are the planned ones, dimmed, or "—"
// where the schedule gives none. The station the train is at or heading for
// is picked out, and kept in view when the columns do not all fit in w.
func (m *model) scheduleLines(w, n int) []string {
	train := m.selectedTrain()
	if train == "" {
		return []string{dim.Render("select a train with tab to see its schedule")}
	}
	var points []vizproto.SchedulePoint
	for _, t := range m.rep.world.Trains {
		if t.Number == train {
			points = t.Schedule
		}
	}
	if len(points) == 0 {
		return []string{dim.Render("no schedule in this recording")}
	}
	calls := m.rep.calls(train, len(points))
	next := len(points) // the first station not left yet
	for i, c := range calls {
		if c.dep < 0 {
			next = i
			break
		}
	}
	at := func(actual, planned float64) string {
		switch {
		case actual >= 0:
			return clock(actual)
		case planned >= 0:
			return dim.Render(clock(planned))
		}
		return dim.Render("—")
	}
	const label, col = 6, 7
	rows := [][]string{{""}, {"kind"}, {"pf"}, {"arr"}, {"dep"}}
	for i, sp := range points {
		stn := fit(sp.Stn, col)
		switch {
		case i == next:
			stn = bold.Reverse(true).Render(fit(sp.Stn, col-1)) + " "
		case i < next:
			stn = dim.Render(stn)
		}
		pf := dim.Render(cmp.Or(sp.Pf, "—"))
		if c := calls[i]; c.pf != "" {
			pf = c.pf
		}
		rows[0] = append(rows[0], stn)
		kind := "stop" // timed or not
		if sp.Kind == "pass" {
			kind = "pass"
		}
		rows[1] = append(rows[1], dim.Render(kind))
		rows[2] = append(rows[2], pf)
		rows[3] = append(rows[3], at(calls[i].arr, sp.Arr))
		rows[4] = append(rows[4], at(calls[i].dep, sp.Dep))
	}
	// drop columns from the left until the next station fits
	first := 0
	if fits := max((w-label)/col, 1); next >= fits {
		first = min(next, len(points)-1) - fits + 1
	}
	var out []string
	for _, row := range rows[:min(len(rows), n)] {
		line := fit(" "+row[0], label)
		for _, cell := range row[1+first:] {
			line += fit(cell, col)
		}
		out = append(out, line)
	}
	return out
}

// clock shows minutes from 0 as HH:MM, as the timetable does.
func clock(t float64) string { return tui.Clock(t) }

func (m *model) eventLines(n int) []string {
	var out []string
	for _, e := range m.rep.recent(n, m.selectedTrain()) {
		parts := []string{fmt.Sprintf("t=%.2f", e.T), e.Event, e.Train}
		for _, f := range e.Fields {
			if f.Key != "name" {
				parts = append(parts, fmt.Sprintf("%s=%v", f.Key, f.Value))
			}
		}
		line := strings.Join(parts, " ")
		if e.Level != "INFO" {
			line = bold.Render(line)
		}
		out = append(out, line)
	}
	return out
}

func (m *model) timeline() string {
	r := m.rep
	state := "paused "
	if m.playing {
		state = "playing"
	}
	head := fmt.Sprintf(" t=%7.2f  %s x%-4g ", r.t, state, speeds[m.speed])
	start, w := m.timelineBar()
	head = fit(head, start)
	at := 0
	if r.end > 0 {
		at = int(r.t / r.end * float64(w-1))
	}
	bar := strings.Repeat("━", at) + "●" + dim.Render(strings.Repeat("─", max(w-at-1, 0)))
	return head + bar + fmt.Sprintf(" %7.2f", r.end)
}

func (m *model) keyLine() string {
	if m.searching {
		return " station: " + m.query + "_"
	}
	own, shared := m.keys()
	if m.note != "" {
		own = " " + bold.Render(m.note) + "  " + dim.Render(own)
	} else if m.rep != nil && m.rep.failure != "" && m.rep.t >= m.rep.end {
		own = " " + bold.Render("run failed: "+m.rep.failure)
	} else {
		own = dim.Render(own)
	}
	// the page's own keys give way, so the shared ones always show
	room := max(m.width-ansi.StringWidth(shared), 0)
	return ansi.Truncate(own, room, "…") + dim.Render(shared)
}

// keys are the page's own keys and those every page shares, as the other
// apps list theirs: moving, then acting, then what to show.
func (m *model) keys() (own, shared string) {
	play := "  space play  [ ] speed  , . step  tab train  ⇧←→ train events  f follow"
	var keys string
	switch {
	case m.rep == nil:
		return " +/- zoom  hjkl pan  0 fit  / station", "  q quit"
	case m.page == pageGraph:
		keys = " +/- time zoom  h l pan  0 fit  click seek  p planned" + play
	case m.page == pageDelay && m.st.rep != nil && m.st.dMap:
		keys = " +/- zoom  hjkl pan  0 fit  / station  m metric  v causes"
	case m.page == pageDelay && m.st.rep != nil && m.st.dCauses:
		keys = " ↑↓ row  tab part  enter narrow/show  c rank  t tracks  b filter  v tables"
	case m.page == pageOccupancy && m.st.rep != nil && m.st.oCauses:
		keys = " ↑↓ row  tab part  enter narrow/show  c rank  t tracks  v schematic"
	case m.page == pageDelay && m.st.rep != nil:
		keys = " ↑↓ row  tab table  pgup/pgdn page  enter show  c sort  b filter  v schematic"
	case m.page == pageOccupancy && m.st.rep != nil && m.st.table:
		keys = " ↑↓ row  tab table  pgup/pgdn page  enter show  c sort  t throats  v causes"
	case m.page == pageOccupancy && m.st.rep != nil:
		keys = " +/- zoom  hjkl pan  0 fit  / station  m metric  v tables"
		if m.st.focus != "" {
			keys += "  esc let go of the track"
		}
	case m.kpiPage():
		keys = ""
	default:
		keys = " +/- zoom  hjkl pan  0 fit  / station" + play + "  d decisions  s schedule"
		if m.bottom == paneDecisions {
			keys += "  enter next decision"
		}
	}
	return keys, "  1-4 tabs  q quit"
}

// tabNames name the pages on the tab bar.
var tabNames = []string{"Schematic", "Graph", "Delay", "Occupancy"}

// tabLabel is page p's label on the tab bar.
func (m *model) tabLabel(p page) string {
	label := fmt.Sprintf(" %d %s ", p+1, tabNames[p])
	switch {
	case p == pageOccupancy && m.st.table:
		label = fmt.Sprintf(" %d %s · tables ", p+1, tabNames[p])
	case p == pageDelay && m.st.dMap:
		label = fmt.Sprintf(" %d %s · schematic ", p+1, tabNames[p])
	case p == pageDelay && m.st.dCauses, p == pageOccupancy && m.st.oCauses:
		label = fmt.Sprintf(" %d %s · causes ", p+1, tabNames[p])
	}
	return label
}

// header is the tab bar, as trainedit's, and the recording's name.
func (m *model) header() string {
	var sb strings.Builder
	for p := range page(len(tabNames)) {
		label := m.tabLabel(p)
		switch {
		case p == m.page:
			sb.WriteString(tui.Accent.Bold(true).Reverse(true).Render(label))
		case p == pageGraph && m.graph == nil:
			sb.WriteString(dim.Render(label))
		default:
			sb.WriteString(label)
		}
	}
	sb.WriteString("   " + bold.Render("trainviz"))
	if m.name != "" {
		sb.WriteString(" · " + m.name)
	}
	return ansi.Truncate(sb.String(), m.width, "")
}

// tabAt is the page whose tab is at column x of the tab bar.
func (m *model) tabAt(x int) (page, bool) {
	at := 0
	for p := range page(len(tabNames)) {
		w := ansi.StringWidth(m.tabLabel(p))
		if x >= at && x < at+w {
			return p, true
		}
		at += w
	}
	return 0, false
}

// box draws a single-line border with a title around lines, w by h cells.
func box(title string, lines []string, w, h int) []string {
	inner := max(w-2, 0)
	label := ansi.Truncate(" "+title+" ", max(inner-1, 0), "")
	top := "┌─" + label + strings.Repeat("─", max(inner-1-ansi.StringWidth(label), 0)) + "┐"
	out := []string{dim.Render(top)}
	for i := range max(h-2, 0) {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, dim.Render("│")+fit(line, inner)+dim.Render("│"))
	}
	return append(out, dim.Render("└"+strings.Repeat("─", inner)+"┘"))
}

// fit cuts or pads s to exactly w cells.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}
