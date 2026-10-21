package kpi

import (
	"cmp"
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
)

// Why trains lost time, and what the tracks' occupied time went on.
//
// A train's delay already splits, call by call, into what it gained on the
// way to each point and what it gained there (callsOf). Each of those is
// split again by cause: its waits, cut where a precedence hold covered
// them; a disruption's own minutes; and what is left, running or dwelling
// longer than it would alone. The losses of a finished train so sum to its
// delay. On the way between two points the rest is spread over the tracks
// by when the front entered each, against the free run's, so the losses
// add up by track too.
//
// A track's occupied minutes split the same way: the train's waits and
// stands while on it are lost; of the rest, as long as the free run
// occupied it is needed, free running or (on a platform) dwell, and the
// remainder is slower running.

// sectionNames names the sections between neighbouring stations: one name
// for both directions on a single line ("PDKT–KKDI", the stations in line
// order), one for each where more than one line joins them ("PDKT→KKDI").
type sectionNames struct {
	lines map[[2]string]int // how many running lines leave the first station for the second
	rank  map[string]int    // the stations' line order
}

// newSectionNames finds the lines between the stations of w. order is the
// stations in line order.
func newSectionNames(w *infra.World, order []StationUse) sectionNames {
	n := sectionNames{lines: map[[2]string]int{}, rank: map[string]int{}}
	for i, su := range order {
		n.rank[su.Stn] = i
	}
	pl := places(w)
	g := w.TrackGraph
	for _, a := range w.ListStations() {
		for _, line := range a.Lines() {
			// walk out along the line over tracks outside every station's
			// area, to the stations it reaches
			reached := map[string]bool{}
			seen := map[ids.TrackID]bool{line: true}
			queue := []ids.TrackID{line}
			for len(queue) > 0 {
				id := queue[0]
				queue = queue[1:]
				if p, ok := pl[id]; ok && id != line {
					if p.stn != a {
						reached[p.stn.Code.String()] = true
					}
					continue
				}
				e := g.Edges[id]
				if e == nil {
					continue
				}
				for _, pt := range []*infra.TrackPoint{e.From, e.To} {
					for _, next := range g.EdgesAt(pt) {
						if !seen[next.Track.Id] {
							seen[next.Track.Id] = true
							queue = append(queue, next.Track.Id)
						}
					}
				}
			}
			for b := range reached {
				n.lines[[2]string{a.Code.String(), b}]++
			}
		}
	}
	return n
}

// multi is whether more than one line joins the stations.
func (n sectionNames) multi(a, b string) bool {
	return max(n.lines[[2]string{a, b}], n.lines[[2]string{b, a}]) > 1
}

// name is the section a train runs over from station from to station to.
func (n sectionNames) name(from, to string) string {
	if n.multi(from, to) {
		return from + "→" + to
	}
	ra, okA := n.rank[from]
	rb, okB := n.rank[to]
	if okA && okB && rb < ra || !okA && okB {
		from, to = to, from
	}
	return from + "–" + to
}

// hold is a precedence hold on a train, from since to end.
type hold struct {
	kind, forTrain string
	since, end     float64
}

// holdsOf are each train's holds, earliest first; open ones end at end.
func holdsOf(ds []sim.Decision, end float64) map[string][]hold {
	out := map[string][]hold{}
	for _, d := range ds {
		if d.Action != sim.ActionHold {
			continue
		}
		to := d.Ended
		if d.Status == sim.StatusActive || to < d.Since {
			to = end
		}
		out[d.Train.Number] = append(out[d.Train.Number], hold{kind: string(d.Kind), forTrain: d.For.Number, since: d.Since, end: to})
	}
	for _, hs := range out {
		slices.SortStableFunc(hs, func(a, b hold) int { return cmp.Compare(a.since, b.since) })
	}
	return out
}

// segment is part of a wait: held by a hold, or not (nil).
type segment struct {
	from, to float64
	hold     *hold
}

// splitByHolds cuts [from, to] where the holds cover it, in time order.
func splitByHolds(holds []hold, from, to float64) []segment {
	var out []segment
	at := from
	for i := range holds {
		h := &holds[i]
		s, e := max(h.since, at), min(h.end, to)
		if e <= s {
			continue
		}
		if s > at {
			out = append(out, segment{from: at, to: s})
		}
		out = append(out, segment{from: s, to: e, hold: h})
		at = e
	}
	if to > at {
		out = append(out, segment{from: at, to: to})
	}
	return out
}

// piece is a wait, or part of one, or a stand: time a train lost standing,
// on the way to or at schedule point k.
type piece struct {
	Loss
	k int
}

