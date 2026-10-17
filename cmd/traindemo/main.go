// Command traindemo picks a scenario to show, with previews of its world and
// schedule, runs it with trainapp and replays it with trainviz. Quitting
// trainviz with q comes back to the picker; ctrl+c quits everything.
//
//	traindemo [-dir scenarios] [-rule thresholds] [-bin dist]
//
// It runs trainapp and trainviz as they are, so build them first (make
// build-traindemo builds all three into dist/). Runs are written to
// logs/demo/: the recording, for trainviz replay, and the KPIs.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"trainapp/railway/kpi"
	"trainapp/railway/scenario"

	tea "charm.land/bubbletea/v2"
)

// exitInterrupted is the exit status after ctrl+c, as trainviz uses it.
const exitInterrupted = 130

// rules are the precedence rules r cycles through; "" is the profile's.
var rules = []string{"", "thresholds", "weighted", scenario.RuleOff}

func main() {
	log.SetFlags(0)
	dir := flag.String("dir", "scenarios", "directory of scenario files (JSON)")
	rule := flag.String("rule", "", "precedence rule to start with: thresholds, weighted or off; empty for the profile's")
	binDir := flag.String("bin", "", "directory with trainapp and trainviz; empty to look next to traindemo, then in dist/, then on $PATH")
	outDir := flag.String("out", filepath.Join("logs", "demo"), "directory for the runs' recordings and KPIs")
	flag.Parse()

	scenarios, err := scenario.LoadDir(*dir)
	if err != nil {
		log.Fatal(err)
	}
	ruleIdx := slices.Index(rules, *rule)
	if ruleIdx < 0 {
		log.Fatalf("unknown rule %q, want thresholds, weighted or off", *rule)
	}
	trainapp, err := findBin("trainapp", *binDir)
	if err != nil {
		log.Fatal(err)
	}
	trainviz, err := findBin("trainviz", *binDir)
	if err != nil {
		log.Fatal(err)
	}

	p := newPicker(scenarios, ruleIdx)
	for {
		p.choice = choiceNone
		if _, err := tea.NewProgram(p).Run(); errors.Is(err, tea.ErrInterrupted) {
			os.Exit(exitInterrupted)
		} else if err != nil {
			log.Fatal(err)
		}
		switch p.choice {
		case choiceQuit:
			return
		case choiceInterrupt:
			os.Exit(exitInterrupted)
		}
		r := demoRun{sc: p.scenarios[p.cursor], rule: rules[p.rule], outDir: *outDir, trainapp: trainapp, trainviz: trainviz}
		p.results[p.resultKey()] = r.do()
	}
}

// findBin finds one of the other commands: in dir if set, else next to this
// one, in dist/ or on $PATH.
func findBin(name, dir string) (string, error) {
	if dir != "" {
		p := filepath.Join(dir, name)
		if !isFile(p) {
			return "", fmt.Errorf("no %s in %s", name, dir)
		}
		return p, nil
	}
	var tried []string
	if exe, err := os.Executable(); err == nil {
		tried = append(tried, filepath.Dir(exe))
	}
	tried = append(tried, "dist")
	for _, d := range tried {
		if p := filepath.Join(d, name); isFile(p) {
			return filepath.Abs(p)
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no %s next to traindemo, in dist/ or on $PATH: build it with make build-traindemo, or give -bin", name)
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// demoRun is one scenario run under one rule: trainapp, then trainviz.
type demoRun struct {
	sc                 *scenario.Scenario
	rule               string
	outDir             string
	trainapp, trainviz string
}

// result is how a run went, for the picker.
type result struct {
	report *kpi.Report // nil if trainapp wrote none
	failed string      // why trainapp or trainviz failed, if one did
	files  string      // where the run was written
}

// do runs the scenario with trainapp in the terminal, so its progress and
// KPIs show, then replays it in trainviz. A run that fails but leaves a
// recording, such as a deadlock, is still replayed. ctrl+c in either quits
// traindemo too.
func (r demoRun) do() *result {
	name := r.sc.Name + "-" + ruleLabel(r.rule)
	rec := filepath.Join(r.outDir, name+".ndjson")
	kpiPath := filepath.Join(r.outDir, name+".kpi.json")
	res := &result{files: filepath.Join(r.outDir, name+".*")}
	if err := os.MkdirAll(r.outDir, 0o755); err != nil {
		res.failed = err.Error()
		return res
	}
	for _, p := range []string{rec, kpiPath} {
		os.Remove(p) // not to show an earlier run's
	}

	fmt.Printf("\n▶ %s, rule %s\n\n", r.sc.Name, ruleLabel(r.rule))
	args := []string{"-scenario", r.sc.Path, "-record", rec, "-kpi", kpiPath}
	if r.rule != "" {
		args = append(args, "-rule", r.rule)
	}
	if err := runAttached(r.trainapp, args...); err != nil {
		res.failed = "trainapp: " + err.Error()
	}
	res.report = readReport(kpiPath)
	if !isFile(rec) {
		if res.failed == "" {
			res.failed = "trainapp wrote no recording"
		}
		fmt.Print("\nno recording to replay; press enter to go back ")
		fmt.Scanln()
		return res
	}
	if err := runAttached(r.trainviz, "replay", rec); err != nil && res.failed == "" {
		res.failed = "trainviz: " + err.Error()
	}
	return res
}

// runAttached runs a command on this terminal. If it was quit with ctrl+c,
// so is traindemo.
func runAttached(bin string, args ...string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == exitInterrupted {
		os.Exit(exitInterrupted)
	}
	return err
}

// readReport reads the run's KPIs from trainapp's -kpi file, nil if there
// are none.
func readReport(path string) *kpi.Report {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var reports map[string]*kpi.Report
	if json.NewDecoder(f).Decode(&reports) != nil {
		return nil
	}
	return reports["run"]
}

// ruleLabel names a rule in file names and on screen.
func ruleLabel(rule string) string {
	if rule == "" {
		return "profile"
	}
	return rule
}
