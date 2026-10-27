// Package compare lines up the KPIs of several runs against the first, the
// baseline: their whole-run figures, trains, stations, tracks, waits and
// causes, with each one's change (Δ) from the baseline. It draws nothing;
// trainviz compare, trainbench -against and traindemo's baseline all use it,
// so they agree on every figure.
//
// Runs can be on different worlds and run different schedules. What still
// compares fairly then is decided here: on different worlds only the
// whole-run figures and the stations the worlds share; with other trains,
// no totals.
package compare

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/vizproto"
)

// maxDiffs is how many differences SameWorld lists.
const maxDiffs = 5

// SameWorld lists how two worlds' layouts differ: points, tracks,
// switches, signals and stations, by id; the first few, empty if they are
// the same. Trains, names and labels don't count (a track's name, lanes,
// direction and siding name; a platform's class), so a recording made
// before a version added one still matches.
func SameWorld(a, b *vizproto.World) []string {
	var out []string
	out = diffByID(out, "point", a.Points, b.Points, func(p vizproto.Point) string { return p.ID })
	out = diffByID(out, "track", layoutTracks(a), layoutTracks(b), func(t vizproto.Track) string { return t.ID })
	out = diffByID(out, "switch", a.Switches, b.Switches, func(s vizproto.SwitchBlock) string { return s.ID })
	out = diffByID(out, "signal", a.Signals, b.Signals, func(s vizproto.Signal) string { return s.ID })
	return diffByID(out, "station", layoutStations(a), layoutStations(b), func(s vizproto.Station) string { return s.Code })
}

// layoutTracks are the world's tracks without their names and labels.
func layoutTracks(w *vizproto.World) []vizproto.Track {
	out := make([]vizproto.Track, len(w.Tracks))
	for i, t := range w.Tracks {
		out[i] = vizproto.Track{ID: t.ID, From: t.From, To: t.To, Kind: t.Kind, Length: t.Length}
	}
	return out
}

// layoutStations are the world's stations with only their platforms'
// numbers and tracks.
func layoutStations(w *vizproto.World) []vizproto.Station {
	out := make([]vizproto.Station, len(w.Stations))
	for i, s := range w.Stations {
		out[i] = vizproto.Station{Code: s.Code}
		for _, p := range s.Platforms {
			out[i].Platforms = append(out[i].Platforms, vizproto.Platform{No: p.No, Track: p.Track})
		}
	}
	return out
}

// diffByID adds to out how two lists' items differ, by id, compared as
// JSON: those only in one, and those that differ, A's first; up to
// maxDiffs in all.
func diffByID[T any](out []string, what string, a, b []T, id func(T) string) []string {
	var order []string
	key := func(xs []T) map[string]string {
		m := map[string]string{}
		for _, x := range xs {
			data, _ := json.Marshal(x)
			if _, ok := m[id(x)]; !ok {
				order = append(order, id(x))
			}
			m[id(x)] = string(data)
		}
		return m
	}
	as := key(a)
	bs := key(b)
	seen := map[string]bool{}
	for _, k := range order {
		if seen[k] || len(out) == maxDiffs {
			continue
		}
		seen[k] = true
		av, inA := as[k]
		bv, inB := bs[k]
		switch {
		case !inB:
			out = append(out, fmt.Sprintf("%s %s only in A", what, k))
		case !inA:
			out = append(out, fmt.Sprintf("%s %s only in B", what, k))
		case av != bv:
			out = append(out, fmt.Sprintf("%s %s differs", what, k))
		}
	}
	return out
}

// Worlds puts the worlds into groups of the same world, numbered from 1 in
// the order first seen: one number per world.
func Worlds(ws []*vizproto.World) []int {
	out := make([]int, len(ws))
	var firsts []int // the first world of each group
	for i, w := range ws {
		for g, f := range firsts {
			if len(SameWorld(ws[f], w)) == 0 {
				out[i] = g + 1
				break
			}
		}
		if out[i] == 0 {
			firsts = append(firsts, i)
			out[i] = len(firsts)
		}
	}
	return out
}

// SharedStations are the station codes every one of the worlds has, in the
// first's order.
func SharedStations(ws []*vizproto.World) []string {
	if len(ws) == 0 {
		return nil
	}
	var out []string
	for _, s := range ws[0].Stations {
		if !slices.ContainsFunc(ws[1:], func(w *vizproto.World) bool {
			return !slices.ContainsFunc(w.Stations, func(o vizproto.Station) bool { return o.Code == s.Code })
		}) {
			out = append(out, s.Code)
		}
	}
	return out
}

