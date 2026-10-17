// Package templates builds the pieces of a single-line world: stations with
// their throats, halts, crossing stations and signalled block sections. They
// are the builders BuildTpjKkdiSingleLineWorld is made of, made reusable, so
// trainedit can stamp them into a world it edits. A template only adds plain
// points, tracks, switches, signals and a station; once built it is ordinary
// track, and nothing remembers which template made it.
//
// Points are named {station, type, seq, other} like the rest of the worlds,
// e.g. "tpjPf1S", "krmgThW0"; switches and signals use the same scheme.
package templates

import (
	"fmt"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
)

const (
	// DefaultSidingLength is a siding's or goods loop's length when its
	// Track gives none.
	DefaultSidingLength = units.Meters(700)
	// SidingKmph is how fast a train runs on a siding or goods loop.
	SidingKmph = 30
)

// Track is one track of a station; a station lists them top to bottom, one
// lane apart.
type Track struct {
	PfNo   string       // platform number; empty for a siding or goods loop
	Length units.Meters // platform length; 0 makes a through track
	// Straight is the straight road through both throats; the station's
	// running line connects to it over straight switch legs only. A station
	// has exactly one.
	Straight bool
	// Kmph is the track's speed, 0 for the world's platform speed (or
	// SidingKmph on a siding), e.g. line speed for a main line.
	Kmph float64
	// Siding is a siding's or goods loop's name, e.g. "Gd1", for a track
	// with no platform; SidingLength its length, 0 for DefaultSidingLength.
	Siding       string
	SidingLength units.Meters
	// Buffer is the side ("W" or "E") where a siding ends at a buffer stop;
	// it joins only the other throat.
	Buffer string
}

// Pf is a platform track of the given length.
func Pf(no string, length units.Meters) Track { return Track{PfNo: no, Length: length} }

// Station builds a station's tracks and joins them into a west and an east
// throat. edge names the side ("W" or "E") that is the world's boundary, if
// any: tracks end there at the boundary and that side gets no throat (nil).
// turnout is the speed (km/h) through the diverging leg of the throats'
// turnouts; 0 for the world's default.
func Station(world *infra.World, code ids.StnCode, name string, edge string, turnout float64, tracks ...Track) (west, east *infra.TrackPoint, err error) {
	straight := -1
	for i, t := range tracks {
		switch {
		case t.Straight && straight >= 0:
			return nil, nil, fmt.Errorf("station %s: tracks %d and %d are both straight", code, straight, i)
		case t.Straight:
			straight = i
		}
		if t.Buffer != "" && t.Buffer != "W" && t.Buffer != "E" {
			return nil, nil, fmt.Errorf("station %s: track %d: buffer %q, want W or E", code, i, t.Buffer)
		}
		if t.PfNo == "" && t.Siding == "" {
			return nil, nil, fmt.Errorf("station %s: track %d needs a platform number or a siding name", code, i)
		}
	}
	if straight < 0 {
		return nil, nil, fmt.Errorf("station %s: no straight track", code)
	}
	if edge != "" && edge != "W" && edge != "E" {
		return nil, nil, fmt.Errorf("station %s: edge %q, want W, E or none", code, edge)
	}

	stn := world.NewStation(code, name)
	westEnds := make([]*infra.TrackPoint, len(tracks))
	eastEnds := make([]*infra.TrackPoint, len(tracks))
	for i, t := range tracks {
		var s, e *infra.TrackPoint
		var trk *infra.TrackSegment
		if t.PfNo != "" {
			s, e, trk = Platform(world, stn, t.PfNo, t.Length)
		} else {
			length := t.SidingLength
			if length == 0 {
				length = DefaultSidingLength
			}
			s = world.NewTrackPoint(ids.PointID{StnCode: code, Type: t.Siding, Other: "S"})
			e = world.NewTrackPoint(ids.PointID{StnCode: code, Type: t.Siding, Other: "E"})
			trk = world.NewTrackSegment(strings.ToLower(string(code))+t.Siding, length)
			trk.SetTrackAttributes(length, units.KMPH(SidingKmph))
			world.TrackGraph.AddTrack(s, e, trk)
		}
		if t.Kmph > 0 {
			trk.MaxSpeed = units.KMPH(t.Kmph)
		}
		westEnds[i], eastEnds[i] = s, e
		switch t.Buffer {
		case "W":
			s.WithDeadEnd(true)
			westEnds[i] = nil
		case "E":
			e.WithDeadEnd(true)
			eastEnds[i] = nil
		}
	}
	for side, ends := range map[string][]*infra.TrackPoint{"W": westEnds, "E": eastEnds} {
		if side != edge {
			continue
		}
		for _, p := range ends {
			if p != nil {
				p.WithDeadEnd(true).WithSimLimit(true)
			}
		}
	}
	if edge != "W" {
		west = Throat(world, code, "W", westEnds, straight, turnout)
	}
	if edge != "E" {
		east = Throat(world, code, "E", eastEnds, straight, turnout)
	}
	return west, east, nil
}

