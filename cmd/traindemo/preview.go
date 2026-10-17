package main

import (
	"fmt"
	"math"
	"strings"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/railway/sim"
	"trainapp/visualization/schematic"
	"trainapp/worlds"

	"github.com/charmbracelet/x/ansi"
)

// diagramPreview is a world's schematic, drawn once.
type diagramPreview struct {
	lines []string
	width int
}

// schedulePreview is a scenario's trains, a row each.
type schedulePreview struct {
	header string
	rows   []string
}

// previews builds each preview the first time it is shown and keeps it.
type previews struct {
	diagrams  map[string]*diagramPreview  // by world and level
	schedules map[string]*schedulePreview // by scenario file
	rules     map[string]string           // the profile's rule, by profile file
}

func newPreviews() *previews {
	return &previews{diagrams: map[string]*diagramPreview{}, schedules: map[string]*schedulePreview{}, rules: map[string]string{}}
}

func (p *previews) diagram(sc *scenario.Scenario, level schematic.Level) *diagramPreview {
	key := sc.WorldName() + "|" + level.String()
	if d, ok := p.diagrams[key]; ok {
		return d
	}
	d := &diagramPreview{}
	if lines, err := drawWorld(sc.WorldName(), level); err != nil {
		d.lines = []string{failed.Render(" no preview: " + err.Error())}
	} else {
		d.lines = lines
	}
	for _, line := range d.lines {
		d.width = max(d.width, ansi.StringWidth(line))
	}
	p.diagrams[key] = d
	return d
}

func drawWorld(name string, level schematic.Level) ([]string, error) {
	w, err := worlds.Load(name)
	if err != nil {
		return nil, err
	}
	d, err := schematic.FromWorld(w)
	if err != nil {
		return nil, err
	}
	return d.Render(schematic.Options{Level: level, Zoom: 1, Color: true}), nil
}

func (p *previews) schedule(sc *scenario.Scenario) *schedulePreview {
	if s, ok := p.schedules[sc.Path]; ok {
		return s
	}
	s := &schedulePreview{header: fmt.Sprintf(" %-6s %-18s %5s  %-11s %-18s %s", "train", "class", "entry", "route", "disruption", "stops")}
	if trains, err := ops.LoadTrains(sc.SchedulePath()); err != nil {
		s.rows = []string{failed.Render(" no schedule: " + err.Error())}
	} else {
		s.rows = scheduleRows(trains, sc.Disruptions)
	}
	p.schedules[sc.Path] = s
	return s
}

// scheduleRows is a row per train: its class, when it enters, where from
// and to, any disruption to it and where it stops on the way.
func scheduleRows(trains []*ops.Train, disruptions []ops.Disruption) []string {
	hit := map[string][]string{}
	for _, d := range disruptions {
		text := fmt.Sprintf("late +%g", d.Min)
		if d.Kind == ops.DisruptStand {
			text = fmt.Sprintf("stand %s +%g", d.Stn, d.Min)
		}
		hit[d.Train] = append(hit[d.Train], text)
	}
	var rows []string
	for _, t := range trains {
		points := t.Schedule()
		if len(points) == 0 {
			continue
		}
		first, last := points[0], points[len(points)-1]
		entry := "—"
		if first.ArrTime >= 0 {
			entry = clock(first.ArrTime)
		}
		var stops []string
		passes := 0
		for _, sp := range points[1 : len(points)-1] {
			if sp.IsPass() {
				passes++
			} else {
				stops = append(stops, string(sp.StnCode))
			}
		}
		calls := strings.Join(stops, " ")
		if passes > 0 {
			calls += dim.Render(fmt.Sprintf(" (+%d passes)", passes))
		}
		dis := fit("", 18)
		if h := hit[t.Number]; len(h) > 0 {
			dis = amber.Render(fit(strings.Join(h, ", "), 18))
		}
		rows = append(rows, fmt.Sprintf(" %-6s %-18s %5s  %-11s ", t.Number, ansi.Truncate(t.Class, 18, "…"), entry,
			first.StnCode+"→"+last.StnCode)+dis+" "+calls)
	}
	return rows
}

// clock shows minutes from 0 as HH:MM, as trainviz does.
func clock(t float64) string {
	mins := int(math.Round(t))
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// ruleText says which precedence rule a run will use.
func (p *previews) ruleText(sc *scenario.Scenario, rule string) string {
	switch rule {
	case "":
	case scenario.RuleOff:
		return "off (no precedence)"
	default:
		return rule
	}
	path := sc.ProfilePath()
	if path == "" {
		return "profile's: none, no profile"
	}
	if text, ok := p.rules[path]; ok {
		return text
	}
	text := "profile's: "
	switch cfg, err := sim.LoadPriorityConfig(path); {
	case err != nil:
		text += "? (" + err.Error() + ")"
	case cfg.Precedence == nil:
		text += "off"
	default:
		text += valueOr(cfg.Precedence.Rule, "thresholds") // the sim's default
	}
	p.rules[path] = text
	return text
}
