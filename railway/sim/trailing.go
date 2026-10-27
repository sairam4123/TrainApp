package sim

import (
	"cmp"
	"fmt"
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/units"
)

// Trailing.
//
// compare judges an overtake one section at a time: what H loses if L takes
// its next section now. Behind a slower train, though, H loses a little at
// every station, and no one section shows it, so the hold rule turns each
// hold down and H trails L for the rest of the way. Trailing looks further
// ahead (follow_horizon_min). It projects H following the trains ahead of it,
// none of which it can get onto a track before they have cleared it, and so
// what L costs H over the whole stretch: the trail loss.
//
// It then compares the places L could be held for H: where it stands, and each
// of its later refuges. Held at a refuge, L costs H only the trailing up to
// it, and waits there until H has got by. Of the places the hold rule
// accepts, the one with the best score (HoldRule.Score) is chosen, the
// earliest on a tie; a later one must beat where L stands by
// defer_margin_min. If a later refuge wins, L goes now (a deferral, logged
// when holding it here would also have done), and the station plan there
// expects it to stand, so it gets a loop. Once L gets there, the same choice
// comes out again, as long as the projections held, since what H loses
// before the refuge is lost either way.
//
// The following projection only moves times later: running behind another
// train costs H no braking, a pass it has to stop at no restart, and L is not
// slowed by H. So a trail loss is on the low side, never the high.

// trailed is a train ahead of the one followed. Its slots before cut count,
// all of them if cut is -1: a train held at a refuge is cut at the refuge's
// track, which the train behind does not use.
type trailed struct {
	pr  *projection
	cut int
}

func (a trailed) slots() []slot {
	if a.cut < 0 {
		return a.pr.slots
	}
	return a.pr.slots[:min(a.cut, len(a.pr.slots))]
}

// follow returns h's projection with each track entered no earlier than every
// train in ahead that runs it the same way, and gets onto it first, has
// cleared it, plus headway. A delay carries on to the tracks after. At a stop,
// the departure follows from the later arrival as the controller would work
// it out, so slack before a timetabled departure takes some up. Tracks h is on
// already are left as they are.
func (p *precedence) follow(h *projection, ahead []trailed) *projection {
	s := p.sim
	hw := float64(p.cfg.HeadwayMin)
	now := s.CurTime()
	out := &projection{train: h.train, slots: slices.Clone(h.slots), visits: slices.Clone(h.visits), stand: h.stand, tail: h.tail}
	visitOfSlot := map[int]int{}
	for i, v := range h.visits {
		visitOfSlot[v.slot] = i
	}
	d := 0.0
	for j, hs := range h.slots {
		enter := hs.enter + d
		if hs.enter > now {
			for _, a := range ahead {
				for _, x := range a.slots() {
					if x.track() == hs.track() && x.step.From == hs.step.From && x.enter < hs.enter {
						enter = max(enter, x.clear+hw)
					}
				}
			}
		}
		d = enter - hs.enter
		if d <= epsilon {
			d = 0
			continue
		}
		sl := &out.slots[j]
		sl.enter = enter
		if i, ok := visitOfSlot[j]; ok {
			free := h.visits[i]
			v := &out.visits[i]
			sp := h.train.Schedule()[free.stop]
			v.arr = free.arr + d
			// the overhang's extra and anything else besides the dwell stay
			extra := free.dep - free.arr - s.dwellAt(sp, free.arr)
			v.dep = v.arr + s.dwellAt(sp, v.arr) + extra
			d = max(v.dep-free.dep, 0)
		}
		sl.exit = hs.exit + d
		sl.clear = hs.clear + d
	}
	return out
}

// lateAtEnd is how much later f, h followed, gets onto h's last track.
func lateAtEnd(h, f *projection) float64 {
	n := len(h.slots)
	if n == 0 {
		return 0
	}
	return f.slots[n-1].enter - h.slots[n-1].enter
}

// runsAhead reports whether a gets onto some track of h's, the same way,
// before h does.
func runsAhead(a, h *projection) bool {
	first := map[*infra.TrackSegment]slot{}
	for _, hs := range h.slots {
		if _, ok := first[hs.track()]; !ok {
			first[hs.track()] = hs
		}
	}
	for _, x := range a.slots {
		if hs, ok := first[x.track()]; ok && hs.step.From == x.step.From && x.enter < hs.enter {
			return true
		}
	}
	return false
}

