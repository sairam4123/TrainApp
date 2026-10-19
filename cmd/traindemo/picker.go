package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"trainapp/railway/scenario"
	"trainapp/visualization/runs"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

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

type (
	vizDoneMsg  struct{ err error }
	editDoneMsg struct {
		err   error
		saved string // the file trainedit says it saved last
		was   string // the scenario it was opened on
	}
	tickMsg time.Time
)

// picker lists the scenarios, with a preview of the selected one: its world,
// schedule and precedence rule. It runs the scenario with trainapp, drawing
// its progress, then hands the terminal to trainviz; and hands it to
// trainedit to edit a scenario or make a new one.
type picker struct {
	dir       string
	scenarios []*scenario.Scenario
	cursor    int
	rule      int // index into runs.Rules
	level     schematic.Level
	panX      int // first column of the schematic shown
	schedTop  int // first schedule row shown
	focus     focus
	train     int // the train picked in the schedule, its timetable shown
	width     int
	height    int

	bins   bins
	outDir string

	previews *previews
	results  map[string]*runs.Result // by resultKey
	run      *runs.Run               // trainapp running, if it is
	viewing  string                  // resultKey of the run trainviz shows
	spin     int
	note     string
	choice   choice
}

// newPicker lists the scenarios in dir; one that does not read is listed
// with its error, so trainedit can be opened on it to put it right.
func newPicker(dir string, rule int, b bins, outDir string) (*picker, error) {
	p := &picker{dir: dir, rule: rule, level: schematic.Layout, bins: b, outDir: outDir}
	if err := p.reload(""); err != nil {
		return nil, err
	}
	return p, nil
}

// reload reads the scenarios again, forgetting the previews and results it
// had, and selects the scenario at path if it is there (else the same
// place in the list).
func (p *picker) reload(path string) error {
	scs, err := scenario.ListDir(p.dir)
	if err != nil {
		return err
	}
	if len(scs) == 0 {
		return fmt.Errorf("no scenarios in %s", p.dir)
	}
	p.scenarios, p.previews, p.results = scs, newPreviews(), map[string]*runs.Result{}
	p.cursor = min(p.cursor, len(scs)-1)
	if path != "" {
		for i, sc := range scs {
			if samePath(sc.Path, path) {
				p.cursor, p.panX, p.schedTop, p.train = i, 0, 0, 0
			}
		}
	}
	p.clamp()
	return nil
}

func samePath(a, b string) bool {
	x, errX := filepath.Abs(a)
	y, errY := filepath.Abs(b)
	return errX == nil && errY == nil && x == y
}

// resultKey names the selected scenario under the selected rule, for its result.
func (p *picker) resultKey() string {
	return p.scenarios[p.cursor].Path + "|" + runs.Rules[p.rule]
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

	case runs.ProgressMsg, runs.OutputMsg:
		if p.run != nil {
			p.run.Handle(msg)
			return p, p.run.Next
		}

	case runs.DoneMsg:
		r := p.run
		p.run = nil
		if r.Cancelled { // by esc or ctrl+c
			r.Discard()
			p.results[r.Key] = nil // the run before went when this one started
			p.note = "run cancelled"
		}
		if p.choice == choiceInterrupt {
			return p, tea.Quit
		}
		if r.Cancelled {
			return p, nil
		}
		p.results[r.Key] = r.Finish(msg.Err)

	case vizDoneMsg:
		if runs.Interrupted(msg.err) {
			p.choice = choiceInterrupt
			return p, tea.Quit
		}
		if res := p.results[p.viewing]; res != nil && msg.err != nil && res.Failed == "" {
			res.Failed = "trainviz: " + msg.err.Error()
		}

	case editDoneMsg:
		if runs.Interrupted(msg.err) {
			p.choice = choiceInterrupt
			return p, tea.Quit
		}
		if err := p.reload(tui.ValueOr(msg.saved, msg.was)); err != nil {
			p.note = "cannot read the scenarios again: " + err.Error()
		} else if msg.err != nil {
			p.note = "trainedit: " + msg.err.Error()
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
			p.run.Cancel()
		case "esc", "q":
			p.run.Cancel()
		}
		return nil // the DoneMsg follows
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
	case "e":
		return p.edit(false, false)
	case "c":
		return p.edit(true, true)
	case "n":
		return p.edit(true, false)
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
		p.rule = (p.rule + 1) % len(runs.Rules)
	case "R":
		p.rule = (p.rule + len(runs.Rules) - 1) % len(runs.Rules)
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
	sc := p.scenarios[p.cursor]
	if sc.Err != nil {
		p.note = "the scenario does not read: e opens it in trainedit"
		return nil
	}
	rec, _ := runs.Files(p.outDir, sc, runs.Rules[p.rule])
	if !runs.IsFile(rec) {
		p.note = "nothing to replay yet: enter runs it"
		return nil
	}
	p.viewing = p.resultKey()
	return runs.Replay(p.bins.trainviz, rec, func(err error) tea.Msg { return vizDoneMsg{err} })
}

