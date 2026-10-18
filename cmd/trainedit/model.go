package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type tab int

const (
	tabWorld tab = iota
	tabSchedules
	tabRun
)

var tabNames = []string{"World", "Schedules", "Run"}

// prompt asks a question answered with a key: each key's action, and esc
// to back out.
type prompt struct {
	title string
	lines []string
	keys  []promptKey
}

type promptKey struct {
	key, label string
	do         func() tea.Cmd
}

// config is how trainedit was started: the binaries it runs and where runs
// and the saved path go.
type config struct {
	trainapp, trainviz string
	binErr             error // why a binary was not found
	outDir             string
	savedTo            string // file to write the saved scenario's path to, for traindemo
}

type model struct {
	s      *session
	cfg    config
	tab    tab
	width  int
	height int

	form   *form
	prompt *prompt
	panel  panel // a full-screen editor over the tab, if one is open
	note   string
	bad    bool // the note is an error

	world *worldTab
	sched *schedTab
	run   *runTab

	interrupted bool
}

func newModel(s *session, cfg config) *model {
	m := &model{s: s, cfg: cfg}
	m.world = newWorldTab(m)
	m.sched = newSchedTab(m)
	m.run = newRunTab(m)
	switch {
	case s.openErr != nil:
		m.fail("the scenario did not read as it was (" + s.openErr.Error() + "); put it right and save")
		m.tab = tabRun
	case s.isNew && len(s.world.S.Points) == 0:
		m.say("a new world: t stamps a station, or e starts a line from a point")
	}
	return m
}

func (m *model) Init() tea.Cmd { return nil }

// say and fail put a line on the status line.
func (m *model) say(format string, args ...any) {
	m.note, m.bad = fmt.Sprintf(format, args...), false
}

func (m *model) fail(text string) { m.note, m.bad = text, true }

// open shows a form.
func (m *model) open(f *form) { m.form = f }

// ask shows a prompt.
func (m *model) ask(p *prompt) { m.prompt = p }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.world.resize()
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseMsg:
		if m.form == nil && m.prompt == nil && m.panel == nil && m.tab == tabWorld {
			m.world.mouse(msg)
		}
		return m, nil
	}
	return m, m.run.update(msg)
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		m.interrupted = true
		return tea.Quit
	}
	if m.form != nil {
		f := m.form
		if f.key(msg) && m.form == f {
			m.form = nil
		}
		return nil
	}
	if p := m.prompt; p != nil {
		if msg.String() == "esc" {
			m.prompt = nil
			return nil
		}
		for _, k := range p.keys {
			if msg.String() == k.key {
				m.prompt = nil
				return k.do()
			}
		}
		return nil
	}
	if p := m.panel; p != nil {
		m.note, m.bad = "", false
		if p.key(msg) && m.panel == p {
			m.panel = nil
		}
		return nil
	}
	if m.run.running() {
		return m.run.key(msg)
	}
	m.note = ""
	switch msg.String() {
	case "1", "f1":
		m.tab = tabWorld
		return nil
	case "2", "f2":
		m.tab = tabSchedules
		return nil
	case "3", "f3":
		m.tab = tabRun
		return nil
	case "ctrl+s":
		return m.save(nil)
	case "q":
		return m.quit()
	}
	switch m.tab {
	case tabWorld:
		return m.world.key(msg)
	case tabSchedules:
		return m.sched.key(msg)
	default:
		return m.run.key(msg)
	}
}

// quit leaves, asking first if anything is unsaved.
func (m *model) quit() tea.Cmd {
	if !m.s.dirty() {
		return tea.Quit
	}
	m.ask(&prompt{
		title: "unsaved changes",
		lines: []string{"The " + strings.Join(m.unsaved(), ", ") + " changed since the last save."},
		keys: []promptKey{
			{"s", "save and quit", func() tea.Cmd { return m.save(tea.Quit) }},
			{"d", "quit without saving", func() tea.Cmd { return tea.Quit }},
		},
	})
	return nil
}

func (m *model) unsaved() []string {
	var out []string
	if m.s.world.Dirty {
		out = append(out, "world")
	}
	if m.s.schedDirty {
		out = append(out, "schedule")
	}
	if m.s.scDirty {
		out = append(out, "scenario")
	}
	return out
}

