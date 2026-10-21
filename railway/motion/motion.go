// Package motion is the run-time model: how a train runs over a sequence of
// tracks, and so how long it takes. Everything that needs a run time asks it,
// so that the sim, its look-ahead, the timetable and the viewer agree.
//
// A train accelerates and brakes at constant rates (Train.Accel and
// Train.Brake, 0 meaning instantly) and never exceeds its limit: the lowest of
// its own top speed and the limits of every track under it, from rear to
// front. A lower limit so holds until the whole train has passed it. The
// curve is built the usual way, with a backward pass for braking and a
// forward pass for acceleration, and is made of pieces at constant
// acceleration, so every time is closed-form. With instant acceleration and
// braking, each track is run at its limit, t = d/s. See
// docs/trainapp/length_dynamics/phase3.md.
package motion

import (
	"math"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/units"
)

// Way is what a curve is planned over.
type Way struct {
	// Behind are the steps behind Steps[0] still under the train, oldest
	// first: their limits hold until its rear has left them.
	Behind []infra.Step
	Steps  []infra.Step
	// From is where the front starts, metres along Steps[0], and Speed its
	// speed there.
	From  units.Meters
	Speed units.MetersPerMin
	// Stop is set when the train must be at rest at the end of the last
	// step.
	Stop bool
}

// Curve is how a train runs a Way: a run of pieces, each at a constant
// acceleration (positive, zero or negative). Times on a step are counted
// from the front at its start, or for the first step from where the curve
// starts.
type Curve struct {
	pieces  []piece
	first   []int // per step, its first piece; first[len] is len(pieces)
	lengths []float64
	from    float64
	speed   float64 // at the start, after any limit it was over
}

// piece is a stretch of a step run at constant acceleration.
type piece struct {
	step int
	off  float64 // m along the step where it starts
	dx   float64 // m
	v0   float64 // m/min at its start
	v1   float64 // m/min at its end
	a    float64 // m/min²; 0 at a constant speed
	dt   float64 // min
}

// timeFor is the minutes the piece takes to run its first s metres.
func (p piece) timeFor(s float64) float64 {
	switch {
	case s <= 0:
		return 0
	case s >= p.dx:
		return p.dt
	case p.a == 0:
		return p.dt * s / p.dx
	}
	// s = v0·τ + a·τ²/2, in the form that stays exact for either sign of a
	return 2 * s / (p.v0 + math.Sqrt(max(p.v0*p.v0+2*p.a*s, 0)))
}

// distIn is how far the piece runs in its first tau minutes.
func (p piece) distIn(tau float64) float64 {
	switch {
	case tau <= 0:
		return 0
	case tau >= p.dt:
		return p.dx
	case p.a == 0:
		return p.dx * tau / p.dt
	}
	return min(p.v0*tau+p.a*tau*tau/2, p.dx)
}

// Steps is how many steps the curve runs over.
func (c Curve) Steps() int { return len(c.lengths) }

// Length is how long step i is.
func (c Curve) Length(i int) units.Meters { return units.Meters(c.lengths[i]) }

// start is where the curve's part of step i starts.
func (c Curve) start(i int) float64 {
	if i == 0 {
		return c.from
	}
	return 0
}

// TimeIn is the minutes from the front at the start of step i (for the first
// step, where the curve starts) until it has run to d metres along it.
func (c Curve) TimeIn(i int, d units.Meters) float64 {
	x := float64(d)
	sum := 0.0
	for _, p := range c.pieces[c.first[i]:c.first[i+1]] {
		if end := p.off + p.dx; x < end {
			return sum + p.timeFor(x-p.off)
		}
		sum += p.dt
	}
	return sum
}

// Time is the minutes the front spends on step i: from its start (for the
// first step, from where the curve starts) to its end.
func (c Curve) Time(i int) float64 {
	sum := 0.0
	for _, p := range c.pieces[c.first[i]:c.first[i+1]] {
		sum += p.dt
	}
	return sum
}

