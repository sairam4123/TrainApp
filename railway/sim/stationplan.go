package sim

import (
	"cmp"
	"math"
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/units"
)

// Station plan.
//
// Which station track a train should get. Through tracks are kept free for
// trains that run through. A stopping train may use the main line, but only
// for a short stay (main_line_max_stay_min). A train that runs through takes
// the fastest track: a through track, then the main line for the line it
// arrives on, then a loop. One expected to stand there, e.g. waiting for a
// crossing, takes a loop instead: the main line and through tracks are its
// last resort, so they stay free for the trains running through. The one
// exception is a short stand (main_line_max_stay_min) on its own main where
// the only loops lie across another line's main: crossing over and back costs
// more than the stand.
//
// With through_track_max_stay_min set, a train that does not call (a pass or
// a halt) takes its own through track first, whether it runs through or
// stands there, up to that long: the platforms stay free for trains that
// call. A higher-priority train due in the same window still gets the
// through track first, as the plan goes highest priority first.
//
// The plan gives out a station's tracks to the trains due there within the
// horizon, highest priority first, each taking the first track in its order
// that is free for its whole stay. So a higher-priority train keeps its
// track, and one move, of the lower-priority train, usually settles a
// conflict. A train left with no track has a capacity conflict.

// Reasons a train is moved off its timetabled platform.
const (
	reasonBooked       = "booked"         // a higher-priority train needs it
	reasonMainLineStay = "main_line_stay" // stop too long for the main line
	reasonThrough      = "through_track"  // a stopping train on a through track
	reasonPassing      = "passing"        // a faster track for a train running through
	reasonStanding     = "standing"       // a loop for a train running through that is expected to stand
	reasonRefuge       = "refuge_through" // a through track for a train not calling that is expected to stand
	reasonTooShort     = "too_short"      // the train is longer than the platform (fit.go)
	reasonOverride     = "override"       // an operator pinned it
)

// plainStay is how long the timetable has the train stand at sp.
func plainStay(sp *ops.SchedulePoint) float64 {
	switch {
	case sp.IsPass():
		return 0
	case sp.DwellTime >= 0:
		return sp.DwellTime
	case sp.DeptTime >= 0 && sp.ArrTime >= 0:
		return sp.DeptTime - sp.ArrTime
	}
	return 0
}

// rank orders a candidate for train t at stop k: lower is better. stay is how
// long the train is expected to stand there. A pinned platform comes first;
// then every candidate the train fits better comes before one it fits worse
// (fit.go), and within a fit the station's rules order them.
func (p *precedence) rank(t *ops.Train, k int, stn *infra.Station, c candidate, stay float64) int {
	r := p.trackRank(t, k, stn, c, stay)
	if r < 0 {
		return r // pinned
	}
	return r + 10*int(fitOf(t, t.Schedule()[k], c.pf))
}

// trackRank orders a candidate by the station's rules alone, from 0 to 9, or
// -1 if an operator pinned it.
func (p *precedence) trackRank(t *ops.Train, k int, stn *infra.Station, c candidate, stay float64) int {
	sp := t.Schedule()[k]
	if pf, ok := p.pinned[pinKey{t.ID(), stn.Code}]; ok && pf == c.pf.PfNo {
		return -1
	}
	class := stn.TrackClass(c.pf.Id)
	timetabled := c.pf.PfNo == sp.SpPfNo
	mine := false
	if m, ok := stn.MainFor(c.line); ok && m == c.pf {
		mine = true
	}
	// another line's main line counts as a loop, through track or not: which
	// of those is best is down to which side of the station the train is on
	// (rankCandidates)
	others := !mine && len(stn.MainLinesOf(c.pf.Id)) > 0
	if p.cfg.throughRefuge() && !passengers(sp) {
		return p.notCallingRank(t, k, stn, c, stay, mine, others)
	}
	if sp.IsPass() && p.standing(stay) {
		// a loop on its own side first; a short stand on its own main
		// before crossing another line's main to reach a loop
		r := 0
		switch {
		case class == infra.Main && mine && stay <= float64(p.cfg.MainLineMaxStayMin):
			r = 2
		case class == infra.Through || class == infra.Main:
			r = 6 // the last resort, whichever line's it is
		default:
			if crossings, _ := p.side(t, k, stn, c); crossings > 0 {
				r = 4
			}
		}
		if !timetabled {
			r++
		}
		return r
	}
	if sp.IsPass() {
		r := 4
		switch {
		case class == infra.Through && !others:
			r = 0
		case class == infra.Main && mine:
			r = 2
		}
		if !timetabled {
			r++
		}
		return r
	}
	// the stay limit keeps the main line free for trains running through; a
	// terminus has none
	mainOK := stay <= float64(p.cfg.MainLineMaxStayMin) || !stn.RunsThrough()
	switch {
	case class == infra.Through:
		return 9
	case timetabled && (class == infra.Loop || mainOK):
		return 0
	case class == infra.Main && mainOK && mine:
		return 1
	case class == infra.Loop || mainOK:
		return 3
	}
	return 5 // a main line, for too long a stay
}

