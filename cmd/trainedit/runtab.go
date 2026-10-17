package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"trainapp/railway/scenario"
	"trainapp/visualization/runs"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// runTab is the scenario as a whole: its name, files and profile, and
// running it under a rule, as traindemo does, exporting its timetable and
// taking that timetable into the scenario.
type runTab struct {
	m       *model
	field   int // the scenario field picked
	rule    int // index into runs.Rules
	run     *runs.Run
	spin    int
	results map[string]*runs.Result // by rule
	scratch map[string]bool         // the rule's last run was of unsaved changes
}

type (
	tickMsg    time.Time
	vizDoneMsg struct{ err error }
)

func newRunTab(m *model) *runTab {
	return &runTab{m: m, results: map[string]*runs.Result{}, scratch: map[string]bool{}}
}

func (r *runTab) running() bool { return r.run != nil }

func (r *runTab) ruleName() string { return runs.Rules[r.rule] }

// scenarioFields are the lines of the scenario box: label, value, and
// whether it can be edited here.
func (r *runTab) scenarioFields() []struct {
	label, value string
	edit         bool
} {
	sc := r.m.s.sc
	dis := "none"
	if n := len(sc.Disruptions); n > 0 {
		dis = fmt.Sprintf("%d (Schedules tab: x adds, X clears)", n)
	}
	return []struct {
		label, value string
		edit         bool
	}{
		{"name", sc.Name, true},
		{"description", tui.ValueOr(sc.Description, "—"), true},
		{"profile", tui.ValueOr(sc.Profile, "none"), true},
		{"world", worldLabel(r.m.s), false},
		{"schedule", tui.ValueOr(sc.Schedule, "none yet (saved with the scenario)"), false},
		{"timetable", tui.ValueOr(sc.Timetable, "none: runs the schedule"), false},
		{"disruptions", dis, false},
		{"file", sc.Path, false},
	}
}

func (r *runTab) key(msg tea.KeyPressMsg) tea.Cmd {
	if r.run != nil {
		if msg.String() == "esc" {
			r.run.Cancel()
		}
		return nil
	}
	switch msg.String() {
	case "up", "k":
		r.field = max(r.field-1, 0)
	case "down", "j":
		r.field = min(r.field+1, len(r.scenarioFields())-1)
	case "e":
		r.editField()
	case "r":
		r.rule = (r.rule + 1) % len(runs.Rules)
	case "R":
		r.rule = (r.rule + len(runs.Rules) - 1) % len(runs.Rules)
	case "enter":
		return r.start(false)
	case "t":
		return r.start(true)
	case "u":
		r.useTimetable()
	case "U":
		if r.m.s.sc.Timetable == "" {
			r.m.say("the scenario runs its schedule already")
			break
		}
		if r.m.s.sc.Schedule == "" {
			r.m.fail("the scenario has no schedule to go back to")
			break
		}
		r.m.s.sc.Timetable = ""
		r.m.s.scDirty = true
		r.m.s.loadTimetable()
		r.m.say("the scenario runs its schedule again (its timetable file stays)")
	case "v":
		return r.replay()
	}
	return nil
}

func (r *runTab) editField() {
	s := r.m.s
	switch r.scenarioFields()[r.field].label {
	case "name":
		r.m.open(newForm("scenario name", func(v values) error { return s.rename(v["name"]) }, text("name", "name", s.sc.Name)))
	case "description":
		r.m.open(newForm("description", func(v values) error {
			s.sc.Description, s.scDirty = v["text"], true
			return nil
		}, text("text", "description", s.sc.Description)))
	case "profile":
		choices := []string{"none"}
		for _, p := range profiles(s.dir) {
			choices = append(choices, s.sc.Ref(p))
		}
		if s.sc.Profile != "" && !slices.Contains(choices, s.sc.Profile) {
			choices = append(choices, s.sc.Profile)
		}
		r.m.open(newForm("profile", func(v values) error {
			s.sc.Profile = v["profile"]
			if s.sc.Profile == "none" {
				s.sc.Profile = ""
			}
			s.scDirty = true
			s.loadProfile()
			return s.profileErr
		}, choice("profile", "profile", tui.ValueOr(s.sc.Profile, "none"), choices...)))
	default:
		r.m.say("the %s follows what you edit and save", r.scenarioFields()[r.field].label)
	}
}

// profiles are the profile files beside the scenarios (../profiles).
func profiles(dir string) []string {
	ps, _ := filepath.Glob(filepath.Join(dir, "..", "profiles", "*.json"))
	return ps
}

// target is the scenario to run: its file if everything is saved, else a
// scratch copy of it as it stands.
func (r *runTab) target() (*scenario.Scenario, bool, error) {
	s := r.m.s
	if !s.dirty() && !s.isNew {
		return s.sc, false, nil
	}
	sc, err := s.scratch(filepath.Join("logs", "trainedit"))
	return sc, true, err
}

