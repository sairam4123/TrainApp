package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
	"trainapp/railway/scenario"
	"trainapp/visualization/schematic"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	dim    = lipgloss.NewStyle().Faint(true)
	bold   = lipgloss.NewStyle().Bold(true)
	accent = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	amber  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // as trainviz draws held trains
	good   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	failed = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// choice is what the picker was left with.
type choice int

const (
	choiceNone choice = iota
	choiceQuit
	choiceInterrupt
)

const listWidth = 24

// timetableWidth is the timetable box's width beside the schedule; a
// narrower screen shows it in the schedule's place while a train is picked.
const timetableWidth = 56

// focus is the box the arrow keys move in.
type focus int

const (
	focusList   focus = iota // the scenarios
	focusTrains              // the selected scenario's trains
)

// picker lists the scenarios, with a preview of the selected one: its world,
// schedule and precedence rule. It runs the scenario with trainapp, drawing
// its progress, then hands the terminal to trainviz.
type picker struct {
	scenarios []*scenario.Scenario
	cursor    int
	rule      int // index into rules
	level     schematic.Level
	panX      int // first column of the schematic shown
	schedTop  int // first schedule row shown
	focus     focus
	train     int // the train picked in the schedule, its timetable shown
	width     int
	height    int

	trainapp, trainviz string
	outDir             string

	previews *previews
	results  map[string]*result // by resultKey
	run      *run               // trainapp running, if it is
	viewing  string             // resultKey of the run trainviz shows
	spin     int
	note     string
	choice   choice
}

func newPicker(scenarios []*scenario.Scenario, rule int, trainapp, trainviz, outDir string) *picker {
	return &picker{scenarios: scenarios, rule: rule, level: schematic.Layout, trainapp: trainapp, trainviz: trainviz, outDir: outDir,
		previews: newPreviews(), results: map[string]*result{}}
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

	case tickMsg:
		if p.run != nil {
			p.spin++
			return p, tick()
		}

	case progressMsg, outputMsg:
		if p.run != nil {
			p.run.handle(msg)
			return p, p.run.next
		}

	case runDoneMsg:
		r := p.run
		p.run = nil
		if r.cancelled { // by esc or ctrl+c
			for _, f := range []string{r.rec, r.kpi} {
				os.Remove(f) // not to replay half a run
			}
			p.results[r.key] = nil // the run before went when this one started
			p.note = "run cancelled"
		}
		if p.choice == choiceInterrupt {
			return p, tea.Quit
		}
		if r.cancelled {
			return p, nil
		}
		p.results[r.key] = r.finish(msg.err)

	case vizDoneMsg:
		if interrupted(msg.err) {
			p.choice = choiceInterrupt
			return p, tea.Quit
		}
		if res := p.results[p.viewing]; res != nil && msg.err != nil && res.failed == "" {
			res.failed = "trainviz: " + msg.err.Error()
		}

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
	if p.run != nil {
		switch key {
		case "ctrl+c":
			p.choice = choiceInterrupt
			p.run.cancel()
		case "esc", "q":
			p.run.cancel()
		}
		return nil // the runDoneMsg follows
	}
	p.note = ""
	switch key {
	case "esc":
		if p.focus == focusTrains {
			p.focus = focusList
			break
		}
		p.choice = choiceQuit
		return tea.Quit
	case "q":
		p.choice = choiceQuit
		return tea.Quit
	case "ctrl+c":
		p.choice = choiceInterrupt
		return tea.Quit
	case "enter":
		return p.start()
	case "v":
		return p.view()
	case "tab", "shift+tab":
		p.focus = 1 - p.focus
	case "up", "k":
		p.step(-1)
	case "down", "j":
		p.step(1)
	case "home", "g":
		p.move(-1 << 20)
	case "end", "G":
		p.move(1 << 20)
	case "left", "h":
		p.panX -= 16
	case "right", "l":
		p.panX += 16
	case "z":
		p.level = map[schematic.Level]schematic.Level{schematic.Overview: schematic.Layout, schematic.Layout: schematic.Overview}[p.level]
		p.panX = 0
	case "r":
		p.rule = (p.rule + 1) % len(rules)
	case "R":
		p.rule = (p.rule + len(rules) - 1) % len(rules)
	case "pgdown", "J", "space":
		p.page(1)
	case "pgup", "K":
		p.page(-1)
	}
	p.clamp()
	return nil
}