// notCallingRank is trackRank for a train that does not call, with
// through_track_max_stay_min set: it takes its own through track first,
// whether it runs through or stands, so the platforms stay free for trains
// that call. Then its own main, for a short stand only, then a loop, its own
// side first. Another line's main counts as a loop for a train running
// through, as in trackRank; for one that stands it is the last resort, as is
// a track it would stand on too long. So is the through track for one that
// stands while a train of the same or higher priority that does not call is
// due (metByEqual): the through track stays free for that train.
func (p *precedence) notCallingRank(t *ops.Train, k int, stn *infra.Station, c candidate, stay float64, mine, others bool) int {
	sp := t.Schedule()[k]
	class := stn.TrackClass(c.pf.Id)
	standing := p.standing(stay)
	r := 4
	switch {
	case others:
		if standing {
			r = 8
		}
	case class == infra.Through:
		r = 0
		if standing && p.metByEqual(t, k, stn, stay) {
			r = 8
		}
	case class == infra.Main && mine:
		r = 2
	case standing:
		if crossings, _ := p.side(t, k, stn, c); crossings > 0 {
			r = 6
		}
	}
	if !others && class != infra.Loop && p.standable(sp, stn, c.pf, stay) != "" {
		r = 8
	}
	if c.pf.PfNo != sp.SpPfNo {
		r++
	}
	return r
}

// metByEqual reports whether another train of the same or higher priority
// that does not call at stn, so may want the through track, is due there,
// whichever way it runs, while train t stands there for stay minutes at its
// stop k. One that calls takes a platform and is not in the way. It is told from the
// projections, so only once t's own is built: the station plan builds every
// train's before ranking their tracks, while a projection being built ranks
// tracks without it. A train whose projection is being built is left out.
func (p *precedence) metByEqual(t *ops.Train, k int, stn *infra.Station, stay float64) bool {
	s := p.sim
	pr, ok := p.projections[t.ID()]
	if !ok {
		return false
	}
	v, ok := pr.visitOf(k)
	if !ok {
		return false
	}
	hw := float64(p.cfg.HeadwayMin)
	from, to := v.arr-hw, v.arr+stay+hw
	mine := s.Priority(t.ID())
	for _, h := range s.trains {
		if h == t || p.building[h.ID()] || s.trainCtrllers[h.ID()].phase == phaseExited || s.Priority(h.ID()) < mine {
			continue
		}
		if hv, ok := p.project(h).visitAt(stn); ok && !passengers(h.Schedule()[hv.stop]) && hv.arr < to && hv.dep > from {
			return true
		}
	}
	return false
}

// minStand is the shortest expected stand that sends a train running through
// to a loop, whatever the headway.
const minStand = 0.5

// standing reports whether a train running through, expected to be at the
// station for stay, stands there long enough to keep it off the main line:
// longer than a headway.
func (p *precedence) standing(stay float64) bool {
	return stay > max(float64(p.cfg.HeadwayMin), minStand)
}

