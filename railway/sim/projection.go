package sim

import (
	"slices"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/motion"
	"trainapp/railway/ops"
	"trainapp/units"
)

// Look-ahead.
//
// A projection is a train's unimpeded future: each track its front will run
// over, when it gets there and when it leaves, up to the horizon. It assumes
// nothing gets in the way. Run times come from the run-time model
// (motion.Plan), dwells from the schedule, and the way is the one
// the dispatcher would choose: the track the station rules prefer (see
// stationplan.go), then the first ranked path to it.

// slot is a track the train's front runs over, entering it at enter and
// leaving it at exit. The track is free of the train at clear, once its rear
// has left it too: exit for a train with no length.
type slot struct {
	step               infra.Step
	enter, exit, clear float64
}

func (s slot) track() *infra.TrackSegment { return s.step.Edge.Track }

// visit is a call at a station: the platform track the train uses and when it
// arrives and leaves.
type visit struct {
	stop     int // index in the schedule
	stn      *infra.Station
	pf       *infra.Platform
	arr, dep float64
	slot     int // index of the platform track's slot

	// from is where the train sets off for the station; nil when it enters
	// the world there.
	from *infra.Position
	// committed is true when the train is on the platform track or already
	// holds a route onto it.
	committed bool
}

type projection struct {
	train  *ops.Train
	slots  []slot
	visits []visit
	// stand is the slot where the train stands if what it asks for next is
	// refused: where it is, or for a running train the end of what it has
	// been granted
	stand int
	// tail are the tracks behind the front's track that the train's tail is
	// on now, oldest first; only their clear is set.
	tail []slot
}

// clearOf is when the train is due to be off the track: its rear has left
// it. It reports false if the train is not projected to be on it.
func (pr *projection) clearOf(track *infra.TrackSegment) (float64, bool) {
	for _, list := range [2][]slot{pr.tail, pr.slots} {
		for _, s := range list {
			if s.track() == track {
				return s.clear, true
			}
		}
	}
	return 0, false
}

// setClears works out when the rear leaves each slot's track, and the
// tracks the tail is on now, by walking the front forward the train's
// length. The front only runs over a platform track until it arrives;
// standing there it clears nothing. A rear exactly at a track's end clears
// it when the front moves on, as the controller does (freeCleared). Past the
// end of the projection, a track counts as clear when the last slot is left.
func (pr *projection) setClears(head *ops.BodyStep, body []ops.BodyStep) {
	t := pr.train
	if t.Length <= 0 {
		for i := range pr.slots {
			pr.slots[i].clear = pr.slots[i].exit
		}
		return
	}
	n := len(pr.slots)
	if n == 0 {
		return
	}
	arrived := map[int]float64{} // a visit's slot: when the front arrives
	for _, v := range pr.visits {
		arrived[v.slot] = v.arr
	}
	starts := make([]float64, n) // odometer at each slot's start
	odo := 0.0
	if head != nil {
		odo = head.Start
	}
	for i, s := range pr.slots {
		starts[i] = odo
		odo += float64(s.track().Length)
	}
	// at is when the front's odometer reaches target
	at := func(target float64) float64 {
		for j, s := range pr.slots {
			length := float64(s.track().Length)
			end := starts[j] + length
			switch {
			case target > end:
				continue
			case target == end || length == 0:
				return s.exit // as it moves on
			}
			running := s.exit
			if arr, ok := arrived[j]; ok {
				running = arr
			}
			return s.enter + (target-starts[j])/length*(running-s.enter)
		}
		return pr.slots[n-1].exit
	}
	for i := range pr.slots {
		pr.slots[i].clear = max(pr.slots[i].exit, at(starts[i]+float64(pr.slots[i].track().Length)+float64(t.Length)))
	}
	for _, b := range body {
		pr.tail = append(pr.tail, slot{step: b.Step, clear: at(t.ClearPoint(b))})
	}
}

// visitAt returns the first visit to the station, if any.
func (pr *projection) visitAt(stn *infra.Station) (visit, bool) {
	for _, v := range pr.visits {
		if v.stn == stn {
			return v, true
		}
	}
	return visit{}, false
}

// visitOf returns the visit for schedule point stop, if projected.
func (pr *projection) visitOf(stop int) (visit, bool) {
	for _, v := range pr.visits {
		if v.stop == stop {
			return v, true
		}
	}
	return visit{}, false
}

