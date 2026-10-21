// Package templates builds the pieces of a world: stations with their
// throats, on one running line or several, halts, crossing stations,
// crossovers and signalled block sections. They
// are the builders BuildTpjKkdiSingleLineWorld is made of, made reusable, so
// trainedit can stamp them into a world it edits. A template only adds plain
// points, tracks, switches, signals and a station; once built it is ordinary
// track, and nothing remembers which template made it.
//
// Points are named {station, type, seq, other} like the rest of the worlds,
// e.g. "tpjPf1S", "krmgThW0"; switches and signals use the same scheme.
package templates

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
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
	// Straight makes the track a main: the straight road through a throat,
	// which a running line joins over straight switch legs only. A station
	// on a single line has exactly one; one on several lines has one for
	// each (see Layout).
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
	// Join is which main a track between two mains hangs off, in both
	// throats: JoinAbove, JoinBelow or JoinBoth; empty shares the tracks
	// between two mains out (see Layout).
	Join string
}

// Which main a track between two mains hangs off (Track.Join).
const (
	JoinAbove = "^"
	JoinBelow = "v"
	JoinBoth  = "^v" // a common loop: either main's trains run onto it without a crossover
)

// Crossovers between two neighbouring mains in a throat (Layout).
const (
	CrossNone     = ""
	CrossDown     = "down" // a single crossover, its diagonal running down going east
	CrossUp       = "up"   // a single crossover, its diagonal running up going east
	CrossScissors = "scissors"
)

// Crossings lists the crossovers between mains, in the order a form offers
// them.
var Crossings = []string{"none", CrossDown, CrossUp, CrossScissors}

// Layout is how a station's tracks meet the lines either side of it.
//
// Each throat joins the station's tracks to one running line for each of
// its mains, top to bottom: the main itself is the line's straight road, and
// the other tracks hang off ladders of switches from it. A track above the
// top main hangs off the top main and one below the bottom main off the
// bottom main. Between two mains a track hangs off the one its Join says;
// with no Join they are shared out, the upper ones to the upper main and the
// lower ones to the lower, and a track left in the middle hangs off both. At
// most one track between two mains can hang off both, and those hanging off
// the upper main must lie above those hanging off the lower.
type Layout struct {
	// Edge names the side ("W" or "E") that is the world's boundary, if any:
	// tracks end there at the boundary and that side gets no throat.
	Edge string
	// TurnoutKmph is the speed through the diverging leg of the throats'
	// ladder switches; 0 for the world's default.
	TurnoutKmph float64
	// WestMains and EastMains are the mains of each throat, as indexes into
	// the tracks; nil for the Straight tracks. A side can have more mains
	// than the other, e.g. a junction station with two lines one way and one
	// the other.
	WestMains, EastMains []int
	// WestCrossovers and EastCrossovers are the crossovers in each throat,
	// one for each pair of neighbouring mains, top to bottom (a short list
	// leaves the rest out). They lie beyond the ladders, on the line side.
	WestCrossovers, EastCrossovers []string
}

// Pf is a platform track of the given length.
func Pf(no string, length units.Meters) Track { return Track{PfNo: no, Length: length} }

// Station builds a station on a single line, with one main (the Straight
// track): its tracks joined into one west and one east throat point. edge
// names the side ("W" or "E") that is the world's boundary, if any (nil
// there). turnout is the speed (km/h) through the diverging leg of the
// throats' turnouts; 0 for the world's default.
func Station(world *infra.World, code ids.StnCode, name string, edge string, turnout float64, tracks ...Track) (west, east *infra.TrackPoint, err error) {
	straight := -1
	for i, t := range tracks {
		switch {
		case t.Straight && straight >= 0:
			return nil, nil, fmt.Errorf("station %s: tracks %d and %d are both straight", code, straight, i)
		case t.Straight:
			straight = i
		}
	}
	ws, es, err := BuildStation(world, code, name, Layout{Edge: edge, TurnoutKmph: turnout}, tracks...)
	if err != nil {
		return nil, nil, err
	}
	if len(ws) > 0 {
		west = ws[0]
	}
	if len(es) > 0 {
		east = es[0]
	}
	return west, east, nil
}

