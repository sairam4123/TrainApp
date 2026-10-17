package sim

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
)

// Overrides.
//
// An operator can change precedence decisions: release a hold (or veto it
// before it happens), force a hold, pin a train's platform at a station, or
// turn precedence off for a train. Sim.Override applies one at once and
// Sim.OverrideAt at a sim time; LoadOverrides reads a list from a JSON file.
// Decision ids are the same from run to run, so ids from one run's log work
// in the next. Every override is logged as PRECEDENCE_OVERRIDE.

// OverrideAction is what an override does.
type OverrideAction string

const (
	// OverrideRelease ends the hold Decision names, or keeps it from ever
	// being placed.
	OverrideRelease OverrideAction = "release"
	// OverrideHold holds Train at Station until For has left Station, or
	// with Until HoldUntilArrived until For has reached it (e.g. for a
	// crossing). The hold rule and max_hold_min do not apply; the refuge,
	// track and safety checks do. It holds a train standing at Station, not
	// one still to enter the world there.
	OverrideHold OverrideAction = "hold"
	// OverridePlatform makes Pf Train's first choice at Station.
	OverridePlatform OverrideAction = "platform"
	// OverrideDisable means Train is never held or moved. Others can still
	// be held for it.
	OverrideDisable OverrideAction = "disable"
)

// What a hold override waits for.
const (
	HoldUntilLeft    = "left"    // For has left Station (the default)
	HoldUntilArrived = "arrived" // For has reached Station
)

// Override is one change to the precedence decisions.
type Override struct {
	// T is when it takes effect, in sim minutes; nil means from the start.
	T        *float64       `json:"t,omitempty"`
	Action   OverrideAction `json:"action"`
	Decision string         `json:"decision,omitempty"`
	Train    string         `json:"train,omitempty"`
	For      string         `json:"for,omitempty"`
	Station  string         `json:"station,omitempty"`
	Pf       string         `json:"pf,omitempty"`
	// Until is, for a hold, what it waits for: HoldUntilLeft (the default)
	// or HoldUntilArrived.
	Until string `json:"until,omitempty"`
}

// LoadOverrides reads overrides from a JSON file of the form
// {"overrides": [{"t": 40, "action": "release", "decision": "hold:A>B@LPA"}, ...]}.
func LoadOverrides(path string) ([]Override, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var file struct {
		Overrides []Override `json:"overrides"`
	}
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file.Overrides, nil
}

// ApplyOverrides applies each override from the start, or schedules it for
// its time. Call it after Init.
func (s *Sim) ApplyOverrides(overrides []Override) error {
	var errs []error
	for i, o := range overrides {
		var err error
		if o.T == nil {
			err = s.Override(o)
		} else {
			err = s.OverrideAt(*o.T, o)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("override %d: %w", i+1, err))
		}
	}
	return errors.Join(errs...)
}

// Override applies an override now. Call it after Init, or from an Observer
// during Run.
func (s *Sim) Override(o Override) error {
	if err := s.checkOverride(o); err != nil {
		return err
	}
	s.applyOverride(o)
	return nil
}

// OverrideAt applies an override at the given sim time. Call it after Init, or
// from an Observer during Run.
func (s *Sim) OverrideAt(at float64, o Override) error {
	if err := s.checkOverride(o); err != nil {
		return err
	}
	return s.des.Add(at, ops.PrecedenceOverride, o, "")
}

func (s *Sim) checkOverride(o Override) error {
	if !s.initialized {
		return errors.New("sim: override before Init")
	}
	if s.prec == nil {
		return errors.New("sim: precedence is off (no precedence block in the priority config)")
	}
	train := func(number string) (*ops.Train, error) {
		tc, ok := s.trainCtrllers[ids.TrainID{Number: number}]
		if !ok {
			return nil, fmt.Errorf("unknown train %q", number)
		}
		return tc.train, nil
	}
	switch o.Action {
	case OverrideRelease:
		if !strings.HasPrefix(o.Decision, "hold:") {
			return fmt.Errorf("release needs a hold decision id (hold:...), got %q", o.Decision)
		}
	case OverrideHold:
		if _, err := train(o.Train); err != nil {
			return err
		}
		h, err := train(o.For)
		if err != nil {
			return err
		}
		if o.Train == o.For {
			return errors.New("a train cannot be held for itself")
		}
		if o.Until != "" && o.Until != HoldUntilLeft && o.Until != HoldUntilArrived {
			return fmt.Errorf("unknown until %q (want %s or %s)", o.Until, HoldUntilLeft, HoldUntilArrived)
		}
		if !slices.ContainsFunc(h.Schedule(), func(sp *ops.SchedulePoint) bool { return sp.StnCode.String() == o.Station }) {
			return fmt.Errorf("%s does not call at %q", o.For, o.Station)
		}
	case OverridePlatform:
		if _, err := train(o.Train); err != nil {
			return err
		}
		stn, ok := s.world.GetStation(ids.StnCode(o.Station))
		if !ok {
			return fmt.Errorf("unknown station %q", o.Station)
		}
		if stn.FindStnPlatform(o.Pf) == nil {
			return fmt.Errorf("%s has no platform %q", o.Station, o.Pf)
		}
	case OverrideDisable:
		if _, err := train(o.Train); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown override action %q", o.Action)
	}
	return nil
}

// applyOverride makes the change and has the train affected looked at again.
func (s *Sim) applyOverride(o Override) {
	p := s.prec
	train := ids.TrainID{Number: o.Train}
	switch o.Action {
	case OverrideRelease:
		p.vetoed[o.Decision] = true
		if d := p.current[o.Decision]; d != nil && d.Status == StatusActive {
			p.end(d, StatusOverridden)
			train = d.Train
		}
	case OverrideHold:
		p.manual[train] = append(p.manual[train], manualHold{forTrain: ids.TrainID{Number: o.For}, stn: ids.StnCode(o.Station), arrived: o.Until == HoldUntilArrived})
	case OverridePlatform:
		p.pinned[pinKey{train, ids.StnCode(o.Station)}] = o.Pf
	case OverrideDisable:
		p.disabled[train] = true
		for _, d := range slices.Clone(p.holds[train]) {
			p.end(d, StatusOverridden)
		}
	}
	p.invalidate()
	if s.running {
		s.logOverride(o, "applied", "")
	} else {
		p.early = append(p.early, o)
	}
	if !train.IsZero() {
		s.ScheduleEventNext(ops.PrecedenceReview, o.Decision, train)
	}
}
