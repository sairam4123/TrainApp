package timetable

import (
	"cmp"
	"maps"
	"math"
	"slices"
)

// Round turns raw run times into timetable times, train by train along its
// points. Running times get the recovery allowance; then arrivals and
// departures are rounded up to the step, so a train is never timed to leave
// before it could, and never stands less than its minimum dwell. A halt is
// timed at least one step long. The first arrival is the train's entry and is
// kept as it is.
//
// Each running time starts from when the train can really be on its way: the
// rounded departure at a stop or halt, where a timed train waits for it, but
// the unrounded time at a pass, which it runs through whenever it gets there.
// So rounding adds up only at stops, not at every station passed.
//
// Rounding one train must not change the order of trains on any track they
// share (see orders): each follower keeps as far behind its leader as it was
// in the run, or orderGap, whichever is less. A follower is held back only at
// a stop or halt, so a pass stays a pass: the wait goes to its last stop or
// halt before, less the running time from there. Moving one train can move
// the trains behind it in turn, so the trains are rounded again until no
// departure moves.
//
// The one exception is two trains that run through a station so close
// together that each needs the other's track there: a crossing on the run,
// say. Their waits go back to stops whose rounded times cannot keep the
// two the same distance apart, so one of them is given a halt there to
// cross the other (see settle).
func Round(tt Timetable, cfg Config) Timetable {
	tt.Trains = slices.Clone(tt.Trains)
	for i := range tt.Trains {
		tt.Trains[i].Points = slices.Clone(tt.Trains[i].Points)
	}
	for {
		out, cycle := settle(tt, cfg)
		if cycle == nil {
			return out
		}
		i, k, other, ok := haltFor(tt, cycle)
		if !ok {
			// no pass to halt at: give up rather than move the trains on
			// for ever
			for _, a := range cycle {
				warnUnsettled(&out.Trains[a.i])
			}
			return out
		}
		p := &tt.Trains[i].Points[k]
		reason := ReasonTechnical
		if runOpposite(tt.Trains[i].Points, k, tt.Trains[other].Points) {
			reason = ReasonCross
		}
		p.Kind = KindHalt
		p.Halt = &Halt{Reason: reason, For: tt.Trains[other].Number, Min: 0}
	}
}

const unsettled = "rounding did not settle: the order with other trains may not hold"

// settle rounds the trains, moving departures on until every order arc
// holds. If the arcs make a cycle that rounding makes ever longer, it stops
// and returns the arcs of one; if it cannot find it, it warns on the trains
// still moving instead.
func settle(tt Timetable, cfg Config) (Timetable, []arc) {
	arcs := orderArcs(tt, cfg)
	notBefore := map[[2]int]float64{} // train, point -> earliest departure, for the order
	setBy := map[[2]int]arc{}         // the arc that last moved it
	held := map[[2]int]bool{}
	for _, a := range arcs {
		held[[2]int{a.i, a.k}] = true
	}
	var out Timetable
	// each round settles at least one more departure held back, so a round
	// for each is enough unless the arcs make a cycle
	rounds := len(held) + 2
	moved := map[[2]int]bool{} // in the second half of the rounds
	for n := range rounds {
		var at [][][2]float64
		out, at = roundOnce(tt, cfg, notBefore)
		still := false
		for _, a := range arcs {
			need := at[a.lead.i][a.lead.k][a.lead.side] + a.w
			if key := [2]int{a.i, a.k}; need > notBefore[key]+1e-9 {
				notBefore[key] = need
				setBy[key] = a
				still = true
				if n >= rounds/2 {
					moved[key] = true
				}
			}
		}
		if !still {
			return out, nil
		}
	}
	cycle := cycleOf(tt, moved, setBy)
	if cycle == nil {
		for key := range moved {
			warnUnsettled(&out.Trains[key[0]])
		}
	}
	return out, cycle
}

// warnUnsettled warns that rounding gave up on the train: not every order
// arc on it may hold.
func warnUnsettled(t *Train) {
	if !slices.Contains(t.Warnings, unsettled) {
		t.Warnings = append(t.Warnings, unsettled)
	}
}

