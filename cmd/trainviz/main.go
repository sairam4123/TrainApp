// Command trainviz draws a world as a line schematic in the terminal and
// replays recordings made with trainapp -record.
//
//	go run ./cmd/trainviz                       pan and zoom the world
//	go run ./cmd/trainviz -world world.json     ... a world snapshot (trainapp -save-world)
//	go run ./cmd/trainviz -print -zoom 2        print it once
//	go run ./cmd/trainviz replay run.ndjson     play a recording
//	go run ./cmd/trainviz replay -print -at 34.5 run.ndjson
//	go run ./cmd/trainviz replay -print -graph run.ndjson   time-distance graph
//	go run ./cmd/trainviz replay -page delay run.ndjson     open on the run's delays
//	go run ./cmd/trainviz replay -print -page occupancy run.ndjson
//	go run ./cmd/trainviz replay -print -page delay-causes run.ndjson   where and why the delay
//	go run ./cmd/trainviz compare a.ndjson b.ndjson [c.ndjson ...]     runs side by side
//	go run ./cmd/trainviz compare -print a.ndjson b.ndjson             their figures, once
//
// The delay and occupancy pages read the run's KPIs from the .kpi.json
// trainapp -kpi wrote beside the recording, or from -kpi.
package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"trainapp/railway/infra"
	"trainapp/visualization/schematic"
	"trainapp/visualization/traingraph"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var builders = map[string]func() *infra.World{
	"tpjkkdi":    worlds.BuildTpjKkdiWorld,
	"tpjkkdi-sl": worlds.BuildTpjKkdiSingleLineWorld,
	"tpjkkdi-dl": worlds.BuildTpjKkdiDoubleLineWorld,
	"test":       worlds.BuildTestWorld,
	"single": func() *infra.World {
		return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2})
	},
	"junction": func() *infra.World { return worlds.BuildJunctionWorld(2) },
}

var levels = map[string]schematic.Level{
	"overview": schematic.Overview,
	"layout":   schematic.Layout,
	"detail":   schematic.Detail,
}

func main() {
	log.SetFlags(0)
	if len(os.Args) > 1 && os.Args[1] == "replay" {
		replayMain(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "compare" {
		compareMain(os.Args[2:])
		return
	}
	viewMain(os.Args[1:])
}

func viewMain(args []string) {
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range builders {
			if !yield(name) {
				return
			}
		}
	})
	fs := flag.NewFlagSet("trainviz", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: trainviz [flags]\n       trainviz replay [flags] recording.ndjson\n       trainviz compare [flags] run.ndjson run.ndjson [run.ndjson ...]")
		fs.PrintDefaults()
	}
	worldName := fs.String("world", "tpjkkdi", "world to draw: "+strings.Join(names, ", ")+", or a world snapshot file (.json)")
	pr := printFlags(fs)
	fs.Parse(args)

	var world *infra.World
	if build, ok := builders[*worldName]; ok {
		world = build()
	} else if filepath.Ext(*worldName) == ".json" {
		var err error
		if world, err = infra.LoadWorld(*worldName); err != nil {
			log.Fatal(err)
		}
	} else {
		log.Fatalf("unknown world %q, want one of %s, or a world snapshot (.json)", *worldName, strings.Join(names, ", "))
	}
	d, err := schematic.FromWorld(world)
	if err != nil {
		log.Fatal(err)
	}
	if *pr.print {
		pr.output(d, schematic.Options{})
		return
	}
	run(newModel(d, nil))
}