// rankCandidates sorts the candidates for train t at stop k, best first.
// Between tracks of the same rank it prefers the one on the train's own side:
// first the way in and out that crosses fewest other running lines' main
// routes, then the quickest way through the station's throats. So a train is
// not sent across the other line to a loop on the far side when there is one
// next to its own. Platform order breaks what ties remain. Tracks the train
// cannot call at (callable) are left out, so the list may be empty.
func (p *precedence) rankCandidates(t *ops.Train, k int, stn *infra.Station, cands []candidate, stay float64) []candidate {
	type keyed struct {
		c         candidate
		rank      int
		crossings int
		time      float64
	}
	var ks []keyed
	for _, c := range cands {
		if !callable(t.Schedule()[k], c.pf) {
			continue
		}
		crossings, time := p.side(t, k, stn, c)
		ks = append(ks, keyed{c, p.rank(t, k, stn, c, stay), crossings, time})
	}
	slices.SortStableFunc(ks, func(a, b keyed) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(a.crossings, b.crossings), cmp.Compare(a.time, b.time))
	})
	out := make([]candidate, len(ks))
	for i, kc := range ks {
		out[i] = kc.c
	}
	return out
}

// side measures how far the candidate's track is from the train's own side
// of the station: how many tracks of its way in and out (the track itself
// included) cross another running line's main route, and the time spent in
// the station's throats on the way, at the track speeds.
func (p *precedence) side(t *ops.Train, k int, stn *infra.Station, c candidate) (int, float64) {
	in := c.steps
	var out []infra.Step
	outLine := ids.TrackID{}
	sched := t.Schedule()
	if k+1 < len(sched) && len(c.steps) > 0 {
		if next, ok := p.sim.world.GetStation(sched[k+1].StnCode); ok && next != stn {
			best := math.Inf(1)
			for _, onward := range p.candidates(t, endOf(c.steps[len(c.steps)-1]), next) {
				steps, line := stationPart(stn, onward.steps)
				if tt := throatTime(stn, c.pf, steps); tt < best {
					best, out, outLine = tt, steps, line
				}
			}
		}
	}
	own := map[ids.TrackID]bool{c.line: true, outLine: true}
	var others []ids.TrackID
	for _, line := range stn.Lines() {
		if !own[line] {
			others = append(others, stn.MainRoute(line)...)
		}
	}
	crossings := 0
	tracks := []*infra.TrackSegment{c.pf.Track}
	for _, st := range append(slices.Clone(in), out...) {
		if st.Edge.Track != c.pf.Track && stn.InArea(st.Edge.Track.Id) {
			tracks = append(tracks, st.Edge.Track)
		}
	}
	for _, trk := range tracks {
		if slices.ContainsFunc(others, func(id ids.TrackID) bool {
			other, _ := p.sim.world.TrackGraph.GetTrack(id)
			return p.tracksConflict(trk, other)
		}) {
			crossings++
		}
	}
	return crossings, throatTime(stn, c.pf, in) + throatTime(stn, c.pf, out)
}

// stationPart is the steps of a way leaving the station that are still in its
// area, and the running line it leaves on (zero if none).
func stationPart(stn *infra.Station, steps []infra.Step) ([]infra.Step, ids.TrackID) {
	for i, st := range steps {
		if !stn.InArea(st.Edge.Track.Id) {
			return steps[:i], st.Edge.Track.Id
		}
	}
	return steps, ids.TrackID{}
}

// throatTime is the time spent on the steps in the station's area, other
// than on the platform track pf itself, at the track speeds. It only ranks
// platforms, with no train in it, so it does not use the run-time model
// (motion.Plan).
func throatTime(stn *infra.Station, pf *infra.Platform, steps []infra.Step) float64 {
	total := 0.0
	for _, st := range steps {
		trk := st.Edge.Track
		if trk != pf.Track && stn.InArea(trk.Id) {
			total += float64(trk.Length) / float64(trk.MaxSpeed)
		}
	}
	return total
}