// aheadOf lists the trains, other than except, that h would run behind over
// its long projection hp and could not be let past: those of its priority or
// above, which precedence never holds for it. A slower train of lower
// priority ahead is one it could be let past too, so it does not hide what
// except costs it. Which trains are ahead is told from their look-ahead
// (project); they are followed over their long projections.
func (p *precedence) aheadOf(hp *projection, except ids.TrainID) []trailed {
	s := p.sim
	var out []trailed
	prio := s.Priority(hp.train.ID())
	for _, t := range s.trains {
		if t.ID() == hp.train.ID() || t.ID() == except || s.trainCtrllers[t.ID()].phase == phaseExited || s.Priority(t.ID()) < prio {
			continue
		}
		if runsAhead(p.project(t), hp) {
			out = append(out, trailed{pr: p.projectLong(t), cut: -1})
		}
	}
	return out
}

// passBy is how long a train setting off over region would wait, held, for h
// to get by: until h has cleared the region's tracks it runs the same way,
// plus headway. release is h's slot of the first of them: once through it, h
// is past. ok is false if h runs over none of them.
func (p *precedence) passBy(region []slot, h *projection) (wait float64, release int, ok bool) {
	hw := float64(p.cfg.HeadwayMin)
	release = -1
	for _, k := range region {
		for j, hs := range h.slots {
			if hs.track() != k.track() || hs.step.From != k.step.From {
				continue
			}
			wait = max(wait, hs.clear+hw-k.enter)
			if release < 0 || j < release {
				release = j
			}
		}
	}
	return max(wait, 0), release, release >= 0
}

func deferID(train, forTrain ids.TrainID, at string) string {
	return fmt.Sprintf("defer:%s>%s@%s", train, forTrain, at)
}

// choiceKey caches a choice: the train to hold, and whether it sets off now.
type choiceKey struct {
	l   ids.TrainID
	now bool
}

// trailFor is a higher-priority train H behind L, and what L costs it.
type trailFor struct {
	h       *ops.Train
	hp      *projection // its long projection
	others  []trailed   // the trains ahead of it it cannot be let past
	full    *projection // it following them and L, L never held
	trail   float64     // what L costs it, L never held
	trailTo string      // the station that is measured up to
}

// spot is a place L could be held: where it stands, or a later refuge.
type spot struct {
	here bool
	stop int    // L's schedule index there; -1 for here
	at   string // where, as hold ids name it
	// fits reports whether L may stand there for wait while the trains of
	// pass get by
	fits func(pass []conflict, wait float64) bool
}

// refugeOption is a place L is to be held, and the trains it is held for
// there, in the order they get by.
type refugeOption struct {
	spot
	pass  []conflict
	wait  float64 // until the last of them is by
	score float64
}

// trailChoice is where L is to be held, and for which trains: the places in
// the order L gets to them.
type trailChoice struct {
	trails map[ids.TrainID]*trailFor
	holds  []*refugeOption
	// alone is, for each train L is to be held for further on, holding L
	// where it stands for that train alone, where the rule would have it
	alone map[ids.TrainID]conflict
}

// here returns the hold where L stands, if it is to be held there.
func (ch *trailChoice) here() *refugeOption {
	if ch != nil && len(ch.holds) > 0 && ch.holds[0].here {
		return ch.holds[0]
	}
	return nil
}

// placeFor returns where L is to be held for train id, and the hold, if
// anywhere.
func (ch *trailChoice) placeFor(id ids.TrainID) (*refugeOption, conflict, bool) {
	if ch == nil {
		return nil, conflict{}, false
	}
	for _, o := range ch.holds {
		for _, c := range o.pass {
			if c.For == id {
				return o, c, true
			}
		}
	}
	return nil, conflict{}, false
}

// Limits on the search for where to hold a train: the trains catching it up
// it is held for (those that lose the most to it), and the places, here and
// its next refuges.
const (
	maxTrailers = 4
	maxSpots    = 8
)

// choice returns where l is to be held for the higher-priority trains
// catching it up, and for which, cached until the state changes. With now, l
// is taken to set off now, whatever it waits for, as when deciding on its
// request; otherwise as its projection has it, as the station plans do.
func (p *precedence) choice(l *ops.Train, now bool) *trailChoice {
	key := choiceKey{l.ID(), now}
	if ch, ok := p.choices[key]; ok {
		return ch
	}
	ch := p.choose(l, now)
	p.choices[key] = ch
	return ch
}

