package infra

import (
	"maps"
	"math"
	"slices"
	"trainapp/railway/ids"
)

// Station tracks.
//
// A station's area is the tracks whose two end points both carry the
// station's code, plus its platform tracks. A running line enters the area at
// an entry point. For each running line, the main line is the platform track
// the line connects to directly: the one reached over the way whose slowest
// track is fastest, then the quickest such way, then the lowest platform
// number. In practice that is the track reached over straight switch legs
// only. A platform track whose platform length is 0 is a through track: it can
// be timetabled, but nobody boards there. Every other platform track is a loop.

// TrackClass is what a station's platform track is for.
type TrackClass int

const (
	Loop TrackClass = iota
	Main
	Through
)

func (c TrackClass) String() string {
	switch c {
	case Main:
		return "main"
	case Through:
		return "through"
	}
	return "loop"
}

// RunsThrough reports whether a train can run through the station: some
// platform track has neither end at a dead end. A terminus cannot be run
// through. Requires Finalize.
func (stn *Station) RunsThrough() bool {
	return stn.runsThrough
}

// IsThrough reports whether the platform is a through track: a track with no
// platform to board from.
func (pf *Platform) IsThrough() bool {
	return pf.Length == 0
}

// InArea reports whether the track is in the station's area. Requires
// Finalize.
func (stn *Station) InArea(track ids.TrackID) bool {
	_, ok := stn.area[track]
	return ok
}

// TrackClass is the class of one of the station's platform tracks; Loop for
// any other track. Requires Finalize.
func (stn *Station) TrackClass(track ids.TrackID) TrackClass {
	return stn.class[track]
}

// MainFor returns the main line for the running line that enters the station
// over line. Requires Finalize.
func (stn *Station) MainFor(line ids.TrackID) (*Platform, bool) {
	pf, ok := stn.mainFor[line]
	return pf, ok
}

// MainRoute returns the tracks of the way from the running line line onto
// its main line, ending with the main line itself. Requires Finalize.
func (stn *Station) MainRoute(line ids.TrackID) []ids.TrackID {
	return stn.mainRoute[line]
}

// Lines returns the running lines that enter the station, sorted. Requires
// Finalize.
func (stn *Station) Lines() []ids.TrackID {
	return slices.SortedFunc(maps.Keys(stn.mainFor), ids.TrackID.Compare)
}

// MainLinesOf returns the running lines the platform track is the main line
// for, sorted. Requires Finalize.
func (stn *Station) MainLinesOf(track ids.TrackID) []ids.TrackID {
	var out []ids.TrackID
	for line, pf := range stn.mainFor {
		if pf.Id == track {
			out = append(out, line)
		}
	}
	slices.SortFunc(out, ids.TrackID.Compare)
	return out
}

// buildStationTracks works out every station's area, main lines and track
// classes.
func (w *World) buildStationTracks() {
	tracks := w.TrackGraph.ListTracks()
	for _, stn := range w.ListStations() {
		stn.area = map[ids.TrackID]struct{}{}
		stn.mainFor = map[ids.TrackID]*Platform{}
		stn.class = map[ids.TrackID]TrackClass{}
		for _, trk := range tracks {
			e := w.TrackGraph.Edges[trk.Id]
			if (e.From.Id.StnCode == stn.Code && e.To.Id.StnCode == stn.Code) || stn.IsPlatform(trk.Id) {
				stn.area[trk.Id] = struct{}{}
			}
		}

		// entry points, in a fixed order
		var entries []Position
		seen := map[ids.PointID]struct{}{}
		for _, trk := range slices.SortedFunc(maps.Keys(stn.area), ids.TrackID.Compare) {
			e := w.TrackGraph.Edges[trk]
			for _, pt := range []*TrackPoint{e.From, e.To} {
				if _, ok := seen[pt.Id]; ok {
					continue
				}
				seen[pt.Id] = struct{}{}
				for _, line := range w.TrackGraph.adjacency[pt.Id] {
					if !stn.InArea(line.Track.Id) {
						entries = append(entries, Position{At: pt, Via: line})
					}
				}
			}
		}
		stn.mainRoute = map[ids.TrackID][]ids.TrackID{}
		for _, entry := range entries {
			if pf, route := w.mainLineFrom(stn, entry); pf != nil {
				stn.mainFor[entry.Via.Track.Id] = pf
				stn.mainRoute[entry.Via.Track.Id] = route
			}
		}

		stn.runsThrough = false
		for _, pf := range stn.Platforms() {
			e := w.TrackGraph.Edges[pf.Id]
			if !e.From.IsDeadEnd && !e.To.IsDeadEnd {
				stn.runsThrough = true
			}
			switch {
			case pf.IsThrough():
				stn.class[pf.Id] = Through
			case len(stn.MainLinesOf(pf.Id)) > 0:
				stn.class[pf.Id] = Main
			default:
				stn.class[pf.Id] = Loop
			}
		}
	}
}

