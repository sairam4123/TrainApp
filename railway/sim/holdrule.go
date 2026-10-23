package sim

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"trainapp/railway/ids"
	"trainapp/units"
)

// PrecedenceConfig holds a railway's numbers for precedence: how far ahead
// the dispatcher looks, and when holding a train back for a higher-priority
// one is worth it. See precedence.go.
type PrecedenceConfig struct {
	// Rule picks the HoldRule by name: "thresholds" (the default) or
	// "weighted".
	Rule string `json:"rule,omitempty"`
	// HorizonMin is how far ahead trains are projected. Defaults to 30.
	HorizonMin units.Minutes `json:"horizon_min,omitempty"`
	// HeadwayMin is the gap kept between two trains using the same track.
	HeadwayMin units.Minutes `json:"headway_min,omitempty"`
	// MaxHoldMin caps how long a train is held for another. Defaults to 30.
	MaxHoldMin units.Minutes `json:"max_hold_min,omitempty"`
	// MainLineMaxStayMin is the longest a stopping train may stand on a main
	// line; a longer stay goes to a loop.
	MainLineMaxStayMin units.Minutes `json:"main_line_max_stay_min,omitempty"`
	// MaxReceptionHoldMin caps how long a train is kept at a station because
	// the next one has no track free for it when it would arrive (a
	// reception hold). Defaults to max_hold_min.
	MaxReceptionHoldMin units.Minutes `json:"max_reception_hold_min,omitempty"`

	// MinPriorityGap and MinSavingMin are the thresholds rule's settings: hold
	// only for a train at least this much higher in priority, that would
	// otherwise lose at least this many minutes.
	MinPriorityGap int           `json:"min_priority_gap,omitempty"`
	MinSavingMin   units.Minutes `json:"min_saving_min,omitempty"`
	// MaxWaitRatio is also the thresholds rule's: hold only if the held
	// train waits at most this many minutes per minute the other saves,
	// whatever either train's class. 0 for no limit.
	MaxWaitRatio float64 `json:"max_wait_ratio,omitempty"`
	// MinNetGain is the weighted rule's setting: hold when priority(H) x
	// saving - priority(L) x wait exceeds it.
	MinNetGain float64 `json:"min_net_gain,omitempty"`
	// FollowHorizonMin is how far ahead a train catching up with a slower one
	// is projected following it, and the slower one's refuges compared for
	// where to let it by (trailing.go). 0 turns both off.
	FollowHorizonMin units.Minutes `json:"follow_horizon_min,omitempty"`
	// DeferMarginMin is how much better, in the hold rule's score, a later
	// refuge must be than where the train stands for it to be let go and
	// held there instead. Defaults to 1.
	DeferMarginMin units.Minutes `json:"defer_margin_min,omitempty"`
}

const (
	defaultHorizon = 30
	defaultMaxHold = 30
)

// Validate checks the settings.
func (c *PrecedenceConfig) Validate() error {
	var errs []error
	if _, ok := holdRules[c.ruleName()]; !ok {
		errs = append(errs, fmt.Errorf("unknown rule %q (have %s)", c.Rule, strings.Join(slices.Sorted(maps.Keys(holdRules)), ", ")))
	}
	for name, v := range map[string]units.Minutes{
		"horizon_min": c.HorizonMin, "headway_min": c.HeadwayMin, "max_hold_min": c.MaxHoldMin,
		"main_line_max_stay_min": c.MainLineMaxStayMin, "min_saving_min": c.MinSavingMin,
		"max_reception_hold_min": c.MaxReceptionHoldMin, "follow_horizon_min": c.FollowHorizonMin,
		"defer_margin_min": c.DeferMarginMin,
	} {
		if v < 0 {
			errs = append(errs, fmt.Errorf("%s is negative", name))
		}
	}
	if c.MaxWaitRatio < 0 {
		errs = append(errs, errors.New("max_wait_ratio is negative"))
	}
	if c.FollowHorizonMin > 0 && float64(c.FollowHorizonMin) < c.horizon() {
		errs = append(errs, fmt.Errorf("follow_horizon_min %.0f is below horizon_min %.0f", float64(c.FollowHorizonMin), c.horizon()))
	}
	return errors.Join(errs...)
}

func (c *PrecedenceConfig) ruleName() string {
	if c.Rule == "" {
		return "thresholds"
	}
	return c.Rule
}

func (c *PrecedenceConfig) horizon() float64 {
	if c.HorizonMin == 0 {
		return defaultHorizon
	}
	return float64(c.HorizonMin)
}

func (c *PrecedenceConfig) maxHold() float64 {
	if c.MaxHoldMin == 0 {
		return defaultMaxHold
	}
	return float64(c.MaxHoldMin)
}

// trailing reports whether trains catching up are projected following the
// trains ahead, and refuges compared (trailing.go).
func (c *PrecedenceConfig) trailing() bool { return c.FollowHorizonMin > 0 }

func (c *PrecedenceConfig) followHorizon() float64 { return float64(c.FollowHorizonMin) }

const defaultDeferMargin = 1

func (c *PrecedenceConfig) deferMargin() float64 {
	if c.DeferMarginMin == 0 {
		return defaultDeferMargin
	}
	return float64(c.DeferMarginMin)
}

