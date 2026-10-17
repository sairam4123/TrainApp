package main

import (
	"fmt"
	"strings"
	"trainapp/railway/kpi"
	"trainapp/railway/scenario"
	"trainapp/visualization/schematic"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	dim    = lipgloss.NewStyle().Faint(true)
	bold   = lipgloss.NewStyle().Bold(true)
	amber  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // as trainviz draws held trains
	failed = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// choice is what the picker was left with.
type choice int

const (
	choiceNone choice = iota
	choiceRun
	choiceQuit
	choiceInterrupt
)

const listWidth = 24

// aboutHeight is the box with the scenario's description, rule and last run.
const aboutHeight = 7

// picker lists the scenarios, with a preview of the selected one's world and
// schedule. It is run again after each run, keeping its place.
type picker struct {
	scenarios []*scenario.Scenario
	cursor    int
	rule      int // index into rules
	level     schematic.Level
	panX      int // first column of the schematic shown
	schedTop  int // first schedule row shown
	width     int
	height    int

	previews *previews
	results  map[string]*result // by resultKey
	choice   choice
}

func newPicker(scenarios []*scenario.Scenario, rule int) *picker {
	return &picker{scenarios: scenarios, rule: rule, level: schematic.Overview, previews: newPreviews(), results: map[string]*result{}}
}

// resultKey names the selected scenario under the selected rule, for its result.
func (p *picker) resultKey() string {
	return p.scenarios[p.cursor].Path + "|" + rules[p.rule]
}

func (p *picker) Init() tea.Cmd { return nil }

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.clamp()
	case tea.KeyPressMsg:
		return p, p.key(msg.String())
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			p.schedTop--
		case tea.MouseWheelDown:
			p.schedTop++
		}
		p.clamp()
	}
	return p, nil
}

func (p *picker) key(key string) tea.Cmd {
	switch key {
	case "q", "esc":
		p.choice = choiceQuit
		return tea.Quit
	case "ctrl+c":
		p.choice = choiceInterrupt
		return tea.Quit
	case "enter":
		p.choice = choiceRun
		return tea.Quit
	case "up", "k":
		p.choose(p.cursor - 1)
	case "down", "j":
		p.choose(p.cursor + 1)
	case "home", "g":
		p.choose(0)
	case "end", "G":
		p.choose(len(p.scenarios) - 1)
	case "left", "h":
		p.panX -= 16
	case "right", "l":
		p.panX += 16
	case "z":
		p.level = map[schematic.Level]schematic.Level{schematic.Overview: schematic.Layout, schematic.Layout: schematic.Overview}[p.level]
		p.panX = 0
	case "r":
		p.rule = (p.rule + 1) % len(rules)
	case "pgdown", "J", "space":
		p.schedTop += max(p.scheduleRows()-1, 1)
	case "pgup", "K":
		p.schedTop -= max(p.scheduleRows()-1, 1)
	}
	p.clamp()
	return nil
}

func (p *picker) choose(i int) {
	i = min(max(i, 0), len(p.scenarios)-1)
	if i != p.cursor {
		p.cursor, p.panX, p.schedTop = i, 0, 0
	}
}

// layout of the right-hand side: the about box, the schematic box sized to
// the diagram but no more than half what is left, and the schedule below.
// listW is the scenario list's width, none on a narrow screen: the box below
// still names the scenario.
func (p *picker) listW() int {
	if p.width < 70 {
		return 0
	}
	return listWidth
}

func (p *picker) rightWidth() int { return max(p.width-p.listW(), 3) }

func (p *picker) schematicHeight() int {
	rest := p.height - 1 - aboutHeight
	lines := len(p.previews.diagram(p.scenarios[p.cursor], p.level).lines)
	return min(lines+2, max(rest/2, 4))
}

func (p *picker) scheduleRows() int {
	return max(p.height-1-aboutHeight-p.schematicHeight()-2-1, 1) // less the box and its header row
}

func (p *picker) clamp() {
	if p.width == 0 {
		return
	}
	sc := p.scenarios[p.cursor]
	d := p.previews.diagram(sc, p.level)
	p.panX = min(p.panX, d.width-(p.rightWidth()-2))
	p.panX = max(p.panX, 0)
	n := len(p.previews.schedule(sc).rows)
	p.schedTop = max(min(p.schedTop, n-p.scheduleRows()), 0)
}