// attributor works out the causes of a run.
type attributor struct {
	w         *infra.World
	names     sectionNames
	trackName map[string]string // track id to name
	holds     map[string][]hold
	late      map[string]float64 // each train's late entry, in minutes
	slow      map[string]bool    // trains with a slow entry
	minStop   units.Minutes
	end       float64
}

func newAttributor(s *sim.Sim, order []StationUse, disruptions []ops.Disruption, end float64) *attributor {
	a := &attributor{
		w:         s.World(),
		names:     newSectionNames(s.World(), order),
		trackName: map[string]string{},
		holds:     holdsOf(s.Decisions(), end),
		late:      map[string]float64{},
		slow:      map[string]bool{},
		minStop:   s.MinStop(),
		end:       end,
	}
	for _, trk := range s.World().TrackGraph.ListTracks() {
		a.trackName[trk.Id.String()] = trk.String()
	}
	for _, d := range disruptions {
		switch d.Kind {
		case ops.DisruptLateEntry:
			a.late[d.Train] += d.Min
		case ops.DisruptSlowEntry:
			a.slow[d.Train] = true
		}
	}
	return a
}

// place is where the way to point k of the train is: its entry, or the
// section from the point before.
func (a *attributor) place(t *ops.Train, k int) (where, at string) {
	sched := t.Schedule()
	if k == 0 {
		return WhereEntry, sched[0].StnCode.String()
	}
	return WhereSection, a.names.name(sched[k-1].StnCode.String(), sched[k].StnCode.String())
}

// platform is the name of the platform track the train used at sp, ""
// if unknown.
func (a *attributor) platform(sp *ops.SchedulePoint) string {
	stn, ok := a.w.GetStation(sp.StnCode)
	if !ok || sp.ActPfNo == "" {
		return ""
	}
	if trk := stn.FindStnPlatform(sp.ActPfNo); trk != nil {
		return trk.String()
	}
	return ""
}

// waitCause is the cause of a wait no hold covers.
var waitCause = map[string]string{WaitEntry: CauseEntry, WaitSignal: CauseSignal, WaitStation: CauseStation}

// pieces are the train's waits, cut where its holds cover them, and its
// stands, in time order. points are the schedule points of the waits; each
// wait is given the cause and train of most of its minutes.
func (a *attributor) pieces(t *ops.Train, ws []Wait, points []int, firstArrival float64) []piece {
	sched := t.Schedule()
	var out []piece
	for i := range ws {
		w := &ws[i]
		k := points[i]
		where, at := a.place(t, k)
		track := w.At // a signal's track
		switch w.Kind {
		case WaitStation:
			where, at, track = WhereStation, w.At, a.platform(sched[k])
		case WaitEntry:
			track = ""
		}
		best := -1.0
		for _, seg := range splitByHolds(a.holds[t.Number], w.FromMin, w.FromMin+w.Min) {
			l := Loss{Cause: waitCause[w.Kind], For: w.For, Where: where, At: at, Track: track, FromMin: seg.from, Min: seg.to - seg.from}
			if seg.hold != nil {
				l.Cause, l.Kind, l.For = CauseHeld, seg.hold.kind, seg.hold.forTrain
			}
			out = append(out, piece{l, k})
			if l.Min > best {
				best, w.Cause = l.Min, l.Cause
				if l.Cause == CauseHeld {
					w.For = l.For
				}
			}
		}
	}
	for k, sp := range sched {
		extra := float64(sp.ExtraStand())
		if extra <= 0 || !sp.Arrived() {
			continue
		}
		arr := sp.ActArrTime
		if k == 0 && firstArrival >= 0 {
			arr = firstArrival
		}
		from := arr + float64(sp.ExpDwellTime(arr, a.minStop))
		to := from + extra
		if dep := sp.ActDeptTime; dep >= 0 {
			to = min(to, dep)
		} else {
			to = min(to, a.end)
		}
		if to > from {
			out = append(out, piece{Loss{Cause: CauseDisruption, Kind: ops.DisruptStand, Where: WhereStation, At: sp.StnCode.String(),
				Track: a.platform(sp), FromMin: from, Min: to - from}, k})
		}
	}
	slices.SortStableFunc(out, func(x, y piece) int { return cmp.Compare(x.FromMin, y.FromMin) })
	return out
}

// running is a loss of running or dwelling longer than running free, or of
// a slow entry while the train got up to speed.
func (a *attributor) running(train string, k int, l Loss) Loss {
	if a.slow[train] && k <= 1 && l.Min > 0 {
		l.Cause, l.Kind = CauseDisruption, ops.DisruptSlowEntry
	}
	return l
}

