// Command trainedit edits a scenario: its world on the lane schematic
// trainviz draws, its trains, and the scenario itself, which it runs with
// trainapp and replays with trainviz as traindemo does. traindemo opens it
// with e (the selected scenario) and n (a new one).
//
//	trainedit -scenario scenarios/sl-12.json      edit a scenario
//	trainedit -new [-from scenarios/sl-12.json]   a new scenario in -dir, from another's files
//	trainedit -new -world tpjkkdi-sl              a new scenario on a world
//
// The world is a snapshot (infra.Snapshot): a built-in world is saved as a
// new snapshot beside the scenario the first time it changes. Saving a world
// or schedule other scenarios use asks whether the change is for them all.
package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"path/filepath"
	"trainapp/visualization/runs"
	"trainapp/worlds"

	tea "charm.land/bubbletea/v2"
)

func main() {
	log.SetFlags(0)
	path := flag.String("scenario", "", "scenario file to edit")
	fresh := flag.Bool("new", false, "start a new scenario in -dir")
	dir := flag.String("dir", "scenarios", "with -new, the directory the scenario goes in")
	from := flag.String("from", "", "with -new, a scenario whose world, schedule and profile to start from")
	world := flag.String("world", "", "with -new, the world to start from: tpjkkdi, tpjkkdi-sl or a world snapshot (.json); empty for an empty world")
	savedTo := flag.String("saved-to", "", "write the path of the scenario saved last to this file (for traindemo)")
	binDir := flag.String("bin", "", "directory with trainapp and trainviz; empty to look next to trainedit, then in dist/, then on $PATH")
	outDir := flag.String("out", filepath.Join("logs", "demo"), "directory for the runs' recordings and KPIs, shared with traindemo")
	flag.Parse()

	var s *session
	var err error
	switch {
	case *path != "" && *fresh:
		log.Fatal("give -scenario to edit one, or -new to start one, not both")
	case *path != "":
		s, err = openSession(*path)
	case *fresh || *world != "":
		s, err = newSession(*dir, *from)
		if err == nil && *world != "" && *from == "" {
			err = s.setWorld(*world)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}

	cfg := config{outDir: *outDir, savedTo: *savedTo}
	hint := "build it with make build-trainedit, or give -bin"
	var errApp, errViz error
	cfg.trainapp, errApp = runs.FindBin("trainapp", *binDir, hint)
	cfg.trainviz, errViz = runs.FindBin("trainviz", *binDir, hint)
	cfg.binErr = errors.Join(errApp, errViz)

	m := newModel(s, cfg)
	if _, err := tea.NewProgram(m).Run(); errors.Is(err, tea.ErrInterrupted) {
		os.Exit(runs.ExitInterrupted)
	} else if err != nil {
		log.Fatal(err)
	}
	if m.interrupted {
		os.Exit(runs.ExitInterrupted)
	}
}

// setWorld starts a new scenario on a world.
func (s *session) setWorld(name string) error {
	if filepath.Ext(name) == ".json" {
		snap, err := readSnapshot(name)
		if err != nil {
			return err
		}
		abs, _ := filepath.Abs(name)
		s.sc.World = s.sc.Ref(abs)
		s.world = newDoc(snap)
		return nil
	}
	w, err := worlds.Load(name)
	if err != nil {
		return err
	}
	snap, err := w.Snapshot()
	if err != nil {
		return err
	}
	s.sc.World, s.builtin = name, true
	s.world = newDoc(snap)
	return nil
}
