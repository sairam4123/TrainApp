// Package kpi measures a finished run against the three objectives: delay,
// line utilization and dispatching. Dispatching is read from the first two
// together, and from how the delay was taken: the waits, holds and signal
// stops, and where trains stood.
//
// Delay is measured against free-running time: how long each train takes
// with the line to itself (FreeRunning). A train's journey runs from its
// planned entry to when it leaves the world, so waiting to enter counts, and
// so does a late entry from a disruption. For timed trains, lateness against
// the timetable is reported too: how late each arrives at its last timed
// point, early counting as on time.
//
// Disruptions (ops.Disruption) are primary delay. Whatever delay the run has
// beyond them, on the disrupted trains and all the others, is reactionary:
// what the dispatching made of them.
//
// Utilization comes from a Collector watching the run; see utilization.go.
package kpi

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"trainapp/des"
	"trainapp/railway/kpi/report"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
)

// Config is the report's settings, the "kpi" section of a railway profile.
type Config struct {
	// PunctualWithinMin are the delays, in minutes, punctuality is reported
	// within: the share of trains no later than each.
	PunctualWithinMin []float64 `json:"punctual_within_min"`
	// CapacityColorBand names how busy a track was, by its occupancy, and
	// the colour trainviz draws it in. Empty for none.
	CapacityColorBand []CapacityBand `json:"capacity_color_band,omitempty"`
	// DelayColorBand does the same for how late a train was. Empty for none.
	DelayColorBand []DelayBand `json:"delay_color_band,omitempty"`
	// ParetoPct is the share of the minutes, in percent, trainviz's Pareto
	// charts mark off their vital few at.
	ParetoPct float64 `json:"pareto_pct,omitempty"`
}

// The report's types live in package report, which the viewers read it
// with without linking the sim.
type (
	Report       = report.Report
	Stats        = report.Stats
	Punctuality  = report.Punctuality
	Split        = report.Split
	Wait         = report.Wait
	Train        = report.Train
	Utilization  = report.Utilization
	Summary      = report.Summary
	StationUse   = report.StationUse
	TrackUse     = report.TrackUse
	CapacityBand = report.CapacityBand
	DelayBand    = report.DelayBand
	Call         = report.Call
	StationDelay = report.StationDelay
	BlockDelay   = report.BlockDelay
	Loss         = report.Loss
	Share        = report.Share
)

// Track kinds and wait kinds, as in package report.
const (
	KindBlock    = report.KindBlock
	KindPlatform = report.KindPlatform
	KindThroat   = report.KindThroat

	WaitEntry   = report.WaitEntry
	WaitSignal  = report.WaitSignal
	WaitStation = report.WaitStation
)

// Causes and places of losses, as in package report.
const (
	CauseDisruption = report.CauseDisruption
	CauseHeld       = report.CauseHeld
	CauseSignal     = report.CauseSignal
	CauseStation    = report.CauseStation
	CauseEntry      = report.CauseEntry
	CauseRunning    = report.CauseRunning
	CauseFree       = report.CauseFree
	CauseDwell      = report.CauseDwell
	CauseSlower     = report.CauseSlower

	RunSection = report.RunSection
	RunDwell   = report.RunDwell

	WhereStation = report.WhereStation
	WhereSection = report.WhereSection
	WhereEntry   = report.WhereEntry
)

// DefaultConfig is used when the profile has no kpi section.
func DefaultConfig() Config {
	return Config{PunctualWithinMin: []float64{1, 5, 15}, ParetoPct: report.DefaultParetoPct}
}

// LoadConfig reads the settings from a profile's raw "kpi" section; fields
// left out keep their defaults.
func LoadConfig(raw json.RawMessage) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("kpi: %w", err)
		}
	}
	return cfg, cfg.Validate()
}

// ConfigOf reads the settings from a profile, which may be nil.
func ConfigOf(prio *sim.PriorityConfig) (Config, error) {
	if prio == nil {
		return DefaultConfig(), nil
	}
	return LoadConfig(prio.KPI)
}

func (c Config) Validate() error {
	for _, m := range c.PunctualWithinMin {
		if !(m >= 0) {
			return errors.New("kpi: punctual_within_min must not be negative")
		}
	}
	if !(c.ParetoPct > 0 && c.ParetoPct <= 100) {
		return errors.New("kpi: pareto_pct must be over 0 and at most 100")
	}
	return errors.Join(
		validateBands("capacity_color_band", "over_pct", len(c.CapacityColorBand), func(i int) (float64, string, string) {
			b := c.CapacityColorBand[i]
			return b.OverPct, b.Name, b.Color
		}),
		validateBands("delay_color_band", "over_min", len(c.DelayColorBand), func(i int) (float64, string, string) {
			b := c.DelayColorBand[i]
			return b.OverMin, b.Name, b.Color
		}))
}

