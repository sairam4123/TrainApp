package worlds

import (
	"fmt"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
)

func newTrackPointId(stnCode ids.StnCode, tpType string, tpId string, other string) ids.PointID {
	return ids.PointID{StnCode: stnCode, Type: tpType, Seq: tpId, Other: other}
}

// newSwitchId names a switch block with the same scheme as track points, e.g. "tpjSw0A".
func newSwitchId(stnCode ids.StnCode, swType string, swId string, other string) string {
	return newTrackPointId(stnCode, swType, swId, other).String()
}

func newPfTrackId(stnCode ids.StnCode, pfNo string) string {
	return fmt.Sprintf("%sPf%s", strings.ToLower(string(stnCode)), pfNo)
}

func constructPlatform(world *infra.World, stn *infra.Station, pfNo string, pfLen units.Meters) (*infra.TrackPoint, *infra.TrackPoint, *infra.TrackSegment) {
	stnPfS := world.NewTrackPoint(newTrackPointId(stn.Code, "Pf", pfNo, "S"))
	stnPfE := world.NewTrackPoint(newTrackPointId(stn.Code, "Pf", pfNo, "E"))

	stnPf := world.NewPlatformTrack(newPfTrackId(stn.Code, pfNo))

	world.TrackGraph.AddTrack(stnPfS, stnPfE, stnPf)
	stn.NewStationPlatform(stnPf, pfNo, pfLen)

	return stnPfS, stnPfE, stnPf

}

// constructCrossover builds a scissors crossover between two parallel lines
// a -> b and c -> d, where a and c are the west ends and c-d is one lane below
// a-b.
func constructCrossover(world *infra.World, stnCode ids.StnCode, a, b, c, d *infra.TrackPoint) {
	infra.NewDiamondCrossing(world, newSwitchId(stnCode, "Dp", "0", ""), a, d, c, b, 1)
	infra.NewSwitch(world, newSwitchId(stnCode, "Sw", "0", "A"), a, b, d, infra.SwitchLayout{Legs: infra.East, Lanes: 1})
	infra.NewSwitch(world, newSwitchId(stnCode, "Sw", "0", "B"), d, c, a, infra.SwitchLayout{Legs: infra.West, Lanes: -1})
	infra.NewSwitch(world, newSwitchId(stnCode, "Sw", "1", "A"), c, d, b, infra.SwitchLayout{Legs: infra.East, Lanes: -1})
	infra.NewSwitch(world, newSwitchId(stnCode, "Sw", "1", "B"), b, a, c, infra.SwitchLayout{Legs: infra.West, Lanes: 1})
}