func (p *precedence) choose(l *ops.Train, now bool) *trailChoice {
	s := p.sim
	ch := &trailChoice{trails: map[ids.TrainID]*trailFor{}, alone: map[ids.TrainID]conflict{}}
	lp := p.projectLong(l)
	if now {
		lp = p.projectLongNow(l)
	}
	if len(lp.slots) == 0 {
		return ch
	}
	var trails []*trailFor
	for _, h := range p.higher(l.ID()) {
		if !runsAhead(p.project(l), p.project(h)) {
			continue // not behind l, or not yet within the horizon
		}
		hp := p.projectLong(h)
		if len(hp.slots) == 0 {
			continue
		}
		others := p.aheadOf(hp, l.ID())
		full := p.follow(hp, append(slices.Clone(others), trailed{lp, -1}))
		tf := &trailFor{h: h, hp: hp, others: others, full: full, trail: lateAtEnd(hp, full) - lateAtEnd(hp, p.follow(hp, others))}
		if tf.trail <= epsilon {
			continue
		}
		if n := len(hp.visits); n > 0 {
			tf.trailTo = hp.visits[n-1].stn.Code.String()
		}
		trails = append(trails, tf)
		ch.trails[h.ID()] = tf
	}
	if len(trails) == 0 {
		return ch
	}
	slices.SortStableFunc(trails, func(a, b *trailFor) int { return cmp.Compare(b.trail, a.trail) })
	trails = trails[:min(len(trails), maxTrailers)]

	tc := s.trainCtrllers[l.ID()]
	here := p.placeOf(l)
	elapsed := p.heldFor(l.ID())
	limit := p.cfg.maxHold()

	// the places, each with l cut there: at its stand, or a visit's track
	var spots []spot
	var cuts []int
	helds := map[[2]int]*projection{} // spot, train: the train following l held there
	addSpot := func(cut int, v *visit, at string) {
		j := len(spots)
		sp := spot{here: v == nil, stop: -1, at: at}
		if v != nil {
			sp.stop = v.stop
		}
		fitsMemo := map[string]bool{}
		sp.fits = func(pass []conflict, wait float64) bool {
			key := fmt.Sprintf("%.6f", wait)
			for _, c := range pass {
				key += "," + c.For.Number
			}
			if ok, done := fitsMemo[key]; done {
				return ok
			}
			var ok bool
			if v == nil {
				ok = elapsed+wait <= limit && p.trackRuledOut(l, wait) == "" &&
					!slices.ContainsFunc(pass, func(c conflict) bool {
						return !p.canReach(s.trainCtrllers[c.For].train, c.milestone, p.blockedBy(l.ID()))
					})
			} else {
				hs := make([]*projection, len(pass))
				for i, c := range pass {
					hs[i] = helds[[2]int{j, slices.IndexFunc(trails, func(tf *trailFor) bool { return tf.h.ID() == c.For })}]
				}
				ok = wait <= limit && p.refugeTrackAt(l, *v, hs, plainStay(l.Schedule()[v.stop])+wait)
			}
			fitsMemo[key] = ok
			return ok
		}
		spots = append(spots, sp)
		cuts = append(cuts, cut)
	}
	if here != "" && (tc.phase == phaseOutside || p.standsAtStation(tc)) {
		addSpot(lp.stand, nil, here)
	}
	last := len(l.Schedule()) - 1
	for _, v := range lp.visits {
		if len(spots) == maxSpots {
			break
		}
		if v.slot > lp.stand && v.stop < last {
			addSpot(v.slot, &v, v.stn.Code.String())
		}
	}

	// the hold for each train at each place, if l can be held there for it
	items := make([][]*conflict, len(trails))
	for i, tf := range trails {
		items[i] = make([]*conflict, len(spots))
		for j, sp := range spots {
			if id := holdID(l.ID(), tf.h.ID(), sp.at); p.vetoed[id] || p.expired[id] {
				continue
			}
			held := p.follow(tf.hp, append(slices.Clone(tf.others), trailed{lp, cuts[j]}))
			wait, rel, ok := p.passBy(p.regionFrom(lp, held, cuts[j]+1), held)
			saving := lateAtEnd(tf.hp, tf.full) - lateAtEnd(tf.hp, held)
			if !ok || saving <= epsilon {
				continue
			}
			c := &conflict{
				Conflict: Conflict{
					Kind: KindOvertake, Held: l.ID(), For: tf.h.ID(), HeldPrio: s.Priority(l.ID()), ForPrio: s.Priority(tf.h.ID()),
					Saving: units.Min(saving), Wait: units.Min(wait), TrailTo: tf.trailTo,
				},
				release:   held.slots[rel].track().Id,
				releaseAt: held.slots[rel].clear,
				milestone: -1,
				trail:     tf.trail,
				chosen:    true,
			}
			for _, hv := range held.visits {
				if hv.slot > rel {
					c.milestone = hv.stop
					break
				}
			}
			items[i][j] = c
			helds[[2]int{j, i}] = held
		}
		if len(spots) > 0 && spots[0].here && items[i][0] != nil {
			// a hold already placed here stands while it is still worth it,
			// and an operator may veto letting l go: it is held here or not
			// at all
			heldHere := slices.ContainsFunc(p.holds[l.ID()], func(d *Decision) bool { return d.ID == holdID(l.ID(), tf.h.ID(), here) })
			if heldHere || p.vetoed[deferID(l.ID(), tf.h.ID(), here)] {
				for j := 1; j < len(spots); j++ {
					items[i][j] = nil
				}
			}
		}
	}

	at := p.assign(spots, items)
	if at == nil {
		return ch
	}
	// a later place must be clearly better than here: held here instead for
	// every train it can be, if that comes within the margin
	if len(spots) > 0 && spots[0].here {
		moved := slices.Clone(at.spotOf)
		for i := range moved {
			if moved[i] > 0 && items[i][0] != nil {
				moved[i] = 0
			}
		}
		if alt, ok := p.judge(spots, items, moved); ok && !slices.Equal(moved, at.spotOf) &&
			alt.count >= at.count && at.score < alt.score+p.cfg.deferMargin() {
			at = alt
		}
	}
	ch.holds = at.holds
	// for the deferrals: holding l here for each train alone
	for i, j := range at.spotOf {
		if j <= 0 || len(spots) == 0 || !spots[0].here || items[i][0] == nil {
			continue
		}
		c := *items[i][0]
		if ok, why := p.rule.Hold(c.Conflict); ok && spots[0].fits([]conflict{c}, float64(c.Wait)) {
			c.why = why
			ch.alone[c.For] = c
		}
	}
	return ch
}

