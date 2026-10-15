package sim

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/units"
)

// Precedence.
//
// Priority alone only orders trains already waiting on the same track. Before
// the dispatcher grants train L anything, precedence looks ahead (see
// projection.go) and asks: if L goes now, does a higher-priority train H lose
// time, and can L wait somewhere H can get past it? If so, and the hold rule
// (holdrule.go) agrees, L is held where it stands until H is past. At
// stations, the plan (stationplan.go) may move L to another track instead,
// which costs it nothing.
//
// One check covers the three kinds of conflict. L's commitment region is the
// tracks it would run over until its next refuge for H: a place where it can
// stand while H gets by. Two tracks conflict if they are the same track or
// share a switch block. Where both run a track the same way and L would get
// there first, H would catch up (an overtake). Where they would meet head on,
// or at a switch, the shared tracks are one resource that L would hold until
// it clears all of them (a crossing). A train left with no station track by
// the plan is a platform conflict.
//
// A hold is a promise that L moves only after H is past. The safety check
// knows about every active hold (safety.go), so no other grant can strand H
// behind L, and a hold that would leave the state unsafe is never placed.
//
// Decisions are worked out again from scratch whenever the held train is
// woken: by H leaving the track that ends the conflict, or by a
// PRECEDENCE_REVIEW event. Each is logged when it starts, changes or ends, and
// can be overridden (overrides.go).

// Decision statuses.
const (
	StatusActive     = "active"
	StatusReleased   = "released"   // the conflict is over
	StatusExpired    = "expired"    // held for max_hold_min
	StatusOverridden = "overridden" // an operator released it
	StatusDone       = "done"       // a platform change, once the train has left the station
)

// Decision actions and sources.
const (
	ActionHold     = "hold"
	ActionPlatform = "platform"
	SourceAuto     = "auto"
	SourceManual   = "manual"
)

// Decision is something precedence did: held a train for another, or moved it
// to another platform.
type Decision struct {
	ID     string
	Action string       // ActionHold or ActionPlatform
	Kind   ConflictKind // for a hold
	Train  ids.TrainID  // the train held or moved
	For    ids.TrainID  // the train it made way for; zero if none in particular
	At     string       // the station, or for a hold outside one the track the train stands on
	PfFrom string       // a platform change: timetabled platform
	PfTo   string       // a platform change: platform given
	Status string
	Source string // SourceAuto or SourceManual
	Rule   string
	Reason string
	Gain   units.Minutes // minutes the other train saves
	Wait   units.Minutes // minutes the held train is expected to wait
	Since  float64
	Until  float64 // when the hold is expected to end
	Ended  float64

	release   ids.TrackID // H leaving it ends the conflict
	milestone int         // H's stop it must reach to be past; -1 for its last
	reviewAt  float64     // latest review event scheduled
}

func holdID(train, forTrain ids.TrainID, at string) string {
	return fmt.Sprintf("hold:%s>%s@%s", train, forTrain, at)
}

func platformID(train ids.TrainID, stn ids.StnCode) string {
	return fmt.Sprintf("pf:%s@%s", train, stn)
}

// HeldError is why a request waits while its train is held for others.
type HeldError struct {
	Decisions []*Decision
}

func (e *HeldError) Error() string {
	parts := make([]string, len(e.Decisions))
	for i, d := range e.Decisions {
		parts[i] = fmt.Sprintf("held at %s for %s (%s)", d.At, d.For, d.Kind)
	}
	return strings.Join(parts, "; ")
}

type pinKey struct {
	train ids.TrainID
	stn   ids.StnCode
}

type manualHold struct {
	forTrain ids.TrainID
	stn      ids.StnCode
}

type precedence struct {
	sim  *Sim
	cfg  *PrecedenceConfig
	rule HoldRule

	// worked out from the state, dropped whenever it changes
	projections    map[ids.TrainID]*projection
	nowProjections map[ids.TrainID]*projection // see projectNow
	building       map[ids.TrainID]bool        // projections being built, to stop cycles
	plans          map[ids.StnCode]*stationPlan
	conflicts      map[[2]*infra.TrackSegment]bool

	all     []*Decision          // every decision, in the order made
	current map[string]*Decision // latest decision for each id
	holds   map[ids.TrainID][]*Decision
	skipped map[string]string // decision id -> reason last logged as skipped
	// heldSince is when the train was first held where it stands now, for
	// whoever: max_hold_min caps the total
	heldSince map[ids.TrainID]float64

	// operator overrides
	vetoed   map[string]bool
	expired  map[string]bool
	manual   map[ids.TrainID][]manualHold
	pinned   map[pinKey]string
	disabled map[ids.TrainID]bool
	early    []Override // applied before Run, logged when it starts
}

