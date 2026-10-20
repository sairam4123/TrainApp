package kpi

import (
	"slices"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
)

// The delay at each call, station and block. A train's delay at a point is
// how much later than running free it got there; what it gained on the way
// there and standing there add up, call to call, to its delay on leaving
// the world. A block's delay is how much longer the trains took over it
// than running free.

// callsOf works out the train's calls against its free running, which may
// be nil.
func callsOf(t *ops.Train, free *FreeRun) []Call {
	sched := t.Schedule()
	out := make([]Call, len(sched))
	var leftDelay *float64 // the delay leaving the point before
	timed := isTimed(t)
	for k, sp := range sched {
		c := Call{Stn: sp.StnCode.String(), Pass: sp.IsPass(), ArrMin: sp.ActArrTime, DepMin: sp.ActDeptTime}
		planned := sp.ArrTime
		if k == 0 {
			planned = sp.DeptTime
		}
		if planned >= 0 && timed {
			at := sp.ActArrTime
			if k == 0 {
				at = sp.ActDeptTime
			}
			if at >= 0 {
				c.LateMin = ptr(max(at-planned, 0))
			}
		}
		if free != nil && k < len(free.Calls) {
			fc := free.Calls[k]
			var arrDelay, depDelay *float64
			if sp.ActArrTime >= 0 && fc.ArrMin >= 0 {
				arrDelay = ptr(sp.ActArrTime - fc.ArrMin)
				if k == 0 {
					c.ApproachMin = arrDelay // getting onto the line
				} else if leftDelay != nil {
					c.ApproachMin = ptr(*arrDelay - *leftDelay)
				}
			}
			if sp.ActDeptTime >= 0 && fc.DepMin >= 0 {
				depDelay = ptr(sp.ActDeptTime - fc.DepMin)
				if arrDelay != nil {
					c.StationMin = ptr(*depDelay - *arrDelay)
				}
			}
			c.DelayMin = arrDelay
			if k == 0 {
				c.DelayMin = depDelay
			}
			leftDelay = depDelay
		}
		out[k] = c
	}
	return out
}

// isTimed is whether the timetable gives the train a time anywhere past its
// entry, as lateness takes it.
func isTimed(t *ops.Train) bool {
	for k, sp := range t.Schedule() {
		if k > 0 && sp.ArrTime >= 0 {
			return true
		}
	}
	return false
}

// stationDelays sums up the calls of the trains at each station, in the
// order of order (the line's) and then as they come.
func stationDelays(trains []Train, order []StationUse, cfg Config) []StationDelay {
	byStn := map[string]*StationDelay{}
	var stns []string
	for _, su := range order {
		stns = append(stns, su.Stn)
	}
	var delays, lates = map[string][]float64{}, map[string][]float64{}
	for _, t := range trains {
		for _, c := range t.Calls {
			sd := byStn[c.Stn]
			if sd == nil {
				sd = &StationDelay{Stn: c.Stn}
				byStn[c.Stn] = sd
				if !slices.Contains(stns, c.Stn) {
					stns = append(stns, c.Stn)
				}
			}
			if c.LateMin != nil {
				lates[c.Stn] = append(lates[c.Stn], *c.LateMin)
			}
			if c.DelayMin == nil {
				continue
			}
			sd.Calls++
			if c.Pass {
				sd.Passes++
			} else {
				sd.Stops++
			}
			d := *c.DelayMin
			delays[c.Stn] = append(delays[c.Stn], d)
			sd.MeanMin += d
			if d > sd.WorstMin {
				sd.Worst, sd.WorstMin = t.Train, d
			}
			if c.ApproachMin != nil {
				sd.ApproachMin += *c.ApproachMin
			}
			if c.StationMin != nil {
				sd.StationMin += *c.StationMin
			}
		}
	}
	var out []StationDelay
	for _, stn := range stns {
		sd := byStn[stn]
		if sd == nil {
			continue
		}
		if sd.Calls > 0 {
			sd.MeanMin /= float64(sd.Calls)
		}
		sd.Punctual = punctuality(delays[stn], cfg)
		if l := lates[stn]; len(l) > 0 {
			sd.LateCalls, sd.LatePunctual = len(l), punctuality(l, cfg)
		}
		out = append(out, *sd)
	}
	return out
}

// punctuality is the share of the delays no later than each threshold.
func punctuality(delays []float64, cfg Config) []Punctuality {
	var out []Punctuality
	for _, within := range slices.Sorted(slices.Values(cfg.PunctualWithinMin)) {
		p := Punctuality{WithinMin: within}
		for _, d := range delays {
			if d <= within+1e-9 {
				p.Trains++
			}
		}
		if len(delays) > 0 {
			p.Share = float64(p.Trains) / float64(len(delays))
		}
		out = append(out, p)
	}
	return out
}

// blockDelays sums up, for each block, how much longer each train took over
// it than running free, in the order of tracks (the line's). Only the time
// on a block the free run took too is counted: a train sent another way has
// nothing to compare with.
func blockDelays(w *infra.World, trains []*ops.Train, free map[string]FreeRun, tracks []TrackUse) []BlockDelay {
	name := map[string]string{} // track id to display name
	for _, trk := range w.TrackGraph.ListTracks() {
		name[trk.Id.String()] = trk.String()
	}
	byTrack := map[string]*BlockDelay{}
	counted := map[[2]string]bool{}
	for _, t := range trains {
		fr, ok := free[t.Number]
		if !ok {
			continue
		}
		for k, sp := range t.Schedule() {
			if k >= len(fr.Calls) {
				break
			}
			for _, v := range sp.Via {
				freeMin, ok := fr.Calls[k].Via[v.Track]
				if v.Out < 0 || !ok {
					continue
				}
				trk := name[v.Track]
				bd := byTrack[trk]
				if bd == nil {
					bd = &BlockDelay{Track: trk}
					byTrack[trk] = bd
				}
				if !counted[[2]string{trk, t.Number}] {
					counted[[2]string{trk, t.Number}] = true
					bd.Trains++
				}
				gained := (v.Out - v.In) - freeMin
				bd.GainedMin += gained
				if gained > bd.WorstMin {
					bd.Worst, bd.WorstMin = t.Number, gained
				}
			}
		}
	}
	var out []BlockDelay
	for _, tu := range tracks {
		if bd := byTrack[tu.Track]; bd != nil && tu.Kind == KindBlock {
			out = append(out, *bd)
		}
	}
	return out
}