// uses reports whether the train runs over the track.
func (pr *projection) uses(track *infra.TrackSegment) bool {
	return slices.ContainsFunc(pr.slots, func(s slot) bool { return s.track() == track })
}

// dwellAt is how long the train stands at sp if it arrives at arr, as the
// controller would schedule it.
func (s *Sim) dwellAt(sp *ops.SchedulePoint, arr float64) float64 {
	return max(float64(sp.ExpDwellTime(arr, s.MinStop())), des.MinDeltaTime)
}

func endOf(step infra.Step) infra.Position {
	return infra.Position{At: step.To, Via: step.Edge}
}

// project returns the train's projection, cached until the state changes. A
// train already waiting for track another train holds sets off no earlier
// than that train is due to leave it.
func (p *precedence) project(t *ops.Train) *projection {
	if pr, ok := p.projections[t.ID()]; ok {
		return pr
	}
	p.building[t.ID()] = true
	pr := p.buildProjection(t, p.blockedUntil(t.ID()))
	delete(p.building, t.ID())
	p.projections[t.ID()] = pr
	return pr
}

// projectNow is the train's projection if it set off now, whatever it waits
// for: what precedence weighs when deciding whether to let it go.
func (p *precedence) projectNow(t *ops.Train) *projection {
	if pr, ok := p.nowProjections[t.ID()]; ok {
		return pr
	}
	pr := p.buildProjection(t, 0)
	p.nowProjections[t.ID()] = pr
	return pr
}

// blockedUntil is when the tracks the train's queued request waits on are
// due to be free: when the trains holding them are projected to leave them.
// 0 if it waits on nothing, or on nothing whose end can be foreseen.
func (p *precedence) blockedUntil(id ids.TrainID) float64 {
	s := p.sim
	if s.dispatcher.requests[id] == nil {
		return 0
	}
	until := 0.0
	for _, trk := range s.dispatcher.waits.BlockedOn(id) {
		track, ok := s.world.TrackGraph.GetTrack(trk)
		if !ok {
			continue
		}
		res, occ := s.ilk.TrackHolder(track)
		holder := occ
		if holder.IsZero() {
			holder = res
		}
		if holder.IsZero() || holder == id || p.building[holder] {
			continue // free already, or the holder's own end depends on this train
		}
		hp := p.project(s.trainCtrllers[holder].train)
		for _, sl := range hp.slots {
			if sl.track() == track {
				until = max(until, sl.exit+float64(p.cfg.HeadwayMin))
				break
			}
		}
	}
	return until
}