// save saves, asking first about each changed file other scenarios use
// too; then runs after, if set.
func (m *model) save(after tea.Cmd) tea.Cmd {
	return m.saveShared(m.s.sharedSaves(), map[string]bool{}, after)
}

func (m *model) saveShared(todo []shared, copies map[string]bool, after tea.Cmd) tea.Cmd {
	if len(todo) == 0 {
		if err := m.s.save(copies); err != nil {
			m.fail("not saved: " + err.Error())
			return nil
		}
		if m.cfg.savedTo != "" {
			os.WriteFile(m.cfg.savedTo, []byte(m.s.sc.Path+"\n"), 0o644)
		}
		m.say("saved %s", m.s.sc.Path)
		m.world.changed()
		return after
	}
	sh := todo[0]
	next := func(copy bool) func() tea.Cmd {
		return func() tea.Cmd {
			copies[sh.what] = copy
			return m.saveShared(todo[1:], copies, after)
		}
	}
	m.ask(&prompt{
		title: "the " + sh.what + " is shared",
		lines: []string{
			sh.path + " is the " + sh.what + " of " + strings.Join(sh.users, ", ") + " too.",
		},
		keys: []promptKey{
			{"a", "save it for all of them", next(false)},
			{"c", "save a copy for " + m.s.sc.Name + " only", next(true)},
		},
	})
	return nil
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
	bodyH := max(m.height-3, 1)
	var body []string
	switch {
	case m.panel != nil:
		body = m.panel.render(m.width, bodyH)
	case m.tab == tabWorld:
		body = m.world.render(m.width, bodyH)
	case m.tab == tabSchedules:
		body = m.sched.render(m.width, bodyH)
	default:
		body = m.run.render(m.width, bodyH)
	}
	for len(body) < bodyH {
		body = append(body, "")
	}
	body = body[:bodyH]
	if m.form != nil {
		body = overlay(body, drawForm(m.form, m.width, bodyH), m.width)
	}
	if m.prompt != nil {
		body = overlay(body, m.drawPrompt(), m.width)
	}
	lines := append([]string{m.header()}, body...)
	keys := ""
	switch {
	case m.form != nil || m.prompt != nil:
	case m.panel != nil:
		keys = m.panel.keyLine()
	case m.run.running():
		keys = " esc cancel the run  ctrl+c quit"
	case m.tab == tabWorld:
		keys = m.world.keyLine()
	case m.tab == tabSchedules:
		keys = m.sched.keyLine()
	default:
		keys = m.run.keyLine()
	}
	if m.panel == nil {
		keys += "  1/2/3 tabs  ctrl+s save  q quit"
	}
	lines = append(lines, tui.Dim.Render(ansi.Truncate(keys, m.width, "")))
	note := m.note
	if m.bad {
		note = tui.Failed.Render(note)
	} else if note != "" {
		note = tui.Good.Render(note)
	}
	lines = append(lines, ansi.Truncate(" "+note, m.width, "…"))
	for i, l := range lines {
		lines[i] = tui.Fit(l, m.width)
	}
	return strings.Join(lines, "\n")
}

func (m *model) header() string {
	var tabs []string
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if tab(i) == m.tab {
			tabs = append(tabs, tui.Accent.Bold(true).Reverse(true).Render(label))
		} else {
			tabs = append(tabs, label)
		}
	}
	name := m.s.sc.Name
	if m.s.dirty() {
		name += tui.Amber.Render(" ●")
	}
	if m.s.isNew {
		name += tui.Dim.Render(" (new)")
	}
	return strings.Join(tabs, "") + "   " + tui.Bold.Render("trainedit") + " · " + name
}

func (m *model) drawPrompt() []string {
	w := min(max(56, m.width/2), m.width-4)
	var lines []string
	for _, l := range m.prompt.lines {
		for _, wl := range tui.Wrap(l, w-4, 6) {
			lines = append(lines, " "+wl)
		}
	}
	lines = append(lines, "")
	keys := slices.Clone(m.prompt.keys)
	for _, k := range keys {
		lines = append(lines, " "+tui.Accent.Render(k.key)+"  "+k.label)
	}
	lines = append(lines, " "+tui.Accent.Render("esc")+"  never mind")
	return tui.Box(m.prompt.title, true, lines, w, len(lines)+2)
}