// edit hands the terminal to trainedit on the selected scenario, or on a new
// one, made from it if from says so (else trainedit asks for its world); the
// scenarios are read again when it quits.
func (p *picker) edit(fresh, from bool) tea.Cmd {
	if p.bins.trainedit == "" {
		p.note = "no trainedit: " + p.bins.editErr.Error()
		return nil
	}
	sc := p.scenarios[p.cursor]
	saved, err := os.CreateTemp("", "traindemo-saved-*")
	if err != nil {
		p.note = "cannot start trainedit: " + err.Error()
		return nil
	}
	saved.Close()
	args := []string{"-saved-to", saved.Name(), "-out", p.outDir, "-bin", filepath.Dir(p.bins.trainapp)}
	if fresh {
		args = append(args, "-new", "-dir", p.dir)
		if from && sc.Err == nil {
			args = append(args, "-from", sc.Path)
		}
	} else {
		args = append(args, "-scenario", sc.Path)
	}
	was := sc.Path
	return tea.ExecProcess(exec.Command(p.bins.trainedit, args...), func(err error) tea.Msg {
		b, _ := os.ReadFile(saved.Name())
		os.Remove(saved.Name())
		return editDoneMsg{err: err, saved: strings.TrimSpace(string(b)), was: was}
	})
}

// result is the selected scenario's last run under the selected rule, read
// from its files if it was run before this session; nil if there is none.
func (p *picker) result() *runs.Result {
	sc := p.scenarios[p.cursor]
	if sc.Err != nil {
		return nil
	}
	key := p.resultKey()
	res, ok := p.results[key]
	if !ok {
		res = runs.LoadResult(p.outDir, sc, runs.Rules[p.rule])
		p.results[key] = res
	}
	return res
}

// start runs the selected scenario under the selected rule.
func (p *picker) start() tea.Cmd {
	sc := p.scenarios[p.cursor]
	if sc.Err != nil {
		p.note = "the scenario does not read: e opens it in trainedit"
		return nil
	}
	r, err := runs.Start(p.bins.trainapp, sc, runs.Rules[p.rule], p.outDir, runs.Options{})
	if err != nil {
		p.note = "cannot run trainapp: " + err.Error()
		return nil
	}
	r.Key = p.resultKey()
	p.run = r
	return tea.Batch(r.Next, tick())
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
	title := fmt.Sprintf("%s · %s", tui.ValueOr(sc.World, "no world"), p.level)
	if d.width > l.inner {
		title += " · " + arrow(p.panX > 0, "◀ ") + fmt.Sprintf("%d-%d of %d", p.panX+1, min(p.panX+l.inner, d.width), d.width) + arrow(p.panX+l.inner < d.width, " ▶")
	}
	var body []string
	if d.err != nil {
		body = []string{tui.Failed.Render("no preview: " + d.err.Error())}
	}
	for _, line := range d.lines {
		if d.width <= l.inner {
			body = append(body, strings.Repeat(" ", (l.inner-d.width)/2)+line) // centred
		} else {
			body = append(body, ansi.Cut(line, p.panX, p.panX+l.inner))
		}
	}
	right = append(right, tui.Box(title, false, body, l.right, l.schematic)...)

	var table, timetable []string
	if l.table > 0 {
		title, body = p.scheduleBox(sc, l)
		table = tui.Box(title, p.focus == focusTrains, body, l.table, l.schedule)
	}
	if l.timetable > 0 {
		title, body = p.timetableBox(sc, l)
		timetable = tui.Box(title, false, body, l.timetable, l.schedule)
	}
	for i := range l.schedule {
		right = append(right, tui.At(table, i)+tui.At(timetable, i))
	}

	var left []string
	if l.list > 0 {
		left = tui.Box("scenarios", p.focus == focusList, p.listLines(), l.list, l.body)
	}
	lines := make([]string, l.body)
	for i := range lines {
		lines[i] = tui.At(left, i) + tui.At(right, i)
	}
	lines = append(lines, p.keyLine())
	return strings.Join(lines, "\n")
}