// stayAt is how long the train is expected to stand at stop k: its timetabled
// stay. The plan adds any hold it predicts.
func (p *precedence) stayAt(t *ops.Train, k int, _ *infra.Station) float64 {
	return plainStay(t.Schedule()[k])
}

// booking is a train's claim on a station track for a time window.
type booking struct {
	train    ids.TrainID
	pf       *infra.Platform
	from, to float64
}

func (b booking) overlaps(from, to float64) bool { return b.from < to && from < b.to }

// assignment is the plan for one train at the station.
type assignment struct {
	pf       *infra.Platform // nil: no track left (a capacity conflict)
	stop     int
	from, to float64     // the window it needs the track for
	stay     float64     // expected stay, with any hold predicted there
	ranked   []candidate // its candidates, best first
	blockers []booking   // with no track: the bookings in its way
	fixed    bool
	// keepBack: a train that does not call, which would stand on a loop a
	// call needs, is better kept at the station before until the calls in
	// keptFor have been made (keepBack). It has no track in the plan;
	// fallback is the loop it takes if it cannot be kept back.
	keepBack bool
	keptFor  []ids.TrainID
	fallback *infra.Platform
}

type stationPlan struct {
	stn      *infra.Station
	assign   map[ids.TrainID]*assignment
	bookings []booking
}

// bookedBy returns the booking of another train on pf that overlaps the
// window, if any.
func (pl *stationPlan) bookedBy(self ids.TrainID, pf *infra.Platform, from, to float64) (booking, bool) {
	for _, b := range pl.bookings {
		if b.train != self && b.pf == pf && b.overlaps(from, to) {
			return b, true
		}
	}
	return booking{}, false
}

// plan returns the station plan, cached until the state changes.
func (p *precedence) plan(stn *infra.Station) *stationPlan {
	if pl, ok := p.plans[stn.Code]; ok {
		return pl
	}
	p.planning[stn.Code] = true
	pl := p.buildPlan(stn)
	delete(p.planning, stn.Code)
	p.plans[stn.Code] = pl
	return pl
}