// buildProjection projects the train from where it stands, setting off no
// earlier than notBefore if it is waiting to.
func (p *precedence) buildProjection(t *ops.Train, notBefore float64) *projection {
	s := p.sim
	tc := s.trainCtrllers[t.ID()]
	now := s.CurTime()
	end := now + p.cfg.horizon()
	sched := t.Schedule()
	pr := &projection{train: t}

	var pos infra.Position
	var at float64 // when the front sets off from pos
	next := 0      // the next stop to run to
	// off and v are where along the first step of the next run the front
	// sets off from, and its speed then; trail is the steps behind it, for
	// a lower limit holding until the rear has left it
	var off units.Meters
	var v units.MetersPerMin
	var trail []infra.Step

	// arrive adds the slot for a platform track of stop k entered at enter
	// and run in run minutes, and the visit. It returns when the train
	// leaves.
	arrive := func(step infra.Step, run float64, k int, enter float64, from *infra.Position, committed bool) float64 {
		stn, _ := s.world.GetStation(sched[k].StnCode)
		pf := platformOf(stn, step.Edge.Track)
		arr := enter + run
		_, extra := s.overhang(t, sched[k], pf)
		dep := arr + s.dwellAt(sched[k], arr) + extra
		pr.slots = append(pr.slots, slot{step: step, enter: enter, exit: dep})
		pr.visits = append(pr.visits, visit{stop: k, stn: stn, pf: pf, arr: arr, dep: dep, slot: len(pr.slots) - 1, from: from, committed: committed})
		return dep
	}
	// visits reports whether step i of steps is the platform track of stop
	// k: at the end, or for a pass anywhere on the way (a train granted a
	// section beyond the pass it runs through)
	visits := func(steps []infra.Step, i, k int) bool {
		return k < len(sched) && (i == len(steps)-1 || sched[k].IsPass()) && p.isPlatformOfStop(steps[i].Edge.Track, sched[k])
	}
	// run adds slots for steps from at, and a visit for each stop it gets
	// to on them. The train runs free: it stops only to call at a stop at
	// the end, and runs through a pass.
	run := func(steps []infra.Step, from *infra.Position, committed bool) {
		if len(steps) == 0 {
			return
		}
		stops := false
		for i, k := 0, next; i < len(steps); i++ {
			if visits(steps, i, k) {
				stops = i == len(steps)-1 && !sched[k].IsPass()
				k++
			}
		}
		c := motion.Plan(t, motion.Way{Behind: trail, Steps: steps, From: off, Speed: v, Stop: stops})
		for i, st := range steps {
			if visits(steps, i, next) {
				at = arrive(st, c.Time(i), next, at, from, committed)
				next++
			} else {
				exit := at + c.Time(i)
				pr.slots = append(pr.slots, slot{step: st, enter: at, exit: exit})
				at = exit
			}
			pos = endOf(st)
		}
		off, v = 0, c.ExitSpeed()
		if stops {
			v = 0
		}
		trail = tailOf(append(trail, steps...), t.Length)
	}

	switch tc.phase {
	case phaseExited:
		return pr
	case phaseOutside:
		first, ok := s.world.GetStation(sched[0].StnCode)
		if !ok {
			return pr
		}
		var step infra.Step
		committed := false
		if r := s.dispatcher.frontier[t.ID()]; r != nil {
			step, committed = r.Step(0), true
		} else {
			cands := p.entryCandidates(first)
			if len(cands) == 0 {
				return pr
			}
			ranked := p.rankCandidates(t, 0, first, cands, p.stayAt(t, 0, first))
			if len(ranked) == 0 {
				return pr
			}
			step = ranked[0].steps[0]
		}
		start := max(sched[0].ArrTime, now)
		if !committed {
			start = max(start, notBefore)
		}
		at, v = start, t.EntersAt()
		run([]infra.Step{step}, nil, committed)
	default:
		route := t.Route()
		if route == nil {
			return pr
		}
		head, ok := route.Head()
		if !ok {
			return pr
		}
		body := t.Body()
		for _, b := range body[:max(len(body)-1, 0)] {
			trail = append(trail, b.Step)
		}
		// the rest of what is already reserved: the current route, then a
		// newer one granted but not started yet
		var committed []infra.Step
		for i := route.Entered(); i < route.End(); i++ {
			committed = append(committed, route.Step(i))
		}
		if r := s.dispatcher.frontier[t.ID()]; r != nil && r != route {
			for i := range r.End() {
				committed = append(committed, r.Step(i))
			}
		}
		switch tc.phase {
		case phaseDwelling, phaseReady:
			dep := max(now, notBefore)
			if tc.phase == phaseDwelling {
				dep = max(tc.dwellEnd, now)
			}
			k := t.StopIndex()
			stn, _ := s.world.GetStation(sched[k].StnCode)
			pr.slots = append(pr.slots, slot{step: head, enter: tc.since, exit: dep})
			pr.visits = append(pr.visits, visit{stop: k, stn: stn, pf: platformOf(stn, head.Edge.Track), arr: tc.arrivedAt, dep: dep, slot: 0, committed: true})
			at, next = dep, k+1
			pos = endOf(head)
			run(committed, nil, true)
		case phaseHeld:
			dep := max(now, notBefore)
			pr.slots = append(pr.slots, slot{step: head, enter: tc.since, exit: dep})
			at, next = dep, t.StopIndex()
			pos = endOf(head)
			run(committed, nil, true)
		default: // running: from where its curve has it, the front's step first
			off, v, at = tc.projectFrom()
			next = t.StopIndex()
			if tc.dynamic() {
				run(append([]infra.Step{head}, committed...), nil, true)
				pr.slots[0].enter = tc.since
				pr.stand = len(pr.slots) - 1
				break
			}
			// A train that stops instantly keeps the look-ahead it had
			// before Phase 3: its front's step is never a visit, so while
			// its front runs onto its stop's platform the projection ends
			// there. Fixing that changes runs; see
			// docs/trainapp/length_dynamics/phase3.md, As built.
			c := motion.Plan(t, motion.Way{Behind: trail, Steps: []infra.Step{head}, From: off, Speed: v})
			at += c.Time(0)
			pr.slots = append(pr.slots, slot{step: head, enter: tc.since, exit: at})
			pos = endOf(head)
			off, v = 0, c.ExitSpeed()
			run(committed, nil, true)
			pr.stand = len(pr.slots) - 1
		}
	}

	for next < len(sched) && at <= end {
		stn, ok := s.world.GetStation(sched[next].StnCode)
		if !ok {
			break
		}
		from := pos
		cands := p.candidates(t, from, stn)
		if len(cands) == 0 {
			break
		}
		ranked := p.rankCandidates(t, next, stn, cands, p.stayAt(t, next, stn))
		if len(ranked) == 0 {
			break
		}
		run(ranked[0].steps, &from, false)
	}

	// in the world, the first slot is the front's track, the last body step
	var head *ops.BodyStep
	var tail []ops.BodyStep
	if body := t.Body(); tc.phase != phaseOutside && len(body) > 0 {
		h := body[len(body)-1]
		head, tail = &h, body[:len(body)-1]
	}
	pr.setClears(head, tail)
	return pr
}

