package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/compare"
	"trainapp/visualization/schematic"
	"trainapp/visualization/traingraph"
	"trainapp/visualization/tui"
	"trainapp/visualization/vizproto"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// trainviz compare: several runs side by side, from their recordings and
// KPI files. Up to three show at a time, picked from the runs list; the
// first shown is the baseline (A), and every Δ is a run's figure less A's.
// Runs on one world compare on every page; on different worlds, only
// their whole-run figures and the stations the worlds share.

// maxShown is how many runs a page shows at once.
const maxShown = 3

// letters name the shown runs, the baseline first.
var letters = []string{"A", "B", "C"}

// cmpRun is one run on the command line.
type cmpRun struct {
	name  string // the recording's file name less .ndjson
	path  string
	hello *vizproto.HelloMsg
	msgs  []vizproto.Message
	rep   *replay
	kpis  *report.Report
	group int // its world's group, from 1 (compare.Worlds)

	d      *schematic.Diagram // drawn on first use; nil if it cannot be
	dErr   error
	graph  *traingraph.Graph
	gTried bool
}

// diagram lays out the run's world.
func (r *cmpRun) diagram() (*schematic.Diagram, error) {
	if r.d == nil && r.dErr == nil {
		r.d, r.dErr = schematic.Build(&r.hello.World)
	}
	return r.d, r.dErr
}

// trainGraph is the run's time-distance graph, nil if its world has no
// single line.
func (r *cmpRun) trainGraph() *traingraph.Graph {
	if !r.gTried {
		r.gTried = true
		if d, err := r.diagram(); err == nil {
			if cor, err := d.Corridor(); err == nil {
				r.graph = traingraph.Build(&r.hello.World, cor, r.msgs)
			}
		}
	}
	return r.graph
}

// loadRun reads a recording and the KPIs beside it.
func loadRun(path string) (*cmpRun, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	hello, msgs, err := vizproto.ReadAll(f)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	kpis, err := readReport(kpiPath(path))
	if err != nil {
		return nil, fmt.Errorf("no KPIs for %s: run trainapp with -kpi %s", path, kpiPath(path))
	}
	return &cmpRun{name: strings.TrimSuffix(filepath.Base(path), ".ndjson"), path: path, hello: hello, msgs: msgs, rep: newReplay(hello, msgs), kpis: kpis}, nil
}

// The compare pages.
type cmpPage int

const (
	cSummary cmpPage = iota
	cDelay
	cOcc
	cGraph
)

var cmpTabs = []string{"Summary", "Delay", "Occupancy", "Graph"}

var cmpPageNames = map[string]cmpPage{"summary": cSummary, "delay": cDelay, "occupancy": cOcc, "occupancy-table": cOcc, "graph": cGraph}

// The summary page's tables, in the order tab moves between them.
const (
	sRuns = iota
	sFigures
	sCauses
	sTables
)

// The delay page's tables.
const (
	cdTrains = iota
	cdStations
	cdPlaces
	cdTables
)

// The occupancy tables.
const (
	coBlocks = iota
	coStations
	coThroats
	coTables
)

type cmpModel struct {
	runs   []*cmpRun
	shown  []int // into runs; shown[0] is the baseline
	prefix string
	width  int
	height int
	page   cmpPage
	note   string
	ask    *replayAsk // the run to replay a train or place in, being asked
	exe    string     // this command, to open a replay with

	// worked out from the shown runs (refresh)
	reps     []*report.Report
	kinds    []compare.Kind
	oneWorld bool
	figs     []compare.Figure
	causes   []compare.CauseRow
	trains   []compare.TrainRow
	stations []compare.StationRow
	shared   []string // on different worlds, the stations they share
	places   []*compare.WaitPlace
	tracks   []compare.TrackRow

	sFocus int
	sSel   [sTables]int
	sOff   [sTables]int

	dFocus  int
	dSel    [cdTables]int
	dOff    [cdTables]int
	dSort   int
	dFilter int

	oTable  bool
	oMetric int // into metrics
	oOther  int // the shown run the schematic and graph set against A: 1 or 2
	oFocus  int
	oSel    [coTables]int
	oOff    [coTables]int
	oSort   int
	throats bool
	cam     *tui.Camera
	camOf   int // the run the camera draws

	gView   int // into graphViews
	gFrom   float64
	gPerCol float64
	gFitted bool
	gTrain  int // into graphTrains, -1 for none

	plain       bool // printed: no row picked out
	interrupted bool
}

// replayAsk is a train's or a place's wait to open in a run's replay.
type replayAsk struct {
	what  string              // for the prompt
	at    func(i int) float64 // the time to open shown run i at
	train string
}

