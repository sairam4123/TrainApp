package ops

import (
	"errors"
	"fmt"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/units"
)

type Train struct {
	Name   string
	Number string

	curSchedulePoint int
	schedule         []*SchedulePoint

	FacingToward *infra.TrackPoint
	MaxSpeed     units.MetersPerMin

	route *interlocking.Route // route the front of the train is on
	ma    *MovementAuthority  // authority on route
}

func (t *Train) ID() ids.TrainID {
	return ids.TrainID{Number: t.Number}
}

func (t *Train) GetFullName() string {
	return t.Number + " - " + t.Name
}

func (t *Train) AddSchedule(sp *SchedulePoint) {
	t.schedule = append(t.schedule, sp)
}

// Validate checks the train can be simulated.
func (t *Train) Validate() error {
	var errs []error
	if t.Number == "" {
		errs = append(errs, errors.New("train has no number"))
	}
	if !(t.MaxSpeed > 0) {
		errs = append(errs, fmt.Errorf("train %s: max speed must be positive", t.Number))
	}
	if len(t.schedule) == 0 {
		errs = append(errs, fmt.Errorf("train %s: empty schedule", t.Number))
	} else if t.schedule[0].ArrTime < 0 {
		errs = append(errs, fmt.Errorf("train %s: first schedule point has no entry time, use SetEntryTime", t.Number))
	}
	return errors.Join(errs...)
}

func (t *Train) Schedule() []*SchedulePoint {
	return t.schedule
}

// StopIndex is the index of the schedule point the train is at or heading to.
func (t *Train) StopIndex() int {
	return t.curSchedulePoint
}

// CurrentStop is the schedule point the train is at or heading to.
func (t *Train) CurrentStop() (*SchedulePoint, bool) {
	if t.curSchedulePoint < 0 || t.curSchedulePoint >= len(t.schedule) {
		return nil, false
	}
	return t.schedule[t.curSchedulePoint], true
}

// NextStop is the schedule point after the current one.
func (t *Train) NextStop() (*SchedulePoint, bool) {
	i := t.curSchedulePoint + 1
	if i < 0 || i >= len(t.schedule) {
		return nil, false
	}
	return t.schedule[i], true
}

// AdvanceStop makes the next schedule point the current one.
func (t *Train) AdvanceStop() {
	t.curSchedulePoint++
}

// Route is the route the front of the train is on, nil before it enters the
// world.
func (t *Train) Route() *interlocking.Route {
	return t.route
}

// Authority is the movement authority the train is running under, if any.
func (t *Train) Authority() *MovementAuthority {
	return t.ma
}

// Authorize hands the train a new movement authority; its route becomes the
// train's route.
func (t *Train) Authorize(ma *MovementAuthority) error {
	if ma.Train() != t.ID() {
		return fmt.Errorf("authority for %s given to %s", ma.Train(), t.Number)
	}
	t.ma = ma
	t.route = ma.Route()
	return nil
}

// ClearMovement drops the train's route and authority, e.g. when it leaves the
// world.
func (t *Train) ClearMovement() {
	t.route = nil
	t.ma = nil
}

// CurrentTrack is the track the front of the train is on, nil if none.
func (t *Train) CurrentTrack() *infra.TrackSegment {
	if t.route == nil {
		return nil
	}
	step, ok := t.route.Head()
	if !ok {
		return nil
	}
	return step.Edge.Track
}

// CurrentEdge is the edge the front of the train is on, nil if none.
func (t *Train) CurrentEdge() *infra.GraphEdge {
	if t.route == nil {
		return nil
	}
	step, ok := t.route.Head()
	if !ok {
		return nil
	}
	return step.Edge
}

func (t *Train) String() string {
	if t == nil {
		return "<nil>"
	}
	return t.GetFullName()
}
