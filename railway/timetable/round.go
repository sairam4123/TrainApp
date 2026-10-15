package timetable

import (
	"math"
	"slices"
)

// Round turns raw run times into timetable times, train by train along its
// points. Running times get the recovery allowance; then arrivals and
// departures are rounded up to the step, so a train is never timed to leave
// before it could, and never stands less than its minimum dwell. The first
// arrival is the train's entry and is kept as it is.
func Round(tt Timetable, cfg Config) Timetable {
	out := Timetable{RoundingMin: cfg.RoundingMin, Check: tt.Check}
	recovery := 1 + cfg.RecoveryPct/100
	up := func(x float64) float64 {
		// tolerate float noise just over a step
		v := math.Ceil(x/cfg.RoundingMin-1e-9) * cfg.RoundingMin
		return math.Round(v*1e6) / 1e6
	}
	for _, src := range tt.Trains {
		tr := src
		tr.Points = slices.Clone(src.Points)
		for i := range tr.Points {
			p, raw := &tr.Points[i], src.Points[i]
			if !raw.Reached() {
				break
			}
			if i > 0 {
				prev := tr.Points[i-1]
				if prev.Dep < 0 {
					break
				}
				run := raw.Arr - src.Points[i-1].Dep
				p.Arr = up(max(raw.Arr, prev.Dep+run*recovery))
			}
			switch {
			case raw.Dep < 0:
			case p.Kind == KindPass:
				p.Dep = p.Arr
			default:
				p.Dep = up(max(raw.Dep, p.Arr+p.MinDwell))
			}
		}
		tr.fill()
		out.Trains = append(out.Trains, tr)
	}
	return out
}