// Platform builds platform pfNo of a station, a track of the world's
// platform speed and length between {code, Pf, pfNo, S} (west) and E, and
// returns its ends and track. length is the platform's, 0 for a through
// track.
func Platform(world *infra.World, stn *infra.Station, pfNo string, length units.Meters) (s, e *infra.TrackPoint, trk *infra.TrackSegment) {
	s = world.NewTrackPoint(ids.PointID{StnCode: stn.Code, Type: "Pf", Seq: pfNo, Other: "S"})
	e = world.NewTrackPoint(ids.PointID{StnCode: stn.Code, Type: "Pf", Seq: pfNo, Other: "E"})
	trk = world.NewPlatformTrack(fmt.Sprintf("%sPf%s", strings.ToLower(string(stn.Code)), pfNo))
	world.TrackGraph.AddTrack(s, e, trk)
	stn.NewStationPlatform(trk, pfNo, length)
	return s, e, trk
}

// Throat joins a station's track ends into the one point the line meets,
// returning that point. ends are listed top to bottom, one lane apart; nil
// leaves out a track that does not reach this throat. ends[straight] is the
// straight road. The tracks below it hang off one diagonal ladder and those
// above it off another, as on a real throat: the line's first switch turns
// down onto the lower ladder, the next one up onto the upper ladder, and
// each ladder switch either lets a train off onto its track or on down (or
// up) the ladder. turnout, if not 0, is the speed (km/h) through each
// switch's diverging leg.
func Throat(world *infra.World, code ids.StnCode, side string, ends []*infra.TrackPoint, straight int, turnout float64) *infra.TrackPoint {
	legs := infra.East
	if side == "E" {
		legs = infra.West
	}
	seq := 0
	sw := func(toe, onward, off *infra.TrackPoint, lanes int) {
		id := ids.PointID{StnCode: code, Type: "Sw" + side, Seq: fmt.Sprint(seq)}.String()
		infra.NewSwitch(world, id, toe, onward, off, infra.SwitchLayout{Legs: legs, Lanes: lanes})
		if turnout > 0 {
			world.TrackGraph.Edges[ids.NewTrackID(toe.Id, off.Id)].Track.MaxSpeed = units.KMPH(turnout)
		}
		seq++
	}
	point := func() *infra.TrackPoint {
		return world.NewTrackPoint(ids.PointID{StnCode: code, Type: "Th" + side, Seq: fmt.Sprint(seq)})
	}
	// ladderFrom builds the ladder for the track lanes in order (nearest the
	// straight road first) and returns the point it starts from, and that
	// point's lane; nil if no track is on it.
	ladderFrom := func(lanes []int) (*infra.TrackPoint, int) {
		var ls []int
		for _, l := range lanes {
			if ends[l] != nil {
				ls = append(ls, l)
			}
		}
		if len(ls) == 0 {
			return nil, 0
		}
		// built from the far end, so each switch's onward leg exists
		next := ends[ls[len(ls)-1]]
		for i := len(ls) - 2; i >= 0; i-- {
			toe := point()
			sw(toe, ends[ls[i]], next, ls[i+1]-ls[i])
			next = toe
		}
		return next, ls[0]
	}
	var below, above []int
	for l := straight + 1; l < len(ends); l++ {
		below = append(below, l)
	}
	for l := straight - 1; l >= 0; l-- {
		above = append(above, l)
	}
	down, downLane := ladderFrom(below)
	up, upLane := ladderFrom(above)

	cur := ends[straight]
	if up != nil {
		toe := point()
		sw(toe, cur, up, upLane-straight)
		cur = toe
	}
	if down != nil {
		toe := point()
		sw(toe, cur, down, downLane-straight)
		cur = toe
	}
	return cur
}

// CrossingStation is the usual crossing station on a single line (KRMG,
// KRUR, VEL, TYM): the main line straight through in the middle with no
// platform (through track 3, at lineKmph), and a loop with a platform of
// pfLength either side of it (PF1 above, PF2 below).
func CrossingStation(world *infra.World, code ids.StnCode, name string, pfLength units.Meters, lineKmph float64) (west, east *infra.TrackPoint, err error) {
	return Station(world, code, name, "", 0,
		Pf("1", pfLength),
		Track{PfNo: "3", Straight: true, Kmph: lineKmph},
		Pf("2", pfLength),
	)
}

// Halt is a stopping place on a single line with one platform of pfLength,
// at lineKmph, and no points.
func Halt(world *infra.World, code ids.StnCode, name string, pfLength units.Meters, lineKmph float64) (west, east *infra.TrackPoint, err error) {
	return Station(world, code, name, "", 0, Track{PfNo: "1", Length: pfLength, Straight: true, Kmph: lineKmph})
}

// SingleLine joins two points with a single bidirectional line of the given
// length, split into blocks equal blocks. There is a signal for each
// direction at every block boundary and at both ends, so each block is a
// signal section either way. name names the block points ({name, Bk, i}),
// tracks (name0, name1, ...) and signals (nameSig0F, nameSig0R, ...).
func SingleLine(world *infra.World, name string, from, to *infra.TrackPoint, length units.Meters, blocks int) {
	pts := []*infra.TrackPoint{from}
	for i := 1; i < blocks; i++ {
		pts = append(pts, world.NewTrackPoint(ids.PointID{StnCode: ids.StnCode(name), Type: "Bk", Seq: fmt.Sprint(i)}))
	}
	pts = append(pts, to)
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		world.TrackGraph.AddTrack(a, b, world.NewTrackSegment(fmt.Sprintf("%s%d", name, i), length/units.Meters(blocks)))
		world.AddSignal(infra.NewSignal(fmt.Sprintf("%sSig%dF", name, i), b, a)) // running from -> to
		world.AddSignal(infra.NewSignal(fmt.Sprintf("%sSig%dR", name, i), a, b)) // running to -> from
	}
}
