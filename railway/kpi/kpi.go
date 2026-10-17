// Package kpi measures a finished run against the three objectives: delay,
// line utilization and, until there is a passenger model, the waits
// passengers sit through.
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
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"trainapp/des"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
)

// Config is the report's settings, the "kpi" section of a railway profile.
type Config struct {
	// PunctualWithinMin are the delays, in minutes, punctuality is reported
	// within: the share of trains no later than each.
	PunctualWithinMin []float64 `json:"punctual_within_min"`
}

// DefaultConfig is used when the profile has no kpi section.
func DefaultConfig() Config {
	return Config{PunctualWithinMin: []float64{1, 5, 15}}
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

// Report is a run's KPIs.
type Report struct {
	Trains     int      `json:"trains"`
	Finished   int      `json:"finished"`
	Unfinished []string `json:"unfinished,omitempty"`
	// Delay is against free-running time; nil without Options.Free.
	Delay *Stats `json:"delay,omitempty"`
	// Lateness is against the timetable, for timed trains; nil if none.
	Lateness *Stats `json:"lateness,omitempty"`
	// Disruption splits the delay; nil without disruptions.
	Disruption *Split `json:"disruption,omitempty"`
	// LongestWait is the longest single wait of any train; nil if none.
	LongestWait *Wait   `json:"longest_wait,omitempty"`
	WaitMin     float64 `json:"wait_min"` // every train's waits, in all
	// Holds and PlatformChanges are precedence's decisions; HeldMin is how
	// long the holds lasted in all.
	Holds           int         `json:"holds"`
	HeldMin         float64     `json:"held_min"`
	PlatformChanges int         `json:"platform_changes"`
	Utilization     Utilization `json:"utilization"`
	PerTrain        []Train     `json:"per_train"`
}

// Stats sums up the delays of the finished trains it counts.
type Stats struct {
	Trains   int     `json:"trains"`
	TotalMin float64 `json:"total_min"`
	MeanMin  float64 `json:"mean_min"`
	// WeightedMeanMin weights each train's delay by its base priority (all
	// alike if no train has one).
	WeightedMeanMin float64       `json:"weighted_mean_min"`
	Punctual        []Punctuality `json:"punctual"`
	// Worst is the train with the largest delay, empty if none has any.
	Worst    string  `json:"worst,omitempty"`
	WorstMin float64 `json:"worst_min"`
}

// Punctuality is the share of trains, 0 to 1, no later than WithinMin.
type Punctuality struct {
	WithinMin float64 `json:"within_min"`
	Trains    int     `json:"trains"`
	Share     float64 `json:"share"`
}

// Split is the delay against free-running time, split into the disruptions'
// own minutes and the rest.
type Split struct {
	PrimaryMin     float64 `json:"primary_min"`
	ReactionaryMin float64 `json:"reactionary_min"`
}

// Wait kinds.
const (
	WaitEntry   = "entry"   // waiting to enter the world
	WaitSignal  = "signal"  // stopped at a signal
	WaitStation = "station" // standing at a station past its dwell, e.g. held for another train
)

// Wait is a train standing longer than its schedule asks.
type Wait struct {
	Train   string  `json:"train"`
	Kind    string  `json:"kind"`
	At      string  `json:"at"` // the station, or for a signal the track stood on
	FromMin float64 `json:"from_min"`
	Min     float64 `json:"min"`
}

// Train is one train's KPIs. Times are -1 and the measures nil where the
// train never got that far.
type Train struct {
	Train    string `json:"train"`
	Class    string `json:"class,omitempty"`
	Priority int    `json:"priority"`
	Finished bool   `json:"finished"`
	// EntryMin is the planned entry, before any disruption.
	EntryMin   float64  `json:"entry_min"`
	ExitMin    float64  `json:"exit_min"`
	JourneyMin float64  `json:"journey_min"`
	FreeRunMin *float64 `json:"free_run_min,omitempty"`
	DelayMin   *float64 `json:"delay_min,omitempty"`
	LateMin    *float64 `json:"late_min,omitempty"`
	PrimaryMin float64  `json:"primary_min,omitempty"`
	WaitMin    float64  `json:"wait_min"`
	Waits      []Wait   `json:"waits,omitempty"`
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

	primary := map[string]float64{}
	for _, d := range opts.Disruptions {
		primary[d.Train] += d.Min
	}

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
		tk.Waits = waits(t, s.MinStop(), c.arrivedAt(t.Number), end)
		for _, w := range tk.Waits {
			tk.WaitMin += w.Min
			if r.LongestWait == nil || w.Min > r.LongestWait.Min {
				r.LongestWait = &w
			}
		}
		r.WaitMin += tk.WaitMin
		r.PerTrain = append(r.PerTrain, tk)
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
// stations past its dwell. firstArrival is when its first dwell began, -1 if
// unknown; open waits end at end.
func waits(t *ops.Train, minStop units.Minutes, firstArrival, end float64) []Wait {
	var out []Wait
	add := func(kind, at string, from, to float64) {
		if to-from >= minWait {
			out = append(out, Wait{Train: t.Number, Kind: kind, At: at, FromMin: from, Min: to - from})
		}
	}
	for k, sp := range t.Schedule() {
		stn := sp.StnCode.String()
		for _, ss := range sp.SignalStops {
			to := ss.To
			if to < 0 {
				to = end
			}
			add(WaitSignal, ss.Track, ss.From, to)
		}
		if !sp.Arrived() {
			break
		}
		arr := sp.ActArrTime
		if k == 0 {
			// the entry is when the train got onto the line; it arrives once
			// it has run the platform track
			if sp.ArrTime >= 0 {
				add(WaitEntry, stn, sp.ArrTime, sp.ActArrTime)
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
		add(WaitStation, stn, arr+dwell, dep)
	}
	return out
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