// Total is the minutes the whole curve takes.
func (c Curve) Total() float64 {
	sum := 0.0
	for i := range c.lengths {
		sum += c.Time(i)
	}
	return sum
}

// At is where the front is on step i, metres along it, and its speed, tau
// minutes after it was at the start of the step (for the first step, at the
// start of the curve). tau is held to the step.
func (c Curve) At(i int, tau float64) (units.Meters, units.MetersPerMin) {
	ps := c.pieces[c.first[i]:c.first[i+1]]
	if len(ps) == 0 {
		return units.Meters(c.lengths[i]), units.MetersPerMin(c.entrySpeed(i))
	}
	if tau <= 0 {
		return units.Meters(ps[0].off), units.MetersPerMin(ps[0].v0)
	}
	for _, p := range ps {
		if tau < p.dt {
			return units.Meters(p.off + p.distIn(tau)), units.MetersPerMin(p.v0 + p.a*tau)
		}
		tau -= p.dt
	}
	last := ps[len(ps)-1]
	return units.Meters(c.lengths[i]), units.MetersPerMin(last.v1)
}

// entrySpeed is the speed the front reaches step i at.
func (c Curve) entrySpeed(i int) float64 {
	for _, p := range c.pieces[c.first[i]:] {
		return p.v0
	}
	if n := len(c.pieces); n > 0 {
		return c.pieces[n-1].v1
	}
	return c.speed
}

// EndSpeed is the speed the front leaves step i at, exactly: 0 at the end of
// a curve that stops.
func (c Curve) EndSpeed(i int) units.MetersPerMin {
	if ps := c.pieces[c.first[i]:c.first[i+1]]; len(ps) > 0 {
		return units.MetersPerMin(ps[len(ps)-1].v1)
	}
	return units.MetersPerMin(c.entrySpeed(i))
}

// StartSpeed is the speed the curve starts at: Way.Speed, or lower if that
// was more than the train could have stopped from in time.
func (c Curve) StartSpeed() units.MetersPerMin { return units.MetersPerMin(c.speed) }

// ExitSpeed is the speed at the end of the last step: 0 if the train stops
// there.
func (c Curve) ExitSpeed() units.MetersPerMin {
	if n := len(c.pieces); n > 0 {
		return units.MetersPerMin(c.pieces[n-1].v1)
	}
	return units.MetersPerMin(c.speed)
}

// Key is a point on a step where the speed changes how it changes: the start
// or end of a piece. Speed is linear in time between two keys.
type Key struct {
	T    float64 // minutes from the start of the step (see TimeIn)
	Off  units.Meters
	Kmph float64
}

// Keys are step i's piece boundaries, in order: its start, the ends of its
// pieces. A step the curve does not run over has none.
func (c Curve) Keys(i int) []Key {
	ps := c.pieces[c.first[i]:c.first[i+1]]
	if len(ps) == 0 {
		return nil
	}
	out := []Key{{T: 0, Off: units.Meters(ps[0].off), Kmph: units.MetersPerMin(ps[0].v0).InKMPH()}}
	t := 0.0
	for _, p := range ps {
		t += p.dt
		out = append(out, Key{T: t, Off: units.Meters(p.off + p.dx), Kmph: units.MetersPerMin(p.v1).InKMPH()})
	}
	return out
}

// Point is a place on a curve: a step, how far along it, and the minutes
// from the start of the curve until the front gets there.
type Point struct {
	Step int
	Off  units.Meters
	T    float64
}

// BrakingPoint is where the train starts its last braking, down to rest at
// the end: the start of the run of braking pieces that ends the curve. It is
// the end itself when the curve does not end braking (instant braking, or
// not stopping).
func (c Curve) BrakingPoint() Point {
	k := len(c.pieces)
	for k > 0 && c.pieces[k-1].a < 0 {
		k--
	}
	if k == len(c.pieces) {
		n := c.Steps()
		if n == 0 {
			return Point{}
		}
		return Point{Step: n - 1, Off: units.Meters(c.lengths[n-1]), T: c.Total()}
	}
	p := c.pieces[k]
	t := 0.0
	for _, q := range c.pieces[:k] {
		t += q.dt
	}
	return Point{Step: p.step, Off: units.Meters(p.off), T: t}
}