func (r *runTab) start(export bool) tea.Cmd {
	if r.m.cfg.trainapp == "" {
		r.m.fail("no trainapp: " + r.m.cfg.binErr.Error())
		return nil
	}
	if ps := r.m.world.problems; len(ps) > 0 {
		r.m.fail(fmt.Sprintf("the world has %d problems (World tab, [ and ] go to them)", len(ps)))
		return nil
	}
	for _, t := range r.m.s.sched.Trains {
		if len(checkTrain(r.m.s.world.S, t)) > 0 && r.m.s.sc.Timetable == "" {
			r.m.fail("train " + t.Number + " has problems (Schedules tab)")
			return nil
		}
	}
	sc, scratch, err := r.target()
	if err != nil {
		r.m.fail("cannot write the scenario to run: " + err.Error())
		return nil
	}
	run, err := runs.Start(r.m.cfg.trainapp, sc, r.ruleName(), r.m.cfg.outDir, runs.Options{Timetable: export})
	if err != nil {
		r.m.fail("cannot run trainapp: " + err.Error())
		return nil
	}
	run.Key = r.ruleName()
	r.run = run
	r.scratch[run.Key] = scratch
	return tea.Batch(run.Next, tick())
}

func (r *runTab) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tickMsg:
		if r.run != nil {
			r.spin++
			return tick()
		}
	case runs.ProgressMsg, runs.OutputMsg:
		if r.run != nil {
			r.run.Handle(msg)
			return r.run.Next
		}
	case runs.DoneMsg:
		run := r.run
		r.run = nil
		if run.Cancelled {
			run.Discard()
			delete(r.results, run.Key)
			r.m.say("run cancelled")
			if r.m.interrupted {
				return tea.Quit
			}
			return nil
		}
		res := run.Finish(msg.Err)
		r.results[run.Key] = res
		switch {
		case res.Failed != "":
			r.m.fail(res.Failed)
		case res.Timetable != nil:
			r.m.say("timetable exported: u makes the scenario run it")
		default:
			r.m.say("run done: v replays it")
		}
	case vizDoneMsg:
		if runs.Interrupted(msg.err) {
			r.m.interrupted = true
			return tea.Quit
		}
		if msg.err != nil {
			r.m.fail("trainviz: " + msg.err.Error())
		}
	}
	return nil
}

// result is the last run under the selected rule: this session's, or one
// read back from the runs' directory.
func (r *runTab) result() *runs.Result {
	key := r.ruleName()
	if res, ok := r.results[key]; ok {
		return res
	}
	res := runs.LoadResult(r.m.cfg.outDir, r.m.s.sc, key)
	r.results[key] = res
	return res
}

func (r *runTab) useTimetable() {
	path := runs.TimetableFile(r.m.cfg.outDir, r.m.s.sc, r.ruleName())
	if !runs.IsFile(path) {
		r.m.fail("no timetable exported under " + runs.RuleLabel(r.ruleName()) + " yet: t exports one")
		return
	}
	if err := r.m.s.useTimetable(path); err != nil {
		r.m.fail("cannot use the timetable: " + err.Error())
		return
	}
	r.m.say("the scenario now runs %s (save to keep it)", r.m.s.sc.Timetable)
}

func (r *runTab) replay() tea.Cmd {
	if r.m.cfg.trainviz == "" {
		r.m.fail("no trainviz: " + r.m.cfg.binErr.Error())
		return nil
	}
	rec, _ := runs.Files(r.m.cfg.outDir, r.m.s.sc, r.ruleName())
	if !runs.IsFile(rec) {
		r.m.fail("nothing to replay yet: enter runs it")
		return nil
	}
	return runs.Replay(r.m.cfg.trainviz, rec, func(err error) tea.Msg { return vizDoneMsg{err} })
}