func newPrecedence(s *Sim, cfg *PrecedenceConfig) *precedence {
	p := &precedence{
		sim:       s,
		cfg:       cfg,
		rule:      holdRules[cfg.ruleName()](cfg),
		current:   map[string]*Decision{},
		holds:     map[ids.TrainID][]*Decision{},
		skipped:   map[string]string{},
		heldSince: map[ids.TrainID]float64{},
		vetoed:    map[string]bool{},
		expired:   map[string]bool{},
		manual:    map[ids.TrainID][]manualHold{},
		pinned:    map[pinKey]string{},
		disabled:  map[ids.TrainID]bool{},
		conflicts: map[[2]*infra.TrackSegment]bool{},
	}
	p.invalidate()
	return p
}

// invalidate drops what was worked out from the state.
func (p *precedence) invalidate() {
	p.projections = map[ids.TrainID]*projection{}
	p.nowProjections = map[ids.TrainID]*projection{}
	p.building = map[ids.TrainID]bool{}
	p.plans = map[ids.StnCode]*stationPlan{}
}

// higher lists the trains still to finish whose priority is above the
// train's, highest first.
func (p *precedence) higher(id ids.TrainID) []*ops.Train {
	s := p.sim
	mine := s.Priority(id)
	var out []*ops.Train
	for _, t := range s.trains {
		if t.ID() != id && s.trainCtrllers[t.ID()].phase != phaseExited && s.Priority(t.ID()) > mine {
			out = append(out, t)
		}
	}
	slices.SortStableFunc(out, func(a, b *ops.Train) int { return cmp.Compare(s.Priority(b.ID()), s.Priority(a.ID())) })
	return out
}

// tracksConflict reports whether two trains cannot use the tracks at once:
// the same track, or two tracks of one switch block.
func (p *precedence) tracksConflict(a, b *infra.TrackSegment) bool {
	if a == b {
		return true
	}
	key := [2]*infra.TrackSegment{a, b}
	if a.Id.Compare(b.Id) > 0 {
		key = [2]*infra.TrackSegment{b, a}
	}
	if c, ok := p.conflicts[key]; ok {
		return c
	}
	w := p.sim.world
	c := slices.ContainsFunc(w.SwitchBlocksOn(a), func(sb *infra.SwitchBlock) bool {
		return slices.Contains(w.SwitchBlocksOn(b), sb)
	})
	p.conflicts[key] = c
	return c
}

// conflict is a Conflict with what the dispatcher needs to hold for it.
type conflict struct {
	Conflict
	// natural is how long l waits anyway, without any hold, because h gets
	// onto a stretch of single line they share in opposite directions first
	natural   float64
	release   ids.TrackID // H leaving it ends the conflict
	releaseAt float64
	milestone int // H's stop it must reach to be past L; -1 for its last
	manual    bool
}