func BuildTpjKkdiWorld() *infra.World {

	PLATFORM_TRACK_LENGTH := units.M(800.0)
	PLATFORM_LENGTH := units.M(600.0)
	PF_SPEED := units.KMPH(50.0)
	ROUTE_SPEED := units.KMPH(110.0)
	SWITCH_SPEED := units.KMPH(30.0)
	SWITCH_LENGTH := units.M(100.0)

	world := &infra.World{}
	world.Init(infra.WorldData{
		DefaultSwMaxSpeed: SWITCH_SPEED,
		DefaultPfMaxSpeed: PF_SPEED,
		DefaultSwLength:   SWITCH_LENGTH,
		DefaultPfLength:   PLATFORM_TRACK_LENGTH,
		DefaultRouteSpeed: ROUTE_SPEED,
	})

	tpj := world.NewStation("TPJ", "Tiruchy Jn")
	tpjPf1S, tpjPf1E, _ := constructPlatform(world, tpj, "1", PLATFORM_LENGTH)
	tpjPf2S, tpjPf2E, _ := constructPlatform(world, tpj, "2", PLATFORM_LENGTH)
	tpjPf3S, tpjPf3E, _ := constructPlatform(world, tpj, "3", PLATFORM_LENGTH)
	tpjPf4S, tpjPf4E, _ := constructPlatform(world, tpj, "4", PLATFORM_LENGTH)

	tpjPf1S.WithDeadEnd(true).WithSimLimit(true)
	tpjPf2S.WithDeadEnd(true).WithSimLimit(true)
	tpjPf3S.WithDeadEnd(true).WithSimLimit(true)
	tpjPf4S.WithDeadEnd(true).WithSimLimit(true)

	tpjBp0 := world.NewTrackPoint(newTrackPointId(tpj.Code, "Bp", "0", ""))
	tpjBp1 := world.NewTrackPoint(newTrackPointId(tpj.Code, "Bp", "1", ""))

	infra.NewSwitch(world, "tpjSw0", tpjBp0, tpjPf2E, tpjPf1E, infra.SwitchLayout{Legs: infra.West, Lanes: -1}) // 1 & 2 conn
	infra.NewSwitch(world, "tpjSw1", tpjBp1, tpjPf3E, tpjPf4E, infra.SwitchLayout{Legs: infra.West, Lanes: 1})  // 3 & 4 conn

	tpjBp2 := world.NewTrackPoint(newTrackPointId(tpj.Code, "Bp", "2", ""))
	tpjBp3 := world.NewTrackPoint(newTrackPointId(tpj.Code, "Bp", "3", ""))

	constructCrossover(world, tpj.Code, tpjBp0, tpjBp2, tpjBp1, tpjBp3)

	pdkt := world.NewStation("PDKT", "Pudukkottai")
	pdktPf1S, pdktPf1E, _ := constructPlatform(world, pdkt, "1", PLATFORM_LENGTH)
	pdktPf2S, pdktPf2E, _ := constructPlatform(world, pdkt, "2", PLATFORM_LENGTH)
	pdktPf3S, pdktPf3E, _ := constructPlatform(world, pdkt, "3", PLATFORM_LENGTH)
	pdktPf4S, pdktPf4E, _ := constructPlatform(world, pdkt, "4", PLATFORM_LENGTH)

	pdktBp0 := world.NewTrackPoint(newTrackPointId(pdkt.Code, "Bp", "0", ""))
	pdktBp1 := world.NewTrackPoint(newTrackPointId(pdkt.Code, "Bp", "1", ""))

	infra.NewSwitch(world, "pdktSw0", pdktBp0, pdktPf2S, pdktPf1S, infra.SwitchLayout{Legs: infra.East, Lanes: -1})
	infra.NewSwitch(world, "pdktSw1", pdktBp1, pdktPf3S, pdktPf4S, infra.SwitchLayout{Legs: infra.East, Lanes: 1})

	pdktBp2 := world.NewTrackPoint(newTrackPointId(pdkt.Code, "Bp", "2", ""))
	pdktBp3 := world.NewTrackPoint(newTrackPointId(pdkt.Code, "Bp", "3", ""))

	infra.NewSwitch(world, "pdktSw2", pdktBp2, pdktPf2E, pdktPf1E, infra.SwitchLayout{Legs: infra.West, Lanes: -1})
	infra.NewSwitch(world, "pdktSw3", pdktBp3, pdktPf3E, pdktPf4E, infra.SwitchLayout{Legs: infra.West, Lanes: 1})

	kkdi := world.NewStation("KKDI", "Karaikudi Jn")

	kkdiPf1S, kkdiPf1E, _ := constructPlatform(world, kkdi, "1", PLATFORM_LENGTH)
	kkdiPf2S, kkdiPf2E, _ := constructPlatform(world, kkdi, "2", PLATFORM_LENGTH)
	kkdiPf3S, kkdiPf3E, _ := constructPlatform(world, kkdi, "3", PLATFORM_LENGTH)
	kkdiPf4S, kkdiPf4E, _ := constructPlatform(world, kkdi, "4", PLATFORM_LENGTH)

	kkdiPf1E.WithDeadEnd(true).WithSimLimit(true)
	kkdiPf2E.WithDeadEnd(true).WithSimLimit(true)
	kkdiPf3E.WithDeadEnd(true).WithSimLimit(true)
	kkdiPf4E.WithDeadEnd(true).WithSimLimit(true)

	kkdiBp2 := world.NewTrackPoint(newTrackPointId(kkdi.Code, "Bp", "2", ""))
	kkdiBp3 := world.NewTrackPoint(newTrackPointId(kkdi.Code, "Bp", "3", ""))

	infra.NewSwitch(world, "kkdiSw0", kkdiBp2, kkdiPf2S, kkdiPf1S, infra.SwitchLayout{Legs: infra.East, Lanes: -1}) // 1 & 2 conn
	infra.NewSwitch(world, "kkdiSw1", kkdiBp3, kkdiPf3S, kkdiPf4S, infra.SwitchLayout{Legs: infra.East, Lanes: 1})  // 3 & 4 conn

	kkdiBp0 := world.NewTrackPoint(newTrackPointId(kkdi.Code, "Bp", "0", ""))
	kkdiBp1 := world.NewTrackPoint(newTrackPointId(kkdi.Code, "Bp", "1", ""))

	constructCrossover(world, kkdi.Code, kkdiBp0, kkdiBp2, kkdiBp1, kkdiBp3)

	// bsTpjKkdi0 := world.NewTrackSegment("bsTpjKkdi0", units.KM(96))
	// bsKkdiTpj0 := world.NewTrackSegment("bsKkdiTpj0", units.KM(96))

	bsTpjPdkt0 := world.NewTrackSegment("bsTpjPdkt0", units.KM(55))
	bsPdktTpj0 := world.NewTrackSegment("bsPdktTpj0", units.KM(55))
	bsPdktKkdi0 := world.NewTrackSegment("bsPdktKkdi0", units.KM(41))
	bsKkdiPdkt0 := world.NewTrackSegment("bsKkdiPdkt0", units.KM(41))

	kkdiSig0 := infra.NewSignal("kkdiSig0", kkdiBp0, pdktBp2)
	tpjSig0 := infra.NewSignal("tpjSig0", tpjBp3, pdktBp1)
	world.AddSignal(tpjSig0)
	world.AddSignal(kkdiSig0)

	pdktSig0 := infra.NewSignal("pdktSig0", pdktBp0, tpjBp2)
	pdktSig1 := infra.NewSignal("pdktSig1", pdktBp3, kkdiBp1)

	world.AddSignal(pdktSig0)
	world.AddSignal(pdktSig1)

	// world.TrackGraph.AddTrack(tpjBp2, kkdiBp0, bsTpjKkdi0)
	// world.TrackGraph.AddTrack(tpjBp3, kkdiBp1, bsKkdiTpj0)

	world.TrackGraph.AddTrack(tpjBp2, pdktBp0, bsTpjPdkt0)
	world.TrackGraph.AddTrack(tpjBp3, pdktBp1, bsPdktTpj0)

	world.TrackGraph.AddTrack(pdktBp2, kkdiBp0, bsPdktKkdi0)
	world.TrackGraph.AddTrack(pdktBp3, kkdiBp1, bsKkdiPdkt0)

	if err := world.Finalize(); err != nil {
		panic(fmt.Sprintf("BuildTpjKkdiWorld: %v", err))
	}
	// connectivity check

	// paths0, _ := world.RankedPaths(tpjPf4E, nil, kkdiPf4)

	// for i, p := range paths0 {
	// 	fmt.Printf("Path %d: %s - dist %.0fm - correct way %v\n", i, p.Path, float64(p.Path.Length()), p.FollowsSignals)
	// }

	// tpjPf1S := world.NewTrackPoint("tpjPf1S").WithDeadEnd(true).WithSimLimit(true)
	// tpjPf1E := world.NewTrackPoint("tpjPf1E")

	// tpjPf2S := world.NewTrackPoint("tpjPf2S").WithDeadEnd(true).WithSimLimit(true)
	// tpjPf2E := world.NewTrackPoint("tpjPf2E")

	// tpjPf3S := world.NewTrackPoint("tpjPf3S").WithDeadEnd(true).WithSimLimit(true)
	// tpjPf3E := world.NewTrackPoint("tpjPf3E")

	// tpjPf1 := world.NewPlatformTrack("tpjPf1")
	// tpjPf2 := world.NewPlatformTrack("tpjPf2")
	// tpjPf3 := world.NewPlatformTrack("tpjPf3")

	// tpjBp0 := world.NewTrackPoint("tpjBp0")

	// infra.NewSwitch(world, "tpjSw1A", tpjBp0, tpjPf1E, tpjPf2E)
	// infra.NewSwitch(world, "tpjSw1B", tpjBp0, tpjPf2E, tpjPf3E)

	// world.TrackGraph.AddTrack(tpjPf1S, tpjPf1E, tpjPf1)
	// world.TrackGraph.AddTrack(tpjPf2S, tpjPf2E, tpjPf2)
	// world.TrackGraph.AddTrack(tpjPf3S, tpjPf3E, tpjPf3)

	// tpj.NewStationPlatform(tpjPf1, "1", PLATFORM_LENGTH)
	// tpj.NewStationPlatform(tpjPf2, "2", PLATFORM_LENGTH)
	// tpj.NewStationPlatform(tpjPf3, "3", PLATFORM_LENGTH)

	// pdkt, pdktBp0, pdktBp1 := infra.NewStationSL2PF(world, "PDKT", "Pudukkottai")

	// kkdiPf1S := world.NewTrackPoint("kkdiPf1S")
	// kkdiPf1E := world.NewTrackPoint("kkdiPf1E").WithDeadEnd(true).WithSimLimit(true)

	// kkdiPf2S := world.NewTrackPoint("kkdiPf2S")
	// kkdiPf2E := world.NewTrackPoint("kkdiPf2E").WithDeadEnd(true).WithSimLimit(true)

	// kkdiPf1 := world.NewPlatformTrack("kkdiPf1")
	// kkdiPf2 := world.NewPlatformTrack("kkdiPf2")

	// kkdi.NewStationPlatform(kkdiPf1, "1", PLATFORM_LENGTH)
	// kkdi.NewStationPlatform(kkdiPf2, "2", PLATFORM_LENGTH)

	// world.TrackGraph.AddTrack(kkdiPf1S, kkdiPf1E, kkdiPf1)
	// world.TrackGraph.AddTrack(kkdiPf2S, kkdiPf2E, kkdiPf2)

	// kkdiBp0 := world.NewTrackPoint("kkdiBp0")

	// infra.NewSwitch(world, "kkdiSw1A", kkdiBp0, kkdiPf1S, kkdiPf2S)

	// // kkdiSw1Pf1S := world.NewSwitchTrack("kkdiSw1Pf1S")
	// // kkdiSw1Pf2S := world.NewSwitchTrack("kkdiSw1Pf2S")

	// // world.TrackGraph.AddTrack(kkdiBp0, kkdiPf1S, kkdiSw1Pf1S)
	// // world.TrackGraph.AddTrack(kkdiBp0, kkdiPf2S, kkdiSw1Pf2S)

	// bsecTpjPdkt := world.NewBlockSection("bsecTpjPdkt")
	// bsecTpjPdkt.Init(tpj, pdkt)

	// bsTpjPdkt0 := world.NewTrackSegment("bsTpjPdkt0", units.KM(5))
	// bsTpjPdkt1 := world.NewTrackSegment("bsTpjPdkt1", units.KM(16))
	// bsTpjPdkt2 := world.NewTrackSegment("bsTpjPdkt2", units.KM(5))

	// krurCp1 := world.NewTrackPoint("krurCp1")
	// tpjCp1 := world.NewTrackPoint("tpjCp1")
	// pdktCp1 := world.NewTrackPoint("pdktCp1")

	// bsTpjKrur0 := world.NewTrackSegment("bsTpjKrur0", units.KM(7))
	// bsKrurPdkt0 := world.NewTrackSegment("bsKrurPdkt0", units.KM(7))

	// world.TrackGraph.AddTrack(tpjBp0, tpjCp1, bsTpjPdkt0)
	// world.TrackGraph.AddTrack(tpjCp1, krurCp1, bsTpjKrur0)
	// world.TrackGraph.AddTrack(krurCp1, pdktCp1, bsKrurPdkt0)
	// world.TrackGraph.AddTrack(tpjCp1, pdktCp1, bsTpjPdkt1)
	// world.TrackGraph.AddTrack(pdktCp1, pdktBp0, bsTpjPdkt2)

	// bsecPdktKkdi := world.NewBlockSection("bsecPdktKkdi")
	// bsecPdktKkdi.Init(pdkt, kkdi)

	// bsPdktKkdi0 := world.NewTrackSegment("bsPdktKkdi0", units.KM(30))
	// bsecPdktKkdi.AddTrack(bsPdktKkdi0)

	// world.TrackGraph.AddTrack(pdktBp1, kkdiBp0, bsPdktKkdi0)

	// bsecTpjPdkt.AddTrack(bsTpjPdkt0)
	// bsecTpjPdkt.AddTrack(bsTpjPdkt1)
	// bsecTpjPdkt.AddTrack(bsTpjPdkt2)

	return world
}