// cycleOf follows the arcs back from a departure that still moves, through
// the leader's last stop or halt that still moves before the time each waits
// for, until it comes round to one it has seen. It returns nil if it finds
// none.
func cycleOf(tt Timetable, moved map[[2]int]bool, setBy map[[2]int]arc) []arc {
	keys := slices.SortedFunc(maps.Keys(moved), func(a, b [2]int) int { return cmp.Or(a[0]-b[0], a[1]-b[1]) })
	key := keys[0]
	seen := map[[2]int]int{}
	var path []arc
	for {
		if n, ok := seen[key]; ok {
			return path[n:]
		}
		seen[key] = len(path)
		a := setBy[key]
		path = append(path, a)
		s := a.lead.k
		if a.lead.side == arrival || tt.Trains[a.lead.i].Points[s].Kind == KindPass {
			s--
		}
		for s >= 0 && !moved[[2]int{a.lead.i, s}] {
			s--
		}
		if s < 0 {
			return nil
		}
		key = [2]int{a.lead.i, s}
	}
}

// haltFor picks the pass in a cycle of arcs to make a halt: where a follower
// takes the track it waits for, so it can wait there. Of those, the lowest
// priority train's, then the one that got there last. It returns the train,
// its point and the train it waits for.
func haltFor(tt Timetable, cycle []arc) (i, k, other int, ok bool) {
	for _, a := range cycle {
		f := a.take
		if f.side == arrival {
			f.k-- // to wait short of the platform
		}
		if f.k <= 0 || tt.Trains[f.i].Points[f.k].Kind != KindPass {
			continue
		}
		better := !ok
		if ok {
			ti, tf := tt.Trains[i], tt.Trains[f.i]
			better = cmp.Or(cmp.Compare(ti.Priority, tf.Priority), cmp.Compare(tf.Points[f.k].Arr, ti.Points[k].Arr)) > 0
		}
		if better {
			i, k, other, ok = f.i, f.k, a.lead.i, true
		}
	}
	return i, k, other, ok
}

// runOpposite reports whether a train with points a, at its k-th, runs the
// other way to one with points b through that station, by the stations
// either side.
func runOpposite(a []Point, k int, b []Point) bool {
	j := slices.IndexFunc(b, func(p Point) bool { return p.Stn == a[k].Stn })
	if j < 0 {
		return false
	}
	stn := func(ps []Point, i int) string {
		if i < 0 || i >= len(ps) {
			return ""
		}
		return ps[i].Stn
	}
	aPrev, aNext, bPrev, bNext := stn(a, k-1), stn(a, k+1), stn(b, j-1), stn(b, j+1)
	return (aPrev != "" && aPrev == bNext) || (aNext != "" && aNext == bPrev)
}

// roundOnce rounds every train, with each point's departure no earlier than
// notBefore says. It also returns when each train really arrives at each point
// and leaves it, as a timed train runs: rounded departures at stops and
// halts, which it waits for, but unrounded times at passes.
func roundOnce(tt Timetable, cfg Config, notBefore map[[2]int]float64) (Timetable, [][][2]float64) {
	out := Timetable{RoundingMin: cfg.RoundingMin, Check: tt.Check}
	recovery := 1 + cfg.RecoveryPct/100
	up := func(x float64) float64 {
		// tolerate float noise just over a step
		v := math.Ceil(x/cfg.RoundingMin-1e-9) * cfg.RoundingMin
		return math.Round(v*1e6) / 1e6
	}
	at := make([][][2]float64, len(tt.Trains))
	for i, src := range tt.Trains {
		tr := src
		tr.Points = slices.Clone(src.Points)
		tr.Warnings = slices.Clone(src.Warnings)
		at[i] = make([][2]float64, len(tr.Points))
		gone := 0.0 // when the train is on its way from the previous point
		for k := range tr.Points {
			p, raw := &tr.Points[k], src.Points[k]
			at[i][k] = [2]float64{raw.Arr, raw.Dep}
			if !raw.Reached() {
				break
			}
			arr := raw.Arr
			if k > 0 {
				if tr.Points[k-1].Dep < 0 {
					break
				}
				run := raw.Arr - src.Points[k-1].Dep
				arr = max(raw.Arr, gone+run*recovery)
				p.Arr = up(arr)
			}
			switch {
			case raw.Dep < 0:
			case p.Kind == KindPass:
				p.Dep = p.Arr
				gone = arr
			default:
				least := p.Arr + p.MinDwell
				if p.Halt != nil {
					least += cfg.RoundingMin
				}
				p.Dep = up(max(raw.Dep, least, notBefore[[2]int{i, k}]))
				gone = p.Dep
			}
			at[i][k] = [2]float64{arr, gone}
		}
		// the way to each point moves on with the departure before it
		for k := 1; k < len(tr.Points); k++ {
			if len(src.Points[k].Via) == 0 || src.Points[k-1].Dep < 0 {
				continue
			}
			shift := at[i][k-1][1] - src.Points[k-1].Dep
			tr.Points[k].Via = slices.Clone(src.Points[k].Via)
			for n := range tr.Points[k].Via {
				v := &tr.Points[k].Via[n]
				v.In += shift
				if v.Out >= 0 {
					v.Out += shift
				}
			}
		}
		tr.fill()
		out.Trains = append(out.Trains, tr)
	}
	return out, at
}

