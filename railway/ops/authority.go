package ops

import (
	"fmt"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
)

// MovementAuthority is permission for a train to run along a reserved route up
// to and including edge Limit(). It can only be created from a Route, so the
// tracks and switches it covers are already reserved and locked for the train,
// and it never runs past a signal: it ends at the first signal (or platform
// end, see infra.World.EndsAtSignal) ahead of the train, and is extended one
// signal section at a time.
type MovementAuthority struct {
	route *interlocking.Route
	limit int
}

// NewMovementAuthority authorizes the route from the train's position on it
// up to the next signal.
func NewMovementAuthority(world *infra.World, route *interlocking.Route) (*MovementAuthority, error) {
	if route == nil {
		return nil, fmt.Errorf("movement authority needs a route")
	}
	if route.Entered() >= route.End() {
		return nil, fmt.Errorf("route of %s has no reserved edge ahead to authorize", route.Train())
	}
	return &MovementAuthority{route: route, limit: nextLimit(world, route, route.Entered())}, nil
}

// nextLimit is the last edge of the signal section starting at edge from.
func nextLimit(world *infra.World, route *interlocking.Route, from int) int {
	steps := make([]infra.Step, route.End())
	for i := range steps {
		steps[i] = route.Step(i)
	}
	return world.SectionEnd(steps, from) - 1
}

func (ma *MovementAuthority) Route() *interlocking.Route { return ma.route }
func (ma *MovementAuthority) Train() ids.TrainID         { return ma.route.Train() }
func (ma *MovementAuthority) Limit() int                 { return ma.limit }

// Permits reports whether the train may enter edge i of the route.
func (ma *MovementAuthority) Permits(i int) bool {
	return i >= 0 && i <= ma.limit && i < ma.route.End()
}

// PermitsNext reports whether the train may enter the next edge of the route.
func (ma *MovementAuthority) PermitsNext() bool {
	return ma.Permits(ma.route.Entered())
}

// CanExtend reports whether the route holds edges beyond the limit.
func (ma *MovementAuthority) CanExtend() bool {
	return ma.limit+1 < ma.route.End()
}

// Extend moves the limit to the end of the next signal section of the route.
func (ma *MovementAuthority) Extend(world *infra.World) error {
	if !ma.CanExtend() {
		return fmt.Errorf("route of %s holds nothing beyond edge %d", ma.Train(), ma.limit)
	}
	ma.limit = nextLimit(world, ma.route, ma.limit+1)
	return nil
}

// Exhausted reports whether the train has entered the last authorized edge.
func (ma *MovementAuthority) Exhausted() bool {
	return ma.route.Entered() > ma.limit
}