// Kind is how a run's trains compare with the baseline's.
type Kind int

const (
	SameSchedule  Kind = iota // the same trains, entries and calls
	Retimed                   // the same trains, some entering or calling otherwise
	OtherSchedule             // other trains
	OtherWorld                // on another world: other trains, whatever their numbers
)

func (k Kind) String() string {
	return [...]string{"same trains", "retimed", "other schedule", "other world"}[k]
}

// SameTrains is whether totals compare fairly: the runs have the same
// trains, if maybe at other times.
func (k Kind) SameTrains() bool { return k == SameSchedule || k == Retimed }

// Schedules is how run's trains compare with base's, from their KPIs alone:
// each train's number, planned entry, and the stations it calls at or
// passes.
func Schedules(base, run *report.Report) Kind {
	if len(base.PerTrain) != len(run.PerTrain) {
		return OtherSchedule
	}
	kind := SameSchedule
	theirs := trainsByNumber(run)
	for _, t := range base.PerTrain {
		o, ok := theirs[t.Train]
		if !ok {
			return OtherSchedule
		}
		if retimed(&t, o) {
			kind = Retimed
		}
	}
	return kind
}

// RetimedTrains counts the trains in both runs that run otherwise in run.
func RetimedTrains(base, run *report.Report) int {
	n := 0
	theirs := trainsByNumber(run)
	for _, t := range base.PerTrain {
		if o, ok := theirs[t.Train]; ok && retimed(&t, o) {
			n++
		}
	}
	return n
}

func trainsByNumber(r *report.Report) map[string]*report.Train {
	out := map[string]*report.Train{}
	for i := range r.PerTrain {
		out[r.PerTrain[i].Train] = &r.PerTrain[i]
	}
	return out
}

// retimed is whether a train enters or calls otherwise in two runs. Calls
// count only where both KPI files list them; older ones don't.
func retimed(a, b *report.Train) bool {
	if math.Abs(a.EntryMin-b.EntryMin) > 1e-6 {
		return true
	}
	if len(a.Calls) == 0 || len(b.Calls) == 0 {
		return false
	}
	return !slices.EqualFunc(a.Calls, b.Calls, func(x, y report.Call) bool { return x.Stn == y.Stn && x.Pass == y.Pass })
}

// Kinds is each run's Kind against the first, given each run's world group
// (Worlds).
func Kinds(reps []*report.Report, groups []int) []Kind {
	out := make([]Kind, len(reps))
	for i := 1; i < len(reps); i++ {
		if groups[i] != groups[0] {
			out[i] = OtherWorld
		} else {
			out[i] = Schedules(reps[0], reps[i])
		}
	}
	return out
}

// TrainRow is one train, by number, in each run: nil where a run has none.
// Retimed marks the runs it enters or calls otherwise in than the baseline.
type TrainRow struct {
	Train   string
	Runs    []*report.Train
	Retimed []bool
}

// Trains matches the runs' trains by number: the baseline's in its order,
// then those only in the others.
func Trains(reps []*report.Report) []TrainRow {
	var out []TrainRow
	at := map[string]int{}
	for i, r := range reps {
		for j := range r.PerTrain {
			t := &r.PerTrain[j]
			k, ok := at[t.Train]
			if !ok {
				k = len(out)
				at[t.Train] = k
				out = append(out, TrainRow{Train: t.Train, Runs: make([]*report.Train, len(reps)), Retimed: make([]bool, len(reps))})
			}
			out[k].Runs[i] = t
		}
	}
	for k := range out {
		row := &out[k]
		for i := 1; i < len(reps); i++ {
			if row.Runs[0] != nil && row.Runs[i] != nil {
				row.Retimed[i] = retimed(row.Runs[0], row.Runs[i])
			}
		}
	}
	return out
}

// MaxDelta is the largest change from vs[0] among the others, by size,
// with its sign; 0 if none has a value. ok[i] says vs[i] has one.
func MaxDelta(vs []float64, ok []bool) float64 {
	best := 0.0
	for i := 1; i < len(vs); i++ {
		if ok[0] && ok[i] {
			if d := vs[i] - vs[0]; math.Abs(d) > math.Abs(best) {
				best = d
			}
		}
	}
	return best
}

// StationRow is one station in each run: how late the trains were there
// and how busy its platforms were; nil where a run has no figures for it.
type StationRow struct {
	Stn   string
	Delay []*report.StationDelay
	Use   []*report.StationUse
}

