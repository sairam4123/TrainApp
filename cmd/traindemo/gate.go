package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"trainapp/railway/kpi/report"
	"trainapp/railway/scenario"
	"trainapp/visualization/runs"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
)

// traindemo is the gate to comparing runs: x picks scenarios under rules
// into the compare set, and X opens them side by side in trainviz compare,
// or b saves them as the baseline. Picked runs that are missing or ran
// before their scenario's last edit are offered to be made first, one
// after another, and the comparison opens once they all are.

// gateAction is what waits on the gate.
type gateAction int

const (
	gateCompare gateAction = iota
	gateBaseline
)

// gate is the runs an action waits on: asked about, then made in turn.
type gate struct {
	action         gateAction
	picks          []runs.Pick // what the action is on
	missing, stale []runs.Pick
	asking         bool
	queue          []runs.Pick // left to make
	total          int         // runs to make in all
}

// ruleInitial is a rule's letter in the scenario list's set marks.
func ruleInitial(rule string) string { return runs.RuleLabel(rule)[:1] }

// inSet is where the scenario under the rule is in the set, -1 if not.
func (p *picker) inSet(sc *scenario.Scenario, rule string) int {
	key := runs.Pick{Scenario: sc, Rule: rule}.Key()
	return slices.IndexFunc(p.set, func(q runs.Pick) bool { return q.Key() == key })
}

// pick adds the selected scenario under the selected rule to the set, or
// takes it out.
func (p *picker) pick() {
	sc := p.scenarios[p.cursor]
	if sc.Err != nil {
		p.note = "the scenario does not read: e opens it in trainedit"
		return
	}
	rule := runs.Rules[p.rule]
	if i := p.inSet(sc, rule); i >= 0 {
		p.set = slices.Delete(p.set, i, i+1)
		return
	}
	p.set = append(p.set, runs.Pick{Scenario: sc, Rule: rule})
}

// pickAll adds the selected scenario under every rule, or takes them all
// out if they are all in.
func (p *picker) pickAll() {
	sc := p.scenarios[p.cursor]
	if sc.Err != nil {
		p.note = "the scenario does not read: e opens it in trainedit"
		return
	}
	all := !slices.ContainsFunc(runs.Rules, func(r string) bool { return p.inSet(sc, r) < 0 })
	for _, r := range runs.Rules {
		i := p.inSet(sc, r)
		switch {
		case all:
			p.set = slices.Delete(p.set, i, i+1)
		case i < 0:
			p.set = append(p.set, runs.Pick{Scenario: sc, Rule: r})
		}
	}
}

// setMarks are the initials of the scenario's rules in the set, "" if none.
func (p *picker) setMarks(sc *scenario.Scenario) string {
	var out string
	for _, r := range runs.Rules {
		if p.inSet(sc, r) >= 0 {
			out += ruleInitial(r)
		}
	}
	return out
}

// setLine shows the set in the order added: plain with a run, dim with ○
// without one, ⟳ where its run is stale.
func (p *picker) setLine() string {
	missing, stale := runs.Missing(p.outDir, p.set)
	var parts []string
	for _, q := range p.set {
		has := func(l []runs.Pick) bool {
			return slices.ContainsFunc(l, func(o runs.Pick) bool { return o.Key() == q.Key() })
		}
		switch {
		case has(missing):
			parts = append(parts, tui.Dim.Render("○ "+q.Label()))
		case has(stale):
			parts = append(parts, q.Label()+tui.Amber.Render(" ⟳"))
		default:
			parts = append(parts, q.Label())
		}
	}
	return " " + tui.Accent.Render("compare:") + " " + strings.Join(parts, ", ") + tui.Dim.Render("   X opens  b baseline  esc clears")
}

// openGate starts an action on the picks: straight away if their runs are
// all made, else asking first.
func (p *picker) openGate(action gateAction, picks []runs.Pick) tea.Cmd {
	missing, stale := runs.Missing(p.outDir, picks)
	g := &gate{action: action, picks: picks, missing: missing, stale: stale}
	if len(missing)+len(stale) == 0 {
		return p.passGate(g)
	}
	g.asking = true
	p.gate = g
	return nil
}

// gatePrompt asks about the runs to make first.
func (g *gate) prompt() string {
	n := len(g.missing) + len(g.stale)
	what := []string{}
	if len(g.missing) > 0 {
		what = append(what, fmt.Sprintf("%d missing", len(g.missing)))
	}
	if len(g.stale) > 0 {
		what = append(what, fmt.Sprintf("%d stale", len(g.stale)))
	}
	runsWord := "runs"
	if n == 1 {
		runsWord = "run"
	}
	s := fmt.Sprintf(" %d %s to make first (%s): enter runs them", n, runsWord, strings.Join(what, ", "))
	if len(g.stale) > 0 {
		s += "  s skip the stale ones"
	}
	return s + "  esc cancel"
}

