package compare

import (
	"fmt"
	"math"
	"slices"
	"trainapp/railway/kpi/report"
)

// Better is which way a figure is better: it decides how a Δ is coloured.
type Better int

const (
	Neither Better = iota // e.g. occupancy: busier is neither
	Lower
	Higher
)

// Unit is how a figure is written.
type Unit int

const (
	Minutes  Unit = iota // "12.3"
	Share                // 0 to 1, as "75%"; a Δ in points
	Count                // "4"
	PerTrain             // "0.25"
	Seconds              // wall time, "1.2s"
)

// Value is one run's figure.
type Value struct {
	V  float64
	OK bool // the run has the figure
	// Fair is whether it compares fairly with the baseline's: a total does
	// only over the same trains. An unfair value is shown dim, with no Δ.
	Fair bool
	// Text, if set, is how it is written, e.g. "6/6" or "0451U 12.3".
	Text string
}

// Figure is one whole-run figure in each run.
type Figure struct {
	Key, Name string
	// Group is the figure box it goes in: trains, delay, lateness,
	// disruption, dispatching, line, or run (wall time).
	Group  string
	Unit   Unit
	Better Better
	Values []Value
}

// Delta is run i's figure less the baseline's, if both have it and it
// compares fairly.
func (f Figure) Delta(i int) (float64, bool) {
	if i == 0 || !f.Values[0].OK || !f.Values[i].OK || !f.Values[i].Fair {
		return 0, false
	}
	return f.Values[i].V - f.Values[0].V, true
}

// Text writes run i's figure; "—" if it has none.
func (f Figure) Text(i int) string {
	v := f.Values[i]
	switch {
	case !v.OK:
		return "—"
	case v.Text != "":
		return v.Text
	}
	switch f.Unit {
	case Share:
		return fmt.Sprintf("%.0f%%", 100*v.V)
	case Count:
		return fmt.Sprintf("%.0f", v.V)
	case PerTrain:
		return fmt.Sprintf("%.2f", v.V)
	case Seconds:
		return fmt.Sprintf("%.1fs", v.V)
	}
	return fmt.Sprintf("%.1f", v.V)
}

// DeltaText writes run i's Δ, signed; "—" if it has none, and "0" if it is
// too small to show.
func (f Figure) DeltaText(i int) string {
	d, ok := f.Delta(i)
	switch {
	case !ok:
		return "—"
	case math.Abs(d) < f.eps():
		return "0"
	}
	switch f.Unit {
	case Share:
		return fmt.Sprintf("%+.0f pts", 100*d)
	case Count:
		return fmt.Sprintf("%+.0f", d)
	case PerTrain:
		return fmt.Sprintf("%+.2f", d)
	case Seconds:
		return fmt.Sprintf("%+.1fs", d)
	}
	return fmt.Sprintf("%+.1f", d)
}

// eps is the smallest change worth showing.
func (f Figure) eps() float64 {
	switch f.Unit {
	case Share:
		return 0.005
	case Count:
		return 0.5
	case PerTrain:
		return 0.005
	}
	return 0.05
}

// Verdict is whether run i did better than the baseline (1), worse (-1),
// or neither (0): no Δ, too small a one, or a figure with no better way.
func (f Figure) Verdict(i int) int {
	d, ok := f.Delta(i)
	if !ok || f.Better == Neither || math.Abs(d) < f.eps() {
		return 0
	}
	if (d < 0) == (f.Better == Lower) {
		return 1
	}
	return -1
}

