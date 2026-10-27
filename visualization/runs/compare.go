package runs

import (
	"os/exec"
	"path/filepath"
	"trainapp/railway/kpi/report"
	"trainapp/railway/scenario"
	"trainapp/visualization/compare"

	tea "charm.land/bubbletea/v2"
)

// Pick is a scenario under a rule: one run, made or to make, of a
// comparison or a baseline.
type Pick struct {
	Scenario *scenario.Scenario
	Rule     string
}

// Key names the pick as the apps keep results: the scenario's path and
// the rule.
func (p Pick) Key() string { return p.Scenario.Path + "|" + p.Rule }

// Label names the pick on screen, e.g. "sl-6·off".
func (p Pick) Label() string { return p.Scenario.Name + "·" + RuleLabel(p.Rule) }

// Missing sorts out the picks that need running before they can be
// compared: those with no recording or KPIs in outDir, and those that ran
// before the scenario's last edit.
func Missing(outDir string, picks []Pick) (missing, stale []Pick) {
	for _, p := range picks {
		rec, kpi := Files(outDir, p.Scenario, p.Rule)
		switch {
		case !IsFile(rec) || !IsFile(kpi):
			missing = append(missing, p)
		case Stale(rec, p.Scenario):
			stale = append(stale, p)
		}
	}
	return missing, stale
}

// Compare opens the picks' runs side by side in trainviz, handing it the
// terminal; done makes the message for when it quits. args go to trainviz
// compare before the recordings.
func Compare(trainviz, outDir string, picks []Pick, done func(error) tea.Msg, args ...string) tea.Cmd {
	cmdArgs := append([]string{"compare"}, args...)
	for _, p := range picks {
		rec, _ := Files(outDir, p.Scenario, p.Rule)
		cmdArgs = append(cmdArgs, rec)
	}
	return tea.ExecProcess(exec.Command(trainviz, cmdArgs...), done)
}

// BaselineFile is where traindemo keeps its baseline: its runs' KPIs as a
// bench file, which trainbench -against and -diff read too.
func BaselineFile(outDir string) string { return filepath.Join(outDir, "baseline.bench.json") }

// Bench reads the picks' runs from outDir as a bench file's rows, leaving
// out those with no KPIs. Runs read back have no wall time.
func Bench(outDir string, picks []Pick) []report.BenchRun {
	var out []report.BenchRun
	for _, p := range picks {
		_, kpi := Files(outDir, p.Scenario, p.Rule)
		if r := ReadReport(kpi); r != nil {
			out = append(out, report.BenchRun{Scenario: p.Scenario.Name, Rule: RuleLabel(p.Rule), Report: r})
		}
	}
	return out
}

// BaselineFor is the baseline's run of the scenario under the rule, nil if
// it has none.
func BaselineFor(base []report.BenchRun, sc *scenario.Scenario, rule string) *report.Report {
	for _, b := range base {
		if b.Scenario == sc.Name && compare.RuleLabel(b.Rule) == RuleLabel(rule) {
			return b.Report
		}
	}
	return nil
}
