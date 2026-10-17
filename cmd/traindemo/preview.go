package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/railway/sim"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"
	"trainapp/worlds"

	"github.com/charmbracelet/x/ansi"
)

// diagramPreview is a world's schematic, drawn once.
type diagramPreview struct {
	lines []string
	width int
	err   error
}

// schedulePreview is a scenario's trains, and their classes.
type schedulePreview struct {
	trains   []trainRow
	classes  []string          // lowest priority first
	priority map[string]int    // each class's base priority, if the profile gives one
	color    map[string]string // by class
	err      error
}

// trainRow is what the schedule table shows of a train.
type trainRow struct {
	number, class string
	entry         string // HH:MM, or "—" if the schedule gives none
	from, to      string
	stops         []string // where it stops on the way, and for how long, e.g. "PDKT 2m"
	passes        int
	disruptions   []string // what happens to it, in words
	points        []*ops.SchedulePoint
	hits          []ops.Disruption
}

// profile is a profile file, read once.
type profile struct {
	cfg *sim.PriorityConfig
	err error
}

// previews builds each preview the first time it is shown and keeps it.
type previews struct {
	diagrams  map[string]*diagramPreview  // by world and level
	schedules map[string]*schedulePreview // by scenario file
	profiles  map[string]*profile         // by profile file
}

func newPreviews() *previews {
	return &previews{diagrams: map[string]*diagramPreview{}, schedules: map[string]*schedulePreview{}, profiles: map[string]*profile{}}
}

// errUnreadable stands in for a preview of a scenario that does not read.
var errUnreadable = errors.New("the scenario does not read")

func (p *previews) diagram(sc *scenario.Scenario, level schematic.Level) *diagramPreview {
	if sc.Err != nil {
		return &diagramPreview{err: errUnreadable}
	}
	key := sc.WorldName() + "|" + level.String()
	if d, ok := p.diagrams[key]; ok {
		return d
	}
	d := &diagramPreview{}
	d.lines, d.err = drawWorld(sc.WorldName(), level)
	for _, line := range d.lines {
		d.width = max(d.width, ansi.StringWidth(line))
	}
	p.diagrams[key] = d
	return d
}

// drawWorld draws the world, less any blank lines at the top and bottom.
func drawWorld(name string, level schematic.Level) ([]string, error) {
	w, err := worlds.Load(name)
	if err != nil {
		return nil, err
	}
	d, err := schematic.FromWorld(w)
	if err != nil {
		return nil, err
	}
	lines := d.Render(schematic.Options{Level: level, Zoom: 1, Color: true})
	blank := func(s string) bool { return strings.TrimSpace(ansi.Strip(s)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

func (p *previews) profile(sc *scenario.Scenario) *profile {
	path := sc.ProfilePath()
	if pr, ok := p.profiles[path]; ok {
		return pr
	}
	pr := &profile{}
	if path != "" {
		pr.cfg, pr.err = sim.LoadPriorityConfig(path)
	}
	p.profiles[path] = pr
	return pr
}

func (p *previews) schedule(sc *scenario.Scenario) *schedulePreview {
	if sc.Err != nil {
		return &schedulePreview{err: errUnreadable}
	}
	if s, ok := p.schedules[sc.Path]; ok {
		return s
	}
	s := &schedulePreview{}
	trains, err := sc.Trains()
	if err != nil {
		s.err = err
	} else {
		s.trains = trainRows(trains, sc.Disruptions)
		s.priority = map[string]int{}
		if cfg := p.profile(sc).cfg; cfg != nil {
			for _, t := range s.trains {
				if pri, ok := cfg.Classes[t.class]; ok {
					s.priority[t.class] = pri
				}
			}
		}
		s.classes, s.color = classColors(s.trains, s.priority)
	}
	p.schedules[sc.Path] = s
	return s
}

// trainRows is a row per train: its class, when it enters, where from and
// to, where it stops on the way and any disruption to it.
func trainRows(trains []*ops.Train, disruptions []ops.Disruption) []trainRow {
	hit := map[string][]ops.Disruption{}
	for _, d := range disruptions {
		hit[d.Train] = append(hit[d.Train], d)
	}
	var rows []trainRow
	for _, t := range trains {
		points := t.Schedule()
		if len(points) == 0 {
			continue
		}
		first, last := points[0], points[len(points)-1]
		r := trainRow{number: t.Number, class: t.Class, entry: "—", from: string(first.StnCode), to: string(last.StnCode), points: points, hits: hit[t.Number]}
		for _, d := range r.hits {
			r.disruptions = append(r.disruptions, disruptionText(d, true))
		}
		if first.ArrTime >= 0 {
			r.entry = tui.Clock(first.ArrTime)
		}
		for _, sp := range points[1 : len(points)-1] {
			if sp.IsPass() {
				r.passes++
			} else if sp.DwellTime > 0 {
				r.stops = append(r.stops, fmt.Sprintf("%s %gm", sp.StnCode, sp.DwellTime))
			} else {
				r.stops = append(r.stops, string(sp.StnCode))
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// disruptionText says what a disruption does, e.g. "stand PDKT +30", or
// "stand +30" without the station.
func disruptionText(d ops.Disruption, stn bool) string {
	if d.Kind != ops.DisruptStand {
		return fmt.Sprintf("late +%g", d.Min)
	}
	if stn {
		return fmt.Sprintf("stand %s +%g", d.Stn, d.Min)
	}
	return fmt.Sprintf("stand +%g", d.Min)
}

// classColors ranks the trains' classes by their priority in the profile,
// then by name, and spreads the palette over them, as trainviz does.
func classColors(rows []trainRow, priority map[string]int) ([]string, map[string]string) {
	var classes []string
	for _, r := range rows {
		if r.class != "" && !slices.Contains(classes, r.class) {
			classes = append(classes, r.class)
		}
	}
	slices.SortFunc(classes, func(a, b string) int {
		return cmp.Or(cmp.Compare(priority[a], priority[b]), cmp.Compare(a, b))
	})
	out := map[string]string{}
	for i, c := range classes {
		out[c] = tui.PaletteColor(i, len(classes))
	}
	return classes, out
}