// Figures lines up the runs' whole-run figures; reps[0] is the baseline and
// kinds[i] how run i's trains compare with it (Kinds).
func Figures(reps []*report.Report, kinds []Kind) []Figure {
	var out []Figure
	// add adds a figure; a total is fair only over the same trains.
	add := func(key, name, group string, unit Unit, better Better, total bool, get func(r *report.Report) Value) {
		f := Figure{Key: key, Name: name, Group: group, Unit: unit, Better: better}
		for i, r := range reps {
			v := Value{}
			if r != nil {
				v = get(r)
			}
			v.Fair = i == 0 || !total || kinds[i].SameTrains()
			f.Values = append(f.Values, v)
		}
		out = append(out, f)
	}
	val := func(v float64) Value { return Value{V: v, OK: true} }
	perTrain := func(r *report.Report, v float64) Value {
		if r.Trains == 0 {
			return Value{}
		}
		return val(v / float64(r.Trains))
	}

	add("finished", "finished", "trains", Share, Higher, false, func(r *report.Report) Value {
		if r.Trains == 0 {
			return Value{}
		}
		return Value{V: float64(r.Finished) / float64(r.Trains), OK: true, Text: fmt.Sprintf("%d/%d", r.Finished, r.Trains)}
	})

	delay := func(get func(*report.Stats) Value) func(*report.Report) Value {
		return func(r *report.Report) Value {
			if r.Delay == nil {
				return Value{}
			}
			return get(r.Delay)
		}
	}
	add("delay_mean", "mean delay", "delay", Minutes, Lower, false, delay(func(s *report.Stats) Value { return val(s.MeanMin) }))
	add("delay_weighted", "weighted mean delay", "delay", Minutes, Lower, false, delay(func(s *report.Stats) Value { return val(s.WeightedMeanMin) }))
	for _, w := range thresholds(reps, func(r *report.Report) *report.Stats { return r.Delay }) {
		add(fmt.Sprintf("on_time_%g", w), fmt.Sprintf("on time ≤%g min", w), "delay", Share, Higher, false, delay(func(s *report.Stats) Value { return punctual(s, w) }))
	}
	add("delay_worst", "worst delay", "delay", Minutes, Lower, false, delay(func(s *report.Stats) Value {
		v := val(s.WorstMin)
		if s.Worst != "" {
			v.Text = fmt.Sprintf("%.1f %s", s.WorstMin, s.Worst)
		}
		return v
	}))
	add("delay_total", "total delay", "delay", Minutes, Lower, true, delay(func(s *report.Stats) Value { return val(s.TotalMin) }))

	late := func(get func(*report.Stats) Value) func(*report.Report) Value {
		return func(r *report.Report) Value {
			if r.Lateness == nil {
				return Value{}
			}
			return get(r.Lateness)
		}
	}
	if slices.ContainsFunc(reps, func(r *report.Report) bool { return r != nil && r.Lateness != nil }) {
		add("late_mean", "mean lateness", "lateness", Minutes, Lower, false, late(func(s *report.Stats) Value { return val(s.MeanMin) }))
		for _, w := range thresholds(reps, func(r *report.Report) *report.Stats { return r.Lateness }) {
			add(fmt.Sprintf("late_on_time_%g", w), fmt.Sprintf("on timetable ≤%g min", w), "lateness", Share, Higher, false, late(func(s *report.Stats) Value { return punctual(s, w) }))
		}
	}

	if slices.ContainsFunc(reps, func(r *report.Report) bool { return r != nil && r.Disruption != nil }) {
		dis := func(get func(*report.Split) Value) func(*report.Report) Value {
			return func(r *report.Report) Value {
				if r.Disruption == nil {
					return Value{}
				}
				return get(r.Disruption)
			}
		}
		add("primary", "primary", "disruption", Minutes, Lower, false, dis(func(s *report.Split) Value { return val(s.PrimaryMin) }))
		add("reactionary", "reactionary", "disruption", Minutes, Lower, false, dis(func(s *report.Split) Value { return val(s.ReactionaryMin) }))
		add("reactionary_share", "reactionary share", "disruption", Share, Lower, false, dis(func(s *report.Split) Value {
			if all := s.PrimaryMin + s.ReactionaryMin; all > 0 {
				return val(s.ReactionaryMin / all)
			}
			return Value{}
		}))
	}

	add("wait_per_train", "waits per train", "dispatching", Minutes, Lower, false, func(r *report.Report) Value { return perTrain(r, r.WaitMin) })
	add("wait_total", "waits", "dispatching", Minutes, Lower, true, func(r *report.Report) Value { return val(r.WaitMin) })
	add("longest_wait", "longest wait", "dispatching", Minutes, Lower, false, func(r *report.Report) Value {
		if r.LongestWait == nil {
			return val(0)
		}
		w := r.LongestWait
		return Value{V: w.Min, OK: true, Text: fmt.Sprintf("%.1f %s@%s", w.Min, w.Train, w.At)}
	})
	add("holds_per_train", "holds per train", "dispatching", PerTrain, Neither, false, func(r *report.Report) Value { return perTrain(r, float64(r.Holds)) })
	add("holds", "holds", "dispatching", Count, Neither, true, func(r *report.Report) Value { return val(float64(r.Holds)) })
	add("held_min", "held", "dispatching", Minutes, Neither, true, func(r *report.Report) Value { return val(r.HeldMin) })
	add("platform_changes", "platform changes", "dispatching", Count, Neither, true, func(r *report.Report) Value { return val(float64(r.PlatformChanges)) })

	add("block_occ_mean", "block occupancy", "line", Share, Neither, false, func(r *report.Report) Value { return val(r.Utilization.Blocks.MeanOccupancy) })
	add("block_occ_max", "busiest block", "line", Share, Neither, false, func(r *report.Report) Value { return val(r.Utilization.Blocks.MaxOccupancy) })
	add("standing", "standing in section", "line", Minutes, Lower, false, func(r *report.Report) Value { return val(r.Utilization.Blocks.StandingMin) })
	add("platform_occ_mean", "platform occupancy", "line", Share, Neither, false, func(r *report.Report) Value { return val(r.Utilization.Platforms.MeanOccupancy) })
	return out
}

// thresholds are the punctuality thresholds of any run, lowest first.
func thresholds(reps []*report.Report, of func(*report.Report) *report.Stats) []float64 {
	var out []float64
	for _, r := range reps {
		if r == nil || of(r) == nil {
			continue
		}
		for _, p := range of(r).Punctual {
			if !slices.Contains(out, p.WithinMin) {
				out = append(out, p.WithinMin)
			}
		}
	}
	slices.Sort(out)
	return out
}

// punctual is the share on time within w, if s has that threshold.
func punctual(s *report.Stats, w float64) Value {
	for _, p := range s.Punctual {
		if p.WithinMin == w {
			return Value{V: p.Share, OK: true}
		}
	}
	return Value{}
}

// Find is the figure with the key, if there is one.
func Find(figs []Figure, key string) (Figure, bool) {
	i := slices.IndexFunc(figs, func(f Figure) bool { return f.Key == key })
	if i < 0 {
		return Figure{}, false
	}
	return figs[i], true
}
