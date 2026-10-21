package worlds

import (
	"fmt"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
	tm "trainapp/worlds/templates"
)

// BuildTpjKkdiDoubleLineWorld builds an imaginary doubling of the
// Tiruchchirappalli - Karaikkudi line: the single line's stations at their
// km (BuildTpjKkdiSingleLineWorld), joined by a double line with left-hand
// running, the upper line east (TPJ to KKDI) and the lower west. E and W
// below are the east and west lines' mains:
//
//	TPJ   PF1, T9, PF2, PF3, PF4 (E), PF5 (W), PF6, PF7, St1, Sd1, Sd2, PF8
//	KRMG  PF1 (E), T3 (W), PF2 a loop off the west line
//	KRUR  PF1 a loop off the east line, T3 (E), T4 (W), PF2 a loop off the west line
//	VEL   as KRUR
//	PDKT  PF1 (E), PF2 (W), PF3 and goods loop Gd1 off the west line
//	NMN   PF1 (E), PF2 (W); no points
//	TYM   as KRUR
//	CTND  PF1 (E), PF2 (W), PF3 and goods loop Gd1 off the west line
//	KTYR  PF1 (E), PF2 (W); no points
//	KKDI  PF1, T6, PF2 (E), PF3 (W), PF4, PF5
//
// TPJ, KRMG, PDKT, CTND and KKDI have a scissors crossover between the
// lines in each throat; KRUR, VEL and TYM have none, so a train keeps to its
// own line's main and loop there. As on the single line, each stretch is one
// block section on each line, signalled the way the line runs, as long as
// the distance between the stations less 1 km.
func BuildTpjKkdiDoubleLineWorld() *infra.World {
	world := &infra.World{}
	world.Init(infra.WorldData{
		DefaultSwMaxSpeed: units.KMPH(30),
		DefaultPfMaxSpeed: units.KMPH(50),
		DefaultSwLength:   units.M(100),
		DefaultPfLength:   units.M(800),
		DefaultRouteSpeed: units.KMPH(slLineSpeed),
	})
	scissors := []string{tm.CrossScissors}
	main := func(no string, length units.Meters) tm.Track {
		return tm.Track{PfNo: no, Length: length, Straight: true, Kmph: slLineSpeed}
	}

	tpj := mustStation(world, "TPJ", "Tiruchchirappalli Jn",
		tm.Layout{Edge: "W", TurnoutKmph: slYardTurnout, EastCrossovers: scissors},
		tm.Pf("1", slBigPf),
		tm.Track{PfNo: "9"},
		tm.Pf("2", slBigPf),
		tm.Pf("3", slBigPf),
		tm.Track{PfNo: "4", Length: slBigPf, Straight: true},
		tm.Track{PfNo: "5", Length: slBigPf, Straight: true},
		tm.Pf("6", slBigPf),
		tm.Pf("7", slBigPf),
		tm.Track{Siding: "St1", SidingLength: slSidingLen},
		tm.Track{Siding: "Sd1", SidingLength: slSidingLen, Buffer: "W"},
		tm.Track{Siding: "Sd2", SidingLength: slSidingLen, Buffer: "W"},
		tm.Pf("8", slBigPf),
	)
	krmg := mustStation(world, "KRMG", "Kumaramangalam",
		tm.Layout{WestCrossovers: scissors, EastCrossovers: scissors},
		main("1", slCrossingPf),
		main("3", 0),
		tm.Pf("2", slCrossingPf),
	)
	crossing := func(code ids.StnCode, name string) station {
		return mustStation(world, code, name, tm.Layout{},
			tm.Pf("1", slCrossingPf),
			main("3", 0),
			main("4", 0),
			tm.Pf("2", slCrossingPf),
		)
	}
	krur := crossing("KRUR", "Keeranur")
	vel := crossing("VEL", "Vellanur")
	pdkt := mustStation(world, "PDKT", "Pudukkottai",
		tm.Layout{WestCrossovers: scissors, EastCrossovers: scissors},
		main("1", slBigPf),
		main("2", slBigPf),
		tm.Pf("3", slBigPf),
		tm.Track{Siding: "Gd1", SidingLength: slSidingLen},
	)
	nmn := mustStation(world, "NMN", "Namanasamudram", tm.Layout{}, main("1", slHaltPf), main("2", slHaltPf))
	tym := crossing("TYM", "Tirumayam")
	ctnd := mustStation(world, "CTND", "Chettinad",
		tm.Layout{WestCrossovers: scissors, EastCrossovers: scissors},
		main("1", slCrossingPf),
		main("2", slCrossingPf),
		tm.Pf("3", slCrossingPf),
		tm.Track{Siding: "Gd1", SidingLength: slSidingLen},
	)
	ktyr := mustStation(world, "KTYR", "Kottaiyur", tm.Layout{}, main("1", slHaltPf), main("2", slHaltPf))
	kkdi := mustStation(world, "KKDI", "Karaikkudi Jn",
		tm.Layout{Edge: "E", TurnoutKmph: slYardTurnout, WestCrossovers: scissors},
		tm.Pf("1", slBigPf),
		tm.Track{PfNo: "6"},
		tm.Track{PfNo: "2", Length: slBigPf, Straight: true},
		tm.Track{PfNo: "3", Length: slBigPf, Straight: true},
		tm.Pf("4", slBigPf),
		tm.Pf("5", slBigPf),
	)

	for _, leg := range []struct {
		from, to     station
		fromKm, toKm float64
	}{
		{tpj, krmg, 0, 12.94},
		{krmg, krur, 12.94, 28.37},
		{krur, vel, 28.37, 41.41},
		{vel, pdkt, 41.41, 52.56},
		{pdkt, nmn, 52.56, 58.57},
		{nmn, tym, 58.57, 68.55},
		{tym, ctnd, 68.55, 77.66},
		{ctnd, ktyr, 77.66, 83.06},
		{ktyr, kkdi, 83.06, 89.41},
	} {
		name := "dl" + title(leg.from.code) + title(leg.to.code)
		tm.DoubleLine(world, name, leg.from.east, leg.to.west, units.KM(leg.toKm-leg.fromKm)-units.M(1000), 1)
	}

	if err := world.Finalize(); err != nil {
		panic(fmt.Sprintf("BuildTpjKkdiDoubleLineWorld: %v", err))
	}
	return world
}

// station is a station on the double line: its code and its throats' line
// points, the east line's first.
type station struct {
	code       ids.StnCode
	west, east [2]*infra.TrackPoint
}

// mustStation builds a station on two lines, panicking on an error: the
// worlds here are built from fixed parameters, so an error is a bug in the
// builder.
func mustStation(world *infra.World, code ids.StnCode, name string, layout tm.Layout, tracks ...tm.Track) station {
	west, east, err := tm.BuildStation(world, code, name, layout, tracks...)
	if err != nil {
		panic(err)
	}
	s := station{code: code}
	for _, side := range []struct {
		pts []*infra.TrackPoint
		to  *[2]*infra.TrackPoint
	}{{west, &s.west}, {east, &s.east}} {
		switch len(side.pts) {
		case 0: // the world's edge
		case 2:
			*side.to = [2]*infra.TrackPoint(side.pts)
		default:
			panic(fmt.Sprintf("station %s: %d line points in a throat, want 2", code, len(side.pts)))
		}
	}
	return s
}
