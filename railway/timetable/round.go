package timetable

import (
	"math"
	"slices"
	"trainapp/railway/sim"
)

// Round turns raw run times into timetable times, train by train along its
// points. Running times get the recovery allowance; then arrivals and
// departures are rounded up to the step, so a train is never timed to leave
// before it could, and never stands less than its minimum dwell. The first
// arrival is the train's entry and is kept as it is.
//
// Each running time starts from when the train can really be on its way: the
// rounded departure at a stop or halt, where a timed train waits for it, but
// the unrounded time at a pass, which it runs through whenever it gets there.
// So rounding adds up only at stops, not at every station passed.
//
// Rounding one train must not undo how it meets another. A train that stood
// at a station for another (see meeting) waits for it there, so its departure
// moves on by as much as the other's time there did: its arrival for one from
// the other way, its departure for one going the same way (an overtake, or a
// train it follows out). For one it follows that was gone before it got there,
// it is the other's arrival at the next station: the line to it is what the
// train waited for. Moving it can move the trains that wait for it in turn, so the
// trains are rounded again until no departure moves.
func Round(tt Timetable, cfg Config) Timetable {
	notBefore := map[[2]int]float64{} // train, point -> earliest departure, for its meeting
	var out Timetable
	var at [][][2]float64 // train, point -> when it really arrives and leaves
	for range len(tt.Trains) + 1 {
		out, at = roundOnce(tt, cfg, notBefore)
		moved := false
		for i, tr := range tt.Trains {
			for k, p := range tr.Points {
				// tt's times are raw, but the meeting must be the one
				// PlanOverrides finds in the rounded times
				j, kj, arrival, ok := meeting(tt, i, k, cfg.RoundingMin)
				if !ok {
					continue
				}
				// how much later the other train's time there now is
				other := tt.Trains[j].Points[kj]
				shift := at[j][kj][1] - other.Dep
				if arrival {
					shift = at[j][kj][0] - other.Arr
				}
				if need := p.Dep + shift; need > notBefore[[2]int{i, k}]+1e-9 {
					notBefore[[2]int{i, k}] = need
					moved = true
				}
			}
		}
		if !moved {
			break
		}
	}
	return out
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
				p.Dep = up(max(raw.Dep, p.Arr+p.MinDwell, notBefore[[2]int{i, k}]))
				gone = p.Dep
			}
			at[i][k] = [2]float64{arr, gone}
		}
		tr.fill()
		out.Trains = append(out.Trains, tr)
	}
	return out, at
}

// meeting finds the train that train i stood at its point k for, when they
// met at that station: one from the other way that got there while it stood
// (arrival is true: it waits for its arrival), or one going the same way that
// left while it stood (it waits for its departure). It returns the other
// train's index and its point at the station. A train going the same way that
// left before it got there is one it follows: it stood until that train was
// clear of the line ahead, so it waits for that train's arrival at its next
// point, which is returned instead. step is the rounding step the times may
// be off by: 0 for raw times.
func meeting(tt Timetable, i, k int, step float64) (j, kj int, arrival, ok bool) {
	tr := tt.Trains[i]
	p := tr.Points[k]
	h := p.Halt
	if h == nil || h.For == "" || h.For == tr.Number || !p.Reached() || p.Dep < 0 {
		return 0, 0, false, false
	}
	j = slices.IndexFunc(tt.Trains, func(o Train) bool { return o.Number == h.For })
	if j < 0 {
		return 0, 0, false, false
	}
	kj = slices.IndexFunc(tt.Trains[j].Points, func(o Point) bool { return o.Stn == p.Stn })
	if kj < 0 {
		return 0, 0, false, false
	}
	pts := tt.Trains[j].Points
	other := pts[kj]
	if !other.Reached() || other.Dep < 0 {
		return 0, 0, false, false
	}
	// which way the other runs, not the halt's reason: a hold for a conflict
	// at the station's points may be called a crossing between trains running
	// the same way
	arrival = runOpposite(tr.Points, k, pts, kj)
	at := other.Dep
	if arrival {
		at = other.Arr
	}
	// only a meeting at this station: the train may have been let go by one
	// elsewhere on the line. Arrivals are taken at the far end of the
	// platform, a little after the other train is clear of the line, so the
	// times may be a run along it apart, and a rounding step on top
	slack := step + meetSlackMin
	near := func(t float64) bool { return t >= p.Arr-slack && t <= p.Dep+slack }
	if near(at) {
		return j, kj, arrival, true
	}
	// one it follows out, gone before it got there
	if arrival || other.Dep > p.Arr || kj+1 >= len(pts) || k+1 >= len(tr.Points) || pts[kj+1].Stn != tr.Points[k+1].Stn {
		return 0, 0, false, false
	}
	if next := pts[kj+1]; next.Reached() && near(next.Arr) {
		return j, kj + 1, true, true
	}
	return 0, 0, false, false
}

// runOpposite reports whether the two trains run through the station, a's k-th
// point and b's j-th, in opposite directions, by the stations either side.
func runOpposite(a []Point, k int, b []Point, j int) bool {
	stn := func(ps []Point, i int) string {
		if i < 0 || i >= len(ps) {
			return ""
		}
		return ps[i].Stn
	}
	aPrev, aNext, bPrev, bNext := stn(a, k-1), stn(a, k+1), stn(b, j-1), stn(b, j+1)
	return (aPrev != "" && aPrev == bNext) || (aNext != "" && aNext == bPrev)
}

// meetSlackMin is how far outside a train's stand the other train's time may
// be and still count as meeting it there: about a run along a platform.
const meetSlackMin = 1.0

// holdUntil is the hold override that keeps a meeting: until the other train
// has arrived, or until it has left.
func holdUntil(arrival bool) string {
	if arrival {
		return sim.HoldUntilArrived
	}
	return sim.HoldUntilLeft
}
