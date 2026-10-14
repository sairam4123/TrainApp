// Command trainviz draws a world as a line schematic in the terminal and
// replays recordings made with trainapp -record.
//
//	go run ./cmd/trainviz                       pan and zoom the world
//	go run ./cmd/trainviz -print -zoom 2        print it once
//	go run ./cmd/trainviz replay run.ndjson     play a recording
//	go run ./cmd/trainviz replay -print -at 34.5 run.ndjson
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"trainapp/railway/infra"
	"trainapp/visualization/schematic"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var builders = map[string]func() *infra.World{
	"tpjkkdi": worlds.BuildTpjKkdiWorld,
	"test":    worlds.BuildTestWorld,
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
		fmt.Fprintln(fs.Output(), "usage: trainviz [flags]\n       trainviz replay [flags] recording.ndjson")
		fs.PrintDefaults()
	}
	worldName := fs.String("world", "tpjkkdi", "world to draw: "+strings.Join(names, ", "))
	pr := printFlags(fs)
	fs.Parse(args)

	build, ok := builders[*worldName]
	if !ok {
		log.Fatalf("unknown world %q, want one of %s", *worldName, strings.Join(names, ", "))
	}
	d, err := schematic.FromWorld(build())
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
	at := fs.Float64("at", -1, "with -print: the sim time to show (default the end)")
	pr := printFlags(fs)
	fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
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
	if !*pr.print {
		run(newModel(d, rep))
		return
	}

	rep.seek(rep.end)
	if *at >= 0 {
		rep.seek(*at)
	}
	color := pr.output(d, schematic.Options{State: rep.state, Time: rep.t})
	fmt.Printf("\nt=%.2f\n", rep.t)
	m := &model{rep: rep, selected: -1}
	for _, line := range m.trainLines(60, len(rep.trains)*2) {
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
	opt.Color = *p.color == "always"
	if *p.color == "auto" {
		fi, err := os.Stdout.Stat()
		opt.Color = err == nil && fi.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == ""
	}
	for _, line := range d.Render(opt) {
		fmt.Println(line)
	}
	return opt.Color
}

func run(m *model) {
	if _, err := tea.NewProgram(m).Run(); err != nil {
		log.Fatal(err)
	}
}