// BuildStation builds a station's tracks and joins them into throats as the
// layout says, returning each throat's line points, one for each of its
// mains, top to bottom: the points the running lines meet (none on the
// world's edge).
func BuildStation(world *infra.World, code ids.StnCode, name string, layout Layout, tracks ...Track) (west, east []*infra.TrackPoint, err error) {
	var straight []int
	for i, t := range tracks {
		if t.Straight {
			straight = append(straight, i)
		}
		if t.Buffer != "" && t.Buffer != "W" && t.Buffer != "E" {
			return nil, nil, fmt.Errorf("station %s: track %d: buffer %q, want W or E", code, i, t.Buffer)
		}
		if t.PfNo == "" && t.Siding == "" {
			return nil, nil, fmt.Errorf("station %s: track %d needs a platform number or a siding name", code, i)
		}
		if t.Join != "" && t.Join != JoinAbove && t.Join != JoinBelow && t.Join != JoinBoth {
			return nil, nil, fmt.Errorf("station %s: track %d: join %q, want %s, %s or %s", code, i, t.Join, JoinAbove, JoinBelow, JoinBoth)
		}
	}
	// each track's ends are named from its number or name: two the same
	// would be one point
	named := map[string]int{}
	for i, t := range tracks {
		end := ids.PointID{StnCode: code, Type: "Pf", Seq: t.PfNo, Other: "S"}
		what := "platform " + t.PfNo
		if t.PfNo == "" {
			end, what = ids.PointID{StnCode: code, Type: t.Siding, Other: "S"}, "siding "+t.Siding
		}
		if j, ok := named[end.String()]; ok {
			return nil, nil, fmt.Errorf("station %s: tracks %d and %d are both %s: number or name them apart", code, j+1, i+1, what)
		}
		named[end.String()] = i
	}
	edge := layout.Edge
	if edge != "" && edge != "W" && edge != "E" {
		return nil, nil, fmt.Errorf("station %s: edge %q, want W, E or none", code, edge)
	}
	sides := map[string]*throatPlan{}
	for _, side := range []string{"W", "E"} {
		if side == edge {
			continue
		}
		mains, crossings := layout.WestMains, layout.WestCrossovers
		if side == "E" {
			mains, crossings = layout.EastMains, layout.EastCrossovers
		}
		if mains == nil {
			mains = straight
		}
		plan, err := planThroat(tracks, side, mains, crossings)
		if err != nil {
			return nil, nil, fmt.Errorf("station %s: %s throat: %w", code, side, err)
		}
		sides[side] = plan
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
	if p := sides["W"]; p != nil {
		west = p.build(world, code, "W", westEnds, layout.TurnoutKmph)
	}
	if p := sides["E"]; p != nil {
		east = p.build(world, code, "E", eastEnds, layout.TurnoutKmph)
	}
	return west, east, nil
}

// throatPlan is which tracks hang off which main in a throat, and the
// crossovers beyond.
type throatPlan struct {
	mains     []int   // track indexes, top to bottom
	groups    [][]int // the tracks hanging off each main, the main among them
	both      []int   // tracks hanging off two mains
	crossings []string
}

func planThroat(tracks []Track, side string, mains []int, crossings []string) (*throatPlan, error) {
	mains = slices.Clone(mains)
	slices.Sort(mains)
	if len(mains) == 0 {
		return nil, errors.New("no straight track")
	}
	reaches := func(i int) bool { return tracks[i].Buffer != side }
	for k, m := range mains {
		switch {
		case m < 0 || m >= len(tracks):
			return nil, fmt.Errorf("main %d is not one of the %d tracks", m, len(tracks))
		case k > 0 && mains[k-1] == m:
			return nil, fmt.Errorf("track %d is a main twice", m)
		case !reaches(m):
			return nil, fmt.Errorf("main track %d ends at a buffer stop", m)
		}
	}
	if len(crossings) > len(mains)-1 {
		return nil, fmt.Errorf("%d crossovers, but %d mains", len(crossings), len(mains))
	}
	for _, c := range crossings {
		if c != CrossNone && c != "none" && c != CrossDown && c != CrossUp && c != CrossScissors {
			return nil, fmt.Errorf("crossover %q, want none, %s, %s or %s", c, CrossDown, CrossUp, CrossScissors)
		}
	}
	p := &throatPlan{mains: mains, groups: make([][]int, len(mains)), crossings: crossings}
	for k, m := range mains {
		p.groups[k] = append(p.groups[k], m)
	}
	for i := 0; i < mains[0]; i++ {
		p.groups[0] = append(p.groups[0], i)
	}
	for i := mains[len(mains)-1] + 1; i < len(tracks); i++ {
		p.groups[len(mains)-1] = append(p.groups[len(mains)-1], i)
	}
	for k := 0; k+1 < len(mains); k++ {
		var gap []int
		for i := mains[k] + 1; i < mains[k+1]; i++ {
			if reaches(i) {
				gap = append(gap, i)
			}
		}
		// share out the tracks with no Join: the upper half up, the lower
		// half down, one in the middle both ways
		var free []int
		for _, i := range gap {
			if tracks[i].Join == "" {
				free = append(free, i)
			}
		}
		join := map[int]string{}
		for n, i := range free {
			switch {
			case 2*n+1 == len(free):
				join[i] = JoinBoth
			case 2*n < len(free):
				join[i] = JoinAbove
			default:
				join[i] = JoinBelow
			}
		}
		rank := map[string]int{JoinAbove: 0, JoinBoth: 1, JoinBelow: 2}
		last, boths := 0, 0
		for _, i := range gap {
			j := cmp.Or(tracks[i].Join, join[i])
			if rank[j] < last {
				return nil, fmt.Errorf("between tracks %d and %d, track %d hangs off a main above one that hangs off the main below it", mains[k], mains[k+1], i)
			}
			last = rank[j]
			if j != JoinBelow {
				p.groups[k] = append(p.groups[k], i)
			}
			if j != JoinAbove {
				p.groups[k+1] = append(p.groups[k+1], i)
			}
			if j == JoinBoth {
				p.both = append(p.both, i)
				boths++
			}
		}
		if boths > 1 {
			return nil, fmt.Errorf("between tracks %d and %d, %d tracks hang off both mains: one can", mains[k], mains[k+1], boths)
		}
	}
	return p, nil
}

// build lays the throat and returns its line points, top to bottom.
func (p *throatPlan) build(world *infra.World, code ids.StnCode, side string, ends []*infra.TrackPoint, turnout float64) []*infra.TrackPoint {
	seq := 0
	out := make([]*infra.TrackPoint, len(p.mains))
	for k, m := range p.mains {
		mine := make([]*infra.TrackPoint, len(ends))
		for _, i := range p.groups[k] {
			mine[i] = ends[i]
		}
		out[k] = throat(world, code, side, mine, m, turnout, &seq)
	}
	// a track hanging off two mains meets both ladders at its end: a switch
	// there lets a train onto it from either
	for _, i := range p.both {
		e := ends[i]
		var legs []*infra.GraphEdge
		for _, edge := range world.TrackGraph.Edges {
			if (side == "W" && edge.To == e) || (side == "E" && edge.From == e) {
				legs = append(legs, edge)
			}
		}
		slices.SortFunc(legs, func(a, b *infra.GraphEdge) int { return a.Track.Id.Compare(b.Track.Id) })
		if len(legs) != 2 {
			panic(fmt.Sprintf("templates: station %s: track %d meets %d ladders, want 2", code, i, len(legs)))
		}
		id := ids.PointID{StnCode: code, Type: "Sw" + side, Seq: fmt.Sprint(seq)}.String()
		seq++
		if side == "W" {
			infra.NewSwitch(world, id, e, legs[0].From, legs[1].From, infra.SwitchLayout{Legs: infra.West, Lanes: -legs[1].Lanes})
		} else {
			infra.NewSwitch(world, id, e, legs[0].To, legs[1].To, infra.SwitchLayout{Legs: infra.East, Lanes: legs[1].Lanes})
		}
	}
	// a main with no track hanging off it would meet a crossover right at its
	// end, a switch's length nearer the station than its neighbour's ladder
	// toe: a plain stretch as long as a switch's straight leg lines the two
	// up, so the crossover is drawn as an X and its legs are alike
	for k, kind := range p.crossings {
		if kind == CrossNone || kind == "none" {
			continue
		}
		for _, j := range []int{k, k + 1} {
			if out[j] != ends[p.mains[j]] {
				continue
			}
			pad := world.NewTrackPoint(ids.PointID{StnCode: code, Type: "Th" + side, Seq: fmt.Sprint(seq)})
			seq++
			trk := world.NewTrackSegment("", world.SwitchLength())
			if side == "W" {
				world.TrackGraph.AddTrack(pad, out[j], trk)
			} else {
				world.TrackGraph.AddTrack(out[j], pad, trk)
			}
			out[j] = pad
		}
	}
	for k, kind := range p.crossings {
		if kind == CrossNone || kind == "none" {
			continue
		}
		lanes := p.mains[k+1] - p.mains[k]
		name := func(other string) ids.PointID {
			return ids.PointID{StnCode: code, Type: "Xo" + side, Seq: fmt.Sprint(k), Other: other}
		}
		upper, lower := world.NewTrackPoint(name("U")), world.NewTrackPoint(name("L"))
		// each line runs aU -> bU (upper) and aL -> bL (lower), west to east
		aU, bU, aL, bL := upper, out[k], lower, out[k+1]
		if side == "E" {
			aU, bU, aL, bL = out[k], upper, out[k+1], lower
		}
		Crossover(world, name("").String(), kind, aU, bU, aL, bL, lanes)
		out[k], out[k+1] = upper, lower
	}
	return out
}

// Crossover lays a crossover of a kind (CrossDown, CrossUp or
// CrossScissors) between two parallel tracks it builds, aU -> bU and,
// lanes lanes below, aL -> bL, the a points at the west end. Its switches
// are named id and A, B, ... after it, and a scissors crossover's diamond
// id and D.
func Crossover(world *infra.World, id, kind string, aU, bU, aL, bL *infra.TrackPoint, lanes int) {
	sw := func(n string, toe, straight, div *infra.TrackPoint, legs infra.Heading, l int) {
		infra.NewSwitch(world, id+n, toe, straight, div, infra.SwitchLayout{Legs: legs, Lanes: l})
	}
	switch kind {
	case CrossDown:
		sw("A", aU, bU, bL, infra.East, lanes)
		sw("B", bL, aL, aU, infra.West, -lanes)
	case CrossUp:
		sw("A", aL, bL, bU, infra.East, -lanes)
		sw("B", bU, aU, aL, infra.West, lanes)
	case CrossScissors:
		infra.NewDiamondCrossing(world, id+"D", aU, bL, aL, bU, lanes)
		sw("A", aU, bU, bL, infra.East, lanes)
		sw("B", bL, aL, aU, infra.West, -lanes)
		sw("C", aL, bL, bU, infra.East, -lanes)
		sw("E", bU, aU, aL, infra.West, lanes)
	default:
		panic(fmt.Sprintf("templates: crossover %s: kind %q", id, kind))
	}
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
	seq := 0
	return throat(world, code, side, ends, straight, turnout, &seq)
}

// throat is Throat numbering its points and switches from *seq on.
func throat(world *infra.World, code ids.StnCode, side string, ends []*infra.TrackPoint, straight int, turnout float64, seq *int) *infra.TrackPoint {
	legs := infra.East
	if side == "E" {
		legs = infra.West
	}
	sw := func(toe, onward, off *infra.TrackPoint, lanes int) {
		id := ids.PointID{StnCode: code, Type: "Sw" + side, Seq: fmt.Sprint(*seq)}.String()
		infra.NewSwitch(world, id, toe, onward, off, infra.SwitchLayout{Legs: legs, Lanes: lanes})
		if turnout > 0 {
			world.TrackGraph.Edges[ids.NewTrackID(toe.Id, off.Id)].Track.MaxSpeed = units.KMPH(turnout)
		}
		*seq++
	}
	point := func() *infra.TrackPoint {
		return world.NewTrackPoint(ids.PointID{StnCode: code, Type: "Th" + side, Seq: fmt.Sprint(*seq)})
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

// Ways a one-way line runs (OneWayLine).
const (
	WayEast = 'E'
	WayWest = 'W'
)

// OneWayLine joins two points with a line signalled for trains running one
// way only (WayEast or WayWest), length long and split into blocks equal
// blocks, as trainedit's Lines lays one: a signal at every block boundary
// and both ends, facing the way the line runs, and its tracks labelled UP
// or DOWN by the world's Up. from is the west end. name names the block
// points ({name, Bk, i}), tracks (name0, name1, ...) and signals (nameSig0F
// for a line running east, nameSig0R for one running west, ...).
func OneWayLine(world *infra.World, name string, from, to *infra.TrackPoint, way byte, length units.Meters, blocks int) {
	if way != WayEast && way != WayWest {
		panic(fmt.Sprintf("templates: line %s: way %q, want E or W", name, way))
	}
	dir := infra.Down
	if (way == WayEast) == (cmp.Or(world.Up(), "W") == "E") {
		dir = infra.Up
	}
	pts := []*infra.TrackPoint{from}
	for i := 1; i < blocks; i++ {
		pts = append(pts, world.NewTrackPoint(ids.PointID{StnCode: ids.StnCode(name), Type: "Bk", Seq: fmt.Sprint(i)}))
	}
	pts = append(pts, to)
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		trk := world.NewTrackSegment(fmt.Sprintf("%s%d", name, i), length/units.Meters(blocks))
		trk.Direction = dir
		world.TrackGraph.AddTrack(a, b, trk)
		if way == WayEast {
			world.AddSignal(infra.NewSignal(fmt.Sprintf("%sSig%dF", name, i), b, a))
		} else {
			world.AddSignal(infra.NewSignal(fmt.Sprintf("%sSig%dR", name, i), a, b))
		}
	}
}

// DoubleLine joins two stations with a double line, the upper line (from[0]
// to to[0]) running east and the lower (from[1] to to[1]) running west, as
// OneWayLine lays each. from are the west ends. The lines are named as
// trainedit names a pattern's lines: name with L1 (upper) and L2 after it.
func DoubleLine(world *infra.World, name string, from, to [2]*infra.TrackPoint, length units.Meters, blocks int) {
	OneWayLine(world, name+"L1", from[0], to[0], WayEast, length, blocks)
	OneWayLine(world, name+"L2", from[1], to[1], WayWest, length, blocks)
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