func (p *picker) View() tea.View {
	v := tea.NewView(p.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (p *picker) render() string {
	if p.width == 0 {
		return ""
	}
	h := p.height - 1
	rw := p.rightWidth()
	sc := p.scenarios[p.cursor]

	right := box(fmt.Sprintf("%s (%d/%d)", sc.Name, p.cursor+1, len(p.scenarios)), p.aboutLines(sc, rw-2), rw, aboutHeight)

	d := p.previews.diagram(sc, p.level)
	sh := p.schematicHeight()
	title := fmt.Sprintf("%s · %s", sc.World, p.level)
	if d.width > rw-2 {
		title += fmt.Sprintf(" · cols %d-%d of %d", p.panX+1, min(p.panX+rw-2, d.width), d.width)
	}
	var body []string
	for _, line := range d.lines {
		body = append(body, ansi.Cut(line, p.panX, p.panX+rw-2))
	}
	right = append(right, box(title, body, rw, sh)...)

	s := p.previews.schedule(sc)
	rows := p.scheduleRows()
	title = fmt.Sprintf("schedule · %d trains", len(s.rows))
	if len(s.rows) > rows {
		title += fmt.Sprintf(" · %d-%d", p.schedTop+1, min(p.schedTop+rows, len(s.rows)))
	}
	body = []string{dim.Render(s.header)}
	end := min(p.schedTop+rows, len(s.rows))
	body = append(body, s.rows[min(p.schedTop, end):end]...)
	right = append(right, box(title, body, rw, h-aboutHeight-sh)...)

	var left []string
	if lw := p.listW(); lw > 0 {
		left = box("scenarios", p.listLines(), lw, h)
	}
	lines := make([]string, h)
	for i := range lines {
		if i < len(left) {
			lines[i] = left[i]
		}
		if i < len(right) {
			lines[i] += right[i]
		}
	}
	keys := " ↑↓ scenario  enter run  r rule  ←→ pan  z overview/layout  pgup/pgdn schedule  q quit"
	lines = append(lines, dim.Render(ansi.Truncate(keys, p.width, "")))
	return strings.Join(lines, "\n")
}

func (p *picker) listLines() []string {
	var out []string
	for i, sc := range p.scenarios {
		line := " " + sc.Name
		if p.ranAny(sc) {
			line += dim.Render(" ✓")
		}
		line = fit(line, listWidth-2)
		if i == p.cursor {
			line = bold.Reverse(true).Render(ansi.Strip(line))
		}
		out = append(out, line)
	}
	return out
}

// ranAny reports whether the scenario has been run under any rule.
func (p *picker) ranAny(sc *scenario.Scenario) bool {
	for _, r := range rules {
		if _, ok := p.results[sc.Path+"|"+r]; ok {
			return true
		}
	}
	return false
}

// aboutLines are the description, what the scenario runs and the last run
// under the selected rule.
func (p *picker) aboutLines(sc *scenario.Scenario, w int) []string {
	var out []string
	if sc.Description != "" {
		out = append(out, strings.Split(ansi.Wordwrap(sc.Description, w-2, ""), "\n")...)
	}
	out = out[:min(len(out), 2)]
	out = append(out, dim.Render(fmt.Sprintf("world %s · schedule %s · profile %s · %d disruptions",
		sc.World, sc.Schedule, valueOr(sc.Profile, "none"), len(sc.Disruptions))))
	out = append(out, "rule: "+bold.Render(p.previews.ruleText(sc, rules[p.rule]))+dim.Render("  (r to change)"))
	out = append(out, p.resultLine(p.results[p.resultKey()]))
	for i := range out {
		out[i] = " " + out[i]
	}
	return out
}

// resultLine sums up the last run, or says there is none.
func (p *picker) resultLine(r *result) string {
	if r == nil {
		return dim.Render("not run yet under this rule: enter to run")
	}
	var parts []string
	if rep := r.report; rep != nil {
		parts = append(parts, fmt.Sprintf("%d/%d done", rep.Finished, rep.Trains))
		if d := rep.Delay; d != nil {
			parts = append(parts, fmt.Sprintf("delay %.1f min (%.1f a train)", d.TotalMin, d.MeanMin), punctuality(d))
		}
		if dis := rep.Disruption; dis != nil {
			parts = append(parts, fmt.Sprintf("reactionary %.1f", dis.ReactionaryMin))
		}
		parts = append(parts, fmt.Sprintf("%d holds", rep.Holds))
	}
	line := "last run: " + strings.Join(parts, " · ")
	if r.failed != "" {
		return failed.Render(line + "  " + r.failed)
	}
	return line + dim.Render("  "+r.files)
}

// punctuality is e.g. "punctual 0/0/42% ≤1/5/15 min".
func punctuality(d *kpi.Stats) string {
	var shares, within []string
	for _, p := range d.Punctual {
		shares = append(shares, fmt.Sprintf("%.0f", 100*p.Share))
		within = append(within, fmt.Sprintf("%g", p.WithinMin))
	}
	return fmt.Sprintf("punctual %s%% ≤%s min", strings.Join(shares, "/"), strings.Join(within, "/"))
}

func valueOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
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