// compare works out what train l running over region now would cost h, and
// how long l would wait if held instead.
func (p *precedence) compare(l *ops.Train, region []slot, h *projection) conflict {
	s := p.sim
	hw := float64(p.cfg.HeadwayMin)
	c := conflict{Conflict: Conflict{Held: l.ID(), For: h.train.ID(), HeldPrio: s.Priority(l.ID()), ForPrio: s.Priority(h.train.ID())}, milestone: -1}

	// pairs used one after the other: the same track the same way (h
	// following l), or two tracks of one switch block (l crossing h's path)
	var seqSave, seqWait, mergeSave float64
	firstSeq := -1
	// the same track in opposite directions: the shared tracks are one
	// stretch that l holds until it clears all of it
	lFirst, lClear, hFirst, hClear := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	lastOpp := -1
	for _, k := range region {
		for j, hs := range h.slots {
			if !p.tracksConflict(k.track(), hs.track()) {
				continue
			}
			if k.track() == hs.track() && k.step.From != hs.step.From {
				lFirst, lClear = min(lFirst, k.enter), max(lClear, k.exit)
				hFirst, hClear = min(hFirst, hs.enter), max(hClear, hs.exit)
				lastOpp = max(lastOpp, j)
				continue
			}
			if k.enter >= hs.enter {
				continue // h gets there first
			}
			save := k.exit + hw - hs.enter
			if save <= 0 {
				continue
			}
			if k.track() != hs.track() {
				mergeSave = max(mergeSave, save)
			}
			seqSave = max(seqSave, save)
			seqWait = max(seqWait, hs.exit+hw-k.enter)
			if firstSeq < 0 || j < firstSeq {
				firstSeq = j
			}
		}
	}
	var oppSave, oppWait float64
	if lastOpp >= 0 && lFirst < hFirst {
		oppSave = lClear + hw - hFirst
		oppWait = hClear + hw - lFirst
	}
	if lastOpp >= 0 && hFirst <= lFirst {
		c.natural = max(hClear+hw-lFirst, 0)
	}

	release := -1
	switch {
	case oppSave > 0 && oppSave >= seqSave:
		c.Kind, c.Saving, c.Wait = KindCross, units.Min(oppSave), units.Min(max(oppWait, 0))
		release = lastOpp // past once it has cleared the stretch
	case seqSave > 0:
		c.Kind, c.Saving, c.Wait = KindOvertake, units.Min(seqSave), units.Min(max(seqWait, 0))
		if mergeSave >= seqSave {
			c.Kind = KindCross // the worst of it is where their paths cross
		}
		release = firstSeq // past once it is through the first shared track
	default:
		return c
	}
	c.release = h.slots[release].track().Id
	c.releaseAt = h.slots[release].exit
	for _, v := range h.visits {
		if v.slot > release {
			c.milestone = v.stop
			break
		}
	}
	return c
}

// region is the tracks l would run over, from where it stands, until its next
// refuge for h: a station track it can stand on while h gets by. It stops
// short of the refuge's track.
func (p *precedence) region(l, h *projection) []slot {
	// the track it stands on (or enters the world on) is not ahead of it
	return p.regionFrom(l, h, 1)
}

// regionFrom is region for l setting off from its slot start.
func (p *precedence) regionFrom(l, h *projection, start int) []slot {
	if len(l.slots) <= start {
		return nil
	}
	for _, v := range l.visits {
		if v.slot >= start && p.refugeAt(v, h) {
			return l.slots[start:v.slot]
		}
	}
	return l.slots[start:]
}

// refugeAt reports whether h can get by a train standing on the visit's
// platform track.
func (p *precedence) refugeAt(v visit, h *projection) bool {
	if v.pf == nil {
		return false
	}
	hv, calls := h.visitAt(v.stn)
	if !calls {
		return !slices.ContainsFunc(h.slots, func(s slot) bool { return p.tracksConflict(s.track(), v.pf.Track) })
	}
	blocked := func(s infra.Step) bool { return p.tracksConflict(s.Edge.Track, v.pf.Track) }
	return p.canReach(h.train, hv.stop+1, blocked)
}

// canReach reports whether the train can call at every stop up to and
// including schedule index upTo (its last if upTo is -1 or out of range)
// without using a step blocked rejects.
func (p *precedence) canReach(t *ops.Train, upTo int, blocked func(infra.Step) bool) bool {
	starts, next := p.sim.dispatcher.standing(t)
	sched := t.Schedule()
	if upTo < 0 || upTo >= len(sched) {
		upTo = len(sched) - 1
	}
	if next > upTo {
		return true
	}
	stops := make([]ids.StnCode, 0, upTo-next+1)
	for _, sp := range sched[next : upTo+1] {
		stops = append(stops, sp.StnCode)
	}
	ends, err := p.sim.world.VisitEnds(starts, stops, blocked)
	return err == nil && len(ends) > 0
}