// validateBands checks the n bands of field: each from a different place,
// not negative, named, and coloured by an ANSI 256-colour number if at all.
func validateBands(field, over string, n int, band func(int) (from float64, name, color string)) error {
	seen := map[float64]bool{}
	for i := range n {
		from, name, color := band(i)
		switch {
		case !(from >= 0):
			return fmt.Errorf("kpi: %s %s must not be negative", field, over)
		case seen[from]:
			return fmt.Errorf("kpi: %s has two bands from %g", field, from)
		case name == "":
			return fmt.Errorf("kpi: %s needs a name for each band", field)
		}
		seen[from] = true
		if color != "" {
			if c, err := strconv.Atoi(color); err != nil || c < 0 || c > 255 {
				return fmt.Errorf("kpi: %s %s: color %q is not an ANSI 256-colour number", field, name, color)
			}
		}
	}
	return nil
}

// Options are what Compute measures against.
type Options struct {
	Config Config
	// Free is each train's free-running time, by number (FreeRunning). Nil
	// to report no delay against it.
	Free map[string]FreeRun
	// Disruptions are those applied to the run.
	Disruptions []ops.Disruption
}

// minWait is the shortest stand counted as a wait: shorter ones are the
// sim's event steps.
const minWait = 0.01

// Compute measures the finished run s, which c watched.
func Compute(s *sim.Sim, c *Collector, opts Options) *Report {
	end := s.CurTime()
	var order []string
	for _, st := range s.Status() {
		order = append(order, st.Train.Number)
	}
	r := &Report{Utilization: c.utilization(s.World(), end, order)}
	r.Utilization.Bands = slices.SortedFunc(slices.Values(opts.Config.CapacityColorBand), func(a, b CapacityBand) int {
		return cmp.Compare(a.OverPct, b.OverPct)
	})
	r.DelayBands = slices.SortedFunc(slices.Values(opts.Config.DelayColorBand), func(a, b DelayBand) int {
		return cmp.Compare(a.OverMin, b.OverMin)
	})

	r.ParetoPct = opts.Config.ParetoPct
	attr := newAttributor(s, r.Utilization.Stations, opts.Disruptions, end)
	pieces := map[string][]piece{}

	primary := map[string]float64{}
	for _, d := range opts.Disruptions {
		primary[d.Train] += d.Min
	}

	var trains []*ops.Train
	for _, st := range s.Status() {
		t := st.Train
		sched := t.Schedule()
		tk := Train{Train: t.Number, Class: t.Class, Priority: t.Priority, Finished: st.Phase == "exited",
			EntryMin: sched[0].ArrTime, ExitMin: -1, JourneyMin: -1, PrimaryMin: primary[t.Number]}
		free, hasFree := opts.Free[t.Number]
		if hasFree {
			tk.EntryMin = free.EntryMin
		}
		r.Trains++
		if tk.Finished {
			r.Finished++
			tk.ExitMin = sched[len(sched)-1].ActDeptTime
			tk.JourneyMin = tk.ExitMin - tk.EntryMin
			if hasFree {
				tk.FreeRunMin = ptr(free.JourneyMin)
				tk.DelayMin = ptr(tk.JourneyMin - free.JourneyMin)
			}
			if late, ok := lateness(t); ok {
				tk.LateMin = ptr(late)
			}
		} else {
			r.Unfinished = append(r.Unfinished, t.Number)
		}
		ws, points := waits(t, s.MinStop(), c.arrivedAt(t.Number), end)
		ps := attr.pieces(t, ws, points, c.arrivedAt(t.Number))
		pieces[t.Number] = ps
		tk.Waits = ws
		var fr *FreeRun
		if hasFree {
			fr = &free
		}
		tk.Calls = callsOf(t, fr)
		if tk.DelayMin != nil {
			tk.Losses = attr.losses(t, tk.Calls, fr, ps)
		}
		trains = append(trains, t)
		for _, w := range tk.Waits {
			tk.WaitMin += w.Min
			if r.LongestWait == nil || w.Min > r.LongestWait.Min {
				r.LongestWait = &w
			}
		}
		r.WaitMin += tk.WaitMin
		r.PerTrain = append(r.PerTrain, tk)
	}

	attr.splitOccupancy(&r.Utilization, c, pieces, opts.Free)
	attr.blockSections(&r.Utilization, trains)
	if opts.Free != nil {
		r.StationDelays = stationDelays(r.PerTrain, r.Utilization.Stations, opts.Config)
		r.BlockDelays = blockDelays(s.World(), trains, opts.Free, r.Utilization.Tracks)
	}
	r.Delay = stats(r.PerTrain, func(t Train) *float64 { return t.DelayMin }, opts.Config)
	r.Lateness = stats(r.PerTrain, func(t Train) *float64 { return t.LateMin }, opts.Config)
	if len(opts.Disruptions) > 0 && r.Delay != nil {
		sp := &Split{}
		for _, t := range r.PerTrain {
			if t.DelayMin != nil {
				sp.PrimaryMin += t.PrimaryMin
			}
		}
		sp.ReactionaryMin = r.Delay.TotalMin - sp.PrimaryMin
		r.Disruption = sp
	}

	for _, d := range s.Decisions() {
		switch d.Action {
		case sim.ActionHold:
			r.Holds++
			ended := d.Ended
			if d.Status == sim.StatusActive {
				ended = end
			}
			r.HeldMin += ended - d.Since
		case sim.ActionPlatform:
			r.PlatformChanges++
		}
	}
	return r
}