// PointAt is where the front is t minutes after the start of the curve, held
// to the curve.
func (c Curve) PointAt(t float64) Point {
	n := c.Steps()
	if n == 0 {
		return Point{}
	}
	if t <= 0 {
		return Point{Step: 0, Off: units.Meters(c.from)}
	}
	elapsed := 0.0
	for i := range n {
		d := c.Time(i)
		if t < elapsed+d {
			off, _ := c.At(i, t-elapsed)
			return Point{Step: i, Off: off, T: t}
		}
		elapsed += d
	}
	return Point{Step: n - 1, Off: units.Meters(c.lengths[n-1]), T: elapsed}
}

// segment is a stretch of a step at one limit.
type segment struct {
	step   int
	o1, o2 float64 // m along the step
	lim    float64 // m/min
}

// Plan works out how train t runs w.
func Plan(t *ops.Train, w Way) Curve {
	n := len(w.Steps)
	c := Curve{lengths: make([]float64, n), first: make([]int, n+1), from: float64(w.From), speed: float64(w.Speed)}
	if n == 0 {
		return c
	}
	for i, st := range w.Steps {
		c.lengths[i] = float64(st.Edge.Track.Length)
	}
	c.from = min(max(c.from, 0), c.lengths[0])
	segs := limits(t, w, c.lengths, c.from)

	acc, brk := float64(t.Accel), float64(t.Brake)
	// bound[j] is the most the speed may be at the start of segment j (the
	// end of the last for j == len): within the limits on either side, and
	// low enough to brake for everything after it
	bound := make([]float64, len(segs)+1)
	if w.Stop {
		bound[len(segs)] = 0
	} else if len(segs) > 0 {
		bound[len(segs)] = segs[len(segs)-1].lim
	}
	for j := len(segs) - 1; j >= 0; j-- {
		b := min(segs[j].lim, reach(bound[j+1], brk, segs[j].o2-segs[j].o1))
		if j > 0 {
			b = min(b, segs[j-1].lim)
		}
		bound[j] = b
	}
	// speed[j] is the speed at the start of segment j: as high as
	// accelerating from the start allows, within the bounds
	speed := make([]float64, len(segs)+1)
	speed[0] = c.speed
	if len(segs) > 0 {
		speed[0] = min(c.speed, bound[0])
	} else if w.Stop {
		speed[0] = 0
	}
	c.speed = speed[0]
	for j, sg := range segs {
		speed[j+1] = min(bound[j+1], reach(speed[j], acc, sg.o2-sg.o1))
	}

	for j, sg := range segs {
		c.pieces = append(c.pieces, run(sg, speed[j], speed[j+1], acc, brk)...)
		c.first[sg.step+1] = len(c.pieces)
	}
	// a step with no pieces (no length, or nothing left of the first)
	// starts where the one before ends
	for i := 1; i <= n; i++ {
		c.first[i] = max(c.first[i], c.first[i-1])
	}
	return c
}

// reach is the speed reached from v over d metres at rate a: unbounded when
// a is 0, which means instantly.
func reach(v, a, d float64) float64 {
	if a <= 0 {
		return math.Inf(1)
	}
	return math.Sqrt(v*v + 2*a*d)
}