// maxReceptionHold is the cap on a reception hold.
func (c *PrecedenceConfig) maxReceptionHold() float64 {
	if c.MaxReceptionHoldMin == 0 {
		return c.maxHold()
	}
	return float64(c.MaxReceptionHoldMin)
}

// ConflictKind is how two trains get in each other's way.
type ConflictKind string

const (
	// KindOvertake: the higher-priority train would catch up with the other,
	// both running the same way.
	KindOvertake ConflictKind = "overtake"
	// KindCross: they need the same track in opposite directions, or meet at
	// a switch or diamond.
	KindCross ConflictKind = "cross"
	// KindPlatform: the other train would take the station track the
	// higher-priority one needs.
	KindPlatform ConflictKind = "platform"
	// KindManual: an operator asked for the hold.
	KindManual ConflictKind = "manual"
	// KindReception: the next station would have no track free for the
	// train when it got there, so it would stand at the home signal and shut
	// the line behind it. Kept at its station instead, whatever the
	// priorities: the hold rule does not apply.
	KindReception ConflictKind = "reception"
)

// Conflict is what a HoldRule decides on: train Held would cost train For
// Saving minutes if it went now, and would wait about Wait minutes if held.
type Conflict struct {
	Kind              ConflictKind
	Held, For         ids.TrainID
	HeldPrio, ForPrio int
	Saving, Wait      units.Minutes
	// TrailTo is set when Saving is what For loses trailing Held up to this
	// station, rather than over Held's next section (trailing.go).
	TrailTo string
}

// HoldRule decides whether a train is held for a higher-priority one. Whatever
// it says, a hold is never longer than max_hold_min and must pass the refuge
// and safety checks.
type HoldRule interface {
	Name() string
	// Hold reports whether c.Held should wait for c.For, and why.
	Hold(c Conflict) (bool, string)
	// Score ranks holds the rule accepts, in minutes of the held train's
	// time, higher better. It picks where a train is held for another (the
	// best refuge, trailing.go), not whether.
	Score(c Conflict) float64
}

// holdRules maps a rule's name to how it is built from the config. A new rule
// only needs an entry here.
var holdRules = map[string]func(*PrecedenceConfig) HoldRule{
	"thresholds": func(c *PrecedenceConfig) HoldRule {
		return thresholdRule{gap: c.MinPriorityGap, saving: c.MinSavingMin, ratio: c.MaxWaitRatio}
	},
	"weighted": func(c *PrecedenceConfig) HoldRule {
		return weightedRule{minNet: c.MinNetGain}
	},
}

// thresholdRule holds a train for one at least gap higher in priority that
// would otherwise lose at least saving minutes, as long as the held train
// waits no more than ratio minutes per minute saved (0: no limit).
type thresholdRule struct {
	gap    int
	saving units.Minutes
	ratio  float64
}

func (thresholdRule) Name() string { return "thresholds" }

func (r thresholdRule) Hold(c Conflict) (bool, string) {
	if gap := c.ForPrio - c.HeldPrio; gap < r.gap {
		return false, fmt.Sprintf("priority gap %d is below %d", gap, r.gap)
	}
	if c.Saving < r.saving {
		return false, fmt.Sprintf("saves %.1f min, below %.1f", c.Saving, r.saving)
	}
	if r.ratio > 0 && float64(c.Wait) > r.ratio*float64(c.Saving) {
		return false, fmt.Sprintf("waits %.1f min to save %.1f, more than %.1f per minute saved", c.Wait, c.Saving, r.ratio)
	}
	if c.TrailTo != "" {
		return true, fmt.Sprintf("%s would lose %.1f min trailing it to %s", c.For, c.Saving, c.TrailTo)
	}
	return true, fmt.Sprintf("%s would lose %.1f min", c.For, c.Saving)
}

// Score is the minutes the two trains lose between them less for the hold:
// priority decides whether a train is held, plain minutes where.
func (thresholdRule) Score(c Conflict) float64 { return float64(c.Saving - c.Wait) }

// weightedRule holds a train when what the higher-priority train saves,
// weighted by its priority, outweighs what the held train waits, weighted by
// its own. Priorities below 1 count as 1.
type weightedRule struct {
	minNet float64
}

func (weightedRule) Name() string { return "weighted" }

func (r weightedRule) Hold(c Conflict) (bool, string) {
	w := func(p int) float64 { return float64(max(p, 1)) }
	net := w(c.ForPrio)*float64(c.Saving) - w(c.HeldPrio)*float64(c.Wait)
	if net <= r.minNet {
		return false, fmt.Sprintf("net gain %.1f is not above %.1f", net, r.minNet)
	}
	return true, fmt.Sprintf("net gain %.1f (%s saves %.1f min, %s waits %.1f)", net, c.For, c.Saving, c.Held, c.Wait)
}

// Score is the net gain in minutes of the held train's time.
func (weightedRule) Score(c Conflict) float64 {
	w := func(p int) float64 { return float64(max(p, 1)) }
	return (w(c.ForPrio)*float64(c.Saving) - w(c.HeldPrio)*float64(c.Wait)) / w(c.HeldPrio)
}