func newCompare(runs []*cmpRun, shown []int) *cmpModel {
	m := &cmpModel{runs: runs, shown: shown, oOther: 1, gTrain: -1, camOf: -1}
	var ws []*vizproto.World
	var names []string
	for _, r := range runs {
		ws = append(ws, &r.hello.World)
		names = append(names, r.name)
	}
	for i, g := range compare.Worlds(ws) {
		runs[i].group = g
	}
	m.prefix = commonPrefix(names)
	if exe, err := os.Executable(); err == nil {
		m.exe = exe
	}
	m.refresh()
	return m
}

// commonPrefix is the "-"-separated parts every name starts with, with
// its "-"; "" if there are none, or a name is nothing more.
func commonPrefix(names []string) string {
	if len(names) < 2 {
		return ""
	}
	first := strings.Split(names[0], "-")
	n := len(first)
	for _, name := range names[1:] {
		parts := strings.Split(name, "-")
		k := 0
		for k < min(n, len(parts)) && parts[k] == first[k] {
			k++
		}
		n = k
		if n == len(parts) {
			return ""
		}
	}
	if n == 0 || n == len(first) {
		return ""
	}
	return strings.Join(first[:n], "-") + "-"
}

// label is a run's name less the prefix the runs share.
func (m *cmpModel) label(i int) string {
	return strings.TrimPrefix(m.runs[i].name, m.prefix)
}

// shownRuns are the shown runs, the baseline first.
func (m *cmpModel) shownRuns() []*cmpRun {
	out := make([]*cmpRun, len(m.shown))
	for k, i := range m.shown {
		out[k] = m.runs[i]
	}
	return out
}

// refresh works out what the shown runs compare on.
func (m *cmpModel) refresh() {
	runs := m.shownRuns()
	m.reps = nil
	groups := []int{}
	var ws []*vizproto.World
	for _, r := range runs {
		m.reps = append(m.reps, r.kpis)
		groups = append(groups, r.group)
		ws = append(ws, &r.hello.World)
	}
	m.kinds = compare.Kinds(m.reps, groups)
	m.oneWorld = !slices.ContainsFunc(groups, func(g int) bool { return g != groups[0] })
	m.figs = compare.Figures(m.reps, m.kinds)
	m.causes = compare.Causes(m.reps)
	slices.SortStableFunc(m.causes, func(a, b compare.CauseRow) int { return byDelta(a.Min, b.Min) })
	m.trains, m.places, m.tracks, m.shared = nil, nil, nil, nil
	if m.oneWorld {
		m.trains = compare.Trains(m.reps)
		m.places = compare.WaitPlaces(m.reps)
		slices.SortStableFunc(m.places, func(a, b *compare.WaitPlace) int { return byDelta(a.Min, b.Min) })
		m.tracks = compare.Tracks(m.reps)
		m.stations = compare.Stations(m.reps, nil)
	} else {
		m.shared = compare.SharedStations(ws)
		m.stations = compare.Stations(m.reps, m.shared)
	}
	m.oOther = min(m.oOther, max(len(m.shown)-1, 1))
	if !m.oneWorld && (m.page == cOcc || m.page == cGraph) {
		m.page = cSummary
	}
	if m.oneWorld && m.camOf != m.shown[0] {
		m.cam, m.camOf = nil, -1
	}
	if m.gTrain >= len(m.graphTrains()) {
		m.gTrain = -1
	}
}

// byDelta orders rows by their largest change from the baseline, by size.
func byDelta(a, b []float64) int {
	da, db := maxDelta(a), maxDelta(b)
	switch {
	case abs64(da) > abs64(db):
		return -1
	case abs64(da) < abs64(db):
		return 1
	}
	return 0
}