// Stations lines up the stations in codes, nil for every station, in the
// baseline's line order.
func Stations(reps []*report.Report, codes []string) []StationRow {
	keep := func(code string) bool { return codes == nil || slices.Contains(codes, code) }
	var out []StationRow
	at := map[string]int{}
	row := func(code string) *StationRow {
		k, ok := at[code]
		if !ok {
			k = len(out)
			at[code] = k
			out = append(out, StationRow{Stn: code, Delay: make([]*report.StationDelay, len(reps)), Use: make([]*report.StationUse, len(reps))})
		}
		return &out[k]
	}
	for i, r := range reps {
		for j := range r.StationDelays {
			if sd := &r.StationDelays[j]; keep(sd.Stn) {
				row(sd.Stn).Delay[i] = sd
			}
		}
		for j := range r.Utilization.Stations {
			if su := &r.Utilization.Stations[j]; keep(su.Stn) {
				row(su.Stn).Use[i] = su
			}
		}
	}
	return out
}

// TrackRow is one track in each run; nil where a run has no figures for it.
type TrackRow struct {
	Track, Kind, Stn, Pf string
	Uses                 []*report.TrackUse
}

// Tracks lines up the runs' tracks, in the baseline's line order. Only
// runs on one world have the same tracks.
func Tracks(reps []*report.Report) []TrackRow {
	var out []TrackRow
	at := map[string]int{}
	for i, r := range reps {
		for j := range r.Utilization.Tracks {
			tu := &r.Utilization.Tracks[j]
			k, ok := at[tu.Track]
			if !ok {
				k = len(out)
				at[tu.Track] = k
				out = append(out, TrackRow{Track: tu.Track, Kind: tu.Kind, Stn: tu.Stn, Pf: tu.Pf, Uses: make([]*report.TrackUse, len(reps))})
			}
			out[k].Uses[i] = tu
		}
	}
	return out
}

// WaitPlace is the waits at one place, of one kind, in each run: how many,
// by how many trains, and how many minutes.
type WaitPlace struct {
	At, Kind string
	Waits    []int
	Trains   []int
	Min      []float64
}

// WaitPlaces groups each run's waits by place and kind, the baseline's most
// minutes first.
func WaitPlaces(reps []*report.Report) []*WaitPlace {
	by := map[[2]string]*WaitPlace{}
	var out []*WaitPlace
	for i, r := range reps {
		for _, t := range r.PerTrain {
			seen := map[*WaitPlace]bool{}
			for _, w := range t.Waits {
				k := [2]string{w.At, w.Kind}
				p := by[k]
				if p == nil {
					p = &WaitPlace{At: w.At, Kind: w.Kind, Waits: make([]int, len(reps)), Trains: make([]int, len(reps)), Min: make([]float64, len(reps))}
					by[k] = p
					out = append(out, p)
				}
				p.Waits[i]++
				p.Min[i] += w.Min
				if !seen[p] {
					seen[p] = true
					p.Trains[i]++
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b *WaitPlace) int { return cmp.Compare(b.Min[0], a.Min[0]) })
	return out
}

// CauseRow is the minutes of delay one cause, of one kind, cost each run,
// or that were lost at one place.
type CauseRow struct {
	Cause, Kind string // by cause
	Where, At   string // by place
	Min         []float64
}

// Causes sums each run's losses by cause and kind, in the order the causes
// come in.
func Causes(reps []*report.Report) []CauseRow {
	return sumLosses(reps, func(l report.Loss) CauseRow { return CauseRow{Cause: l.Cause, Kind: l.Kind} })
}

// CausesByPlace sums each run's losses by place: a station, a section or an
// entry. Only runs on one world have the same places.
func CausesByPlace(reps []*report.Report) []CauseRow {
	return sumLosses(reps, func(l report.Loss) CauseRow { return CauseRow{Where: l.Where, At: l.At} })
}

func sumLosses(reps []*report.Report, key func(report.Loss) CauseRow) []CauseRow {
	var out []CauseRow
	at := map[[4]string]int{}
	for i, r := range reps {
		for _, t := range r.PerTrain {
			for _, l := range t.Losses {
				row := key(l)
				k := [4]string{row.Cause, row.Kind, row.Where, row.At}
				j, ok := at[k]
				if !ok {
					j = len(out)
					at[k] = j
					row.Min = make([]float64, len(reps))
					out = append(out, row)
				}
				out[j].Min[i] += l.Min
			}
		}
	}
	return out
}