// mainLineFrom finds the platform track a train arriving at entry reaches over
// the way whose slowest track is fastest, then the quickest way, then the
// lowest platform number, and that way. It stays inside the station's area.
func (w *World) mainLineFrom(stn *Station, entry Position) (*Platform, []ids.TrackID) {
	var speeds []float64
	for trk := range stn.area {
		t, _ := w.TrackGraph.GetTrack(trk)
		speeds = append(speeds, float64(t.MaxSpeed))
	}
	slices.Sort(speeds)
	speeds = slices.Compact(speeds)
	slices.Reverse(speeds)

	pfs := stn.Platforms()
	for _, floor := range speeds {
		times, routes := w.quickestToPlatforms(stn, entry, floor)
		var best *Platform
		bestTime := math.Inf(1)
		for _, pf := range pfs { // in platform order, so ties go to the lower number
			if t, ok := times[pf.Id]; ok && t < bestTime {
				best, bestTime = pf, t
			}
		}
		if best != nil {
			return best, routes[best.Id]
		}
	}
	return nil, nil
}

// quickestToPlatforms is the quickest time from entry to each platform track
// of the station, and the tracks of that way (ending with the platform
// track), using only tracks in the station's area no slower than floor. It
// does not run through one platform to reach another. Times are at the track
// speeds, with no train: they only rank platforms, and are not run times
// (see package motion).
func (w *World) quickestToPlatforms(stn *Station, entry Position, floor float64) (map[ids.TrackID]float64, map[ids.TrackID][]ids.TrackID) {
	type node struct {
		pos  Position
		time float64
	}
	best := map[positionKey]float64{entry.key(): 0}
	way := map[positionKey][]ids.TrackID{}
	out := map[ids.TrackID]float64{}
	outWay := map[ids.TrackID][]ids.TrackID{}
	queue := []node{{entry, 0}}
	for len(queue) > 0 {
		// the graph is tiny, so a linear scan for the nearest is fine
		i := 0
		for j := range queue {
			if queue[j].time < queue[i].time {
				i = j
			}
		}
		cur := queue[i]
		queue = slices.Delete(queue, i, i+1)
		if cur.time > best[cur.pos.key()] {
			continue
		}
		for _, edge := range w.TrackGraph.adjacency[cur.pos.At.Id] {
			trk := edge.Track
			if !stn.InArea(trk.Id) || float64(trk.MaxSpeed) < floor {
				continue
			}
			if cur.pos.Via != nil && !w.TrackGraph.TransitionAllowed(cur.pos.At, cur.pos.Via, edge) {
				continue
			}
			t := cur.time + float64(trk.Length)/float64(trk.MaxSpeed)
			here := append(slices.Clone(way[cur.pos.key()]), trk.Id)
			if stn.IsPlatform(trk.Id) {
				if old, ok := out[trk.Id]; !ok || t < old {
					out[trk.Id], outWay[trk.Id] = t, here
				}
				continue
			}
			next := Position{At: edge.Other(cur.pos.At), Via: edge}
			if old, ok := best[next.key()]; ok && old <= t {
				continue
			}
			best[next.key()], way[next.key()] = t, here
			queue = append(queue, node{next, t})
		}
	}
	return out, outWay
}
