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
	"trainapp/railway/infra"
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
	// OverhangExtraMinPer100m lengthens a stop at a platform shorter than the
	// train: minutes for each started 100 m of train beyond the platform.
	// Unset or 0, no extra dwell. See overhangDwell.
	OverhangExtraMinPer100m *units.Minutes `json:"overhang_extra_min_per_100m,omitempty"`
	// ApproachLeadMin is how long before its braking point a running train
	// asks for the section after its authority, so that it brakes for a
	// signal only when the section is really held against it. Unset or 0,
	// it asks at the braking point. Only trains that brake (Train.Brake)
	// ask on approach; see controller.go.
	ApproachLeadMin *units.Minutes `json:"approach_lead_min,omitempty"`
	// Precedence, if set, lets the dispatcher hold a train back, or move it
	// to another platform, for a higher-priority one; see precedence.go.
	Precedence *PrecedenceConfig `json:"precedence,omitempty"`
	// Timetable holds the timetable generator's settings, which share the
	// profile file. The sim does not read them; see package timetable.
	Timetable json.RawMessage `json:"timetable,omitempty"`
	// KPI holds the KPI report's settings, which share the profile file too.
	// The sim does not read them; see package kpi.
	KPI json.RawMessage `json:"kpi,omitempty"`
	// RollingStock gives each class's makeup, from which trains' length,
	// acceleration and braking are worked out (see initStock).
	RollingStock *ops.RollingStock `json:"rolling_stock,omitempty"`
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
	if m := c.OverhangExtraMinPer100m; m != nil && !(*m >= 0) {
		errs = append(errs, errors.New("overhang_extra_min_per_100m must not be negative"))
	}
	if m := c.ApproachLeadMin; m != nil && !(*m >= 0) {
		errs = append(errs, errors.New("approach_lead_min must not be negative"))
	}
	if c.Precedence != nil {
		if err := c.Precedence.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("precedence: %w", err))
		}
	}
	if c.RollingStock != nil {
		if err := c.RollingStock.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("rolling_stock: %w", err))
		}
	}
	return errors.Join(errs...)
}

// ApproachLead is how long before its braking point a running train asks for
// its next section (PriorityConfig.ApproachLeadMin).
func (s *Sim) ApproachLead() units.Minutes {
	if cfg := s.Priorities; cfg != nil && cfg.ApproachLeadMin != nil {
		return *cfg.ApproachLeadMin
	}
	return 0
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

// overhang is how many metres of train t stand beyond platform pf at its
// call sp, and the extra dwell that costs (OverhangExtraMinPer100m): only at
// a stop with passengers, and only if the train is longer than the platform.
func (s *Sim) overhang(t *ops.Train, sp *ops.SchedulePoint, pf *infra.Platform) (units.Meters, float64) {
	if pf == nil || !passengers(sp) || t.Length <= pf.Length {
		return 0, 0
	}
	over := t.Length - pf.Length
	per := 0.0
	if cfg := s.Priorities; cfg != nil && cfg.OverhangExtraMinPer100m != nil {
		per = float64(*cfg.OverhangExtraMinPer100m)
	}
	return over, math.Ceil(float64(over)/100) * per
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

// initStock works out each train's length, acceleration and braking from its
// class's rolling stock and its own Stock. A train with neither keeps zeros.
func (s *Sim) initStock() error {
	var rs *ops.RollingStock
	if s.Priorities != nil {
		rs = s.Priorities.RollingStock
	}
	var errs []error
	for _, t := range s.trains {
		if t.Stock != nil {
			if err := t.Stock.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("train %s: stock: %w", t.Number, err))
				continue
			}
		}
		c, err := rs.ConsistOf(t)
		if err != nil {
			errs = append(errs, fmt.Errorf("train %s: stock: %w", t.Number, err))
			continue
		}
		t.Length, t.Accel, t.Brake = c.Length, c.Accel, c.Brake
	}
	return errors.Join(errs...)
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
