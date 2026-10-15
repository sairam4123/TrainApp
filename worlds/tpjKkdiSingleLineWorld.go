package worlds

import (
	"fmt"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
)

// The Tiruchchirappalli - Karaikkudi line as it is on the ground: a single
// electrified broad-gauge line with every station on it. Station positions
// are from OpenStreetMap, measured along the line from TPJ (they agree with
// the passenger timetables' 13, 29, 41, 53, 59, 68, 78, 83 and 89 km). Track
// layouts are from OpenStreetMap too; platform numbers OSM does not record
// (the crossing stations', CTND's) are a guess. Platform lengths are
// approximate.

const (
	slLineSpeed  = 110 // km/h, the line's sectional speed
	slBigPf      = 600 // m, platforms at TPJ, PDKT and KKDI
	slCrossingPf = 450 // m, platforms at the crossing stations (KRMG's in OSM)
	slHaltPf     = 400 // m, platforms at the halts (NMN's in OSM)
	slSidingLen  = 700 // m, sidings and goods loops
)

// slTrack is one track of a station; a station lists them top to bottom, one
// lane apart.
type slTrack struct {
	pfNo   string       // platform number; empty for a siding or goods loop
	length units.Meters // platform length; 0 makes a through track
	// straight is the straight road through both throats; the station's
	// running line connects to it over straight switch legs only.
	straight bool
	fast     bool   // runs at line speed rather than platform speed
	siding   string // a siding's or goods loop's name, e.g. "Gd1"
	// buffer is the side ("W" or "E") where a siding ends at a buffer stop;
	// it joins only the other throat.
	buffer string
}

func pf(no string, length units.Meters) slTrack { return slTrack{pfNo: no, length: length} }

// slStation builds a station's tracks and joins them into a west and an east
// throat. edge names the side ("W" or "E") that is the world's boundary, if
// any: tracks end there at the boundary and that side gets no throat (nil).
func slStation(world *infra.World, code ids.StnCode, name string, edge string, tracks ...slTrack) (west, east *infra.TrackPoint) {
	stn := world.NewStation(code, name)
	westEnds := make([]*infra.TrackPoint, len(tracks))
	eastEnds := make([]*infra.TrackPoint, len(tracks))
	straight := -1
	for i, t := range tracks {
		var s, e *infra.TrackPoint
		var trk *infra.TrackSegment
		if t.pfNo != "" {
			s, e, trk = constructPlatform(world, stn, t.pfNo, t.length)
		} else {
			s = world.NewTrackPoint(newTrackPointId(code, t.siding, "", "S"))
			e = world.NewTrackPoint(newTrackPointId(code, t.siding, "", "E"))
			trk = world.NewTrackSegment(strings.ToLower(string(code))+t.siding, units.M(slSidingLen))
			trk.SetTrackAttributes(units.M(slSidingLen), units.KMPH(30))
			world.TrackGraph.AddTrack(s, e, trk)
		}
		if t.fast {
			trk.MaxSpeed = units.KMPH(slLineSpeed)
		}
		if t.straight {
			straight = i
		}
		westEnds[i], eastEnds[i] = s, e
		switch t.buffer {
		case "W":
			s.WithDeadEnd(true)
			westEnds[i] = nil
		case "E":
			e.WithDeadEnd(true)
			eastEnds[i] = nil
		}
	}
	if straight < 0 {
		panic(fmt.Sprintf("slStation %s: no straight track", code))
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
		west = slThroat(world, code, "W", westEnds, straight)
	}
	if edge != "E" {
		east = slThroat(world, code, "E", eastEnds, straight)
	}
	return west, east
}

