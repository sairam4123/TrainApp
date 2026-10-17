package worlds

import (
	"fmt"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
	tm "trainapp/worlds/templates"
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
	// km/h through the diverging leg of the turnouts in TPJ's and KKDI's
	// throats (1 in 8.5); elsewhere the world's 30 (1 in 12)
	slYardTurnout = 15
)

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
	_, tpjE := must(tm.Station(world, "TPJ", "Tiruchchirappalli Jn", "W", slYardTurnout,
		tm.Pf("1", slBigPf),
		tm.Track{PfNo: "9"},
		tm.Pf("2", slBigPf),
		tm.Pf("3", slBigPf),
		tm.Pf("4", slBigPf),
		tm.Track{PfNo: "5", Length: slBigPf, Straight: true},
		tm.Pf("6", slBigPf),
		tm.Pf("7", slBigPf),
		tm.Track{Siding: "St1", SidingLength: slSidingLen},
		tm.Track{Siding: "Sd1", SidingLength: slSidingLen, Buffer: "W"},
		tm.Track{Siding: "Sd2", SidingLength: slSidingLen, Buffer: "W"},
		tm.Pf("8", slBigPf),
	))
	krmgW, krmgE := must(tm.CrossingStation(world, "KRMG", "Kumaramangalam", slCrossingPf, slLineSpeed))
	krurW, krurE := must(tm.CrossingStation(world, "KRUR", "Keeranur", slCrossingPf, slLineSpeed))
	velW, velE := must(tm.CrossingStation(world, "VEL", "Vellanur", slCrossingPf, slLineSpeed))
	// PDKT: PF1 on the main line, PF2 and PF3 on loops either side of an
	// island, and a goods loop beyond PF3
	pdktW, pdktE := must(tm.Station(world, "PDKT", "Pudukkottai", "", 0,
		tm.Track{PfNo: "1", Length: slBigPf, Straight: true, Kmph: slLineSpeed},
		tm.Pf("2", slBigPf),
		tm.Pf("3", slBigPf),
		tm.Track{Siding: "Gd1", SidingLength: slSidingLen},
	))
	nmnW, nmnE := must(tm.Halt(world, "NMN", "Namanasamudram", slHaltPf, slLineSpeed))
	tymW, tymE := must(tm.CrossingStation(world, "TYM", "Tirumayam", slCrossingPf, slLineSpeed))
	// CTND: loop, main, loop, goods loop; the island is between the main line
	// and PF3
	ctndW, ctndE := must(tm.Station(world, "CTND", "Chettinad", "", 0,
		tm.Pf("1", slCrossingPf),
		tm.Track{PfNo: "2", Length: slCrossingPf, Straight: true, Kmph: slLineSpeed},
		tm.Pf("3", slCrossingPf),
		tm.Track{Siding: "Gd1", SidingLength: slSidingLen},
	))
	ktyrW, ktyrE := must(tm.Halt(world, "KTYR", "Kottaiyur", slHaltPf, slLineSpeed))
	// KKDI: the line runs straight onto the PF3 road; the through loop lies
	// between PF1 and PF2
	kkdiW, _ := must(tm.Station(world, "KKDI", "Karaikkudi Jn", "E", slYardTurnout,
		tm.Pf("1", slBigPf),
		tm.Track{PfNo: "6"},
		tm.Pf("2", slBigPf),
		tm.Track{PfNo: "3", Length: slBigPf, Straight: true},
		tm.Pf("4", slBigPf),
		tm.Pf("5", slBigPf),
	))

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
		tm.SingleLine(world, name, leg.from, leg.to, units.KM(leg.toKm-leg.fromKm)-units.M(1000), 1)
	}

	if err := world.Finalize(); err != nil {
		panic(fmt.Sprintf("BuildTpjKkdiSingleLineWorld: %v", err))
	}
	return world
}

// must is a template's two points, panicking on its error: the worlds here
// are built from fixed parameters, so an error is a bug in the builder.
func must(west, east *infra.TrackPoint, err error) (*infra.TrackPoint, *infra.TrackPoint) {
	if err != nil {
		panic(err)
	}
	return west, east
}

// title is a station code as it reads inside a name, e.g. "PDKT" -> "Pdkt".
func title(code ids.StnCode) string {
	s := strings.ToLower(string(code))
	return strings.ToUpper(s[:1]) + s[1:]
}