// run is the pieces of one segment: from speed v0 at its start to v1 at its
// end, accelerating at acc and braking at brk, and as close to its limit as
// they allow. A rate of 0 is instant.
func run(sg segment, v0, v1, acc, brk float64) []piece {
	d := sg.o2 - sg.o1
	top := sg.lim
	if acc <= 0 && brk <= 0 {
		// at the limit throughout: t = d/s
		return []piece{{step: sg.step, off: sg.o1, dx: d, v0: top, v1: top, dt: d / top}}
	}
	da, db := 0.0, 0.0
	if acc > 0 {
		da = (top*top - v0*v0) / (2 * acc)
	}
	if brk > 0 {
		db = (top*top - v1*v1) / (2 * brk)
	}
	if da+db > d {
		// it never reaches the limit: accelerate to a peak, then brake
		var peak2 float64
		switch {
		case acc <= 0:
			peak2 = v1*v1 + 2*brk*d
		case brk <= 0:
			peak2 = v0*v0 + 2*acc*d
		default:
			peak2 = (2*acc*brk*d + brk*v0*v0 + acc*v1*v1) / (acc + brk)
		}
		top = min(math.Sqrt(max(peak2, 0)), top)
		switch {
		case acc <= 0:
			da, db = 0, d
		case brk <= 0:
			da, db = d, 0
		default:
			da = min(max((top*top-v0*v0)/(2*acc), 0), d)
			db = d - da
		}
	}
	da, db = max(da, 0), max(db, 0)
	cruise := max(d-da-db, 0)
	var out []piece
	off := sg.o1
	add := func(dx, from, to float64) {
		if dx <= 0 {
			return
		}
		p := piece{step: sg.step, off: off, dx: dx, v0: from, v1: to}
		if from == to {
			p.dt = dx / from
		} else {
			p.dt = 2 * dx / (from + to)
			p.a = (to - from) / p.dt
		}
		out = append(out, p)
		off += dx
	}
	add(da, v0, top)
	add(cruise, top, top)
	add(db, top, v1)
	return out
}

// limits splits the way into segments, each at one limit: the lowest of the
// train's top speed and the limits of every track under it, from its rear to
// its front. A track's limit holds from when the front enters it until the
// rear has left it.
func limits(t *ops.Train, w Way, lengths []float64, from float64) []segment {
	length := float64(t.Length)
	type span struct {
		s, e, lim float64 // where the track starts and ends, on the front's scale
	}
	top := float64(t.MaxSpeed)
	limOf := func(st infra.Step) float64 { return math.Min(top, float64(st.Edge.Track.MaxSpeed)) }

	starts := make([]float64, len(lengths)) // where each step starts
	for i := 1; i < len(lengths); i++ {
		starts[i] = starts[i-1] + lengths[i-1]
	}
	var spans []span // the tracks behind, then the steps
	if length > 0 {
		e := 0.0
		for i := len(w.Behind) - 1; i >= 0; i-- {
			l := float64(w.Behind[i].Edge.Track.Length)
			if e+length > from {
				spans = append(spans, span{e - l, e, limOf(w.Behind[i])})
			}
			e -= l
		}
	}
	for i, st := range w.Steps {
		spans = append(spans, span{starts[i], starts[i] + lengths[i], limOf(st)})
	}

	var segs []segment
	for i := range lengths {
		o1, o2 := 0.0, lengths[i]
		if i == 0 {
			o1 = from
		}
		if o2 <= o1 {
			continue
		}
		cuts := []float64{o1}
		if length > 0 {
			for _, sp := range spans {
				if x := sp.e + length - starts[i]; x > o1 && x < o2 {
					cuts = insertSorted(cuts, x)
				}
			}
		}
		cuts = append(cuts, o2)
		for k := 0; k+1 < len(cuts); k++ {
			a, b := cuts[k], cuts[k+1]
			if b <= a {
				continue
			}
			lim := limOf(w.Steps[i])
			if length > 0 {
				mid := starts[i] + (a+b)/2
				for _, sp := range spans {
					if sp.s < mid && mid < sp.e+length {
						lim = math.Min(lim, sp.lim)
					}
				}
			}
			segs = append(segs, segment{step: i, o1: a, o2: b, lim: lim})
		}
	}
	return segs
}

// insertSorted adds x to the sorted xs, unless it is there already.
func insertSorted(xs []float64, x float64) []float64 {
	for i, y := range xs {
		if x == y {
			return xs
		}
		if x < y {
			return append(xs[:i], append([]float64{x}, xs[i:]...)...)
		}
	}
	return append(xs, x)
}