// tailOf is the last of steps that a train of the given length can be on:
// those within its length of the end.
func tailOf(steps []infra.Step, length units.Meters) []infra.Step {
	if length <= 0 {
		return nil
	}
	covered := 0.0
	i := len(steps)
	for i > 0 && covered < float64(length) {
		i--
		covered += float64(steps[i].Edge.Track.Length)
	}
	return steps[i:]
}

func (p *precedence) isPlatformOfStop(track *infra.TrackSegment, sp *ops.SchedulePoint) bool {
	stn, ok := p.sim.world.GetStation(sp.StnCode)
	return ok && stn.IsPlatform(track.Id)
}

func platformOf(stn *infra.Station, track *infra.TrackSegment) *infra.Platform {
	for _, pf := range stn.Platforms() {
		if pf.Track == track {
			return pf
		}
	}
	return nil
}

// candidate is a way from where a train stands to one of a station's platform
// tracks.
type candidate struct {
	pf      *infra.Platform
	steps   []infra.Step
	correct bool        // runs the way the signals face
	line    ids.TrackID // the running line it enters the station's area over; zero if none
}

// candidates lists the best way to each of the station's platform tracks from
// from, in platform order. As the dispatcher does, wrong-direction ways are
// dropped when any platform can be reached the right way.
func (p *precedence) candidates(t *ops.Train, from infra.Position, stn *infra.Station) []candidate {
	var out []candidate
	anyCorrect := false
	for _, pf := range stn.Platforms() {
		ranked := p.sim.dispatcher.rankedPaths(from.At, from.Via, pf.Track)
		if len(ranked) == 0 {
			continue
		}
		steps, err := ranked[0].Path.Steps()
		if err != nil {
			continue
		}
		c := candidate{pf: pf, steps: steps, correct: ranked[0].FollowsSignals}
		for i := len(steps) - 1; i >= 0; i-- {
			if !stn.InArea(steps[i].Edge.Track.Id) {
				c.line = steps[i].Edge.Track.Id
				break
			}
		}
		if c.line.IsZero() && from.Via != nil && !stn.InArea(from.Via.Track.Id) {
			c.line = from.Via.Track.Id
		}
		out = append(out, c)
		anyCorrect = anyCorrect || c.correct
	}
	if anyCorrect {
		out = slices.DeleteFunc(out, func(c candidate) bool { return !c.correct })
	}
	return out
}

// entryCandidates lists the station's platforms a train can enter the world
// on, each as the one step from the world boundary.
func (p *precedence) entryCandidates(stn *infra.Station) []candidate {
	var out []candidate
	for _, pf := range stn.Platforms() {
		b := p.sim.world.TrackGraph.FindWorldBoundaryPoint(pf.Track)
		if b == nil {
			continue
		}
		edge, _ := p.sim.world.TrackGraph.Edge(pf.Track)
		out = append(out, candidate{pf: pf, steps: []infra.Step{{Edge: edge, From: b, To: edge.Other(b)}}, correct: true})
	}
	return out
}
