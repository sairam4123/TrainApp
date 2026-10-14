package worlds

import (
	"fmt"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
)

// Building blocks for single-line worlds. Points are named
// {station, type, seq, other} like the rest of the worlds; signals and
// switches use the same scheme.

func defaultData() infra.WorldData {
	return infra.WorldData{
		DefaultSwMaxSpeed: units.KMPH(30),
		DefaultPfMaxSpeed: units.KMPH(50),
		DefaultSwLength:   units.M(100),
		DefaultPfLength:   units.M(800),
		DefaultRouteSpeed: units.KMPH(100),
	}
}

func newWorld() *infra.World {
	world := &infra.World{}
	world.Init(defaultData())
	return world
}

// platforms builds n parallel platform tracks for a station and joins their
// two ends into single throat points with chains of switches. It returns the
// west and east throat points. Each platform is pfS (west) -> pfE (east).
func platforms(world *infra.World, stn *infra.Station, n int) (west, east *infra.TrackPoint, pfS, pfE []*infra.TrackPoint) {
	for i := range n {
		no := fmt.Sprint(i + 1)
		s, e, _ := constructPlatform(world, stn, no, 600)
		pfS = append(pfS, s)
		pfE = append(pfE, e)
	}
	west = throat(world, stn.Code, "W", pfS)
	east = throat(world, stn.Code, "E", pfE)
	return west, east, pfS, pfE
}

// throat joins the given platform ends into one point through a ladder of
// switches, returning that point. A single end is its own throat.
func throat(world *infra.World, code ids.StnCode, side string, ends []*infra.TrackPoint) *infra.TrackPoint {
	cur := ends[0]
	for i, next := range ends[1:] {
		toe := world.NewTrackPoint(newTrackPointId(code, "Th"+side, fmt.Sprint(i), ""))
		infra.NewSwitch(world, newSwitchId(code, "Sw"+side, fmt.Sprint(i), ""), toe, cur, next)
		cur = toe
	}
	return cur
}

// singleLine joins two points with a single bidirectional line of the given
// length, split into blocks equal blocks. There is a signal for each
// direction at every block boundary and at both ends, so each block is a
// signal section either way.
func singleLine(world *infra.World, name string, from, to *infra.TrackPoint, length units.Meters, blocks int) {
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

// westTerminus builds a terminus whose platforms end at the world boundary on
// the west; the line leaves eastwards from the returned point.
func westTerminus(world *infra.World, code ids.StnCode, name string, n int) (*infra.Station, *infra.TrackPoint) {
	stn := world.NewStation(code, name)
	_, east, pfS, _ := platforms(world, stn, n)
	for _, p := range pfS {
		p.WithDeadEnd(true).WithSimLimit(true)
	}
	return stn, east
}

// eastTerminus is the mirror image of westTerminus.
func eastTerminus(world *infra.World, code ids.StnCode, name string, n int) (*infra.Station, *infra.TrackPoint) {
	stn := world.NewStation(code, name)
	west, _, _, pfE := platforms(world, stn, n)
	for _, p := range pfE {
		p.WithDeadEnd(true).WithSimLimit(true)
	}
	return stn, west
}

// throughStation builds a station with n platforms between a west and an east
// throat. With n >= 2 it is a passing loop.
func throughStation(world *infra.World, code ids.StnCode, name string, n int) (*infra.Station, *infra.TrackPoint, *infra.TrackPoint) {
	stn := world.NewStation(code, name)
	west, east, _, _ := platforms(world, stn, n)
	return stn, west, east
}

// SingleLineOptions sizes BuildSingleLineWorld.
type SingleLineOptions struct {
	TerminusPlatforms int // platforms at each terminus (WST and EST)
	LoopPlatforms     int // platforms at the passing loop LPA
	BlocksPerLine     int // signal sections on each stretch of line
}

// BuildSingleLineWorld builds a single line WST - LPA - HLT - EST:
//
//	WST terminus  (boundary to the west)
//	LPA through station, a passing loop when it has 2+ platforms
//	HLT halt with a single platform: trains cannot pass here
//	EST terminus  (boundary to the east)
//
// Every stretch is single track, signalled in both directions.
func BuildSingleLineWorld(opt SingleLineOptions) *infra.World {
	world := newWorld()

	_, wstE := westTerminus(world, "WST", "West Terminus", opt.TerminusPlatforms)
	_, lpaW, lpaE := throughStation(world, "LPA", "Loop A", opt.LoopPlatforms)
	_, hltW, hltE := throughStation(world, "HLT", "Halt", 1)
	_, estW := eastTerminus(world, "EST", "East Terminus", opt.TerminusPlatforms)

	singleLine(world, "slWL", wstE, lpaW, units.KM(12), opt.BlocksPerLine)
	singleLine(world, "slLH", lpaE, hltW, units.KM(8), opt.BlocksPerLine)
	singleLine(world, "slHE", hltE, estW, units.KM(10), opt.BlocksPerLine)

	if err := world.Finalize(); err != nil {
		panic(fmt.Sprintf("BuildSingleLineWorld: %v", err))
	}
	return world
}

// BuildJunctionWorld builds a single-line Y: termini WJN (west), EJN (east)
// and NJN (north branch) meet at a junction switch JCT next to a two-platform
// station MID on the west leg.
//
//	WJN ==== MID ==== JCT ==== EJN
//	                    \\
//	                     ==== NJN
//
// Trains can run WJN <-> EJN and WJN <-> NJN; EJN <-> NJN would need to
// reverse at the junction, which is not modelled.
func BuildJunctionWorld(terminusPlatforms int) *infra.World {
	world := newWorld()

	_, wjnE := westTerminus(world, "WJN", "West Junction Terminus", terminusPlatforms)
	_, midW, midE := throughStation(world, "MID", "Middle", 2)
	_, ejnW := eastTerminus(world, "EJN", "East Junction Terminus", terminusPlatforms)
	_, njnW := eastTerminus(world, "NJN", "North Junction Terminus", terminusPlatforms)

	toe := world.NewTrackPoint(ids.PointID{StnCode: "JCT", Type: "Tp"})
	east := world.NewTrackPoint(ids.PointID{StnCode: "JCT", Type: "Ea"})
	north := world.NewTrackPoint(ids.PointID{StnCode: "JCT", Type: "No"})
	infra.NewSwitch(world, "jctSw", toe, east, north)

	singleLine(world, "jlWM", wjnE, midW, units.KM(10), 2)
	singleLine(world, "jlMJ", midE, toe, units.KM(5), 1)
	singleLine(world, "jlJE", east, ejnW, units.KM(9), 2)
	singleLine(world, "jlJN", north, njnW, units.KM(7), 2)

	if err := world.Finalize(); err != nil {
		panic(fmt.Sprintf("BuildJunctionWorld: %v", err))
	}
	return world
}