// step moves the cursor of the focused box one up or down, wrapping round
// from one end to the other.
func (p *picker) step(dir int) {
	if p.focus == focusTrains {
		n := len(p.previews.schedule(p.scenarios[p.cursor]).trains)
		p.setTrain((p.train + dir + n) % max(n, 1))
		return
	}
	n := len(p.scenarios)
	p.choose((p.cursor + dir + n) % n)
}

// move moves the cursor of the focused box by n, stopping at the ends.
func (p *picker) move(n int) {
	if p.focus == focusTrains {
		p.setTrain(p.train + n)
		return // clamp keeps it in view
	}
	p.choose(min(max(p.cursor+n, 0), len(p.scenarios)-1))
}

// page scrolls the schedule a page, moving the picked train with it.
func (p *picker) page(dir int) {
	n := dir * max(p.layout().rows-1, 1)
	p.schedTop += n
	if p.focus == focusTrains {
		p.setTrain(p.train + n)
	}
}

// setTrain picks train i, kept to the scenario's trains.
func (p *picker) setTrain(i int) {
	n := len(p.previews.schedule(p.scenarios[p.cursor]).trains)
	p.train = max(min(i, n-1), 0)
}

// view replays the selected scenario's run under the selected rule in
// trainviz, even one that failed, such as a deadlock.
func (p *picker) view() tea.Cmd {
	rec, _ := runFiles(p.outDir, p.scenarios[p.cursor], rules[p.rule])
	if !isFile(rec) {
		p.note = "nothing to replay yet: enter runs it"
		return nil
	}
	p.viewing = p.resultKey()
	return replay(p.trainviz, rec)
}

// result is the selected scenario's last run under the selected rule, read
// from its files if it was run before this session; nil if there is none.
func (p *picker) result() *result {
	key := p.resultKey()
	res, ok := p.results[key]
	if !ok {
		res = loadResult(p.outDir, p.scenarios[p.cursor], rules[p.rule])
		p.results[key] = res
	}
	return res
}

// start runs the selected scenario under the selected rule.
func (p *picker) start() tea.Cmd {
	r, err := startRun(p.trainapp, p.scenarios[p.cursor], rules[p.rule], p.outDir)
	if err != nil {
		p.note = "cannot run trainapp: " + err.Error()
		return nil
	}
	r.key = p.resultKey()
	p.run = r
	return tea.Batch(r.next, tick())
}

func (p *picker) choose(i int) {
	i = min(max(i, 0), len(p.scenarios)-1)
	if i != p.cursor {
		p.cursor, p.panX, p.schedTop, p.train = i, 0, 0, 0
	}
}

func (p *picker) clamp() {
	if p.width == 0 {
		return
	}
	l := p.layout()
	d := p.previews.diagram(p.scenarios[p.cursor], p.level)
	p.panX = max(min(p.panX, d.width-l.inner), 0)
	n := len(p.previews.schedule(p.scenarios[p.cursor]).trains)
	p.train = max(min(p.train, n-1), 0)
	if p.focus == focusTrains { // keep the picked train in view
		p.schedTop = min(max(p.schedTop, p.train-l.rows+1), p.train)
	}
	p.schedTop = max(min(p.schedTop, n-l.rows), 0)
}

