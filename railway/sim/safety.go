package sim

import (
	"slices"
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
//
// Precedence holds (precedence.go) are part of the order: a train held for
// another can only come after it, once the other has reached its milestone,
// the first station past the held train. A train with a milestone can make
// that much progress on its own: it moves to the milestone, giving up the
// track it holds now and standing on whichever platform track it could reach
// there. A train that is the beneficiary of a hold counts even before it has
// entered the world.
//
// With precedence on, the check also reasons the way the milestone step does,
// so that a move judged safe while a hold was in place does not look unsafe
// once the hold ends:
//   - every train is judged where it will stand once its granted route is
//     run, holding only the track it ends on: routes always run to the end,
//     and each track (with its switches) is released as the train leaves it;
//   - when no train can finish, one train may move on a single stop, after
//     which the check looks again.
// Both are still sound: an order found is made of moves the trains can
// really make. With precedence off the check is the plain one it always was.

// participant is a train as the safety check sees it.
type participant struct {
	train     *ops.Train
	pos       []infra.Position // where it may stand once its granted route is run
	remaining []ids.StnCode    // stops still to call at from pos
	next      int              // schedule index of remaining[0]
	holds     map[ids.TrackID]bool
}

// stopAfter returns the index of the first stop the train still has to call
// at once it has run route: the stop it is heading to, or the one after if the
// route ends on one of its platforms. headingTo is the index of that stop.
func (disp *Dispatcher) stopAfter(train *ops.Train, headingTo int, route *interlocking.Route) int {
	sched := train.Schedule()
	k := headingTo
	if route != nil && route.End() > 0 && k < len(sched) {
		last := route.Step(route.End() - 1).Edge.Track
		if stn, ok := disp.world.GetStation(sched[k].StnCode); ok && stn.IsPlatform(last.Id) {
			k++
		}
	}
	return min(k, len(sched))
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

// standing returns where the train stands for planning, once its granted
// route is run, and the index of the first stop it still has to call at. A
// train not in the world yet stands at the boundary of its first station.
func (disp *Dispatcher) standing(t *ops.Train) ([]infra.Position, int) {
	return disp.standingWith(t, disp.frontier[t.ID()])
}

func (disp *Dispatcher) standingWith(t *ops.Train, r *interlocking.Route) ([]infra.Position, int) {
	tc := disp.sim.trainCtrllers[t.ID()]
	sched := t.Schedule()
	switch {
	case tc.phase == phaseExited:
		return nil, len(sched)
	case r != nil:
		return []infra.Position{routeEnd(r)}, disp.stopAfter(t, headingTo(tc), r)
	case t.FacingToward != nil:
		return []infra.Position{{At: t.FacingToward, Via: t.CurrentEdge()}}, disp.stopAfter(t, headingTo(tc), nil)
	}
	var out []infra.Position
	if stn, ok := disp.world.GetStation(sched[0].StnCode); ok {
		for _, pf := range stn.Platforms() {
			if b := disp.world.TrackGraph.FindWorldBoundaryPoint(pf.Track); b != nil {
				out = append(out, infra.Position{At: b})
			}
		}
	}
	return out, 0
}

// participants lists the trains in the world as they would be if route were
// granted to requester, in the order trains were added, plus the trains holds
// wait for that have not entered yet. requester may be nil.
func (disp *Dispatcher) participants(requester *TrainController, route *interlocking.Route, holds []holdConstraint) []participant {
	needed := map[ids.TrainID]bool{}
	for _, h := range holds {
		needed[h.forTrain] = true
	}
	var out []participant
	for _, t := range disp.sim.trains {
		tc := disp.sim.trainCtrllers[t.ID()]
		r := disp.frontier[t.ID()]
		if tc == requester {
			r = route
		}
		// a train granted its entry route counts even before it has moved
		inWorld := tc.phase != phaseExited && (tc.phase != phaseOutside || r != nil)
		if !inWorld && tc != requester && !(needed[t.ID()] && tc.phase == phaseOutside) {
			continue
		}
		p := participant{train: t, holds: map[ids.TrackID]bool{}}
		p.pos, p.next = disp.standingWith(t, r)
		if len(p.pos) == 0 {
			continue // not placed yet and nothing granted: holds nothing, needs nothing yet
		}
		if disp.prec != nil {
			for _, pos := range p.pos {
				if pos.Via != nil {
					p.holds[pos.Via.Track.Id] = true
				}
			}
		} else {
			for _, trk := range disp.ilk.Holdings(t.ID()) {
				p.holds[trk.Id] = true
			}
		}
		for _, sp := range t.Schedule()[p.next:] {
			p.remaining = append(p.remaining, sp.StnCode)
		}
		out = append(out, p)
	}
	return out
}

// safe reports whether the state is safe if route is granted to requester,
// with the given holds in place. requester and route may be nil to check the
// holds alone. When it is not safe, it returns the tracks held by the trains
// that could not be shown to finish: the state can only improve when one of
// them is released.
func (disp *Dispatcher) safe(requester *TrainController, route *interlocking.Route, holds []holdConstraint) (bool, []*infra.TrackSegment) {
	parts := disp.participants(requester, route, holds)
	index := map[ids.TrainID]int{}
	for i, p := range parts {
		index[p.train.ID()] = i
	}

	// mile[i] is how many of i's remaining stops it must call at to be past
	// the trains held for it (0: none held for it, or it must finish);
	// after[i] are the trains i is held for.
	mile := make([]int, len(parts))
	mustFinish := make([]bool, len(parts))
	after := make([][]int, len(parts))
	for _, h := range holds {
		i, okHeld := index[h.held]
		j, okFor := index[h.forTrain]
		if !okHeld || !okFor {
			continue // the held train holds nothing, or the other has left
		}
		if h.milestone < 0 {
			mustFinish[j] = true
		} else if n := h.milestone - parts[j].next + 1; n > 0 {
			mile[j] = max(mile[j], n)
		} else {
			continue // already past
		}
		after[i] = append(after[i], j)
	}
	for j := range parts {
		if mustFinish[j] || mile[j] >= len(parts[j].remaining) {
			mile[j] = 0
		}
	}

	ck := &safetyCheck{disp: disp, parts: parts, after: after, mile: mile, budget: searchBudget}
	root := ck.start()
	ok := ck.solve(root)
	finished := root.finished

	if ok {
		return true, nil
	}
	var blockers []*infra.TrackSegment
	for i, p := range parts {
		if finished[i] || (requester != nil && p.train.ID() == requester.train.ID()) {
			continue
		}
		blockers = append(blockers, disp.ilk.Holdings(p.train.ID())...)
	}
	return false, blockers
}

// searchBudget caps how many states the check looks at when it has to try
// moving trains on a stop at a time. Running out counts as unsafe.
const searchBudget = 64

// safetyCheck searches for an order in which every participant can finish.
type safetyCheck struct {
	disp   *Dispatcher
	parts  []participant
	after  [][]int // trains each is held for
	mile   []int   // stops each must move on by to be past those held for it
	budget int
}

// checkState is where the participants stand at one point of the search.
type checkState struct {
	pos       [][]infra.Position
	remaining [][]ids.StnCode
	holds     []map[ids.TrackID]bool
	finished  []bool
	advanced  []int // stops each has moved on by
}

func (ck *safetyCheck) start() *checkState {
	n := len(ck.parts)
	st := &checkState{pos: make([][]infra.Position, n), remaining: make([][]ids.StnCode, n), holds: make([]map[ids.TrackID]bool, n), finished: make([]bool, n), advanced: make([]int, n)}
	for i, p := range ck.parts {
		st.pos[i], st.remaining[i], st.holds[i] = p.pos, p.remaining, p.holds
	}
	return st
}

func (st *checkState) clone() *checkState {
	c := &checkState{
		pos: slices.Clone(st.pos), remaining: slices.Clone(st.remaining), holds: slices.Clone(st.holds),
		finished: slices.Clone(st.finished), advanced: slices.Clone(st.advanced),
	}
	return c
}

// past reports whether train j no longer holds anyone up.
func (ck *safetyCheck) past(st *checkState, j int) bool {
	return st.finished[j] || (ck.mile[j] > 0 && st.advanced[j] >= ck.mile[j])
}

// held reports whether train i still waits for a train it is held for.
func (ck *safetyCheck) held(st *checkState, i int) bool {
	return slices.ContainsFunc(ck.after[i], func(j int) bool { return !ck.past(st, j) })
}

// blocked rejects steps train i cannot take: onto track another unfinished
// train holds, or over a switch one has locked the other way.
func (ck *safetyCheck) blocked(st *checkState, i int) func(infra.Step) bool {
	disp := ck.disp
	return func(s infra.Step) bool {
		for j := range ck.parts {
			if j != i && !st.finished[j] && st.holds[j][s.Edge.Track.Id] {
				return true
			}
		}
		for _, sb := range disp.world.SwitchBlocksOn(s.Edge.Track) {
			active, lockedBy := disp.ilk.SwitchState(sb)
			if lockedBy.IsZero() || active == s.Edge {
				continue // free, or set for this very edge (the track check covers that)
			}
			for j, q := range ck.parts {
				if j == i || st.finished[j] || q.train.ID() != lockedBy {
					continue
				}
				// judged where it ends up, a train keeps only the locks on
				// the track it stands on
				if disp.prec != nil && !slices.ContainsFunc(sb.ListManagedEdges(), func(e *infra.GraphEdge) bool { return st.holds[j][e.Track.Id] }) {
					continue
				}
				return true
			}
		}
		return false
	}
}

// moveTo has train i move on by n stops to end: it stands there and gives up
// what it held.
func (st *checkState) moveTo(i, n int, end infra.Position) {
	st.pos[i] = []infra.Position{end}
	st.remaining[i] = st.remaining[i][n:]
	st.holds[i] = map[ids.TrackID]bool{end.Via.Track.Id: true}
	st.advanced[i] += n
}

// solve finishes every train it can, which only ever frees track. If some
// are left and precedence is on, it tries moving one on, a stop at a time
// (or to its milestone), to each platform track it could reach, and looks
// again from there.
func (ck *safetyCheck) solve(st *checkState) bool {
	world := ck.disp.world
	for progress := true; progress; {
		progress = false
		for i := range ck.parts {
			if st.finished[i] || ck.held(st, i) {
				continue
			}
			if len(st.remaining[i]) > 0 {
				ends, err := world.VisitEnds(st.pos[i], st.remaining[i], ck.blocked(st, i))
				if err != nil || len(ends) == 0 {
					continue
				}
			}
			st.finished[i] = true
			progress = true
		}
	}
	if !slices.Contains(st.finished, false) {
		return true
	}
	if ck.disp.prec == nil {
		return false
	}
	for i := range ck.parts {
		if st.finished[i] || len(st.remaining[i]) == 0 || ck.held(st, i) {
			continue
		}
		n := 1
		if ck.mile[i] > st.advanced[i] {
			n = ck.mile[i] - st.advanced[i] // trains held for it wait for all of it
		} else if len(st.remaining[i]) < 2 {
			continue // one stop left: moving on is finishing
		}
		ends, err := world.VisitEnds(st.pos[i], st.remaining[i][:n], ck.blocked(st, i))
		if err != nil {
			continue
		}
		for _, end := range ends {
			if ck.budget <= 0 {
				return false
			}
			ck.budget--
			next := st.clone()
			next.moveTo(i, n, end)
			if ck.solve(next) {
				return true
			}
		}
	}
	return false
}