// placeOf is where the train would be held: its station, or the track it
// stands on outside one.
func (p *precedence) placeOf(t *ops.Train) string {
	tc := p.sim.trainCtrllers[t.ID()]
	if tc.phase == phaseOutside {
		return t.Schedule()[0].StnCode.String()
	}
	trk := t.CurrentTrack()
	if trk == nil {
		return ""
	}
	if stn, ok := p.sim.world.StationOfPlatform(trk); ok {
		return stn.Code.String()
	}
	return trk.String()
}

// verdict is what precedence says about a request.
type verdict struct {
	hold   bool
	waitOn []ids.TrackID
	err    error
	order  []*infra.Platform // platforms to try, in order; nil leaves it to the dispatcher
}

// evaluate decides whether the request's train is held, and which platforms
// it should try.
func (p *precedence) evaluate(req *RouteRequest) verdict {
	t := req.Train
	id := t.ID()
	tc := p.sim.trainCtrllers[id]
	k := 0
	if !req.Entry {
		k = headingTo(tc)
	}
	if tc.phase == phaseReady {
		p.finishPlatform(id, t.Schedule()[t.StopIndex()].StnCode)
	}
	if p.disabled[id] {
		p.syncHolds(t, nil)
		return verdict{}
	}
	order, booked, fits := p.order(req, k)
	at := p.placeOf(t)

	byTrain := map[ids.TrainID]conflict{}
	add := func(c conflict) {
		if old, ok := byTrain[c.For]; !ok || c.manual || (!old.manual && c.Saving > old.Saving) {
			byTrain[c.For] = c
		}
	}
	lp := p.projectNow(t)
	for _, h := range p.higher(id) {
		if c := p.compare(t, p.region(lp, p.project(h)), p.project(h)); c.Saving > 0 {
			add(c)
		}
	}
	pl := p.plan(req.Station)
	if a := pl.assign[id]; !fits {
		for _, c := range p.platformConflicts(req, a, a.blockers) {
			add(c)
		}
	} else if len(booked) > 0 && !slices.ContainsFunc(order, p.isFree) {
		// the only tracks free now are booked for higher-priority trains
		var bs []booking
		for _, pf := range booked {
			if b, ok := p.bookingAgainst(id, pl, a, pf); ok && p.isFree(pf) {
				bs = append(bs, b)
			}
		}
		for _, c := range p.platformConflicts(req, a, bs) {
			add(c)
		}
	}
	for _, c := range p.manualConflicts(t, at) {
		add(c)
	}

	var cs []conflict
	for _, c := range byTrain {
		cs = append(cs, c)
	}
	slices.SortFunc(cs, func(a, b conflict) int {
		return cmp.Or(cmp.Compare(b.ForPrio, a.ForPrio), cmp.Compare(a.For.Number, b.For.Number))
	})
	var held []*Decision
	for _, c := range cs {
		if d := p.tryHold(t, c, at, held); d != nil {
			held = append(held, d)
		}
	}
	p.syncHolds(t, held)
	if len(held) > 0 {
		var waitOn []ids.TrackID
		for _, d := range held {
			if !d.release.IsZero() {
				waitOn = append(waitOn, d.release)
			}
		}
		return verdict{hold: true, waitOn: waitOn, err: &HeldError{Decisions: held}}
	}
	if order == nil {
		return verdict{}
	}
	return verdict{order: append(order, booked...)}
}

func (p *precedence) isFree(pf *infra.Platform) bool {
	return p.sim.ilk.IsTrackFree(pf.Track)
}

// platformConflicts are the higher-priority trains whose bookings, bs, would
// leave the request's train without a station track.
func (p *precedence) platformConflicts(req *RouteRequest, a *assignment, bs []booking) []conflict {
	s := p.sim
	hw := float64(p.cfg.HeadwayMin)
	var out []conflict
	for _, b := range bs {
		if s.Priority(b.train) <= s.Priority(req.Train.ID()) {
			continue
		}
		h := s.trainCtrllers[b.train].train
		c := conflict{
			Conflict: Conflict{
				Kind: KindPlatform, Held: req.Train.ID(), For: b.train,
				HeldPrio: s.Priority(req.Train.ID()), ForPrio: s.Priority(b.train),
				Saving: units.Min(a.to - (b.from + hw)), Wait: units.Min(b.to - (a.from + hw)),
			},
			release:   b.pf.Track.Id,
			releaseAt: b.to - hw,
			milestone: -1,
		}
		if hv, ok := p.project(h).visitAt(req.Station); ok && hv.stop+1 < len(h.Schedule()) {
			c.milestone = hv.stop + 1
		}
		if c.Saving > 0 {
			out = append(out, c)
		}
	}
	return out
}