// screenLayout is how the screen is split into boxes: the scenario list on
// the left, and on the right the selected scenario, its schematic sized to
// the diagram, and its schedule below.
type screenLayout struct {
	list      int      // the list's width, 0 on a narrow screen
	right     int      // the right-hand boxes' width
	inner     int      // inside their borders
	body      int      // lines above the key line
	top       []string // the scenario, rule and run boxes, drawn
	schematic int      // the schematic box's height
	schedule  int      // the schedule and timetable boxes' height
	rows      int      // schedule rows shown
	table     int      // the schedule box's width, 0 when the timetable takes its place
	timetable int      // the timetable box's width, 0 for none
}

// scheduleExtra are the schedule box's lines other than its rows: the
// borders, the column headings and the class key.
const scheduleExtra = 4

func (p *picker) layout() screenLayout {
	l := screenLayout{body: max(p.height-1, 1)}
	if p.width >= 70 {
		l.list = listWidth
	}
	l.right = max(p.width-l.list, 3)
	l.inner = l.right - 2
	sc := p.scenarios[p.cursor]
	l.top = p.topBoxes(sc, l)
	rest := max(l.body-len(l.top), 0)
	// the schedule and the picked train's timetable come first, up to 3/5
	s := p.previews.schedule(sc)
	need := len(s.trains) + scheduleExtra
	if p.train >= 0 && p.train < len(s.trains) {
		need = max(need, len(s.trains[p.train].points)+4) // its borders, header and key
	}
	diagram := len(p.previews.diagram(sc, p.level).lines)
	l.schematic = min(diagram+2, max(rest-min(need, rest*3/5), 3))
	l.schedule = rest - l.schematic
	l.rows = max(l.schedule-scheduleExtra, 1)
	switch {
	case l.right >= 2*timetableWidth:
		l.table, l.timetable = l.right-timetableWidth, timetableWidth
	case p.focus == focusTrains:
		l.timetable = l.right
	default:
		l.table = l.right
	}
	return l
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
	l := p.layout()
	sc := p.scenarios[p.cursor]

	right := l.top

	d := p.previews.diagram(sc, p.level)
	title := fmt.Sprintf("%s · %s", sc.World, p.level)
	if d.width > l.inner {
		title += " · " + arrow(p.panX > 0, "◀ ") + fmt.Sprintf("%d-%d of %d", p.panX+1, min(p.panX+l.inner, d.width), d.width) + arrow(p.panX+l.inner < d.width, " ▶")
	}
	var body []string
	if d.err != nil {
		body = []string{failed.Render("no preview: " + d.err.Error())}
	}
	for _, line := range d.lines {
		if d.width <= l.inner {
			body = append(body, strings.Repeat(" ", (l.inner-d.width)/2)+line) // centred
		} else {
			body = append(body, ansi.Cut(line, p.panX, p.panX+l.inner))
		}
	}
	right = append(right, box(title, false, body, l.right, l.schematic)...)

	var table, timetable []string
	if l.table > 0 {
		title, body = p.scheduleBox(sc, l)
		table = box(title, p.focus == focusTrains, body, l.table, l.schedule)
	}
	if l.timetable > 0 {
		title, body = p.timetableBox(sc, l)
		timetable = box(title, false, body, l.timetable, l.schedule)
	}
	for i := range l.schedule {
		right = append(right, at(table, i)+at(timetable, i))
	}

	var left []string
	if l.list > 0 {
		left = box("scenarios", p.focus == focusList, p.listLines(), l.list, l.body)
	}
	lines := make([]string, l.body)
	for i := range lines {
		lines[i] = at(left, i) + at(right, i)
	}
	lines = append(lines, p.keyLine())
	return strings.Join(lines, "\n")
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func (p *picker) keyLine() string {
	keys := " ↑↓ scenario  tab trains  enter run  v replay  r rule  ←→ pan  z overview/layout  pgup/pgdn schedule  q quit"
	if p.focus == focusTrains {
		keys = " ↑↓ train  tab/esc scenarios  enter run  v replay  r rule  ←→ pan  z overview/layout  pgup/pgdn page  q quit"
	}
	if p.run != nil {
		keys = " esc cancel the run  ctrl+c quit"
	}
	return dim.Render(ansi.Truncate(keys, p.width, ""))
}

func (p *picker) listLines() []string {
	var out []string
	for i, sc := range p.scenarios {
		line := fit(" "+sc.Name, listWidth-4)
		if i == p.cursor {
			line = bold.Reverse(true).Faint(p.focus != focusList).Render(line)
		}
		mark := " "
		if p.ranAny(sc) {
			mark = good.Render("✓")
		}
		out = append(out, line+mark)
	}
	return out
}

// ranAny reports whether the scenario has been run under any rule, in this
// session or before.
func (p *picker) ranAny(sc *scenario.Scenario) bool {
	for _, r := range rules {
		if rec, _ := runFiles(p.outDir, sc, r); p.results[sc.Path+"|"+r] != nil || isFile(rec) {
			return true
		}
	}
	return false
}

// stackBelow is the right-hand width under which the top boxes stack
// rather than sit side by side.
const stackBelow = 96

// topBoxes are the boxes above the schematic, drawn: the selected scenario's
// description, the precedence rules and its run, side by side as tall as
// the tallest, or one above the other on a narrow screen.
func (p *picker) topBoxes(sc *scenario.Scenario, l screenLayout) []string {
	title := sc.Name
	if l.list == 0 {
		title += fmt.Sprintf(" (%d/%d)", p.cursor+1, len(p.scenarios))
	}
	widths := []int{l.right / 4, l.right * 2 / 5}
	widths = append(widths, l.right-widths[0]-widths[1])
	if l.right < stackBelow {
		widths = []int{l.right, l.right, l.right}
	}
	runTitle, run := p.runBox(widths[2] - 4)
	boxes := []struct {
		title string
		lines []string
	}{
		{title, wrap(valueOr(sc.Description, dim.Render("no description")), widths[0]-4, 4)},
		{"precedence rule", append([]string{p.ruleTabs(), ""}, dimAll(wrap(p.ruleExplanation(sc, rules[p.rule]), widths[1]-4, 4))...)},
		{runTitle, run},
	}
	pad := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, line := range lines {
			out[i] = " " + line
		}
		return out
	}
	if l.right < stackBelow {
		var out []string
		for i, b := range boxes {
			out = append(out, box(b.title, false, pad(b.lines), widths[i], len(b.lines)+2)...)
		}
		return out
	}
	h := 0
	for _, b := range boxes {
		h = max(h, len(b.lines)+2)
	}
	drawn := make([][]string, len(boxes))
	for i, b := range boxes {
		drawn[i] = box(b.title, false, pad(b.lines), widths[i], h)
	}
	out := make([]string, h)
	for i := range out {
		out[i] = at(drawn[0], i) + at(drawn[1], i) + at(drawn[2], i)
	}
	return out
}