func (p *precedence) buildPlan(stn *infra.Station) *stationPlan {
	s := p.sim
	now := s.CurTime()
	end := now + p.cfg.horizon()
	hw := float64(p.cfg.HeadwayMin)
	pl := &stationPlan{stn: stn, assign: map[ids.TrainID]*assignment{}}

	type due struct {
		t    *ops.Train
		v    visit
		a    *assignment
		prio int
		seq  int
	}
	var open []due
	for i, t := range s.trains {
		pr := p.project(t)
		v, ok := pr.visitAt(stn)
		if !ok || v.arr > end || v.dep < now {
			continue
		}
		// the platform track stays taken after it leaves, until its rear is off
		linger := pr.slots[v.slot].clear - pr.slots[v.slot].exit
		a := &assignment{stop: v.stop, from: v.arr - hw, to: v.dep + linger + hw, stay: v.dep - v.arr}
		pl.assign[t.ID()] = a
		if v.committed && v.pf != nil {
			a.pf, a.fixed = v.pf, true
			a.from = min(a.from, now)
			pl.bookings = append(pl.bookings, booking{t.ID(), v.pf, a.from, a.to})
			continue
		}
		var cands []candidate
		if v.from == nil {
			cands = p.entryCandidates(stn)
		} else {
			cands = p.candidates(t, *v.from, stn)
		}
		// a stopping train stands its dwell and any hold; one running
		// through counts as standing only if the hold is long enough
		hold := max(p.predictedHold(t, pr, v), p.predictedReception(t, pr, v))
		if !t.Schedule()[v.stop].IsPass() || p.standing(a.stay+hold) {
			a.stay += hold
			a.to = v.arr + a.stay + linger + hw
		}
		a.ranked = p.rankCandidates(t, v.stop, stn, cands, a.stay)
		open = append(open, due{t, v, a, s.Priority(t.ID()), i})
	}

	slices.SortStableFunc(open, func(x, y due) int {
		return cmp.Or(cmp.Compare(y.prio, x.prio), cmp.Compare(x.v.arr, y.v.arr), cmp.Compare(x.seq, y.seq))
	})
	rest := make([]*ops.Train, len(open))
	for i, d := range open {
		rest[i] = d.t
	}
	for i, d := range open {
		a := d.a
		// a train that does not call and stands there keeps off the loops
		// the calls after it need (keepBack)
		refuge := p.cfg.throughRefuge() && !passengers(d.t.Schedule()[a.stop]) && p.standing(a.stay)
		// whether it is in their way is judged by when it would get there
		// if it set off now: a train already kept back stays kept back
		// until they have called
		win := a
		if refuge {
			if v, ok := p.keepBackFrom(d.t, stn); ok {
				w := *a
				w.from, w.to = v.arr-hw, v.arr-hw+(a.to-a.from)
				win = &w
			}
		}
		var first *infra.Platform
		var calls []*ops.Train
		for _, c := range a.ranked {
			if _, busy := pl.bookedBy(d.t.ID(), c.pf, a.from, a.to); busy {
				continue
			}
			if refuge && !c.pf.IsThrough() {
				if blocked := p.blockedCalls(pl, d.t, win, c.pf, rest[i+1:]); len(blocked) > 0 {
					if first == nil {
						first, calls = c.pf, blocked
					}
					continue
				}
			}
			a.pf = c.pf
			break
		}
		if a.pf == nil && first != nil {
			if p.keepBack(pl, d.t, win, calls) {
				a.keepBack, a.fallback = true, first
				for _, c := range calls {
					a.keptFor = append(a.keptFor, c.ID())
				}
				continue
			}
			a.pf = first
		}
		if a.pf == nil {
			for _, c := range a.ranked {
				if b, busy := pl.bookedBy(d.t.ID(), c.pf, a.from, a.to); busy {
					a.blockers = append(a.blockers, b)
				}
			}
			continue
		}
		pl.bookings = append(pl.bookings, booking{d.t.ID(), a.pf, a.from, a.to})
	}
	return pl
}

// blockedCalls lists the trains of rest, still to be given a track, that call
// at the station during train t's window a and would be left with no track
// if t took pf, though they would have one otherwise. Each takes the first of
// its tracks free, in turn, as the plan gives them out.
func (p *precedence) blockedCalls(pl *stationPlan, t *ops.Train, a *assignment, pf *infra.Platform, rest []*ops.Train) []*ops.Train {
	left := func(extra []booking) map[ids.TrainID]bool {
		bs := append(slices.Clone(pl.bookings), extra...)
		out := map[ids.TrainID]bool{}
		for _, c := range rest {
			e := pl.assign[c.ID()]
			if !passengers(c.Schedule()[e.stop]) || !(e.from < a.to && a.from < e.to) {
				continue
			}
			i := slices.IndexFunc(e.ranked, func(cand candidate) bool {
				return !slices.ContainsFunc(bs, func(b booking) bool { return b.pf == cand.pf && b.overlaps(e.from, e.to) })
			})
			if i < 0 {
				out[c.ID()] = true
				continue
			}
			bs = append(bs, booking{c.ID(), e.ranked[i].pf, e.from, e.to})
		}
		return out
	}
	anyway := left(nil)
	with := left([]booking{{t.ID(), pf, a.from, a.to}})
	var out []*ops.Train
	for _, c := range rest {
		if with[c.ID()] && !anyway[c.ID()] {
			out = append(out, c)
		}
	}
	return out
}

