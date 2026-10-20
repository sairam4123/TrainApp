package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"trainapp/railway/kpi/report"

	"charm.land/lipgloss/v2"
)

// The delay and occupancy pages show the run's KPIs, read from the file
// trainapp -kpi wrote beside the recording, so they agree with trainapp's
// own report. They are worked out for the whole run, not up to the replay
// time. Both are laid out as the other apps are: a row of figure boxes on
// top, and below them tables, one focused at a time (tab), beside their
// details.

// readReport reads the run's KPIs from trainapp's -kpi file.
func readReport(path string) (*report.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reports map[string]*report.Report
	if err := json.Unmarshal(data, &reports); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r := reports["run"]
	if r == nil {
		return nil, fmt.Errorf("%s has no run", path)
	}
	return r, nil
}

// kpiPath is where trainapp writes a recording's KPIs: run.ndjson's are in
// run.kpi.json.
func kpiPath(rec string) string {
	return strings.TrimSuffix(rec, ".ndjson") + ".kpi.json"
}

// The delay page's tables, in the order tab moves between them.
const (
	dTrains   = iota
	dStations // the delay at each station
	dCalls    // the calls at the picked station
	dPlaces   // the waits by place
	dTables
)

// The occupancy table view's tables.
const (
	tableBlocks = iota
	tableStations
	tablePlatforms
	tableThroats
)

// stats are the KPI pages' data and where each page is.
type stats struct {
	rep     *report.Report // nil without KPIs
	missing string         // why there are none
	trackID map[string]string

	// the delay page
	dSort    int // into delaySorts
	dStnSort int // into stationSorts
	dFilter  int // into filters()
	dFocus   int // dTrains to dPlaces
	dSel     [dTables]int
	dOff     [dTables]int
	dMap     bool // the schematic rather than the tables
	dMetric  int  // into delayMetrics

	// the occupancy page
	metric  int  // into metrics
	table   bool // the tables rather than the schematic
	oSort   int  // into occSorts
	oFocus  int  // tableBlocks to tableThroats
	oSel    [4]int
	oOff    [4]int
	throats bool   // list the throats too
	focus   string // track id picked out on the schematic

	plain bool // printed: no row picked out
}

// sel is the row picked out of a table, -1 when printing.
func (s *stats) sel(row int) int {
	if s.plain {
		return -1
	}
	return row
}

func newStats(missing string) *stats {
	return &stats{missing: missing}
}

// set takes in the run's KPIs; the world names the tracks they are kept by.
func (s *stats) set(r *report.Report, rep *replay) {
	s.rep, s.trackID = r, map[string]string{}
	for id, name := range rep.trackName {
		s.trackID[name] = id
	}
}

// heatRamp colours a figure from low to high where the profile sets no
// colours, leaving out held trains' amber.
var heatRamp = []string{"33", "39", "42", "149", "203"}

// rampColor is the colour of the i-th of n steps.
func rampColor(i, n int) string {
	if n <= 1 {
		return heatRamp[len(heatRamp)-1]
	}
	return heatRamp[min(i*(len(heatRamp)-1)/(n-1), len(heatRamp)-1)]
}

func colored(c, s string) string {
	if c == "" {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(s)
}

// bar is a bar w cells wide, share of it filled, in colour c.
func bar(share float64, w int, c string) string {
	w = max(w, 1)
	filled := min(max(int(share*float64(w)+0.5), 0), w)
	return colored(cmp.Or(c, "39"), strings.Repeat("━", filled)) + dim.Render(strings.Repeat("─", w-filled))
}

func pct(share float64) string { return fmt.Sprintf("%.0f%%", 100*share) }

func mins(v float64) string { return fmt.Sprintf("%.1f", v) }

// optMins is a figure the train may not have, "—" if not.
func optMins(v *float64) string {
	if v == nil {
		return "—"
	}
	return mins(*v)
}