func (p *picker) keyLine() string {
	keys := " ↑↓ scenario  tab trains  enter run  v replay  e edit  c copy  n new  r rule  ←→ pan  z overview/layout  pgup/pgdn schedule  q quit"
	if p.focus == focusTrains {
		keys = " ↑↓ train  tab/esc scenarios  enter run  v replay  e edit  c copy  n new  r rule  ←→ pan  z overview/layout  pgup/pgdn page  q quit"
	}
	if p.run != nil {
		keys = " esc cancel the run  ctrl+c quit"
	}
	return tui.Dim.Render(ansi.Truncate(keys, p.width, ""))
}

func (p *picker) listLines() []string {
	var out []string
	for i, sc := range p.scenarios {
		line := tui.Fit(" "+sc.Name, listWidth-4)
		if i == p.cursor {
			line = tui.Bold.Reverse(true).Faint(p.focus != focusList).Render(line)
		}
		mark := " "
		switch {
		case sc.Err != nil:
			mark = tui.Failed.Render("✗")
		case p.ranAny(sc):
			mark = tui.Good.Render("✓")
		}
		out = append(out, line+mark)
	}
	return out
}

// ranAny reports whether the scenario has been run under any rule, in this
// session or before.
func (p *picker) ranAny(sc *scenario.Scenario) bool {
	for _, r := range runs.Rules {
		if rec, _ := runs.Files(p.outDir, sc, r); p.results[sc.Path+"|"+r] != nil || runs.IsFile(rec) {
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
	about := tui.Wrap(tui.ValueOr(sc.Description, tui.Dim.Render("no description")), widths[0]-4, 4)
	if sc.Err != nil {
		about = tui.Wrap(tui.Failed.Render("does not read: "+sc.Err.Error()), widths[0]-4, 4)
	} else if sc.Timetable != "" {
		about = append(tui.Wrap(tui.ValueOr(sc.Description, tui.Dim.Render("no description")), widths[0]-4, 3), tui.Accent.Render("runs a timetable"))
	}
	boxes := []struct {
		title string
		lines []string
	}{
		{title, about},
		{"precedence rule", append([]string{p.ruleTabs(), ""}, tui.DimAll(tui.Wrap(p.ruleExplanation(sc, runs.Rules[p.rule]), widths[1]-4, 4))...)},
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
			out = append(out, tui.Box(b.title, false, pad(b.lines), widths[i], len(b.lines)+2)...)
		}
		return out
	}
	h := 0
	for _, b := range boxes {
		h = max(h, len(b.lines)+2)
	}
	drawn := make([][]string, len(boxes))
	for i, b := range boxes {
		drawn[i] = tui.Box(b.title, false, pad(b.lines), widths[i], h)
	}
	out := make([]string, h)
	for i := range out {
		out[i] = tui.At(drawn[0], i) + tui.At(drawn[1], i) + tui.At(drawn[2], i)
	}
	return out
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
	for i, r := range runs.Rules {
		label := " " + runs.RuleLabel(r) + " "
		if i == p.rule {
			tabs = append(tabs, tui.Accent.Bold(true).Reverse(true).Render(label))
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
	return runs.RuleExplanation(pr.cfg, rule, sc.Timetable != "")
}

// runBox is the run box's title and lines: trainapp's progress while it
// runs, else the last run's figures under the selected rule.
func (p *picker) runBox(w int) (string, []string) {
	r := p.run
	if r == nil {
		var lines []string
		title := "run · " + runs.RuleLabel(runs.Rules[p.rule])
		switch res := p.result(); {
		case p.scenarios[p.cursor].Err != nil:
			lines = []string{tui.Dim.Render("cannot run"), "", tui.Dim.Render("e opens it in trainedit")}
		case res == nil:
			lines = []string{tui.Dim.Render("not run yet under this rule"), "", tui.Dim.Render("enter runs it")}
		case res.Stale:
			title = "last run · " + runs.RuleLabel(runs.Rules[p.rule]) + " · stale"
			lines = []string{tui.Amber.Render("ran before the last edit"), "", tui.Dim.Render("enter runs it again, v replays the old run")}
		default:
			title = "last run · " + runs.RuleLabel(runs.Rules[p.rule])
			lines = runs.SummaryLines(res)
			if res.Failed != "" {
				lines = append(lines, tui.Failed.Render(ansi.Truncate(res.Failed, w, "…")))
			}
		}
		if p.note != "" {
			lines = append(lines, tui.Amber.Render(p.note))
		}
		return title, lines
	}

	spin := tui.Accent.Render(tui.Spinner[p.spin%len(tui.Spinner)])
	elapsed := time.Since(r.Started).Truncate(time.Second)
	title := fmt.Sprintf("running · %s · %d:%02d", runs.RuleLabel(r.Rule), int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	pr := r.Progress
	switch {
	case r.Cancelled:
		return title, []string{spin + " cancelling…"}
	case r.Measuring:
		return title, []string{tui.Bar(1, w), "", spin + " working out the KPIs"}
	case !pr.OK:
		return title, []string{spin + " starting trainapp…"}
	}
	return title, []string{
		tui.Bar(float64(pr.Reached)/float64(max(pr.Points, 1)), w), "",
		spin + fmt.Sprintf(" %d/%d points", pr.Reached, pr.Points),
		tui.Figure("sim time", fmt.Sprintf("%.1f min", pr.T)),
		tui.Figure("done", trains(pr.Done)),
		tui.Figure("waiting", trains(pr.Waiting)),
	}
}

func trains(n int) string {
	if n == 1 {
		return "1 train"
	}
	return fmt.Sprintf("%d trains", n)
}

// scheduleBox is the schedule box's title and lines: a table of the trains,
// columns as wide as their widest entry, and a key to the class colours.
func (p *picker) scheduleBox(sc *scenario.Scenario, l screenLayout) (string, []string) {
	s := p.previews.schedule(sc)
	title := fmt.Sprintf("schedule · %d trains", len(s.trains))
	if sc.Timetable != "" {
		title = fmt.Sprintf("timetable · %d trains", len(s.trains))
	}
	if len(s.trains) > l.rows {
		title += fmt.Sprintf(" · %d-%d", p.schedTop+1, min(p.schedTop+l.rows, len(s.trains)))
	}
	if s.err != nil {
		return title, []string{tui.Failed.Render(" no schedule: " + s.err.Error())}
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
		line := " " + tui.Fit(number, 7) + tui.Fit(class, classW+2) + tui.Fit(entry, 7) + tui.Fit(route, routeW+2)
		if disW > 0 {
			line += tui.Fit(dis, disW+2)
		}
		return line + stops
	}
	out := []string{tui.Dim.Render(cols("train", "class", "entry", "route", "disruption", "stops"))}

	end := min(p.schedTop+l.rows, len(s.trains))
	for i := min(p.schedTop, end); i < end; i++ {
		t := s.trains[i]
		if i == p.train && p.focus == focusTrains {
			out = append(out, tui.Bold.Reverse(true).Render(tui.Fit(cols(t.number, "■ "+tui.ValueOr(t.class, "—"), t.entry, t.from+"→"+t.to,
				strings.Join(append([]string{}, t.disruptions...), ", "), strings.Join(t.stops, " ")), l.table-2)))
			continue
		}
		number, dis := t.number, ""
		if len(t.disruptions) > 0 {
			number, dis = tui.Amber.Render(t.number), tui.Amber.Render("⚠ "+strings.Join(t.disruptions, ", "))
		}
		class := tui.Dim.Render("—")
		if t.class != "" {
			class = lipgloss.NewStyle().Foreground(lipgloss.Color(s.color[t.class])).Render("■ ") + t.class
		}
		stops := strings.Join(t.stops, " ")
		if t.passes > 0 {
			stops += tui.Dim.Render(fmt.Sprintf(" +%d passes", t.passes))
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
			item += tui.Dim.Render(fmt.Sprintf(" %d", pri))
		}
		key = append(key, item)
	}
	if len(sc.Disruptions) > 0 {
		key = append(key, tui.Amber.Render("⚠ disrupted"))
	}
	return title, append(out, " "+strings.Join(key, "  "))
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
	var calls []runs.Call
	if res := p.result(); res != nil && !res.Stale {
		calls = res.Calls[t.number]
	}
	title := fmt.Sprintf("timetable · %s · %s→%s", t.number, t.from, t.to)

	cols := func(stn, kind, pf, arr, dep, plan, actual, note string) string {
		return " " + tui.Fit(stn, 6) + tui.Fit(kind, 5) + tui.Fit(pf, 4) + tui.Fit(arr, 6) + tui.Fit(dep, 6) + tui.Fit(plan, 8) + tui.Fit(actual, 7) + note
	}
	planned := func(t float64) string {
		if t < 0 {
			return tui.Dim.Render("—")
		}
		return tui.Dim.Render(tui.Clock(t))
	}
	out := []string{tui.Dim.Render(cols("stn", "kind", "pf", "arr", "dep", "planned", "actual", ""))}
	for i, sp := range t.points {
		kind := "stop"
		if sp.IsPass() {
			kind = tui.Dim.Render("pass")
		}
		pf, arr, dep, plan, actual := tui.Dim.Render(tui.ValueOr(sp.SpPfNo, "—")), planned(sp.ArrTime), planned(sp.DeptTime), "", ""
		if !sp.IsPass() {
			plan = fmt.Sprintf("%g", sp.DwellTime) // the schedule's, so not dimmed
		}
		if i < len(calls) {
			c := calls[i]
			pf, arr = tui.ValueOr(c.Pf, pf), tui.Clock(c.Arr)
			if c.Dep >= 0 {
				dep = tui.Clock(c.Dep)
				if stood := c.Dep - c.Arr; !sp.IsPass() || stood >= 0.05 { // a pass that stopped shows its stand
					actual = fmt.Sprintf("%.1f", stood)
					if stood > sp.DwellTime+0.5 || sp.IsPass() { // stood longer than planned
						actual = tui.Amber.Render(actual)
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
			note = tui.Amber.Render("⚠ " + strings.Join(notes, ", "))
		}
		out = append(out, cols(string(sp.StnCode), kind, pf, arr, dep, plan, actual, note))
	}

	h := l.schedule - 2 // inside the box
	key := tui.Dim.Render(" stops in min; run it for the actuals")
	if len(calls) > 0 {
		key = tui.Dim.Render(" stops in min; times as run under " + runs.RuleLabel(runs.Rules[p.rule]))
	}
	if len(out) >= h {
		out = out[:max(h-1, 1)]
	}
	for len(out) < h-1 {
		out = append(out, "")
	}
	return title, append(out, key)
}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