// gateKey answers the gate's question.
func (p *picker) gateKey(key string) tea.Cmd {
	g := p.gate
	switch key {
	case "enter":
		g.queue = append(slices.Clone(g.missing), g.stale...)
	case "s":
		if len(g.stale) == 0 {
			return nil
		}
		g.queue = slices.Clone(g.missing)
	case "esc", "q":
		p.gate = nil
		return nil
	case "ctrl+c":
		p.choice = choiceInterrupt
		return tea.Quit
	default:
		return nil
	}
	g.asking, g.total = false, len(g.queue)
	return p.nextGateRun()
}

// nextGateRun makes the next run the gate waits on, or passes the gate
// when none is left.
func (p *picker) nextGateRun() tea.Cmd {
	g := p.gate
	if len(g.queue) == 0 {
		p.gate = nil
		return p.passGate(g)
	}
	q := g.queue[0]
	g.queue = g.queue[1:]
	r, err := runs.Start(p.bins.trainapp, q.Scenario, q.Rule, p.outDir, runs.Options{})
	if err != nil {
		p.gate = nil
		p.note = "cannot run trainapp: " + err.Error()
		return nil
	}
	r.Key = q.Key()
	p.run = r
	return tea.Batch(r.Next, tick())
}

// gateRunDone takes in how a gate's run went: on to the next, or the gate
// shut if it failed.
func (p *picker) gateRunDone(r *runs.Run, res *runs.Result) tea.Cmd {
	if res.Failed != "" {
		p.gate = nil
		p.note = fmt.Sprintf("%s·%s failed, so nothing opens: x takes it out of the set", r.Scenario.Name, runs.RuleLabel(r.Rule))
		return nil
	}
	return p.nextGateRun()
}

// passGate does the action.
func (p *picker) passGate(g *gate) tea.Cmd {
	switch g.action {
	case gateBaseline:
		p.saveBaseline(g.picks)
		return nil
	}
	p.viewing = ""
	return runs.Compare(p.bins.trainviz, p.outDir, g.picks, func(err error) tea.Msg { return vizDoneMsg{err} })
}

// compareSet opens the set side by side.
func (p *picker) compareSet() tea.Cmd {
	if len(p.set) < 2 {
		p.note = "pick at least two runs with x"
		return nil
	}
	return p.openGate(gateCompare, slices.Clone(p.set))
}

// baselinePicks are what b saves: the set, else every run made so far.
func (p *picker) baselinePicks() []runs.Pick {
	if len(p.set) > 0 {
		return slices.Clone(p.set)
	}
	var out []runs.Pick
	for _, sc := range p.scenarios {
		if sc.Err != nil {
			continue
		}
		for _, r := range runs.Rules {
			if _, kpi := runs.Files(p.outDir, sc, r); runs.IsFile(kpi) {
				out = append(out, runs.Pick{Scenario: sc, Rule: r})
			}
		}
	}
	return out
}

// startBaseline saves the baseline once its runs are made.
func (p *picker) startBaseline() tea.Cmd {
	picks := p.baselinePicks()
	if len(picks) == 0 {
		p.note = "no runs to save as the baseline: enter runs one"
		return nil
	}
	return p.openGate(gateBaseline, picks)
}

// saveBaseline writes the picks' runs as the baseline, in trainbench's
// format, to the output dir (never over a file given with -against), and
// uses it from now on.
func (p *picker) saveBaseline(picks []runs.Pick) {
	rows := runs.Bench(p.outDir, picks)
	path := runs.BaselineFile(p.outDir)
	f, err := os.Create(path)
	if err == nil {
		err = errors.Join(report.WriteBench(f, rows), f.Close())
	}
	if err != nil {
		p.note = "cannot save the baseline: " + err.Error()
		return
	}
	p.baseline, p.baselinePath = rows, path
	p.note = fmt.Sprintf("baseline: %d runs saved", len(rows))
}

// loadBaseline reads the baseline: the bench file given, else the one b
// saved in the output dir, if there is one.
func (p *picker) loadBaseline(path string) error {
	if path == "" {
		path = runs.BaselineFile(p.outDir)
		if !runs.IsFile(path) {
			return nil
		}
	}
	rows, err := report.ReadBench(path)
	if err != nil {
		return err
	}
	p.baseline, p.baselinePath = rows, path
	return nil
}
