package ops

import (
	"errors"
	"fmt"
	"strings"
	"trainapp/units"
)

// Stock is a train's makeup, from which its length, acceleration and braking
// are worked out (Consist). A profile gives one per class; a train may
// override any field. Unset fields are nil, since 0 can mean something (an
// EMU has no locomotive, so loco_length_m 0). A Stock's values are never
// changed in place, so copies may share them.
type Stock struct {
	// HP is the power at the rail before losses: the locomotive's, or an
	// EMU's or Vande Bharat's in total.
	HP *float64 `json:"hp,omitempty"`
	// Efficiency is the share of HP that accelerates the train. It also
	// stands in for the locomotive's weight and train resistance.
	Efficiency   *float64 `json:"efficiency,omitempty"`
	LocoLengthM  *float64 `json:"loco_length_m,omitempty"`
	Coaches      *int     `json:"coaches,omitempty"`
	CoachLengthM *float64 `json:"coach_length_m,omitempty"`
	CoachTonnes  *float64 `json:"coach_tonnes,omitempty"` // loaded
	BrakeMS2     *float64 `json:"brake_ms2,omitempty"`    // service braking

	// Set directly, these win over the values worked out from the makeup.
	LengthM  *float64 `json:"length_m,omitempty"`
	HPPerTL  *float64 `json:"hp_per_tl,omitempty"`
	AccelMS2 *float64 `json:"accel_ms2,omitempty"`
}

// With is s with the fields over sets replacing its own.
func (s Stock) With(over *Stock) Stock {
	if over == nil {
		return s
	}
	pick := func(dst **float64, src *float64) {
		if src != nil {
			*dst = src
		}
	}
	pick(&s.HP, over.HP)
	pick(&s.Efficiency, over.Efficiency)
	pick(&s.LocoLengthM, over.LocoLengthM)
	if over.Coaches != nil {
		s.Coaches = over.Coaches
	}
	pick(&s.CoachLengthM, over.CoachLengthM)
	pick(&s.CoachTonnes, over.CoachTonnes)
	pick(&s.BrakeMS2, over.BrakeMS2)
	pick(&s.LengthM, over.LengthM)
	pick(&s.HPPerTL, over.HPPerTL)
	pick(&s.AccelMS2, over.AccelMS2)
	return s
}

// Validate checks each field that is set is in range.
func (s Stock) Validate() error {
	var errs []error
	positive := func(name string, v *float64) {
		if v != nil && !(*v > 0) {
			errs = append(errs, fmt.Errorf("%s must be positive", name))
		}
	}
	positive("hp", s.HP)
	if e := s.Efficiency; e != nil && !(*e > 0 && *e <= 1) {
		errs = append(errs, errors.New("efficiency must be above 0 and at most 1"))
	}
	if l := s.LocoLengthM; l != nil && !(*l >= 0) {
		errs = append(errs, errors.New("loco_length_m must not be negative"))
	}
	if c := s.Coaches; c != nil && *c <= 0 {
		errs = append(errs, errors.New("coaches must be positive"))
	}
	positive("coach_length_m", s.CoachLengthM)
	positive("coach_tonnes", s.CoachTonnes)
	positive("brake_ms2", s.BrakeMS2)
	positive("length_m", s.LengthM)
	positive("hp_per_tl", s.HPPerTL)
	positive("accel_ms2", s.AccelMS2)
	return errors.Join(errs...)
}

// Consist is what a train's Stock works out to. A zero value means today's
// behaviour: no length, instant acceleration or braking.
type Consist struct {
	Length  units.Meters
	HPPerTL float64
	Accel   units.MetersPerMin2
	Brake   units.MetersPerMin2
}

// kWPerHP turns horsepower into kilowatts, so HP/TL into watts per kilogram.
const kWPerHP = 0.7457

// Consist works out the stock's length, HP/TL, acceleration and braking. ref
// is the reference speed the acceleration is worked out at: the constant rate
// that reaches ref in the same time as the train's power would,
// 2·efficiency·0.7457·HP/TL / ref. A value whose inputs are all unset stays
// 0; one whose inputs are only partly set is an error.
func (s Stock) Consist(ref units.MetersPerMin) (Consist, error) {
	var c Consist
	var errs []error
	// complete reports whether every input is set, and records an error if
	// only some are.
	complete := func(what string, inputs ...input) bool {
		var unset []string
		for _, in := range inputs {
			if !in.set {
				unset = append(unset, in.name)
			}
		}
		if len(unset) > 0 && len(unset) < len(inputs) {
			errs = append(errs, fmt.Errorf("%s needs %s too", what, strings.Join(unset, ", ")))
		}
		return len(unset) == 0
	}
	loco := input{"loco_length_m", s.LocoLengthM != nil}
	coaches := input{"coaches", s.Coaches != nil}
	coachLen := input{"coach_length_m", s.CoachLengthM != nil}
	hp := input{"hp", s.HP != nil}
	tonnes := input{"coach_tonnes", s.CoachTonnes != nil}

	switch {
	case s.LengthM != nil:
		c.Length = units.M(*s.LengthM)
	case complete("length", loco, coaches, coachLen):
		c.Length = units.M(*s.LocoLengthM + float64(*s.Coaches)**s.CoachLengthM)
	}

	switch {
	case s.HPPerTL != nil:
		c.HPPerTL = *s.HPPerTL
	case complete("HP/TL", hp, coaches, tonnes):
		c.HPPerTL = *s.HP / (float64(*s.Coaches) * *s.CoachTonnes)
	}

	switch {
	case s.AccelMS2 != nil:
		c.Accel = units.MS2(*s.AccelMS2)
	case complete("acceleration", input{"hp_per_tl or its inputs", c.HPPerTL > 0}, input{"efficiency", s.Efficiency != nil}):
		if !(ref > 0) {
			errs = append(errs, errors.New("acceleration needs a positive reference speed"))
			break
		}
		refMS := float64(ref) / 60
		c.Accel = units.MS2(2 * *s.Efficiency * kWPerHP * c.HPPerTL / refMS)
	}

	if s.BrakeMS2 != nil {
		c.Brake = units.MS2(*s.BrakeMS2)
	}
	if err := errors.Join(errs...); err != nil {
		return Consist{}, err
	}
	return c, nil
}

// input is one input to a worked-out value, and whether it is set.
type input struct {
	name string
	set  bool
}

// RollingStock is a profile's stock per class, with the reference speed
// accelerations are worked out at (Stock.Consist).
type RollingStock struct {
	ReferenceKmph float64          `json:"reference_kmph"`
	Classes       map[string]Stock `json:"classes"`
}

// Validate checks the reference speed and every class's stock.
func (r *RollingStock) Validate() error {
	var errs []error
	if !(r.ReferenceKmph > 0) {
		errs = append(errs, errors.New("reference_kmph must be positive"))
	}
	for class, s := range r.Classes {
		if err := s.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("class %q: %w", class, err))
		}
	}
	return errors.Join(errs...)
}

// ConsistOf works out a train's consist: its class's stock, if r has one,
// with the train's own Stock on top. r may be nil, leaving the train's own.
func (r *RollingStock) ConsistOf(t *Train) (Consist, error) {
	var base Stock
	var ref units.MetersPerMin
	if r != nil {
		base = r.Classes[t.Class]
		ref = units.KMPH(r.ReferenceKmph)
	}
	return base.With(t.Stock).Consist(ref)
}