// keepBack reports whether train t, which does not call at the plan's
// station and stands there, but would leave the calls with no track whichever
// loop it took, is better kept at the station before until they have called: if that delays it no more than taking pf would delay them, as the
// hold rule weighs it (HoldRule.KeepBack). Kept back, it gets there once the
// last of them has left, and is delayed only past when it would have left
// anyway. Only a train that can still be kept back counts: one ready to
// leave the station before, or running through it (keepBackFrom).
func (p *precedence) keepBack(pl *stationPlan, t *ops.Train, a *assignment, calls []*ops.Train) bool {
	s := p.sim
	hw := float64(p.cfg.HeadwayMin)
	if _, ok := p.keepBackFrom(t, pl.stn); !ok {
		return false
	}
	dep := a.from + hw + a.stay // when it would leave, kept back or not
	own := 0.0
	var others []Waiter
	for _, c := range calls {
		e := pl.assign[c.ID()]
		own = max(own, e.to-dep)
		others = append(others, Waiter{Prio: s.Priority(c.ID()), Wait: units.Min(max(a.to-(e.from+hw), 0))})
	}
	return p.rule.KeepBack(Waiter{Prio: s.Priority(t.ID()), Wait: units.Min(max(own, 0))}, others)
}

// keepBackFrom is the train's visit to stn if it can be kept back from it:
// it is ready to leave, or running through, the station before, and stn is
// the next one that can take it off the line. The visit is projected as if
// it set off now, whatever holds it, as a reception hold is (so a train
// kept back stays kept back until the calls are made).
func (p *precedence) keepBackFrom(t *ops.Train, stn *infra.Station) (visit, bool) {
	s := p.sim
	tc := s.trainCtrllers[t.ID()]
	if !(tc.phase == phaseReady || tc.passing) {
		return visit{}, false
	}
	if trk := tc.standTrack(); trk == nil {
		return visit{}, false
	} else if at, ok := s.world.StationOfPlatform(trk); !ok || at == stn {
		return visit{}, false
	}
	v, ok := receivingVisit(p.projectNow(t), headingTo(tc))
	if !ok || v.stn != stn {
		return visit{}, false
	}
	return v, true
}

// predictedHold is how long the train is likely to stand at the visit beyond
// its dwell: waiting anyway for a train, of any priority, that gets onto a
// shared stretch of single line first, or held for a higher-priority one the
// way a hold would be decided when it sets off. With trailing on, a hold for
// a higher-priority train catching it up is predicted only at the refuge
// chosen for it (trailing.go).
func (p *precedence) predictedHold(t *ops.Train, pr *projection, v visit) float64 {
	s := p.sim
	wait := 0.0
	mine := s.Priority(t.ID())
	for _, h := range s.trains {
		if h == t || s.trainCtrllers[h.ID()].phase == phaseExited {
			continue
		}
		hp := p.project(h)
		c := p.compare(t, p.regionFrom(pr, hp, v.slot+1), hp, !p.trailing())
		wait = max(wait, c.natural)
		if s.Priority(h.ID()) <= mine {
			continue
		}
		if c.Saving <= 0 {
			continue
		}
		if ok, _ := p.rule.Hold(c.Conflict); ok && float64(c.Wait) <= p.cfg.maxHold() {
			wait = max(wait, float64(c.Wait))
		}
	}
	if p.trailing() {
		// held here if this is where it is best let by the trains catching
		// it up
		for _, o := range p.choice(t, false).holds {
			if !o.here && o.stop == v.stop {
				wait = max(wait, o.wait)
			}
		}
	}
	return min(max(wait, p.lineTaken(t, pr, v)), p.cfg.maxHold())
}

// lineTaken is how long the train would stand at the visit because the line
// it leaves on is already reserved or occupied by another train: until that
// train is projected to have left it, or, if that cannot be told, a stand
// long enough to count. Projections can be wrong about who gets onto a line
// first; a track already taken cannot.
func (p *precedence) lineTaken(t *ops.Train, pr *projection, v visit) float64 {
	s := p.sim
	hw := float64(p.cfg.HeadwayMin)
	wait := 0.0
	for _, k := range pr.slots[min(v.slot+1, len(pr.slots)):] {
		if v.stn.InArea(k.track().Id) {
			continue // the station's throat
		}
		if _, ok := s.world.StationOfPlatform(k.track()); ok {
			break // the next station: the line is behind it
		}
		res, occ := s.ilk.TrackHolder(k.track())
		holder := occ
		if holder.IsZero() {
			holder = res
		}
		if holder.IsZero() || holder == t.ID() {
			continue
		}
		free := math.Inf(1)
		if clear, ok := p.project(s.trainCtrllers[holder].train).clearOf(k.track()); ok {
			free = clear + hw
		}
		if math.IsInf(free, 1) {
			wait = max(wait, 2*minStand)
			continue
		}
		wait = max(wait, free-k.enter)
	}
	return wait
}