func replayMain(args []string) {
	fs := flag.NewFlagSet("trainviz replay", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: trainviz replay [flags] recording.ndjson")
		fs.PrintDefaults()
	}
	at := fs.Float64("at", -1, "the sim time to show: with -print, default the end; else the time to open at")
	trainFlag := fs.String("train", "", "a train to select when the viewer opens")
	graph := fs.Bool("graph", false, "with -print: print the time-distance graph instead of the schematic")
	width := fs.Int("width", 120, "with -print -graph: width in columns")
	height := fs.Int("height", 30, "with -print -graph: height in lines")
	planned := fs.Bool("planned", false, "with -print -graph: mark the trains' planned times")
	pageName := fs.String("page", "schematic", "page to open on: schematic, graph, delay, delay-schematic, delay-causes, occupancy, occupancy-table or occupancy-causes; with -print, the delay and occupancy ones print that page")
	kpiFile := fs.String("kpi", "", "the run's KPIs (trainapp -kpi); empty for the .kpi.json beside the recording")
	pr := printFlags(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	table, delayMap := *pageName == "occupancy-table", *pageName == "delay-schematic"
	delayCauses, occCauses := *pageName == "delay-causes", *pageName == "occupancy-causes"
	switch {
	case table, occCauses:
		*pageName = "occupancy"
	case delayMap, delayCauses:
		*pageName = "delay"
	}
	start, ok := pageNames[*pageName]
	if !ok {
		log.Fatalf("unknown page %q, want schematic, graph, delay, delay-schematic, delay-causes, occupancy, occupancy-table or occupancy-causes", *pageName)
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	hello, msgs, err := vizproto.ReadAll(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	d, err := schematic.Build(&hello.World)
	if err != nil {
		log.Fatal(err)
	}
	rep := newReplay(hello, msgs)
	path := cmp.Or(*kpiFile, kpiPath(fs.Arg(0)))
	report, err := readReport(path)
	if err != nil && *kpiFile != "" {
		log.Fatal(err)
	}
	m := newModel(d, rep)
	if err != nil {
		m.st.missing = "no KPIs for this recording: run trainapp with -kpi " + path
	} else {
		m.st.set(report, rep)
	}
	m.st.table, m.st.dMap, m.st.dCauses, m.st.oCauses = table, delayMap, delayCauses, occCauses
	m.name = strings.TrimSuffix(filepath.Base(fs.Arg(0)), ".ndjson")
	if !*pr.print {
		if *at >= 0 {
			rep.seek(*at)
		}
		if i := slices.Index(rep.trains, *trainFlag); i >= 0 {
			m.selected, m.follow = i, true
		}
		m.setPage(start)
		m.note = ""
		run(m)
		return
	}
	if start == pageDelay || start == pageOccupancy {
		if report == nil {
			log.Fatal(m.st.missing)
		}
		printPage(m, d, start, pr, *width)
		return
	}

	rep.seek(rep.end)
	if *at >= 0 {
		rep.seek(*at)
	}
	if *graph {
		cor, err := d.Corridor()
		if err != nil {
			log.Fatal(err)
		}
		g := traingraph.Build(&hello.World, cor, msgs)
		from, to := g.Span()
		now := -1.0
		if *at >= 0 {
			now = rep.t
		}
		for _, line := range g.Draw(traingraph.Options{Width: *width, Height: *height, From: from, To: to, Now: now, Planned: *planned, Color: pr.useColor()}) {
			fmt.Println(strings.TrimRight(line, " "))
		}
		return
	}
	color := pr.output(d, schematic.Options{State: rep.state, Time: rep.t})
	fmt.Printf("\nt=%.2f\n", rep.t)
	m = &model{rep: rep, selected: -1}
	for _, line := range m.trainLines(60, len(rep.trains)*2) {
		if !color {
			line = ansi.Strip(line)
		}
		fmt.Println(strings.TrimRight(line, " "))
	}
}

// printPage prints the delay page, or the occupancy schematic with its key
// or table, w cells wide.
func printPage(m *model, d *schematic.Diagram, p page, pr printOptions, w int) {
	var lines []string
	color := pr.useColor()
	switch {
	case p == pageDelay && m.st.dMap:
		heat, notes := m.st.delayHeat()
		color = pr.output(d, schematic.Options{Heat: heat, StationNotes: notes})
		fmt.Println()
		lines = m.st.delaySide(40, 0)
	case p == pageDelay && m.st.dCauses:
		m.st.plain, m.st.dc.focus = true, -1
		lines = m.st.causesPage(true, w, 0, m.rep)
	case p == pageDelay:
		m.st.plain, m.st.dFocus = true, -1
		lines = m.st.delayPage(w, 0, m.rep)
	case m.st.oCauses:
		m.st.plain, m.st.oc.focus = true, -1
		lines = m.st.causesPage(false, w, 0, m.rep)
	case m.st.table:
		m.st.plain, m.st.oFocus = true, -1
		lines = m.st.occTablePage(w, 0)
	default:
		color = pr.output(d, schematic.Options{Heat: m.st.heat()})
		fmt.Println()
		lines = m.st.sideLines(40, 0)
	}
	for _, line := range lines {
		if !color {
			line = ansi.Strip(line)
		}
		fmt.Println(strings.TrimRight(line, " "))
	}
}

type printOptions struct {
	print *bool
	zoom  *int
	level *string
	color *string
}

func printFlags(fs *flag.FlagSet) printOptions {
	return printOptions{
		print: fs.Bool("print", false, "print once instead of opening the viewer"),
		zoom:  fs.Int("zoom", 1, "with -print: 1 is the most compact; higher widens every track"),
		level: fs.String("level", "layout", "with -print: overview, layout or detail"),
		color: fs.String("color", "auto", "with -print: auto, always or never"),
	}
}

// output prints the diagram and reports whether it used colour.
func (p printOptions) output(d *schematic.Diagram, opt schematic.Options) bool {
	level, ok := levels[*p.level]
	if !ok {
		log.Fatalf("unknown level %q, want overview, layout or detail", *p.level)
	}
	opt.Level, opt.Zoom = level, *p.zoom
	opt.Color = p.useColor()
	for _, line := range d.Render(opt) {
		fmt.Println(line)
	}
	return opt.Color
}

// useColor reports whether to print in colour.
func (p printOptions) useColor() bool {
	if *p.color != "auto" {
		return *p.color == "always"
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == ""
}

// exitInterrupted is the exit status after ctrl+c, as for a shell's SIGINT,
// so a launcher such as traindemo can tell it from q and quit too.
const exitInterrupted = 130

func run(m *model) {
	_, err := tea.NewProgram(m).Run()
	if errors.Is(err, tea.ErrInterrupted) || m.interrupted {
		os.Exit(exitInterrupted)
	}
	if err != nil {
		log.Fatal(err)
	}
}