// wrap wraps text to w cells, at most n lines, the last cut short with "…".
func wrap(text string, w, n int) []string {
	lines := strings.Split(ansi.Wordwrap(text, max(w, 1), " "), "\n")
	if len(lines) > n {
		lines = lines[:n]
		lines[n-1] = ansi.Truncate(lines[n-1]+" …", w, "…")
	}
	return lines
}

func dimAll(lines []string) []string {
	for i, line := range lines {
		lines[i] = dim.Render(line)
	}
	return lines
}

func arrow(on bool, a string) string {
	if on {
		return a
	}
	return ""
}

// ruleTabs shows every rule, the selected one picked out.
func (p *picker) ruleTabs() string {
	var tabs []string
	for i, r := range rules {
		label := " " + ruleLabel(r) + " "
		if i == p.rule {
			tabs = append(tabs, accent.Bold(true).Reverse(true).Render(label))
		} else {
			tabs = append(tabs, label)
		}
	}
	return strings.Join(tabs, "")
}

// ruleExplanation says what the rule does, with the profile's settings.
func (p *picker) ruleExplanation(sc *scenario.Scenario, rule string) string {
	pr := p.previews.profile(sc)
	if pr.err != nil {
		return "the profile does not load: " + pr.err.Error()
	}
	if rule == "" {
		if pr.cfg == nil || pr.cfg.Precedence == nil {
			return "the profile's: " + p.ruleExplanation(sc, scenario.RuleOff)
		}
		own := valueOr(pr.cfg.Precedence.Rule, "thresholds") // the sim's default
		return "the profile's " + own + ": " + p.ruleExplanation(sc, own)
	}
	if rule == scenario.RuleOff {
		return "no precedence: no train is held back or moved to another platform for a higher-priority one"
	}
	if pr.cfg == nil || pr.cfg.Precedence == nil {
		return "the profile has no precedence settings, so this rule cannot run"
	}
	c := pr.cfg.Precedence
	var text string
	switch rule {
	case "thresholds":
		text = fmt.Sprintf("hold a train for one ≥%d priority higher that would lose ≥%g min", c.MinPriorityGap, float64(c.MinSavingMin))
		if c.MaxWaitRatio > 0 {
			text += fmt.Sprintf(", if it waits ≤%g min per minute saved", c.MaxWaitRatio)
		}
	case "weighted":
		text = fmt.Sprintf("hold a train when the other's priority × minutes saved, less its own × minutes waited, is over %g", c.MinNetGain)
	default:
		text = "rule " + rule
	}
	if c.MaxHoldMin > 0 {
		text += fmt.Sprintf("; holds last ≤%g min", float64(c.MaxHoldMin))
	}
	return text
}