func maxDelta(vs []float64) float64 {
	ok := make([]bool, len(vs))
	for i := range ok {
		ok[i] = true
	}
	return compare.MaxDelta(vs, ok)
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// kindOf is how run i's trains compare with the baseline's.
func (m *cmpModel) kindOf(i int) string {
	base := m.runs[m.shown[0]]
	r := m.runs[i]
	switch {
	case i == m.shown[0]:
		return "baseline"
	case r.group != base.group:
		return compare.OtherWorld.String()
	}
	k := compare.Schedules(base.kpis, r.kpis)
	if k == compare.Retimed {
		return fmt.Sprintf("retimed: %d", compare.RetimedTrains(base.kpis, r.kpis))
	}
	return k.String()
}

// letterOf is run i's letter if it shows, else "".
func (m *cmpModel) letterOf(i int) string {
	if k := slices.Index(m.shown, i); k >= 0 {
		return letters[k]
	}
	return ""
}

// toggle shows or hides run i.
func (m *cmpModel) toggle(i int) {
	if k := slices.Index(m.shown, i); k >= 0 {
		if len(m.shown) == 1 {
			m.note = "one run at least shows"
			return
		}
		m.shown = slices.Delete(m.shown, k, k+1)
	} else {
		if len(m.shown) == maxShown {
			m.note = fmt.Sprintf("%d runs at most on a page: hide one first", maxShown)
			return
		}
		m.shown = append(m.shown, i)
	}
	m.refresh()
}

// setBaseline makes run i the baseline, showing it.
func (m *cmpModel) setBaseline(i int) {
	if k := slices.Index(m.shown, i); k >= 0 {
		m.shown = slices.Delete(m.shown, k, k+1)
	} else if len(m.shown) == maxShown {
		m.shown = m.shown[:maxShown-1]
	}
	m.shown = slices.Insert(m.shown, 0, i)
	m.refresh()
}

// tabOK is whether a page can show for the shown runs, and why not.
func (m *cmpModel) tabOK(p cmpPage) (bool, string) {
	switch {
	case p != cOcc && p != cGraph:
		return true, ""
	case !m.oneWorld:
		return false, "the shown runs are on different worlds: only the figures and shared stations compare"
	case len(m.shown) < 2:
		return false, "show a second run (space on the runs list)"
	case p == cGraph && m.runs[m.shown[0]].trainGraph() == nil:
		return false, "no time-distance graph: the world is not a single line"
	}
	if _, err := m.runs[m.shown[0]].diagram(); err != nil {
		return false, "no schematic: " + err.Error()
	}
	return true, ""
}

func (m *cmpModel) setPage(p cmpPage) {
	if ok, why := m.tabOK(p); !ok {
		m.note = why
		return
	}
	m.page = p
	if p == cGraph && !m.gFitted {
		m.fitGraph()
	}
}

func (m *cmpModel) Init() tea.Cmd { return nil }

func (m *cmpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.cam != nil {
			m.sizeCam()
		}
		if m.page == cGraph {
			m.fitGraph()
		}
	case tea.KeyPressMsg:
		return m, m.key(msg.String())
	case tea.MouseWheelMsg:
		d := map[bool]int{true: -3, false: 3}[msg.Button == tea.MouseWheelUp]
		switch {
		case m.page == cOcc && !m.oTable && m.cam != nil:
			m.cam.ZoomTo(m.cam.Step-d/3, msg.X-1, msg.Y-2)
		case m.page == cGraph:
			m.zoomGraph(msg.Button == tea.MouseWheelUp)
		default:
			m.scroll(d)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && msg.Y == 0 {
			if p, ok := m.tabAt(msg.X); ok {
				m.setPage(p)
			}
		}
	case replayDoneMsg:
		if msg.err != nil && !isInterrupt(msg.err) {
			m.note = "trainviz replay: " + msg.err.Error()
		}
		if isInterrupt(msg.err) {
			m.interrupted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// replayDoneMsg is a replay opened from here ending.
type replayDoneMsg struct{ err error }

func isInterrupt(err error) bool {
	ee, ok := err.(*exec.ExitError)
	return ok && ee.ExitCode() == exitInterrupted
}

func (m *cmpModel) key(key string) tea.Cmd {
	m.note = ""
	if m.ask != nil {
		return m.askKey(key)
	}
	switch key {
	case "q":
		return tea.Quit
	case "ctrl+c":
		m.interrupted = true
		return tea.Quit
	case "1", "2", "3", "4", "f1", "f2", "f3", "f4":
		n, _ := strconv.Atoi(strings.TrimPrefix(key, "f"))
		m.setPage(cmpPage(n - 1))
		return nil
	}
	switch m.page {
	case cSummary:
		m.summaryKey(key)
	case cDelay:
		m.delayKey(key)
	case cOcc:
		m.occKey(key)
	case cGraph:
		m.graphKey(key)
	}
	return nil
}

// askKey takes the run to open a replay in.
func (m *cmpModel) askKey(key string) tea.Cmd {
	a := m.ask
	m.ask = nil
	k := slices.Index([]string{"a", "b", "c"}, key)
	if k < 0 || k >= len(m.shown) {
		return nil
	}
	if m.exe == "" {
		m.note = "cannot find trainviz to open the replay"
		return nil
	}
	args := []string{"replay", "-at", fmt.Sprintf("%.2f", a.at(k))}
	if a.train != "" {
		args = append(args, "-train", a.train)
	}
	cmd := exec.Command(m.exe, append(args, m.runs[m.shown[k]].path)...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return replayDoneMsg{err} })
}

// tableMove moves a table's selection by key, and reports whether the key
// was one that does.
func tableMove(key string, sel *int, rows, page int) bool {
	switch key {
	case "up", "k":
		*sel--
	case "down", "j":
		*sel++
	case "pgdown", "space":
		*sel += page
	case "pgup":
		*sel -= page
	case "home":
		*sel = 0
	case "end":
		*sel = rows - 1
	default:
		return false
	}
	*sel = min(max(*sel, 0), max(rows-1, 0))
	return true
}

// scroll moves the focused table, for the mouse wheel.
func (m *cmpModel) scroll(d int) {
	key := map[bool]string{true: "down", false: "up"}[d > 0]
	for range max(d, -d) {
		switch m.page {
		case cSummary:
			m.summaryKey(key)
		case cDelay:
			m.delayKey(key)
		case cOcc:
			m.occKey(key)
		}
	}
}

func (m *cmpModel) pageRows() int { return max(m.height/3, 1) }

func (m *cmpModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *cmpModel) render() string {
	if m.width == 0 {
		return ""
	}
	h := max(m.height-2, 3)
	var body []string
	switch m.page {
	case cSummary:
		body = m.summaryPage(m.width, h)
	case cDelay:
		body = m.delayPage(m.width, h)
	case cOcc:
		body = m.occPage(m.width, h)
	case cGraph:
		body = m.graphPage(m.width, h)
	}
	for len(body) < h {
		body = append(body, "")
	}
	return strings.Join(append(append([]string{m.header()}, body[:h]...), m.keyLine()), "\n")
}

func (m *cmpModel) tabLabel(p cmpPage) string {
	label := fmt.Sprintf(" %d %s ", p+1, cmpTabs[p])
	if p == cOcc && m.oTable {
		label = fmt.Sprintf(" %d %s · tables ", p+1, cmpTabs[p])
	}
	return label
}

// header is the tab bar, then the runs shown.
func (m *cmpModel) header() string {
	var sb strings.Builder
	for p := range cmpPage(len(cmpTabs)) {
		label := m.tabLabel(p)
		ok, _ := m.tabOK(p)
		switch {
		case p == m.page:
			sb.WriteString(tui.Accent.Bold(true).Reverse(true).Render(label))
		case !ok:
			sb.WriteString(dim.Render(label))
		default:
			sb.WriteString(label)
		}
	}
	var names []string
	for _, i := range m.shown {
		names = append(names, m.label(i))
	}
	title := strings.Join(names, " ⟷ ")
	if m.prefix != "" {
		title = strings.TrimSuffix(m.prefix, "-") + " · " + title
	}
	sb.WriteString("   " + bold.Render("trainviz compare") + " · " + title)
	return ansi.Truncate(sb.String(), m.width, "")
}

func (m *cmpModel) tabAt(x int) (cmpPage, bool) {
	at := 0
	for p := range cmpPage(len(cmpTabs)) {
		w := ansi.StringWidth(m.tabLabel(p))
		if x >= at && x < at+w {
			return p, true
		}
		at += w
	}
	return 0, false
}

func (m *cmpModel) keyLine() string {
	shared := "  1-4 tabs  q quit"
	var own string
	switch {
	case m.ask != nil:
		var opts []string
		for k, i := range m.shown {
			opts = append(opts, strings.ToLower(letters[k])+" "+m.runs[i].name)
		}
		return " " + bold.Render("replay "+m.ask.what+" in: ") + strings.Join(opts, "  ") + dim.Render("  esc cancel")
	case m.page == cSummary:
		own = " ↑↓ row  tab table  space show/hide run  b baseline"
	case m.page == cDelay && m.oneWorld:
		own = " ↑↓ row  tab table  pgup/pgdn page  enter replay  c sort  f filter"
	case m.page == cDelay:
		own = " ↑↓ row"
	case m.page == cOcc && m.oTable:
		own = " ↑↓ row  tab table  enter show  c sort  t throats  v schematic"
	case m.page == cOcc:
		own = " +/- zoom  hjkl pan  0 fit  m metric  n run against A  v tables"
	case m.page == cGraph:
		own = " +/- time zoom  h l pan  0 fit  tab train  n run  o view"
	}
	if m.note != "" {
		own = " " + bold.Render(m.note) + "  " + dim.Render(own)
	} else {
		own = dim.Render(own)
	}
	room := max(m.width-ansi.StringWidth(shared), 0)
	return ansi.Truncate(own, room, "…") + dim.Render(shared)
}

// deltaCell is run i's Δ for a figure, green where better, red where
// worse.
func deltaCell(f compare.Figure, i int) string {
	t := f.DeltaText(i)
	switch v := f.Verdict(i); {
	case v > 0:
		return tui.Good.Render(t)
	case v < 0:
		return tui.Failed.Render(t)
	case t == "—":
		return dim.Render(t)
	}
	return t
}

// valueCell is run i's figure, dim where it has none or it does not
// compare fairly.
func valueCell(f compare.Figure, i int) string {
	v := f.Values[i]
	if !v.OK || !v.Fair {
		return dim.Render(f.Text(i))
	}
	return f.Text(i)
}

// delta is b less a as a Δ cell, coloured as a figure's.
func delta(a, b float64, okA, okB bool, unit compare.Unit, better compare.Better) string {
	return deltaCell(compare.Figure{Unit: unit, Better: better, Values: []compare.Value{{V: a, OK: okA, Fair: true}, {V: b, OK: okB, Fair: true}}}, 1)
}

// runHead heads a run's columns: its letter and name, cut to w.
func (m *cmpModel) runHead(k, w int) string {
	return ansi.Truncate(letters[k]+" "+m.label(m.shown[k]), w-1, "…")
}

func compareMain(args []string) {
	fs := flag.NewFlagSet("trainviz compare", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: trainviz compare [flags] run.ndjson run.ndjson [run.ndjson ...]\n\nEach run's KPIs are read from the .kpi.json beside its recording.")
		fs.PrintDefaults()
	}
	showFlag := fs.String("show", "", "the runs to show at the start, by place on the command line from 1, e.g. 1,3,4; the first is the baseline (default the first three)")
	pageName := fs.String("page", "summary", "page to open on: summary, delay, occupancy, occupancy-table or graph; with -print, summary, delay, occupancy or occupancy-table")
	width := fs.Int("width", 120, "with -print: width in columns")
	pr := printFlags(fs)
	fs.Parse(args)
	if fs.NArg() < 2 {
		fs.Usage()
		os.Exit(2)
	}
	start, ok := cmpPageNames[*pageName]
	if !ok {
		log.Fatalf("unknown page %q, want summary, delay, occupancy, occupancy-table or graph", *pageName)
	}
	var runs []*cmpRun
	for _, p := range fs.Args() {
		r, err := loadRun(p)
		if err != nil {
			log.Fatal(err)
		}
		runs = append(runs, r)
	}
	shown, err := parseShow(*showFlag, len(runs))
	if err != nil {
		log.Fatal(err)
	}
	m := newCompare(runs, shown)
	m.oTable = *pageName == "occupancy-table"
	if !*pr.print {
		m.setPage(start)
		m.note = ""
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil && !m.interrupted {
			log.Fatal(err)
		}
		if m.interrupted {
			os.Exit(exitInterrupted)
		}
		return
	}
	if ok, why := m.tabOK(start); !ok {
		log.Fatal(why)
	}
	m.plain, m.width = true, *width
	m.sFocus, m.dFocus, m.oFocus = -1, -1, -1
	var lines []string
	color := pr.useColor()
	switch start {
	case cSummary:
		lines = m.summaryPage(*width, 0)
	case cDelay:
		lines = m.delayPage(*width, 0)
	case cOcc:
		if m.oTable {
			lines = m.occTablePage(*width, 0)
			break
		}
		d, _ := m.runs[m.shown[0]].diagram()
		color = pr.output(d, schematic.Options{Heat: m.deltaHeat()})
		fmt.Println()
		lines = m.occSide(40, 0)
	default:
		log.Fatal("-print shows the summary, delay or occupancy pages")
	}
	for _, line := range lines {
		if !color {
			line = ansi.Strip(line)
		}
		fmt.Println(strings.TrimRight(line, " "))
	}
}

// parseShow reads -show: places from 1, at most maxShown of them.
func parseShow(s string, n int) ([]int, error) {
	if s == "" {
		var out []int
		for i := range min(n, maxShown) {
			out = append(out, i)
		}
		return out, nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		i, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || i < 1 || i > n {
			return nil, fmt.Errorf("-show: %q is not a run's place, 1 to %d", part, n)
		}
		if slices.Contains(out, i-1) {
			return nil, fmt.Errorf("-show: run %d twice", i)
		}
		out = append(out, i-1)
	}
	if len(out) > maxShown {
		return nil, fmt.Errorf("-show: %d runs at most", maxShown)
	}
	return out, nil
}
