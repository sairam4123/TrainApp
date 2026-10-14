package sim

import (
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/railway/ops"
)

// Deadlock avoidance.
//
// Before a route is granted the dispatcher checks that the railway would still
// be in a safe state: one where the trains in the world can be ordered so that
// each, in turn, can call at all its remaining stops using only track that is
// free or held by trains earlier in the order (which will have moved on by
// then). This is the banker's algorithm with routes as the resources. If no
// such order exists the grant could close a cycle of trains each waiting for
// track another holds, so it is refused and the request waits.
//
// It covers, without special cases: entering trains filling a terminus that
// arriving trains need, a train waiting at a home signal on the only way out
// of a full station, and opposing trains meeting on a single line.

// participant is a train as the safety check sees it.
type participant struct {
	train     *ops.Train
	pos       infra.Position // where it will stand once its granted route is run
	remaining []ids.StnCode  // stops still to call at from pos
	holds     []*infra.TrackSegment
}

// stopsAfter returns the stations the train still has to call at once it has
// run route: from the stop it is heading to, skipping it if the route ends on
// one of its platforms. headingTo is the index of that stop.
func (disp *Dispatcher) stopsAfter(train *ops.Train, headingTo int, route *interlocking.Route) []ids.StnCode {
	sched := train.Schedule()
	k := headingTo
	if route != nil && route.End() > 0 && k < len(sched) {
		last := route.Step(route.End() - 1).Edge.Track
		if stn, ok := disp.world.GetStation(sched[k].StnCode); ok && stn.IsPlatform(last.Id) {
			k++
		}
	}
	out := make([]ids.StnCode, 0, len(sched))
	for _, sp := range sched[min(k, len(sched)):] {
		out = append(out, sp.StnCode)
	}
	return out
}

// headingTo is the index of the stop the train is at or will run to next.
func headingTo(tc *TrainController) int {
	k := tc.train.StopIndex()
	if tc.phase == phaseReady {
		k++ // dwell is over; any route it is granted leads to the next stop
	}
	return k
}

func routeEnd(route *interlocking.Route) infra.Position {
	last := route.Step(route.End() - 1)
	return infra.Position{At: last.To, Via: last.Edge}
}

// participants lists the trains in the world as they would be if route were
// granted to requester, in the order trains were added.
func (disp *Dispatcher) participants(requester *TrainController, route *interlocking.Route) []participant {
	var out []participant
	for _, t := range disp.sim.trains {
		tc := disp.sim.trainCtrllers[t.ID()]
		r := disp.frontier[t.ID()]
		// a train granted its entry route counts even before it has moved
		inWorld := tc.phase != phaseExited && (tc.phase != phaseOutside || r != nil)
		if !inWorld && tc != requester {
			continue
		}
		if tc == requester {
			r = route
		}
		p := participant{train: t, holds: disp.ilk.Holdings(t.ID())}
		switch {
		case r != nil:
			p.pos = routeEnd(r)
		case t.FacingToward != nil:
			p.pos = infra.Position{At: t.FacingToward, Via: t.CurrentEdge()}
		default:
			continue // not placed yet and nothing granted: holds nothing, needs nothing yet
		}
		p.remaining = disp.stopsAfter(t, headingTo(tc), r)
		out = append(out, p)
	}
	return out
}

// safe reports whether the state is safe if route is granted to requester.
// When it is not, it returns the tracks held by the trains that could not be
// shown to finish: the state can only improve when one of them is released.
func (disp *Dispatcher) safe(requester *TrainController, route *interlocking.Route) (bool, []*infra.TrackSegment) {
	parts := disp.participants(requester, route)

	finished := make([]bool, len(parts))
	holder := make(map[ids.TrackID]int) // track -> index of the unfinished participant holding it
	for i, p := range parts {
		for _, trk := range p.holds {
			holder[trk.Id] = i
		}
	}
	lockedByOther := func(self int, edge *infra.GraphEdge) bool {
		for _, sb := range disp.world.SwitchBlocksOn(edge.Track) {
			active, lockedBy := disp.ilk.SwitchState(sb)
			if lockedBy.IsZero() || active == edge {
				continue // free, or set for this very edge (the track check covers that)
			}
			for j, q := range parts {
				if j != self && !finished[j] && q.train.ID() == lockedBy {
					return true
				}
			}
		}
		return false
	}

	for progress := true; progress; {
		progress = false
		for i, p := range parts {
			if finished[i] {
				continue
			}
			if len(p.remaining) > 0 {
				blocked := func(s infra.Step) bool {
					if j, ok := holder[s.Edge.Track.Id]; ok && j != i && !finished[j] {
						return true
					}
					return lockedByOther(i, s.Edge)
				}
				ok, err := disp.world.CanVisit(p.pos, p.remaining, blocked)
				if err != nil || !ok {
					continue
				}
			}
			finished[i] = true
			progress = true
		}
	}

	var blockers []*infra.TrackSegment
	allDone := true
	for i, p := range parts {
		if finished[i] {
			continue
		}
		allDone = false
		if p.train.ID() != requester.train.ID() {
			blockers = append(blockers, p.holds...)
		}
	}
	return allDone, blockers
}