// manualConflicts are the operator's holds for the train at where it stands.
// A manual hold lasts until the other train has left the station.
func (p *precedence) manualConflicts(t *ops.Train, at string) []conflict {
	s := p.sim
	var out []conflict
	keep := p.manual[t.ID()][:0]
	for _, m := range p.manual[t.ID()] {
		htc := s.trainCtrllers[m.forTrain]
		k := slices.IndexFunc(htc.train.Schedule(), func(sp *ops.SchedulePoint) bool { return sp.StnCode == m.stn })
		if htc.phase == phaseExited || k < 0 || htc.train.Schedule()[k].Departed() {
			continue // done
		}
		keep = append(keep, m)
		if string(m.stn) != at {
			continue
		}
		hp := p.project(htc.train)
		c := p.compare(t, p.region(p.projectNow(t), hp), hp)
		if c.Saving <= 0 {
			c = conflict{Conflict: Conflict{Kind: KindManual, Held: t.ID(), For: m.forTrain, HeldPrio: s.Priority(t.ID()), ForPrio: s.Priority(m.forTrain)}}
		}
		c.manual = true
		c.milestone = -1
		if k+1 < len(htc.train.Schedule()) {
			c.milestone = k + 1
		}
		if hv, ok := hp.visitOf(k); ok && hv.pf != nil {
			c.release, c.releaseAt = hv.pf.Track.Id, hv.dep
		}
		out = append(out, c)
	}
	p.manual[t.ID()] = keep
	return out
}

// tryHold holds the train for c, if the rule agrees and nothing rules it out.
// held are the holds already placed in this evaluation.
func (p *precedence) tryHold(t *ops.Train, c conflict, at string, held []*Decision) *Decision {
	s := p.sim
	now := s.CurTime()
	id := holdID(t.ID(), c.For, at)
	if p.vetoed[id] || (p.expired[id] && !c.manual) {
		return nil
	}
	d := p.current[id]
	if d != nil && d.Status != StatusActive {
		d = nil
	}
	elapsed := 0.0 // held here so far, for anyone
	if since, ok := p.heldSince[t.ID()]; ok {
		elapsed = now - since
	}
	var why string
	if !c.manual {
		if elapsed >= p.cfg.maxHold()-epsilon {
			if d != nil {
				p.end(d, StatusExpired)
			}
			p.expired[id] = true
			return nil
		}
		ok, reason := p.rule.Hold(c.Conflict)
		if !ok {
			return nil
		}
		if elapsed+float64(c.Wait) > p.cfg.maxHold() {
			p.skip(t, id, c, at, "max_hold")
			return nil
		}
		why = reason
	}
	if reason := p.ruledOut(t, c, held); reason != "" {
		p.skip(t, id, c, at, reason)
		return nil
	}
	delete(p.skipped, id)

	if d == nil {
		d = &Decision{ID: id, Action: ActionHold, Train: t.ID(), For: c.For, At: at, Status: StatusActive, Source: SourceAuto, Rule: p.rule.Name(), Since: now}
		if c.manual {
			d.Source, d.Rule = SourceManual, ""
		}
	}
	d.Kind, d.Gain, d.Wait = c.Kind, c.Saving, c.Wait
	d.Until = now + float64(c.Wait)
	if c.releaseAt > 0 {
		d.Until = max(d.Until, c.releaseAt+float64(p.cfg.HeadwayMin))
	}
	d.release, d.milestone = c.release, c.milestone
	if why != "" {
		d.Reason = why
	} else if d.Reason == "" {
		d.Reason = "held by the operator"
	}
	return d
}