// refugePlan is a place for each train l could be held for (-1 for none),
// and what comes of it.
type refugePlan struct {
	spotOf []int
	holds  []*refugeOption
	count  int     // trains held for
	score  float64 // the hold rule's scores, added up
	places int
}

// assign works out where l is best held for each train. items[i][j] is the
// hold for train i at spot j, nil if it cannot be held there for it. At each
// place l waits once, until the last train held for there is by, and each
// train pays for its extra wait beyond the train before it: the hold rule
// judges it on that, and must accept every train held for. Of the
// assignments that are allowed, the one that holds for the most trains wins,
// then the best score (for thresholds: the fewest minutes lost between them),
// then the fewest places; on a tie, the earlier places. nil if l is not
// to be held for any.
func (p *precedence) assign(spots []spot, items [][]*conflict) *refugePlan {
	n := len(items)
	cur := make([]int, n)
	var best *refugePlan
	var walk func(i int)
	walk = func(i int) {
		if i == n {
			a, ok := p.judge(spots, items, cur)
			if ok && a.count > 0 && (best == nil || a.count > best.count ||
				a.count == best.count && (a.score > best.score+epsilon ||
					a.score > best.score-epsilon && a.places < best.places)) {
				best = a
			}
			return
		}
		for j := range spots {
			if items[i][j] != nil {
				cur[i] = j
				walk(i + 1)
			}
		}
		cur[i] = -1
		walk(i + 1)
	}
	walk(0)
	return best
}

