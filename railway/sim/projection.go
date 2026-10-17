package sim

import (
	"slices"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
)

// Look-ahead.
//
// A projection is a train's unimpeded future: each track its front will run
// over, when it gets there and when it leaves, up to the horizon. It assumes
// nothing gets in the way. Travel times come from the speed limits
// (TrackSegment.TravelTime), dwells from the schedule, and the way is the one
// the dispatcher would choose: the track the station rules prefer (see
// stationplan.go), then the first ranked path to it.

// slot is a track the train's front runs over, entering it at enter and
// leaving it at exit.
type slot struct {
	step        infra.Step
	enter, exit float64
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

	// arrive adds the slot for a platform track of stop k entered at enter,
	// and the visit. It returns when the train leaves.
	arrive := func(step infra.Step, k int, enter float64, from *infra.Position, committed bool) float64 {
		stn, _ := s.world.GetStation(sched[k].StnCode)
		pf := platformOf(stn, step.Edge.Track)
		arr := enter + float64(step.Edge.Track.TravelTime(t.MaxSpeed))
		dep := arr + s.dwellAt(sched[k], arr)
		pr.slots = append(pr.slots, slot{step, enter, dep})
		pr.visits = append(pr.visits, visit{stop: k, stn: stn, pf: pf, arr: arr, dep: dep, slot: len(pr.slots) - 1, from: from, committed: committed})
		return dep
	}
	// run adds slots for steps from at, arriving at stop next if the last
	// one is its platform track.
	run := func(steps []infra.Step, from *infra.Position, committed bool) {
		for i, st := range steps {
			if i == len(steps)-1 && next < len(sched) && p.isPlatformOfStop(st.Edge.Track, sched[next]) {
				at = arrive(st, next, at, from, committed)
				next++
			} else {
				exit := at + float64(st.Edge.Track.TravelTime(t.MaxSpeed))
				pr.slots = append(pr.slots, slot{st, at, exit})
				at = exit
			}
			pos = endOf(st)
		}
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
			step = p.rankCandidates(t, 0, first, cands, p.stayAt(t, 0, first))[0].steps[0]
		}
		start := max(sched[0].ArrTime, now)
		if !committed {
			start = max(start, notBefore)
		}
		at = arrive(step, 0, start, nil, committed)
		next = 1
		pos = endOf(step)
	default:
		route := t.Route()
		if route == nil {
			return pr
		}
		head, ok := route.Head()
		if !ok {
			return pr
		}
		switch tc.phase {
		case phaseDwelling, phaseReady:
			dep := max(now, notBefore)
			if tc.phase == phaseDwelling {
				dep = max(tc.dwellEnd, now)
			}
			k := t.StopIndex()
			stn, _ := s.world.GetStation(sched[k].StnCode)
			pr.slots = append(pr.slots, slot{head, tc.since, dep})
			pr.visits = append(pr.visits, visit{stop: k, stn: stn, pf: platformOf(stn, head.Edge.Track), arr: tc.arrivedAt, dep: dep, slot: 0, committed: true})
			at, next = dep, k+1
		case phaseHeld:
			dep := max(now, notBefore)
			pr.slots = append(pr.slots, slot{head, tc.since, dep})
			at, next = dep, t.StopIndex()
		default: // running
			exit := max(tc.since+float64(head.Edge.Track.TravelTime(t.MaxSpeed)), now)
			pr.slots = append(pr.slots, slot{head, tc.since, exit})
			at, next = exit, t.StopIndex()
		}
		pos = endOf(head)

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
		run(committed, nil, true)
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
		best := p.rankCandidates(t, next, stn, cands, p.stayAt(t, next, stn))[0]
		run(best.steps, &from, false)
	}
	return pr
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
