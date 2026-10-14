package main

import (
	"fmt"
	"strings"
	"time"
	"trainapp/visualization/schematic"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// zoomSteps run from the overview to the most detailed drawing.
var zoomSteps = []struct {
	level schematic.Level
	zoom  int
}{
	{schematic.Overview, 1},
	{schematic.Layout, 1},
	{schematic.Layout, 2},
	{schematic.Layout, 3},
	{schematic.Detail, 1},
	{schematic.Detail, 2},
}

// speeds are sim minutes per real minute.
var speeds = []float64{1, 10, 60, 600}

const tickEvery = 50 * time.Millisecond

var (
	dim  = lipgloss.NewStyle().Faint(true)
	bold = lipgloss.NewStyle().Bold(true)
)

type tickMsg time.Time

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
}

func newModel(d *schematic.Diagram, rep *replay) *model {
	m := &model{d: d, rep: rep, step: 1, speed: 2, selected: -1}
	m.redraw()
	return m
}

func (m *model) Init() tea.Cmd { return nil }

// layout of the screen: the schematic pane and, for a replay, the trains
// pane beside it and the events pane, timeline and key line below.
func (m *model) sideWidth() int {
	if m.rep == nil || m.width < 100 {
		return 0
	}
	return 34
}

func (m *model) eventsHeight() int {
	if m.rep == nil || m.height < 20 {
		return 0
	}
	return 7
}

func (m *model) bottomLines() int {
	if m.rep == nil {
		return 1
	}
	return 2
}

// pane is the size of the schematic inside its border.
func (m *model) pane() (w, h int) {
	return max(m.width-m.sideWidth()-2, 1), max(m.height-m.eventsHeight()-m.bottomLines()-2, 1)
}

func (m *model) selectedTrain() string {
	if m.rep == nil || m.selected < 0 {
		return ""
	}
	return m.rep.trains[m.selected]
}

func (m *model) options() schematic.Options {
	s := zoomSteps[m.step]
	opt := schematic.Options{Level: s.level, Zoom: s.zoom, Selected: m.selectedTrain()}
	if m.rep != nil {
		opt.State, opt.Time = m.rep.state, m.rep.t
	}
	return opt
}

func (m *model) redraw() {
	m.frame = m.d.Draw(m.options())
	if m.follow {
		if x, y, ok := m.frame.Train(m.selectedTrain()); ok {
			pw, ph := m.pane()
			if x < m.x+pw/5 || x > m.x+pw*4/5 {
				m.x = x - pw/2
			}
			if y < m.y || y >= m.y+ph {
				m.y = y - ph/2
			}
		}
	}
	m.clamp()
}

// clamp keeps the frame on screen, centred if it is smaller than the pane.
func (m *model) clamp() {
	pw, ph := m.pane()
	fw, fh := m.frame.Width(), m.frame.Height()
	if fw <= pw {
		m.x = (fw - pw) / 2
	} else {
		m.x = min(max(m.x, 0), fw-pw)
	}
	if fh <= ph {
		m.y = (fh - ph) / 2
	} else {
		m.y = min(max(m.y, 0), fh-ph)
	}
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
		if zoomSteps[i].level != schematic.Layout {
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
		m.redraw()

	case tickMsg:
		m.ticking = false
		if !m.playing || m.rep == nil {
			return m, nil
		}
		now := time.Time(msg)
		dt := now.Sub(m.lastTick).Minutes() * speeds[m.speed]
		m.lastTick = now
		if !m.rep.advance(dt) {
			m.playing = false
		}
		m.redraw()
		if m.playing {
			return m, m.tick()
		}

	case tea.KeyPressMsg:
		if m.searching {
			m.searchKey(msg)
			return m, nil
		}
		return m, m.key(msg)

	case tea.MouseWheelMsg:
		sx, sy := msg.X-1, msg.Y-1
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
		if m.rep != nil && msg.Y == m.height-2 {
			m.seekTimeline(msg.X)
			break
		}
		m.dragging, m.dragX, m.dragY = true, msg.X, msg.Y

	case tea.MouseMotionMsg:
		if m.dragging {
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
	case "q", "ctrl+c":
		return tea.Quit
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
	if m.rep != nil {
		m.replayKey(msg.String())
		if m.playing {
			return m.tick()
		}
	}
	m.clamp()
	return nil
}

func (m *model) replayKey(key string) {
	r := m.rep
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
	case "home":
		m.playing = false
		r.reset()
	case "end":
		m.playing = false
		r.seek(r.end)
	default:
		return
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
	m.redraw()
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
	pw, ph := m.pane()
	s := zoomSteps[m.step]
	title := fmt.Sprintf("%s, zoom %d", s.level, s.zoom)
	lines := box(title, m.frame.View(m.x, m.y, pw, ph, true), pw+2, ph+2)
	if sw := m.sideWidth(); sw > 0 {
		side := box("trains", m.trainLines(sw-2, ph), sw, ph+2)
		for i := range lines {
			lines[i] += side[i]
		}
	}
	if eh := m.eventsHeight(); eh > 0 {
		title := "events"
		if t := m.selectedTrain(); t != "" {
			title += " " + t
		}
		lines = append(lines, box(title, m.eventLines(eh-2), m.width, eh)...)
	}
	if m.rep != nil {
		lines = append(lines, m.timeline())
	}
	lines = append(lines, m.keyLine())
	return strings.Join(lines, "\n")
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
		text := n + "  " + tr.Phase
		switch {
		case !ok:
			text = n
		case tr.Waiting != nil:
			text = fmt.Sprintf("%s  waiting %s %s", n, tr.Waiting.Station, tr.Waiting.Pf)
		case tr.Track != "":
			text += " " + r.trackName[tr.Track]
		}
		style := lipgloss.NewStyle()
		switch {
		case i == m.selected:
			style = bold.Reverse(true)
		case tr.Phase == "exited" || tr.Phase == "outside":
			style = dim
		}
		out = append(out, style.Render(fit(text, w)))
		if ok && tr.Waiting != nil && tr.Waiting.BlockedBy != "" {
			out = append(out, dim.Render(fit("   blocked by "+tr.Waiting.BlockedBy, w)))
		}
	}
	if at >= h {
		out = out[at-h+1:]
	}
	return out
}

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
	keys := "+/- zoom  hjkl pan  0 fit  / station  q quit"
	if m.rep != nil {
		keys = "space play  [ ] speed  , . step  tab train  ⇧←→ train events  f follow  " + keys
	}
	line := dim.Render(" " + keys)
	if m.note != "" {
		line = " " + bold.Render(m.note) + "  " + line
	} else if m.rep != nil && m.rep.failure != "" && m.rep.t >= m.rep.end {
		line = " " + bold.Render("run failed: "+m.rep.failure)
	}
	return ansi.Truncate(line, m.width, "")
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
