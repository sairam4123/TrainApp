package vizproto

import (
	"cmp"
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

// FromSnapshot describes a world snapshot as it stands, without building it:
// a snapshot being edited need not build (a track end may lead nowhere, say),
// and can still be drawn. Unlike FromWorld it lists points with no track,
// and tells only through tracks from the other platforms, since main lines
// take a built world. Tracks whose ends are not in the snapshot, and switch,
// signal and platform entries naming tracks or points it lacks, are left
// out.
func FromSnapshot(s *infra.Snapshot) *World {
	out := &World{}
	type point struct {
		id ids.PointID
		p  infra.SnapshotPoint
	}
	var pts []point
	known := map[string]ids.PointID{}
	for _, p := range s.Points {
		id := ids.PointID{StnCode: ids.StnCode(p.Stn), Type: p.Type, Seq: p.Seq, Other: p.Other}
		if id.IsZero() {
			continue
		}
		if _, dup := known[id.String()]; dup {
			continue
		}
		known[id.String()] = id
		pts = append(pts, point{id, p})
	}
	// in id order, as FromWorld lists them; the layout breaks ties by it
	slices.SortFunc(pts, func(a, b point) int { return a.id.Compare(b.id) })
	for _, p := range pts {
		out.Points = append(out.Points, Point{ID: p.id.String(), DeadEnd: p.p.DeadEnd, Boundary: p.p.Boundary})
	}

	platform := map[string]bool{}
	for _, st := range s.Stations {
		for _, pf := range st.Platforms {
			platform[pf.Track] = true
		}
	}
	switched := map[string]bool{}
	for _, sw := range s.Switches {
		for _, t := range sw.Tracks {
			switched[t] = true
		}
	}

	type track struct {
		id ids.TrackID
		t  Track
	}
	var tracks []track
	seen := map[string]bool{}
	for _, t := range s.Tracks {
		from, okF := known[t.From]
		to, okT := known[t.To]
		if !okF || !okT || from == to {
			continue
		}
		id := ids.NewTrackID(from, to)
		if seen[id.String()] {
			continue
		}
		seen[id.String()] = true
		kind := KindLine
		if platform[id.String()] {
			kind = KindPlatform
		} else if switched[id.String()] {
			kind = KindSwitch
		}
		tracks = append(tracks, track{id, Track{
			ID: id.String(), Name: cmp.Or(t.Name, id.String()), From: t.From, To: t.To,
			Lanes: t.Lanes, Kind: kind, Length: t.LengthM, Direction: t.Direction,
		}})
	}
	slices.SortFunc(tracks, func(a, b track) int { return a.id.Compare(b.id) })
	for _, t := range tracks {
		out.Tracks = append(out.Tracks, t.t)
	}

	for _, sw := range s.Switches {
		blk := SwitchBlock{ID: sw.ID, Default: sw.Default}
		for _, t := range sw.Tracks {
			if seen[t] {
				blk.Tracks = append(blk.Tracks, t)
			}
		}
		if !seen[sw.Default] {
			blk.Default = ""
		}
		out.Switches = append(out.Switches, blk)
	}
	slices.SortFunc(out.Switches, func(a, b SwitchBlock) int { return cmp.Compare(a.ID, b.ID) })
	for _, sig := range s.Signals {
		_, okA := known[sig.At]
		_, okF := known[sig.Facing]
		if okA && okF {
			out.Signals = append(out.Signals, Signal{ID: sig.ID, At: sig.At, Facing: sig.Facing})
		}
	}
	slices.SortFunc(out.Signals, func(a, b Signal) int { return cmp.Compare(a.ID, b.ID) })
	for _, st := range s.Stations {
		stn := Station{Code: st.Code, Name: st.Name}
		for _, pf := range st.Platforms {
			if seen[pf.Track] {
				p := Platform{No: pf.No, Track: pf.Track}
				if pf.LengthM == 0 {
					p.Class = "through" // as infra.Through; main lines take a built world
				}
				stn.Platforms = append(stn.Platforms, p)
			}
		}
		out.Stations = append(out.Stations, stn)
	}
	slices.SortFunc(out.Stations, func(a, b Station) int { return cmp.Compare(a.Code, b.Code) })
	return out
}