// losses splits the finished train's delay by place and cause, nil if a
// call lacks its delay. ps are its pieces.
func (a *attributor) losses(t *ops.Train, calls []Call, free *FreeRun, ps []piece) []Loss {
	sched := t.Schedule()
	if free == nil || len(calls) != len(sched) || len(free.Calls) < len(sched) {
		return nil
	}
	for _, c := range calls {
		if c.ApproachMin == nil || c.StationMin == nil {
			return nil
		}
	}
	var out []Loss
	for _, p := range ps {
		out = append(out, p.Loss)
	}
	for k, c := range calls {
		sp := sched[k]
		stn := sp.StnCode.String()
		var way []piece
		var wayMin, atMin float64
		for _, p := range ps {
			switch {
			case p.k != k:
			case p.Where == WhereStation:
				atMin += p.Min
			default:
				way = append(way, p)
				wayMin += p.Min
			}
		}
		if k == 0 {
			rest := *c.ApproachMin - wayMin
			if late := a.late[t.Number]; late > 0 {
				out = append(out, Loss{Cause: CauseDisruption, Kind: ops.DisruptLateEntry, Where: WhereEntry, At: stn, FromMin: sp.ArrTime - late, Min: late})
				rest -= late
			}
			out = append(out, a.running(t.Number, k, Loss{Cause: CauseRunning, Kind: RunSection, Where: WhereEntry, At: stn, FromMin: sp.ActArrTime, Min: rest}))
		} else {
			out = append(out, a.wayRunning(t, k, free, way, *c.ApproachMin)...)
		}
		arr := sp.ActArrTime
		out = append(out, a.running(t.Number, k, Loss{Cause: CauseRunning, Kind: RunDwell, Where: WhereStation, At: stn, Track: a.platform(sp),
			FromMin: arr, Min: *c.StationMin - atMin}))
	}
	out = slices.DeleteFunc(out, func(l Loss) bool { return l.Min > -1e-9 && l.Min < 1e-9 })
	slices.SortStableFunc(out, func(x, y Loss) int { return cmp.Compare(x.FromMin, y.FromMin) })
	return out
}

// wayRunning is the running loss on the way to point k: what the train
// gained there, gained, less its waits on the way, spread over the tracks
// it ran over. Each track's share is how much longer the front took from
// entering it to entering the next (the last, to arriving) than running
// free. Where it went another way than the free run, what follows the
// tracks both took is spread over its own in proportion to its time on
// them.
func (a *attributor) wayRunning(t *ops.Train, k int, free *FreeRun, way []piece, gained float64) []Loss {
	sched := t.Schedule()
	sp, prev := sched[k], sched[k-1]
	_, at := a.place(t, k)
	base := Loss{Cause: CauseRunning, Kind: RunSection, Where: WhereSection, At: at, FromMin: prev.ActDeptTime}
	via := sp.Via
	if len(via) == 0 {
		rest := gained
		for _, p := range way {
			rest -= p.Min
		}
		base.Min = rest
		return []Loss{a.running(t.Number, k, base)}
	}
	// the front's time on each track, actual and free
	spans := func(start, arrive float64, in func(i int) float64, n int) []float64 {
		out := make([]float64, n)
		for i := range n {
			from, to := start, arrive
			if i > 0 {
				from = in(i)
			}
			if i+1 < n {
				to = in(i + 1)
			}
			out[i] = to - from
		}
		return out
	}
	act := spans(prev.ActDeptTime, sp.ActArrTime, func(i int) float64 { return via[i].In }, len(via))
	front := free.Calls[k].Front
	fre := spans(free.Calls[k-1].DepMin, free.Calls[k].ArrMin, func(i int) float64 { return front[i].InMin }, len(front))
	shared := 0
	for shared < len(via) && shared < len(front) && via[shared].Track == front[shared].Track {
		shared++
	}
	gain := make([]float64, len(via))
	rest := gained
	if shared == len(via) && shared == len(front) {
		for i := range via {
			gain[i] = act[i] - fre[i]
		}
	} else {
		for i := range shared {
			gain[i] = act[i] - fre[i]
			rest -= gain[i]
		}
		time := 0.0
		for i := shared; i < len(via); i++ {
			time += act[i]
		}
		for i := shared; i < len(via); i++ {
			switch {
			case time > 0:
				gain[i] = rest * act[i] / time
			case i == len(via)-1:
				gain[i] = rest
			}
		}
	}
	// the waits on the way stood on their own tracks; one on a track
	// not of the way (at the point's own platform) comes off the last
	for _, p := range way {
		j := len(via) - 1
		for i := range via {
			if a.trackName[via[i].Track] == p.Track {
				j = i
			}
		}
		gain[j] -= p.Min
	}
	out := make([]Loss, len(via))
	for i, v := range via {
		l := base
		l.Track, l.Min = a.trackName[v.Track], gain[i]
		if i > 0 {
			l.FromMin = v.In
		}
		out[i] = a.running(t.Number, k, l)
	}
	return out
}