func ptr(v float64) *float64 { return &v }

// lateness is how late the finished train arrived at its last timed point
// after its entry, early counting as on time. False if it has none.
func lateness(t *ops.Train) (float64, bool) {
	sched := t.Schedule()
	for k := len(sched) - 1; k >= 1; k-- {
		if sp := sched[k]; sp.ArrTime >= 0 && sp.ActArrTime >= 0 {
			return max(sp.ActArrTime-sp.ArrTime, 0), true
		}
	}
	return 0, false
}

// waits lists the train's waits: to enter the world, at signals, and at
// stations past its dwell, and the schedule point each is on the way to or
// at. firstArrival is when its first dwell began, -1 if unknown; open waits
// end at end.
func waits(t *ops.Train, minStop units.Minutes, firstArrival, end float64) ([]Wait, []int) {
	var out []Wait
	var points []int
	k := 0
	add := func(kind, at, waitedFor string, from, to float64) {
		if to-from >= minWait {
			out = append(out, Wait{Train: t.Number, Kind: kind, At: at, FromMin: from, Min: to - from, For: waitedFor})
			points = append(points, k)
		}
	}
	for k = range t.Schedule() {
		sp := t.Schedule()[k]
		stn := sp.StnCode.String()
		for _, ss := range sp.SignalStops {
			to := ss.To
			if to < 0 {
				to = end
			}
			add(WaitSignal, ss.Track, ss.WaitedFor.Number, ss.From, to)
		}
		if !sp.Arrived() {
			break
		}
		arr := sp.ActArrTime
		if k == 0 {
			// the entry is when the train got onto the line; it arrives once
			// it has run the platform track
			if sp.ArrTime >= 0 {
				add(WaitEntry, stn, "", sp.ArrTime, sp.ActArrTime)
			}
			if firstArrival >= 0 {
				arr = firstArrival
			}
		}
		dep := sp.ActDeptTime
		if dep < 0 {
			dep = end
		}
		dwell := float64(sp.ExpDwellTime(arr, minStop)+sp.ExtraStand()) + des.MinDeltaTime
		add(WaitStation, stn, sp.WaitedFor.Number, arr+dwell, dep)
	}
	return out, points
}

// stats sums up the measure of the finished trains that have it; nil if
// none does.
func stats(trains []Train, measure func(Train) *float64, cfg Config) *Stats {
	st := &Stats{}
	var weighted, weights float64
	for _, t := range trains {
		m := measure(t)
		if m == nil || !t.Finished {
			continue
		}
		st.Trains++
		st.TotalMin += *m
		weighted += float64(t.Priority) * *m
		weights += float64(t.Priority)
		if *m > st.WorstMin {
			st.Worst, st.WorstMin = t.Train, *m
		}
	}
	if st.Trains == 0 {
		return nil
	}
	st.MeanMin = st.TotalMin / float64(st.Trains)
	st.WeightedMeanMin = st.MeanMin
	if weights > 0 {
		st.WeightedMeanMin = weighted / weights
	}
	for _, within := range slices.Sorted(slices.Values(cfg.PunctualWithinMin)) {
		p := Punctuality{WithinMin: within}
		for _, t := range trains {
			if m := measure(t); m != nil && t.Finished && *m <= within+1e-9 {
				p.Trains++
			}
		}
		p.Share = float64(p.Trains) / float64(st.Trains)
		st.Punctual = append(st.Punctual, p)
	}
	return st
}