func (r *runTab) render(width, h int) []string {
	lw := width / 2
	// the scenario
	var sc []string
	for i, f := range r.scenarioFields() {
		line := " " + tui.Fit(f.label, 13) + f.value
		if !f.edit {
			line = " " + tui.Dim.Render(tui.Fit(f.label, 13)) + f.value
		}
		if i == r.field {
			line = tui.Bold.Reverse(true).Render(tui.Fit(ansi.Strip(line), lw-2))
		}
		sc = append(sc, ansi.Truncate(line, lw-2, "…"))
	}
	if err := r.m.s.profileErr; err != nil {
		sc = append(sc, tui.Failed.Render(" profile: "+err.Error()))
	}
	if err := r.m.s.ttErr; err != nil {
		sc = append(sc, tui.Failed.Render(" timetable: "+err.Error()))
	}
	if r.m.s.tt != nil {
		sc = append(sc, "", tui.Accent.Render(fmt.Sprintf(" runs %d timed trains", len(r.m.s.tt.Trains)))+" · check "+runs.CheckText(r.m.s.tt.Check))
	}
	top := tui.Box("scenario", true, sc, lw, len(sc)+2)

	// the rule
	var tabs []string
	for i, rl := range runs.Rules {
		label := " " + runs.RuleLabel(rl) + " "
		if i == r.rule {
			tabs = append(tabs, tui.Accent.Bold(true).Reverse(true).Render(label))
		} else {
			tabs = append(tabs, label)
		}
	}
	expl := runs.RuleExplanation(r.m.s.profile, r.ruleName(), r.m.s.sc.Timetable != "")
	rl := append([]string{" " + strings.Join(tabs, ""), ""}, tui.DimAll(prefix(tui.Wrap(expl, width-lw-4, 4)))...)
	ruleBox := tui.Box("precedence rule", false, rl, width-lw, max(len(rl)+2, len(sc)+2))
	out := tui.Side([]int{lw, width - lw}, top, ruleBox)

	// the run
	title, lines := r.runLines(width - 4)
	out = append(out, tui.Box(title, false, prefix(lines), width, max(h-len(out), 3))...)
	return out
}

func prefix(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = " " + l
	}
	return out
}

func (r *runTab) runLines(w int) (string, []string) {
	run := r.run
	if run != nil {
		spin := tui.Accent.Render(tui.Spinner[r.spin%len(tui.Spinner)])
		elapsed := time.Since(run.Started).Truncate(time.Second)
		title := fmt.Sprintf("running · %s · %d:%02d", runs.RuleLabel(run.Rule), int(elapsed.Minutes()), int(elapsed.Seconds())%60)
		if run.Timetable != "" {
			title += " · exporting the timetable"
		}
		pr := run.Progress
		switch {
		case run.Cancelled:
			return title, []string{spin + " cancelling…"}
		case run.Measuring:
			return title, []string{tui.Bar(1, w), "", spin + " working out the KPIs" + map[bool]string{true: ", then the timetable and its check run"}[run.Timetable != ""]}
		case !pr.OK:
			return title, []string{spin + " starting trainapp…"}
		}
		return title, []string{
			tui.Bar(float64(pr.Reached)/float64(max(pr.Points, 1)), w), "",
			spin + fmt.Sprintf(" %d/%d points", pr.Reached, pr.Points),
			tui.Figure("sim time", fmt.Sprintf("%.1f min", pr.T)),
			tui.Figure("done", fmt.Sprintf("%d trains", pr.Done)),
			tui.Figure("waiting", fmt.Sprintf("%d trains", pr.Waiting)),
		}
	}
	title := "run · " + runs.RuleLabel(r.ruleName())
	res := r.result()
	if res == nil {
		return title, []string{tui.Dim.Render("not run yet under this rule"), "",
			tui.Dim.Render("enter runs it, t runs it and exports its timetable" + map[bool]string{true: " (unsaved changes run from a scratch copy)"}[r.m.s.dirty()])}
	}
	title = "last run · " + runs.RuleLabel(r.ruleName())
	var lines []string
	switch {
	case res.Stale:
		title += " · stale"
		lines = []string{tui.Amber.Render("ran before the last edit: enter runs it again, v replays the old run")}
	default:
		lines = runs.SummaryLines(res)
		if r.scratch[r.ruleName()] {
			lines = append(lines, tui.Dim.Render("(of the unsaved changes)"))
		}
	}
	if res.Failed != "" {
		lines = append(lines, tui.Failed.Render(ansi.Truncate(res.Failed, w, "…")))
	}
	if tt := res.Timetable; tt != nil && tt.Check != nil {
		for i, l := range tt.Check.Late {
			if i == 4 {
				lines = append(lines, tui.Dim.Render(fmt.Sprintf("  … %d more late", len(tt.Check.Late)-i)))
				break
			}
			lines = append(lines, tui.Amber.Render(fmt.Sprintf("  %s up to %.1f min late (%s %s)", l.Train, l.LateMin, l.Event, l.Stn)))
		}
		if len(tt.Check.Unfinished) > 0 {
			lines = append(lines, tui.Failed.Render("  unfinished: "+strings.Join(tt.Check.Unfinished, ", ")))
		}
		lines = append(lines, "", tui.Dim.Render("u makes the scenario run this timetable"))
	}
	return title, lines
}

func (r *runTab) keyLine() string {
	return " ↑↓ field  e edit it  r/R rule  enter run  t run and export the timetable  u use the timetable  U back to the schedule  v replay"
}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