// ruledOut says why the train cannot be held for c where it stands, or "".
func (p *precedence) ruledOut(t *ops.Train, c conflict, held []*Decision) string {
	s := p.sim
	tc := s.trainCtrllers[t.ID()]
	if trk := t.CurrentTrack(); trk != nil && tc.phase != phaseOutside {
		if stn, ok := s.world.StationOfPlatform(trk); ok {
			switch stn.TrackClass(trk.Id) {
			case infra.Through:
				return "through_track"
			case infra.Main:
				if stn.RunsThrough() && tc.phase == phaseReady && s.CurTime()-tc.arrivedAt+float64(c.Wait) > float64(p.cfg.MainLineMaxStayMin) {
					return "main_line_limit"
				}
			}
		}
	}
	h := s.trainCtrllers[c.For].train
	if !p.canReach(h, c.milestone, p.blockedBy(t.ID())) {
		return "no_refuge"
	}
	if p.waitsFor(c.For, t.ID(), held) {
		return "cycle"
	}
	cons := p.constraints(t.ID())
	for _, d := range held {
		cons = append(cons, holdConstraint{held: d.Train, forTrain: d.For, milestone: d.milestone})
	}
	cons = append(cons, holdConstraint{held: t.ID(), forTrain: c.For, milestone: c.milestone})
	if ok, _ := s.dispatcher.safe(nil, nil, cons); !ok {
		return "unsafe"
	}
	return ""
}

// blockedBy rejects steps onto the train's tracks, or through a switch it has
// locked the other way.
func (p *precedence) blockedBy(id ids.TrainID) func(infra.Step) bool {
	ilk := p.sim.ilk
	held := map[ids.TrackID]bool{}
	for _, trk := range ilk.Holdings(id) {
		held[trk.Id] = true
	}
	return func(s infra.Step) bool {
		if held[s.Edge.Track.Id] {
			return true
		}
		for _, sb := range p.sim.world.SwitchBlocksOn(s.Edge.Track) {
			if active, by := ilk.SwitchState(sb); by == id && active != s.Edge {
				return true
			}
		}
		return false
	}
}

// waitsFor reports whether train from already waits, through holds, for
// train to.
func (p *precedence) waitsFor(from, to ids.TrainID, held []*Decision) bool {
	edges := map[ids.TrainID][]ids.TrainID{}
	for _, ds := range p.holds {
		for _, d := range ds {
			edges[d.Train] = append(edges[d.Train], d.For)
		}
	}
	for _, d := range held {
		edges[d.Train] = append(edges[d.Train], d.For)
	}
	seen := map[ids.TrainID]bool{}
	stack := []ids.TrainID{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == to {
			return true
		}
		if seen[cur] {
			continue
		}
		seen[cur] = true
		stack = append(stack, edges[cur]...)
	}
	return false
}

// holdConstraint tells the safety check that held moves only once forTrain
// has reached its stop milestone (or finished, if milestone is -1).
type holdConstraint struct {
	held, forTrain ids.TrainID
	milestone      int
}

// constraints lists the active holds, leaving out the train's own.
func (p *precedence) constraints(except ids.TrainID) []holdConstraint {
	if p == nil {
		return nil
	}
	var out []holdConstraint
	for _, t := range p.sim.trains { // a fixed order
		if t.ID() == except {
			continue
		}
		for _, d := range p.holds[t.ID()] {
			out = append(out, holdConstraint{held: d.Train, forTrain: d.For, milestone: d.milestone})
		}
	}
	return out
}

// syncHolds makes held the train's active holds: new ones are logged and get
// review events, ones no longer held are released.
func (p *precedence) syncHolds(t *ops.Train, held []*Decision) {
	s := p.sim
	now := s.CurTime()
	for _, d := range slices.Clone(p.holds[t.ID()]) {
		if !slices.Contains(held, d) {
			p.end(d, StatusReleased)
		}
	}
	if _, ok := p.heldSince[t.ID()]; !ok && len(held) > 0 {
		p.heldSince[t.ID()] = now
	}
	for _, d := range held {
		if p.current[d.ID] != d {
			p.current[d.ID] = d
			p.all = append(p.all, d)
			s.logHold(d)
			if d.Source == SourceAuto {
				// just past the limit, so float noise cannot leave it short
				p.review(d, p.heldSince[t.ID()]+p.cfg.maxHold()+des.MinDeltaTime)
			}
		}
		switch {
		case d.Until > now && d.Until > d.reviewAt+0.5:
			p.review(d, d.Until)
		case d.reviewAt <= now:
			p.review(d, now+reviewStep) // outlasted its expected end
		}
	}
	p.holds[t.ID()] = held
}