// receptionWait is how long a train getting to the plan's station at arr would
// wait there for one of the tracks cands: 0 if one is free when it gets there.
// Only the bookings of trains that get there before it count: one that comes
// later is the plan's business, not the arrival's, and so is one that skip
// says can only come behind it. It also returns the last booking in the way on
// the track that frees first.
func (p *precedence) receptionWait(id ids.TrainID, pl *stationPlan, cands []candidate, arr float64, skip func(ids.TrainID) bool) (float64, booking) {
	hw := float64(p.cfg.HeadwayMin)
	best, by := math.Inf(1), booking{}
	for _, c := range cands {
		at, last := arr, booking{}
		for range len(pl.bookings) { // each booking can put it back once
			i := slices.IndexFunc(pl.bookings, func(b booking) bool {
				return b.train != id && b.pf == c.pf && b.from <= at && b.to > at-hw && !skip(b.train)
			})
			if i < 0 {
				break
			}
			at, last = pl.bookings[i].to+hw, pl.bookings[i]
		}
		if at < best {
			best, by = at, last
		}
	}
	if math.IsInf(best, 1) {
		return 0, booking{}
	}
	return max(best-arr, 0), by
}

// predictedReception is how long the train is likely to wait at the visit
// because the next station will have no track free for it (see
// receptionConflict). The next station's plan is looked at only if it is not
// itself being built, so plans of neighbouring stations do not wait on each
// other.
func (p *precedence) predictedReception(t *ops.Train, pr *projection, v visit) float64 {
	next, ok := receivingVisit(pr, v.stop+1)
	if !ok || next.from == nil || next.stn == v.stn || p.planning[next.stn.Code] {
		return 0
	}
	wait, _ := p.receptionWait(t.ID(), p.plan(next.stn), p.candidates(t, *next.from, next.stn), next.arr, p.behind(pr, v.slot+1, next.slot))
	return min(wait, p.cfg.maxReceptionHold())
}

// receivingVisit is the projection's first visit from schedule index stop on
// at a station that can take a train off the line: one with more than one
// track. A halt, whose only track is the line, cannot.
func receivingVisit(pr *projection, stop int) (visit, bool) {
	for _, v := range pr.visits {
		if v.stop >= stop && len(v.stn.Platforms()) > 1 {
			return v, true
		}
	}
	return visit{}, false
}

// behind returns a test for the trains that would follow the projected train
// over its slots from up to to: they get onto one of those tracks after it,
// running the same way. Projected unimpeded, such a train may seem to reach
// the next station first, but it can only get there behind the train.
func (p *precedence) behind(pr *projection, from, to int) func(ids.TrainID) bool {
	from, to = max(from, 0), min(to, len(pr.slots))
	memo := map[ids.TrainID]bool{}
	return func(id ids.TrainID) bool {
		if v, ok := memo[id]; ok {
			return v
		}
		h := p.project(p.sim.trainCtrllers[id].train)
		follows := false
		for _, k := range pr.slots[from:max(from, to)] {
			if slices.ContainsFunc(h.slots, func(hs slot) bool {
				return hs.track() == k.track() && hs.step.From == k.step.From && hs.enter > k.enter
			}) {
				follows = true
				break
			}
		}
		memo[id] = follows
		return follows
	}
}