// orderGap is the most a follower must keep behind its leader, however far
// behind it was in the run: a rounding step and about a run along a platform.
func orderGap(cfg Config) float64 { return cfg.RoundingMin + platformRunMin }

const platformRunMin = 1.0

// arc holds train i's departure from its point k back until w after the
// leader's time lead. take is where the follower takes the resource.
type arc struct {
	i, k       int
	lead, take end
	w          float64
}

// orderArcs are the arcs that keep every order in tt (see orders), each on
// the follower's last stop or halt at or before where it takes the resource.
// An order the follower takes at its entry, which is kept as it is, has none.
func orderArcs(tt Timetable, cfg Config) []arc {
	recovery := 1 + cfg.RecoveryPct/100
	var out []arc
	for _, o := range orders(tt) {
		f, l := o.follow.take, o.lead.free
		g := tt.Trains[f.i].Points
		// the follower may take the resource no sooner after the leader
		// has freed it than in the run, or orderGap; a track it takes some
		// way after its departure, which the leader frees some way before
		// its arrival
		w := min(o.follow.in-o.lead.out, orderGap(cfg)) - (timeOf(tt, l) - o.lead.out) - (o.follow.in - timeOf(tt, f))
		// back to the stop or halt, less the running time from there
		s := f.k
		if f.side == arrival || g[s].Kind == KindPass {
			for s--; s >= 0 && g[s].Kind == KindPass; s-- {
			}
		}
		if s < 0 {
			continue
		}
		for q := s + 1; q <= f.k; q++ {
			w -= (g[q].Arr - g[q-1].Dep) * recovery
		}
		out = append(out, arc{i: f.i, k: s, lead: l, take: f, w: w})
	}
	return out
}

// Sides of a point.
const (
	arrival   = 0
	departure = 1
)

// end is a train's arrival at a point or its departure from it.
type end struct{ i, k, side int }

func timeOf(tt Timetable, e end) float64 {
	p := tt.Trains[e.i].Points[e.k]
	if e.side == arrival {
		return p.Arr
	}
	return p.Dep
}

// use is a train holding a resource from in to out. The train takes it on
// its way from take and frees it on its way to free: a platform from its
// arrival to its departure there, a track from its departure from the point
// before to its arrival at the next.
type use struct {
	take, free end
	in, out    float64
}

// order is two trains that used a resource one after the other.
type order struct {
	resource     string
	lead, follow use
	track        bool // a track between points, not a platform
}

// orders are, for every resource, each consecutive pair of the trains that
// used it, in the order they took it in tt. A resource is a station's
// platform, a track between two points (Point.Via), or a switch block one of
// those tracks is in (Via.Blocks): that covers single and double lines, loops
// and throats alike, and two trains on different tracks over one switch.
// Points TimedTrains leaves out are left out here too.
func orders(tt Timetable) []order {
	type user struct {
		use
		track bool
	}
	users := map[string][]user{}
	for i, tr := range tt.Trains {
		for k, p := range tr.Points {
			if !p.Reached() || p.Dep < 0 {
				break // as TimedTrains
			}
			if p.Pf != "" {
				key := "pf " + p.Stn + " " + p.Pf
				users[key] = append(users[key], user{use{end{i, k, arrival}, end{i, k, departure}, p.Arr, p.Dep}, false})
			}
			if k == 0 {
				continue
			}
			for _, v := range p.Via {
				out := v.Out
				if out < 0 {
					out = p.Arr
				}
				u := user{use{end{i, k - 1, departure}, end{i, k, arrival}, v.In, out}, true}
				users["track "+v.Track] = append(users["track "+v.Track], u)
				for _, b := range v.Blocks {
					users["switch "+b] = append(users["switch "+b], u)
				}
			}
		}
	}
	var out []order
	for _, key := range slices.Sorted(maps.Keys(users)) {
		us := users[key]
		slices.SortStableFunc(us, func(a, b user) int {
			return cmp.Or(cmp.Compare(a.in, b.in), cmp.Compare(a.out, b.out))
		})
		for n := 1; n < len(us); n++ {
			if us[n-1].take.i == us[n].take.i {
				continue // its own times keep it in order
			}
			out = append(out, order{resource: key, lead: us[n-1].use, follow: us[n].use, track: us[n].track})
		}
	}
	return out
}
