package sim

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/units"
)

// Train priority.
//
// When a track is released, the requests waiting on it are retried highest
// rank first, then longest-waiting first. A train's priority is its base
// priority (ops.Train.Priority, or the one its Class maps to) adjusted by how
// late it is running. Its rank in the queue is its priority, raised by ageing
// for how long the request has waited, so no train is kept waiting for ever
// behind a stream of higher-priority ones. All of it comes from a
// PriorityConfig, which holds a railway's own numbers; the sim attaches no
// meaning to classes or thresholds.

// PriorityConfig maps service classes to base priorities and adjusts priority
// by how late a train is running.
type PriorityConfig struct {
	// Classes maps a train's Class to its base priority.
	Classes map[string]int `json:"classes"`
	// DelayBands adjust the priority of late trains. The band with the
	// largest OverMin below the train's delay applies; none applies to a
	// train running no later than the smallest OverMin.
	DelayBands []DelayBand `json:"delay_bands"`
	// Ageing, if set, raises a waiting request's rank the longer it waits.
	// It orders the queue only: priority itself, and so precedence, is
	// unchanged.
	Ageing *Ageing `json:"ageing,omitempty"`
	// MinStopMin is the shortest a train stands at a stop, even running late
	// or timetabled for less: DefaultMinStopMin if unset. Passes and halts
	// are not stops (see ops.SchedulePoint.AsHalt).
	MinStopMin *units.Minutes `json:"min_stop_min,omitempty"`
	// Precedence, if set, lets the dispatcher hold a train back, or move it
	// to another platform, for a higher-priority one; see precedence.go.
	Precedence *PrecedenceConfig `json:"precedence,omitempty"`
	// Timetable holds the timetable generator's settings, which share the
	// profile file. The sim does not read them; see package timetable.
	Timetable json.RawMessage `json:"timetable,omitempty"`
	// KPI holds the KPI report's settings, which share the profile file too.
	// The sim does not read them; see package kpi.
	KPI json.RawMessage `json:"kpi,omitempty"`
}

// DelayBand adjusts the priority of trains running more than OverMin minutes
// late by Adjust.
type DelayBand struct {
	OverMin units.Minutes `json:"over_min"`
	Adjust  int           `json:"adjust"`
}

// Ageing raises a waiting request's rank by Adjust for every EveryMin minutes
// it has waited.
type Ageing struct {
	EveryMin units.Minutes `json:"every_min"`
	Adjust   int           `json:"adjust"`
}

// LoadPriorityConfig reads a PriorityConfig from a JSON file.
func LoadPriorityConfig(path string) (*PriorityConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var cfg PriorityConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// Validate checks the delay bands do not overlap and the precedence settings
// make sense.
func (c *PriorityConfig) Validate() error {
	seen := map[units.Minutes]bool{}
	var errs []error
	for _, b := range c.DelayBands {
		if seen[b.OverMin] {
			errs = append(errs, fmt.Errorf("two delay bands over %v min", b.OverMin))
		}
		seen[b.OverMin] = true
	}
	if a := c.Ageing; a != nil {
		if !(a.EveryMin > 0) {
			errs = append(errs, errors.New("ageing: every_min must be positive"))
		}
		if a.Adjust < 0 {
			errs = append(errs, errors.New("ageing: adjust must not be negative"))
		}
	}
	if m := c.MinStopMin; m != nil && !(*m >= 0) {
		errs = append(errs, errors.New("min_stop_min must not be negative"))
	}
	if c.Precedence != nil {
		if err := c.Precedence.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("precedence: %w", err))
		}
	}
	return errors.Join(errs...)
}

// DefaultMinStopMin is the minimum stop without a PriorityConfig.MinStopMin.
const DefaultMinStopMin units.Minutes = 1

// MinStop is the shortest a train stands at a stop (PriorityConfig.MinStopMin).
func (s *Sim) MinStop() units.Minutes {
	if cfg := s.Priorities; cfg != nil && cfg.MinStopMin != nil {
		return *cfg.MinStopMin
	}
	return DefaultMinStopMin
}

// initPriorities checks the config and gives trains with a Class their base
// priority.
func (s *Sim) initPriorities() error {
	s.delayBands = nil
	if cfg := s.Priorities; cfg != nil {
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("sim: priorities: %w", err)
		}
		s.delayBands = slices.SortedFunc(slices.Values(cfg.DelayBands), func(a, b DelayBand) int {
			return cmp.Compare(a.OverMin, b.OverMin)
		})
	}
	for _, t := range s.trains {
		if t.Class == "" {
			continue
		}
		p, ok := 0, false
		if s.Priorities != nil {
			p, ok = s.Priorities.Classes[t.Class]
		}
		if !ok {
			return fmt.Errorf("train %s: no priority for class %q", t.Number, t.Class)
		}
		t.Priority = p
	}
	return nil
}

// delayAdjust is the priority adjustment for a train running delay late.
func (s *Sim) delayAdjust(delay units.Minutes) int {
	adj := 0
	for _, b := range s.delayBands {
		if delay <= b.OverMin {
			break
		}
		adj = b.Adjust
	}
	return adj
}

// ageAdjust is how much ageing raises the rank of a request that has waited
// that many minutes.
func (s *Sim) ageAdjust(waited float64) int {
	if s.Priorities == nil || s.Priorities.Ageing == nil || waited <= 0 {
		return 0
	}
	a := s.Priorities.Ageing
	return int(math.Floor(waited/float64(a.EveryMin)+epsilon)) * a.Adjust
}

// Priority is the train's priority now: its base priority adjusted for how
// late it is running.
func (s *Sim) Priority(id ids.TrainID) int {
	tc, ok := s.trainCtrllers[id]
	if !ok {
		return 0
	}
	return tc.train.Priority + s.delayAdjust(tc.train.Delay(s.CurTime()))
}

// priorityChange is the data of a PriorityChanged event.
type priorityChange struct {
	to int
}

// SetPriorityAt changes the train's base priority at the given time. Call it
// after Init, or from an Observer during Run.
func (s *Sim) SetPriorityAt(at float64, id ids.TrainID, priority int) error {
	if !s.initialized {
		return errors.New("sim: SetPriorityAt before Init")
	}
	if _, ok := s.trainCtrllers[id]; !ok {
		return fmt.Errorf("sim: SetPriorityAt for unknown train %s", id)
	}
	_, err := s.des.Add(at, ops.PriorityChanged, priorityChange{to: priority}, id.Number)
	return err
}
