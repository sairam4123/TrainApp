package kpi

import (
	"cmp"
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

type place struct {
	kind string
	stn  *infra.Station
	pf   string
}

// places tells every track's kind.
func places(w *infra.World) map[ids.TrackID]place {
	out := map[ids.TrackID]place{}
	for _, stn := range w.ListStations() {
		for _, trk := range w.TrackGraph.ListTracks() {
			if stn.InArea(trk.Id) {
				out[trk.Id] = place{kind: KindThroat, stn: stn}
			}
		}
		for _, pf := range stn.Platforms() {
			out[pf.Id] = place{kind: KindPlatform, stn: stn, pf: pf.PfNo}
		}
	}
	return out
}

// utilization works out the tracks' use from the occupations, with any
// still open closed at end. trains are the train numbers in the order they
// were added, which orders the tracks (lineOrder).
func (c *Collector) utilization(w *infra.World, end float64, trains []string) Utilization {
	u := Utilization{}
	if len(c.occs) > 0 {
		u.FromMin, u.ToMin = c.occs[0].enter, c.occs[0].enter
		for _, o := range c.occs {
			_, _, leave := o.span(end)
			u.FromMin = min(u.FromMin, o.enter)
			u.ToMin = max(u.ToMin, leave)
		}
	}
	window := u.ToMin - u.FromMin

	pl := places(w)
	byTrack := map[ids.TrackID]*TrackUse{}
	tracks := w.TrackGraph.ListTracks()
	for _, trk := range tracks {
		p := pl[trk.Id]
		tu := &TrackUse{Track: trk.String(), Kind: cmp.Or(p.kind, KindBlock), Pf: p.pf}
		if p.stn != nil {
			tu.Stn = p.stn.Code.String()
		}
		byTrack[trk.Id] = tu
	}
	for _, o := range c.occs {
		tu := byTrack[o.track.Id]
		enter, travelEnd, leave := o.span(end)
		tu.Entries++
		tu.RunningMin += travelEnd - enter
		tu.StandingMin += leave - travelEnd
	}

	rank := c.lineOrder(pl, trains)
	slices.SortStableFunc(tracks, func(a, b *infra.TrackSegment) int {
		return cmp.Or(cmp.Compare(rank(a.Id), rank(b.Id)), cmp.Compare(a.String(), b.String()))
	})
	stations := map[string]*StationUse{}
	for _, trk := range tracks {
		tu := byTrack[trk.Id]
		tu.OccupiedMin = tu.RunningMin + tu.StandingMin
		if window > 0 {
			tu.Occupancy, tu.Running, tu.Standing = tu.OccupiedMin/window, tu.RunningMin/window, tu.StandingMin/window
			tu.TrainsPerHour = float64(tu.Entries) / window * 60
		}
		u.Tracks = append(u.Tracks, *tu)
		if tu.Kind != KindPlatform {
			continue
		}
		su := stations[tu.Stn]
		if su == nil {
			su = &StationUse{Stn: tu.Stn}
			stations[tu.Stn] = su
			u.Stations = append(u.Stations, StationUse{Stn: tu.Stn}) // filled in below, in line order
		}
		su.Platforms++
		su.Occupancy += tu.Occupancy
		su.StandingMin += tu.StandingMin
	}
	for i, s := range u.Stations {
		su := stations[s.Stn]
		su.Occupancy /= float64(su.Platforms)
		u.Stations[i] = *su
	}
	u.Blocks = summarize(u.Tracks, KindBlock)
	u.Platforms = summarize(u.Tracks, KindPlatform)
	return u
}

// lineOrder ranks tracks in the order a train runs over them: the first
// train added of those that covered the most blocks, from where it entered.
// Tracks it never ran over rank last.
func (c *Collector) lineOrder(pl map[ids.TrackID]place, trains []string) func(ids.TrackID) int {
	blocks := map[string]map[ids.TrackID]bool{}
	for _, o := range c.occs {
		if _, inStn := pl[o.track.Id]; inStn {
			continue
		}
		if blocks[o.train] == nil {
			blocks[o.train] = map[ids.TrackID]bool{}
		}
		blocks[o.train][o.track.Id] = true
	}
	best := ""
	for _, t := range trains {
		if best == "" || len(blocks[t]) > len(blocks[best]) {
			best = t
		}
	}
	order := map[ids.TrackID]int{}
	for _, o := range c.occs {
		if _, seen := order[o.track.Id]; o.train == best && !seen {
			order[o.track.Id] = len(order)
		}
	}
	// a station's other tracks rank with the first of its tracks the train
	// ran over
	stnRank := map[*infra.Station]int{}
	for id, r := range order {
		if p, ok := pl[id]; ok {
			if old, ok := stnRank[p.stn]; !ok || r < old {
				stnRank[p.stn] = r
			}
		}
	}
	return func(id ids.TrackID) int {
		if r, ok := order[id]; ok {
			return r
		}
		if p, ok := pl[id]; ok {
			if r, ok := stnRank[p.stn]; ok {
				return r
			}
		}
		return len(order)
	}
}

func summarize(tracks []TrackUse, kind string) Summary {
	s := Summary{}
	for _, tu := range tracks {
		if tu.Kind != kind {
			continue
		}
		if s.Tracks == 0 {
			s.MinOccupancy, s.MaxOccupancy = tu.Occupancy, tu.Occupancy
		}
		s.Tracks++
		s.MeanOccupancy += tu.Occupancy
		s.MinOccupancy = min(s.MinOccupancy, tu.Occupancy)
		s.MaxOccupancy = max(s.MaxOccupancy, tu.Occupancy)
		s.MeanStanding += tu.Standing
		s.StandingMin += tu.StandingMin
	}
	if s.Tracks > 0 {
		s.MeanOccupancy /= float64(s.Tracks)
		s.MeanStanding /= float64(s.Tracks)
	}
	return s
}