// order is the order the dispatcher should try the station's platforms in for
// the request: the planned track first, then the rest best first. Tracks a
// higher-priority train is planned on during this train's stay come back
// separately in booked, to be tried last: leaving them out altogether would
// be a hold that skipped the refuge and safety checks. pfs is nil when the
// plan has nothing to say. fits is false when the plan has no track left for
// the train at all.
func (p *precedence) order(req *RouteRequest, k int) (pfs, booked []*infra.Platform, fits bool) {
	pl := p.plan(req.Station)
	a, planned := pl.assign[req.Train.ID()]
	if !planned || a.stop != k || a.fixed {
		return nil, nil, true
	}
	first := a.pf
	if a.keepBack {
		first = a.fallback // let go after all: the loop it was kept back from
	}
	if first == nil {
		return nil, nil, false
	}
	pfs = []*infra.Platform{first}
	for _, c := range a.ranked {
		if c.pf == first {
			continue
		}
		if _, ok := p.bookingAgainst(req.Train.ID(), pl, a, c.pf); ok {
			booked = append(booked, c.pf)
		} else {
			pfs = append(pfs, c.pf)
		}
	}
	return pfs, booked, true
}

// bookingAgainst returns the booking of a higher-priority train on pf during
// the train's window, if any.
func (p *precedence) bookingAgainst(id ids.TrainID, pl *stationPlan, a *assignment, pf *infra.Platform) (booking, bool) {
	b, busy := pl.bookedBy(id, pf, a.from, a.to)
	if busy && p.sim.Priority(b.train) > p.sim.Priority(id) {
		return b, true
	}
	return booking{}, false
}

// changeReason says why the train ends up on pf at stop k rather than its
// timetabled platform, and for whom. An empty reason means the change was not
// a precedence decision (e.g. the timetabled platform was simply busy).
func (p *precedence) changeReason(t *ops.Train, k int, stn *infra.Station, pf *infra.Platform) (string, ids.TrainID) {
	sp := t.Schedule()[k]
	if pf.PfNo == sp.SpPfNo {
		return "", ids.TrainID{}
	}
	if pin, ok := p.pinned[pinKey{t.ID(), stn.Code}]; ok && pin == pf.PfNo {
		return reasonOverride, ids.TrainID{}
	}
	pl := p.plan(stn)
	a, ok := pl.assign[t.ID()]
	if !ok || a.pf != pf {
		return "", ids.TrainID{}
	}
	var tt, got *candidate
	for i := range a.ranked {
		if a.ranked[i].pf == pf {
			got = &a.ranked[i]
		}
		if a.ranked[i].pf.PfNo == sp.SpPfNo {
			tt = &a.ranked[i]
		}
	}
	// a call timetabled at a through track is never planned there (callable)
	if i := slices.IndexFunc(stn.Platforms(), func(pf *infra.Platform) bool { return pf.PfNo == sp.SpPfNo }); i >= 0 && got != nil && !callable(sp, stn.Platforms()[i]) {
		return reasonThrough, ids.TrainID{}
	}
	if tt == nil || got == nil {
		return "", ids.TrainID{} // the timetabled platform cannot be reached from here
	}
	if b, ok := p.bookingAgainst(t.ID(), pl, a, tt.pf); ok {
		return reasonBooked, b.train
	}
	if fitOf(t, sp, tt.pf) > fitOf(t, sp, got.pf) {
		return reasonTooShort, ids.TrainID{}
	}
	class := stn.TrackClass(tt.pf.Id)
	switch {
	case sp.IsPass() || (p.cfg.throughRefuge() && !passengers(sp)):
		if p.rank(t, k, stn, *tt, a.stay) > p.rank(t, k, stn, *got, a.stay) {
			switch {
			case p.standing(a.stay) && p.cfg.throughRefuge() && stn.TrackClass(got.pf.Id) == infra.Through:
				return reasonRefuge, ids.TrainID{}
			case p.standing(a.stay):
				return reasonStanding, ids.TrainID{}
			}
			return reasonPassing, ids.TrainID{}
		}
	case class == infra.Through:
		return reasonThrough, ids.TrainID{}
	case class == infra.Main && stn.RunsThrough() && a.stay > float64(p.cfg.MainLineMaxStayMin):
		return reasonMainLineStay, ids.TrainID{}
	}
	return "", ids.TrainID{}
}