// runBox is the run box's title and lines: trainapp's progress while it
// runs, else the last run's figures under the selected rule.
func (p *picker) runBox(w int) (string, []string) {
	r := p.run
	if r == nil {
		var lines []string
		title := "run · " + ruleLabel(rules[p.rule])
		switch res := p.result(); {
		case res == nil:
			lines = []string{dim.Render("not run yet under this rule"), "", dim.Render("enter runs it")}
		default:
			title = "last run · " + ruleLabel(rules[p.rule])
			lines = resultLines(res)
			if res.failed != "" {
				lines = append(lines, failed.Render(ansi.Truncate(res.failed, w, "…")))
			}
		}
		if p.note != "" {
			lines = append(lines, amber.Render(p.note))
		}
		return title, lines
	}

	spin := accent.Render(spinner[p.spin%len(spinner)])
	elapsed := time.Since(r.started).Truncate(time.Second)
	title := fmt.Sprintf("running · %s · %d:%02d", ruleLabel(r.rule), int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	pr := r.progress
	switch {
	case r.cancelled:
		return title, []string{spin + " cancelling…"}
	case r.measuring:
		return title, []string{bar(1, w), "", spin + " working out the KPIs"}
	case !pr.ok:
		return title, []string{spin + " starting trainapp…"}
	}
	return title, []string{
		bar(float64(pr.reached)/float64(max(pr.points, 1)), w), "",
		spin + fmt.Sprintf(" %d/%d points", pr.reached, pr.points),
		figure("sim time", fmt.Sprintf("%.1f min", pr.t)),
		figure("done", trains(pr.done)),
		figure("waiting", trains(pr.waiting)),
	}
}

func trains(n int) string {
	if n == 1 {
		return "1 train"
	}
	return fmt.Sprintf("%d trains", n)
}

// figure is a labelled figure, labels lined up.
func figure(label, value string) string {
	return dim.Render(fit(label, 11)) + value
}

// bar is a progress bar w cells wide, share of it filled.
func bar(share float64, w int) string {
	w = max(w, 4)
	filled := min(int(share*float64(w)), w)
	return accent.Render(strings.Repeat("━", filled)) + dim.Render(strings.Repeat("─", w-filled))
}

// resultLines are a run's figures, a line each.
func resultLines(r *result) []string {
	rep := r.report
	if rep == nil {
		return []string{failed.Render("no KPIs: the run did not finish")}
	}
	done := fmt.Sprintf("%d/%d trains", rep.Finished, rep.Trains)
	if rep.Finished == rep.Trains {
		done = good.Render(done)
	} else {
		done = failed.Render(done)
	}
	out := []string{figure("done", done)}
	if d := rep.Delay; d != nil {
		out = append(out, figure("avg delay", fmt.Sprintf("%.1f min", d.MeanMin)))
		if n := len(d.Punctual); n > 0 { // the widest band
			pu := d.Punctual[n-1]
			out = append(out, figure("on time", fmt.Sprintf("%.0f%%", 100*pu.Share)+dim.Render(fmt.Sprintf(" within %g min", pu.WithinMin))))
		}
	}
	if dis := rep.Disruption; dis != nil {
		out = append(out, figure("knock-on", fmt.Sprintf("%.1f min", dis.ReactionaryMin)))
	}
	return append(out, figure("holds", fmt.Sprint(rep.Holds)))
}

// scheduleBox is the schedule box's title and lines: a table of the trains,
// columns as wide as their widest entry, and a key to the class colours.
func (p *picker) scheduleBox(sc *scenario.Scenario, l screenLayout) (string, []string) {
	s := p.previews.schedule(sc)
	title := fmt.Sprintf("schedule · %d trains", len(s.trains))
	if len(s.trains) > l.rows {
		title += fmt.Sprintf(" · %d-%d", p.schedTop+1, min(p.schedTop+l.rows, len(s.trains)))
	}
	if s.err != nil {
		return title, []string{failed.Render(" no schedule: " + s.err.Error())}
	}

	classW, routeW, disW := len("class"), len("route"), 0
	for _, t := range s.trains {
		classW = max(classW, ansi.StringWidth(t.class)+2)
		routeW = max(routeW, ansi.StringWidth(t.from+"→"+t.to))
		if len(t.disruptions) > 0 {
			disW = max(disW, ansi.StringWidth(strings.Join(t.disruptions, ", "))+2)
		}
	}
	cols := func(number, class, entry, route, dis, stops string) string {
		line := " " + fit(number, 7) + fit(class, classW+2) + fit(entry, 7) + fit(route, routeW+2)
		if disW > 0 {
			line += fit(dis, disW+2)
		}
		return line + stops
	}
	out := []string{dim.Render(cols("train", "class", "entry", "route", "disruption", "stops"))}

	end := min(p.schedTop+l.rows, len(s.trains))
	for i := min(p.schedTop, end); i < end; i++ {
		t := s.trains[i]
		if i == p.train && p.focus == focusTrains {
			out = append(out, bold.Reverse(true).Render(fit(cols(t.number, "■ "+valueOr(t.class, "—"), t.entry, t.from+"→"+t.to,
				strings.Join(append([]string{}, t.disruptions...), ", "), strings.Join(t.stops, " ")), l.table-2)))
			continue
		}
		number, dis := t.number, ""
		if len(t.disruptions) > 0 {
			number, dis = amber.Render(t.number), amber.Render("⚠ "+strings.Join(t.disruptions, ", "))
		}
		class := dim.Render("—")
		if t.class != "" {
			class = lipgloss.NewStyle().Foreground(lipgloss.Color(s.color[t.class])).Render("■ ") + t.class
		}
		stops := strings.Join(t.stops, " ")
		if t.passes > 0 {
			stops += dim.Render(fmt.Sprintf(" +%d passes", t.passes))
		}
		out = append(out, cols(number, class, t.entry, t.from+"→"+t.to, dis, stops))
	}
	for len(out) < 1+l.rows {
		out = append(out, "")
	}

	// the class colours, highest priority first, as trainviz's key
	var key []string
	for _, c := range slices.Backward(s.classes) {
		item := lipgloss.NewStyle().Foreground(lipgloss.Color(s.color[c])).Render("■") + " " + c
		if pri, ok := s.priority[c]; ok {
			item += dim.Render(fmt.Sprintf(" %d", pri))
		}
		key = append(key, item)
	}
	if len(sc.Disruptions) > 0 {
		key = append(key, amber.Render("⚠ disrupted"))
	}
	return title, append(out, " "+strings.Join(key, "  "))
}

func valueOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// box draws a single-line border with a title around lines, w by h cells.
// The focused box's title stands out.
func box(title string, focused bool, lines []string, w, h int) []string {
	if h < 2 {
		return nil
	}
	inner := max(w-2, 0)
	label := ansi.Truncate(" "+title+" ", max(inner-1, 0), "")
	fill := strings.Repeat("─", max(inner-1-ansi.StringWidth(label), 0)) + "┐"
	top := dim.Render("┌─" + label + fill)
	if focused {
		top = dim.Render("┌─") + accent.Bold(true).Render(label) + dim.Render(fill)
	}
	out := []string{top}
	for i := range h - 2 {
		out = append(out, dim.Render("│")+fit(at(lines, i), inner)+dim.Render("│"))
	}
	return append(out, dim.Render("└"+strings.Repeat("─", inner)+"┘"))
}

// fit cuts or pads s to exactly w cells.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// timetableBox is the picked train's timetable: a row per schedule point,
// with the times and platforms of the last run under the selected rule if
// there is one, else the planned ones, dimmed, and "—" where the schedule
// leaves them to the sim.
func (p *picker) timetableBox(sc *scenario.Scenario, l screenLayout) (string, []string) {
	s := p.previews.schedule(sc)
	if s.err != nil || len(s.trains) == 0 {
		return "timetable", nil
	}
	t := s.trains[p.train]
	var calls []call
	if res := p.result(); res != nil {
		calls = res.calls[t.number]
	}
	title := fmt.Sprintf("timetable · %s · %s→%s", t.number, t.from, t.to)

	cols := func(stn, kind, pf, arr, dep, plan, actual, note string) string {
		return " " + fit(stn, 6) + fit(kind, 5) + fit(pf, 4) + fit(arr, 6) + fit(dep, 6) + fit(plan, 8) + fit(actual, 7) + note
	}
	planned := func(t float64) string {
		if t < 0 {
			return dim.Render("—")
		}
		return dim.Render(clock(t))
	}
	out := []string{dim.Render(cols("stn", "kind", "pf", "arr", "dep", "planned", "actual", ""))}
	for i, sp := range t.points {
		kind := "stop"
		if sp.IsPass() {
			kind = dim.Render("pass")
		}
		pf, arr, dep, plan, actual := dim.Render(valueOr(sp.SpPfNo, "—")), planned(sp.ArrTime), planned(sp.DeptTime), "", ""
		if !sp.IsPass() {
			plan = fmt.Sprintf("%g", sp.DwellTime) // the schedule's, so not dimmed
		}
		if i < len(calls) {
			c := calls[i]
			pf, arr = valueOr(c.pf, pf), clock(c.arr)
			if c.dep >= 0 {
				dep = clock(c.dep)
				if stood := c.dep - c.arr; !sp.IsPass() || stood >= 0.05 { // a pass that stopped shows its stand
					actual = fmt.Sprintf("%.1f", stood)
					if stood > sp.DwellTime+0.5 || sp.IsPass() { // stood longer than planned
						actual = amber.Render(actual)
					}
				}
			}
		}
		var notes []string
		for _, d := range t.hits {
			if (d.Kind == "late_entry" && i == 0) || (d.Kind != "late_entry" && string(d.Stn) == string(sp.StnCode)) {
				notes = append(notes, disruptionText(d, false))
			}
		}
		note := ""
		if len(notes) > 0 {
			note = amber.Render("⚠ " + strings.Join(notes, ", "))
		}
		out = append(out, cols(string(sp.StnCode), kind, pf, arr, dep, plan, actual, note))
	}

	h := l.schedule - 2 // inside the box
	key := dim.Render(" stops in min; run it for the actuals")
	if len(calls) > 0 {
		key = dim.Render(" stops in min; times as run under " + ruleLabel(rules[p.rule]))
	}
	if len(out) >= h {
		out = out[:max(h-1, 1)]
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	return title, append(out, key)
}