// epsilon absorbs float noise in sim times.
const epsilon = 1e-6

// reviewStep is how often a hold is looked at again once it has outlasted
// its expected end.
const reviewStep = 1.0

// review schedules a PRECEDENCE_REVIEW for the held train at time at.
func (p *precedence) review(d *Decision, at float64) {
	s := p.sim
	if at < s.CurTime()+des.MinDeltaTime {
		at = s.CurTime() + des.MinDeltaTime
	}
	d.reviewAt = max(d.reviewAt, at)
	s.ScheduleEventAt(at, ops.PrecedenceReview, d.ID, d.Train)
}

// end closes a decision with the given status.
func (p *precedence) end(d *Decision, status string) {
	if d.Status != StatusActive {
		return
	}
	d.Status, d.Ended = status, p.sim.CurTime()
	if d.Action == ActionHold {
		p.holds[d.Train] = slices.DeleteFunc(p.holds[d.Train], func(x *Decision) bool { return x == d })
		p.sim.logRelease(d)
		// requests refused as unsafe may have been refused because of it,
		// and no track release will tell them it is gone
		p.sim.dispatcher.reviewUnsafe()
		// once let go, a train is not held for the same train at the same
		// place again: holding it back and forth would only churn
		if status == StatusReleased {
			p.expired[d.ID] = true
		}
	}
}

// skip logs that a hold was ruled out, once per decision and reason.
func (p *precedence) skip(t *ops.Train, id string, c conflict, at, reason string) {
	if p.skipped[id] == reason {
		return
	}
	p.skipped[id] = reason
	p.sim.logSkipped(t, id, c, at, reason)
}

// granted notes a route granted to the train: it is no longer held where it
// stood, and if the route ends on a platform other than the timetabled one
// because of precedence, that is a decision.
func (p *precedence) granted(req *RouteRequest, k int, last *infra.TrackSegment) {
	t := req.Train
	delete(p.heldSince, t.ID())
	pf := platformOf(req.Station, last)
	if pf == nil || p.disabled[t.ID()] {
		return
	}
	reason, forTrain := p.changeReason(t, k, req.Station, pf)
	if reason == "" {
		return
	}
	id := platformID(t.ID(), req.Station.Code)
	if d := p.current[id]; d != nil && d.Status == StatusActive && d.PfTo == pf.PfNo {
		return
	}
	d := &Decision{
		ID: id, Action: ActionPlatform, Train: t.ID(), For: forTrain, At: req.Station.Code.String(),
		PfFrom: t.Schedule()[k].SpPfNo, PfTo: pf.PfNo, Status: StatusActive, Source: SourceAuto,
		Reason: reason, Since: p.sim.CurTime(),
	}
	if reason == reasonOverride {
		d.Source = SourceManual
	}
	if old := p.current[id]; old != nil {
		old.Status, old.Ended = StatusReleased, d.Since
	}
	p.current[id] = d
	p.all = append(p.all, d)
	p.sim.logPlatformChange(d)
}

// finishPlatform closes the train's platform change at the station once it
// leaves.
func (p *precedence) finishPlatform(id ids.TrainID, stn ids.StnCode) {
	if d := p.current[platformID(id, stn)]; d != nil && d.Status == StatusActive {
		d.Status, d.Ended = StatusDone, p.sim.CurTime()
	}
}

// forget closes everything about a train that left, and has the trains held
// for it looked at again.
func (p *precedence) forget(t *ops.Train) {
	id := t.ID()
	for _, d := range slices.Clone(p.holds[id]) {
		p.end(d, StatusReleased)
	}
	for _, sp := range t.Schedule() {
		p.finishPlatform(id, sp.StnCode)
	}
	delete(p.manual, id)
	for _, ds := range p.holds {
		for _, d := range ds {
			if d.For == id {
				p.review(d, p.sim.CurTime())
			}
		}
	}
}

// decisions returns copies of every decision, in the order they were made.
func (p *precedence) decisions() []Decision {
	out := make([]Decision, len(p.all))
	for i, d := range p.all {
		out[i] = *d
	}
	return out
}