// judge works out a refuge plan (assign), and whether it is allowed.
func (p *precedence) judge(spots []spot, items [][]*conflict, spotOf []int) (*refugePlan, bool) {
	a := &refugePlan{spotOf: slices.Clone(spotOf)}
	for j, sp := range spots {
		var pass []conflict
		for i, k := range spotOf {
			if k == j {
				pass = append(pass, *items[i][j])
			}
		}
		if len(pass) == 0 {
			continue
		}
		slices.SortStableFunc(pass, func(x, y conflict) int {
			return cmp.Or(cmp.Compare(x.Wait, y.Wait), cmp.Compare(x.For.Number, y.For.Number))
		})
		before, score := 0.0, 0.0
		for k := range pass {
			extra := pass[k].Conflict
			extra.Wait = units.Min(max(float64(pass[k].Wait)-before, 0))
			ok, why := p.rule.Hold(extra)
			if !ok {
				return nil, false
			}
			pass[k].why = why
			score += p.rule.Score(extra)
			before = float64(pass[k].Wait)
		}
		if !sp.fits(pass, before) {
			return nil, false
		}
		a.holds = append(a.holds, &refugeOption{spot: sp, pass: pass, wait: before, score: score})
		a.count += len(pass)
		a.score += score
		a.places++
	}
	return a, true
}

// standsAtStation reports whether the train stands, or will once its granted
// route is run, on a station track.
func (p *precedence) standsAtStation(tc *TrainController) bool {
	trk := tc.standTrack()
	if trk == nil {
		return false
	}
	_, ok := p.sim.world.StationOfPlatform(trk)
	return ok
}

// refugeTrackAt reports whether l has a track at the visit's station it could
// stand on for stay while each of hs gets by: one it can call at and may
// stand on that long (standable), and that all of them can get past.
func (p *precedence) refugeTrackAt(l *ops.Train, v visit, hs []*projection, stay float64) bool {
	if v.from == nil {
		return false
	}
	sp := l.Schedule()[v.stop]
	for _, c := range p.candidates(l, *v.from, v.stn) {
		if !callable(sp, c.pf) {
			continue
		}
		if p.standable(sp, v.stn, c.pf, stay) != "" {
			continue
		}
		at := v
		at.pf = c.pf
		if !slices.ContainsFunc(hs, func(h *projection) bool { return !p.refugeAt(at, h) }) {
			return true
		}
	}
	return false
}

// syncDefers keeps the train's deferrals up to date with the choice just made,
// ch (nil if none was made): one ends when the train gets to where it is to
// be held, or when the choice changes; and, if the train is let go now, one
// starts for each train it could have been held here for but is to be held
// for further on instead.
func (p *precedence) syncDefers(t *ops.Train, at string, ch *trailChoice, letGo bool) {
	s := p.sim
	for _, d := range slices.Clone(p.defers[t.ID()]) {
		o, _, still := ch.placeFor(d.For)
		switch {
		case at == d.To || t.StopIndex() > d.toStop:
			p.end(d, StatusDone)
		case !still || o.here || o.at != d.To:
			p.end(d, StatusReleased)
		}
	}
	if !letGo || at == "" || ch == nil || ch.here() != nil {
		return
	}
	for _, o := range ch.holds {
		for _, bc := range o.pass {
			hc, ok := ch.alone[bc.For]
			if !ok {
				continue // it would not have been held here for it
			}
			id := deferID(t.ID(), bc.For, at)
			if d := p.current[id]; d != nil && d.Status == StatusActive {
				d.Gain, d.Wait = bc.Saving, bc.Wait
				continue
			}
			tf := ch.trails[bc.For]
			d := &Decision{
				ID: id, Action: ActionDefer, Kind: KindOvertake, Train: t.ID(), For: bc.For, At: at, To: o.at,
				Status: StatusActive, Source: SourceAuto, Rule: p.rule.Name(), Since: s.CurTime(),
				Gain: bc.Saving, Wait: bc.Wait, Trail: units.Min(tf.trail), TrailTo: tf.trailTo,
				GainHere: hc.Saving, WaitHere: hc.Wait,
				Reason: fmt.Sprintf("held at %s instead: %s saves %.1f min with %.1f min waiting there, against %.1f with %.1f here",
					o.at, bc.For, bc.Saving, bc.Wait, hc.Saving, hc.Wait),
				toStop: o.stop,
			}
			p.current[id] = d
			p.all = append(p.all, d)
			p.defers[t.ID()] = append(p.defers[t.ID()], d)
			s.logDefer(d)
		}
	}
}