// lost is whether time of the cause is capacity lost to a wait or a stand.
func lost(cause string) bool {
	switch cause {
	case CauseHeld, CauseSignal, CauseStation, CauseDisruption:
		return true
	}
	return false
}

// shareOrder is the order a track's split is listed in.
var shareOrder = []string{CauseFree, CauseDwell, CauseSlower, CauseHeld, CauseSignal, CauseStation, CauseDisruption}

// overlap is how long [a, b] and [c, d] overlap.
func overlap(a, b, c, d float64) float64 {
	return max(min(b, d)-max(a, c), 0)
}

// splitOccupancy splits each track's occupied minutes by cause (see the top
// of the file). ps are each train's pieces.
func (a *attributor) splitOccupancy(u *Utilization, c *Collector, ps map[string][]piece, free map[string]FreeRun) {
	type key struct {
		train string
		track ids.TrackID
	}
	type use struct {
		running, standing float64 // not lost
		lost              map[[2]string]float64
		kind              string
	}
	pl := places(a.w)
	uses := map[key]*use{}
	var keys []key
	for _, o := range c.occs {
		enter, travelEnd, leave := o.span(a.end)
		k := key{o.train, o.track.Id}
		u := uses[k]
		if u == nil {
			u = &use{lost: map[[2]string]float64{}, kind: cmp.Or(pl[o.track.Id].kind, KindBlock)}
			uses[k] = u
			keys = append(keys, k)
		}
		run, stand := travelEnd-enter, leave-travelEnd
		for _, p := range ps[o.train] {
			if !lost(p.Cause) {
				continue
			}
			to := p.FromMin + p.Min
			if ov := overlap(p.FromMin, to, enter, leave); ov > 0 {
				u.lost[[2]string{p.Cause, p.Kind}] += ov
				run -= overlap(p.FromMin, to, enter, travelEnd)
				stand -= overlap(p.FromMin, to, travelEnd, leave)
			}
		}
		u.running += run
		u.standing += stand
	}
	byTrack := map[string]map[[2]string]float64{}
	for _, k := range keys {
		u := uses[k]
		split := byTrack[k.track.String()]
		if split == nil {
			split = map[[2]string]float64{}
			byTrack[k.track.String()] = split
		}
		for c, m := range u.lost {
			split[c] += m
		}
		rest := u.running + u.standing
		needed := rest
		if fr, ok := free[k.train]; ok {
			if fo, ok := fr.Occupied[k.track.String()]; ok {
				needed = min(rest, fo)
			}
		}
		needed = max(needed, 0)
		split[[2]string{CauseSlower, ""}] += rest - needed
		if u.kind == KindPlatform {
			dwell := min(needed, max(u.standing, 0))
			split[[2]string{CauseDwell, ""}] += dwell
			needed -= dwell
		}
		split[[2]string{CauseFree, ""}] += needed
	}
	name := map[string]string{} // track id to name
	for _, trk := range a.w.TrackGraph.ListTracks() {
		name[trk.Id.String()] = trk.String()
	}
	byName := map[string]map[[2]string]float64{}
	for id, split := range byTrack {
		byName[name[id]] = split
	}
	for i := range u.Tracks {
		split := byName[u.Tracks[i].Track]
		var out []Share
		for c, m := range split {
			if m > 1e-9 || m < -1e-9 {
				out = append(out, Share{Cause: c[0], Kind: c[1], Min: m})
			}
		}
		slices.SortFunc(out, func(x, y Share) int {
			return cmp.Or(cmp.Compare(slices.Index(shareOrder, x.Cause), slices.Index(shareOrder, y.Cause)), cmp.Compare(x.Kind, y.Kind))
		})
		u.Tracks[i].Split = out
	}
}

// blockSections names the section each block is part of: the section of
// the ways that ran over it, the most common if they differ.
func (a *attributor) blockSections(u *Utilization, trains []*ops.Train) {
	count := map[string]map[string]int{}
	for _, t := range trains {
		sched := t.Schedule()
		for k := 1; k < len(sched); k++ {
			_, sec := a.place(t, k)
			for _, v := range sched[k].Via {
				n := a.trackName[v.Track]
				if count[n] == nil {
					count[n] = map[string]int{}
				}
				count[n][sec]++
			}
		}
	}
	for i := range u.Tracks {
		tu := &u.Tracks[i]
		if tu.Kind != KindBlock {
			continue
		}
		best := 0
		for sec, n := range count[tu.Track] {
			if n > best || n == best && sec < tu.Section {
				tu.Section, best = sec, n
			}
		}
	}
}