// slThroat joins a station's track ends into the one point the line meets,
// returning that point. ends are listed top to bottom, one lane apart; nil
// leaves out a track that does not reach this throat. ends[straight] is the
// straight road. The tracks below it hang off one diagonal ladder and those
// above it off another, as on a real throat: the line's first switch turns
// down onto the lower ladder, the next one up onto the upper ladder, and
// each ladder switch either lets a train off onto its track or on down (or
// up) the ladder.
func slThroat(world *infra.World, code ids.StnCode, side string, ends []*infra.TrackPoint, straight int) *infra.TrackPoint {
	legs := infra.East
	if side == "E" {
		legs = infra.West
	}
	seq := 0
	sw := func(toe, onward, off *infra.TrackPoint, lanes int) {
		infra.NewSwitch(world, newSwitchId(code, "Sw"+side, fmt.Sprint(seq), ""), toe, onward, off, infra.SwitchLayout{Legs: legs, Lanes: lanes})
		seq++
	}
	point := func() *infra.TrackPoint {
		return world.NewTrackPoint(newTrackPointId(code, "Th"+side, fmt.Sprint(seq), ""))
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

// crossingStation is the usual crossing station on the line (KRMG, KRUR, VEL,
// TYM): the main line straight through in the middle with no platform, and a
// loop with a platform either side of it.
func crossingStation(world *infra.World, code ids.StnCode, name string) (west, east *infra.TrackPoint) {
	return slStation(world, code, name, "",
		pf("1", slCrossingPf),
		slTrack{pfNo: "3", straight: true, fast: true},
		pf("2", slCrossingPf),
	)
}

// halt is a stopping place on the single line with one platform and no
// points.
func halt(world *infra.World, code ids.StnCode, name string) (west, east *infra.TrackPoint) {
	return slStation(world, code, name, "", slTrack{pfNo: "1", length: slHaltPf, straight: true, fast: true})
}

// BuildTpjKkdiSingleLineWorld builds the single line from Tiruchchirappalli
// Jn (west) to Karaikkudi Jn (east), stations and km from TPJ:
//
//	TPJ   0.00  Tiruchchirappalli Jn  PF1-8 and a through track (T9); the world's west edge
//	KRMG 12.94  Kumaramangalam        crossing station
//	KRUR 28.37  Keeranur              crossing station
//	VEL  41.41  Vellanur              crossing station
//	PDKT 52.56  Pudukkottai           PF1 on the main line, two loops, a goods loop
//	NMN  58.57  Namanasamudram        halt
//	TYM  68.55  Tirumayam             crossing station
//	CTND 77.66  Chettinad             PF2 on the main line, two loops, a goods loop
//	KTYR 83.06  Kottaiyur             halt
//	KKDI 89.41  Karaikkudi Jn         PF1-5 and a through loop (T6); the world's east edge
//
// Each stretch between stations is one block section, signalled both ways
// (absolute block), as long as the distance between the stations less 1 km
// for their own tracks. The other lines at the two junctions are left out:
// trains enter and leave the world on TPJ's and KKDI's platforms.
//
// TPJ's bay platform 1A, KKDI's siding (it opens away from this line) and the
// yards are left out. A halt's platform ends act as signals in the sim, so
// unlike on the real line a halt splits its block section.
func BuildTpjKkdiSingleLineWorld() *infra.World {
	world := &infra.World{}
	world.Init(infra.WorldData{
		DefaultSwMaxSpeed: units.KMPH(30),
		DefaultPfMaxSpeed: units.KMPH(50),
		DefaultSwLength:   units.M(100),
		DefaultPfLength:   units.M(800),
		DefaultRouteSpeed: units.KMPH(slLineSpeed),
	})

	// TPJ: the line runs straight onto the PF5 road. The through track lies
	// between PF1 and PF2; between PF7 and PF8 are a stabling line (open at
	// both ends) and two stabling sidings that end at buffer stops on the far
	// side, so they open towards this line.
	_, tpjE := slStation(world, "TPJ", "Tiruchchirappalli Jn", "W",
		pf("1", slBigPf),
		slTrack{pfNo: "9"},
		pf("2", slBigPf),
		pf("3", slBigPf),
		pf("4", slBigPf),
		slTrack{pfNo: "5", length: slBigPf, straight: true},
		pf("6", slBigPf),
		pf("7", slBigPf),
		slTrack{siding: "St1"},
		slTrack{siding: "Sd1", buffer: "W"},
		slTrack{siding: "Sd2", buffer: "W"},
		pf("8", slBigPf),
	)
	krmgW, krmgE := crossingStation(world, "KRMG", "Kumaramangalam")
	krurW, krurE := crossingStation(world, "KRUR", "Keeranur")
	velW, velE := crossingStation(world, "VEL", "Vellanur")
	// PDKT: PF1 on the main line, PF2 and PF3 on loops either side of an
	// island, and a goods loop beyond PF3
	pdktW, pdktE := slStation(world, "PDKT", "Pudukkottai", "",
		slTrack{pfNo: "1", length: slBigPf, straight: true, fast: true},
		pf("2", slBigPf),
		pf("3", slBigPf),
		slTrack{siding: "Gd1"},
	)
	nmnW, nmnE := halt(world, "NMN", "Namanasamudram")
	tymW, tymE := crossingStation(world, "TYM", "Tirumayam")
	// CTND: loop, main, loop, goods loop; the island is between the main line
	// and PF3
	ctndW, ctndE := slStation(world, "CTND", "Chettinad", "",
		pf("1", slCrossingPf),
		slTrack{pfNo: "2", length: slCrossingPf, straight: true, fast: true},
		pf("3", slCrossingPf),
		slTrack{siding: "Gd1"},
	)
	ktyrW, ktyrE := halt(world, "KTYR", "Kottaiyur")
	// KKDI: the line runs straight onto the PF3 road; the through loop lies
	// between PF1 and PF2
	kkdiW, _ := slStation(world, "KKDI", "Karaikkudi Jn", "E",
		pf("1", slBigPf),
		slTrack{pfNo: "6"},
		pf("2", slBigPf),
		slTrack{pfNo: "3", length: slBigPf, straight: true},
		pf("4", slBigPf),
		pf("5", slBigPf),
	)

	for _, leg := range []struct {
		from, to     *infra.TrackPoint
		fromKm, toKm float64
	}{
		{tpjE, krmgW, 0, 12.94},
		{krmgE, krurW, 12.94, 28.37},
		{krurE, velW, 28.37, 41.41},
		{velE, pdktW, 41.41, 52.56},
		{pdktE, nmnW, 52.56, 58.57},
		{nmnE, tymW, 58.57, 68.55},
		{tymE, ctndW, 68.55, 77.66},
		{ctndE, ktyrW, 77.66, 83.06},
		{ktyrE, kkdiW, 83.06, 89.41},
	} {
		name := "sl" + title(leg.from.Id.StnCode) + title(leg.to.Id.StnCode)
		singleLine(world, name, leg.from, leg.to, units.KM(leg.toKm-leg.fromKm)-units.M(1000), 1)
	}

	if err := world.Finalize(); err != nil {
		panic(fmt.Sprintf("BuildTpjKkdiSingleLineWorld: %v", err))
	}
	return world
}

// title is a station code as it reads inside a name, e.g. "PDKT" -> "Pdkt".
func title(code ids.StnCode) string {
	s := strings.ToLower(string(code))
	return strings.ToUpper(s[:1]) + s[1:]
}
