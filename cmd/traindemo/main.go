// Command traindemo picks a scenario to show, with previews of its world and
// schedule, runs it with trainapp, drawing its progress, and replays it with
// trainviz. Quitting trainviz with q comes back to the picker; ctrl+c quits
// everything. e opens the selected scenario in trainedit, and n starts a new
// one from it; the picker reads the scenarios again when trainedit quits.
//
//	traindemo [-dir scenarios] [-rule thresholds] [-bin dist]
//
// It runs trainapp, trainviz and trainedit as they are, so build them first
// (make build-traindemo builds them all into dist/). Runs are written to
// logs/demo/: the recording, for trainviz replay, and the KPIs.
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"slices"
	"trainapp/visualization/runs"

	tea "charm.land/bubbletea/v2"
)

const buildHint = "build it with make build-traindemo, or give -bin"

func main() {
	log.SetFlags(0)
	dir := flag.String("dir", "scenarios", "directory of scenario files (JSON)")
	rule := flag.String("rule", "", "precedence rule to start with: thresholds, weighted or off; empty for the profile's")
	binDir := flag.String("bin", "", "directory with trainapp, trainviz and trainedit; empty to look next to traindemo, then in dist/, then on $PATH")
	outDir := flag.String("out", filepath.Join("logs", "demo"), "directory for the runs' recordings and KPIs")
	flag.Parse()

	ruleIdx := slices.Index(runs.Rules, *rule)
	if ruleIdx < 0 {
		log.Fatalf("unknown rule %q, want thresholds, weighted or off", *rule)
	}
	trainapp, err := runs.FindBin("trainapp", *binDir, buildHint)
	if err != nil {
		log.Fatal(err)
	}
	trainviz, err := runs.FindBin("trainviz", *binDir, buildHint)
	if err != nil {
		log.Fatal(err)
	}
	// trainedit is only needed to edit; without it e and n say how to get it
	trainedit, editErr := runs.FindBin("trainedit", *binDir, buildHint)

	p, err := newPicker(*dir, ruleIdx, bins{trainapp: trainapp, trainviz: trainviz, trainedit: trainedit, editErr: editErr}, *outDir)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := tea.NewProgram(p).Run(); errors.Is(err, tea.ErrInterrupted) {
		os.Exit(runs.ExitInterrupted)
	} else if err != nil {
		log.Fatal(err)
	}
	if p.choice == choiceInterrupt {
		os.Exit(runs.ExitInterrupted)
	}
}

// bins are the commands the picker runs.
type bins struct {
	trainapp, trainviz, trainedit string
	editErr                       error // why there is no trainedit
}
