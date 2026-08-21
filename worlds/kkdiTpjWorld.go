package worlds

import (
	"fmt"
	"strings"
	"trainapp/railway"
	"trainapp/units"
)

func newTrackPointId(stnCode string, tpType string, tpId string, other string) string {
	return fmt.Sprintf("%s%s%s%s", strings.ToLower(stnCode), tpType, tpId, other)
}

func newPfTrackId(stnCode string, pfNo string) string {
	return fmt.Sprintf("%sPf%s", strings.ToLower(stnCode), pfNo)
}

func constructPlatform(world *railway.World, stn *railway.Station, pfNo string, pfLen units.Meters) (*railway.TrackPoint, *railway.TrackPoint, *railway.TrackSegment) {
	stnPfS := world.NewTrackPoint(newTrackPointId(stn.Code, "Pf", pfNo, "S"))
	stnPfE := world.NewTrackPoint(newTrackPointId(stn.Code, "Pf", pfNo, "E"))

	stnPf := world.NewPlatformTrack(newPfTrackId(stn.Code, pfNo))

	world.TrackGraph.AddTrack(stnPfS, stnPfE, stnPf)
	stn.NewStationPlatform(stnPf, pfNo, pfLen)

	return stnPfS, stnPfE, stnPf

}

func pathDistance(path *railway.Path) float64 {
	dist := 0.0

	for _, edge := range path.Edges {
		dist += float64(edge.Track.Length)
	}
	return dist
}

func IsCorrectWay(world *railway.World, path *railway.Path, initPoint *railway.TrackPoint) bool {
	curPoint := initPoint
	curPtIdx := 0

	trackSigMap := make(map[string]string) // TEMP: trackId -> signalId / "" (if no sig is present)
	for _, edge := range world.TrackGraph.Edges {
		trackSigMap[edge.Track.Id] = ""
	}

	for _, sig := range world.ListSignals() {
		atPMap := world.TrackGraph.NeighborMap[sig.AtPoint.Id]
		if atPMap != nil {
			trk := atPMap[sig.FacingPoint.Id]
			trackSigMap[trk.Id] = sig.Id
		}
	}

	facesRight := true

	for {
		if curPtIdx >= len(path.Edges) {
			break
		}
		edge := path.Edges[curPtIdx]
		prevPoint := curPoint
		curPoint = world.TrackGraph.OtherEnd(edge.Track, curPoint.Id)
		sigId := trackSigMap[edge.Track.Id]
		if sigId == "" {
			curPtIdx++
			continue
		}
		sig := world.GetSignal(sigId)
		if sig == nil {
			curPtIdx++
			continue
		}
		if !sig.FacesMovement(prevPoint, curPoint) {
			facesRight = false
		}
		curPtIdx++
	}
	return facesRight
}

func constructCrossover(world *railway.World, stnCode string, a, b, c, d *railway.TrackPoint) {
	railway.NewDiamondCrossing(world, newTrackPointId(stnCode, "Dp", "0", ""), a, d, b, c)
	railway.NewSwitch(world, newTrackPointId(stnCode, "Sw", "0", "A"), a, b, d)
	railway.NewSwitch(world, newTrackPointId(stnCode, "Sw", "0", "B"), d, c, a)
	railway.NewSwitch(world, newTrackPointId(stnCode, "Sw", "1", "A"), c, d, b)
	railway.NewSwitch(world, newTrackPointId(stnCode, "Sw", "1", "B"), b, a, c)
}

func BuildTpjKkdiWorld() *railway.World {

	PLATFORM_TRACK_LENGTH := units.M(800.0)
	PLATFORM_LENGTH := units.M(600.0)
	PF_SPEED := units.KMPH(50.0)
	ROUTE_SPEED := units.KMPH(110.0)
	SWITCH_SPEED := units.KMPH(30.0)
	SWITCH_LENGTH := units.M(100.0)

	world := &railway.World{}
	world.Init(railway.WorldData{
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

	railway.NewSwitch(world, "tpjSw0", tpjBp0, tpjPf1E, tpjPf2E) // 1 & 2 conn
	railway.NewSwitch(world, "tpjSw1", tpjBp1, tpjPf3E, tpjPf4E) // 3 & 4 conn

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

	railway.NewSwitch(world, "pdktSw0", pdktBp0, pdktPf2S, pdktPf1S)
	railway.NewSwitch(world, "pdktSw1", pdktBp1, pdktPf3S, pdktPf4S)

	pdktBp2 := world.NewTrackPoint(newTrackPointId(pdkt.Code, "Bp", "2", ""))
	pdktBp3 := world.NewTrackPoint(newTrackPointId(pdkt.Code, "Bp", "3", ""))

	railway.NewSwitch(world, "pdktSw2", pdktBp2, pdktPf2E, pdktPf1E)
	railway.NewSwitch(world, "pdktSw3", pdktBp3, pdktPf3E, pdktPf4E)

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

	railway.NewSwitch(world, "kkdiSw0", kkdiBp2, kkdiPf1S, kkdiPf2S) // 1 & 2 conn
	railway.NewSwitch(world, "kkdiSw1", kkdiBp3, kkdiPf3S, kkdiPf4S) // 3 & 4 conn

	kkdiBp0 := world.NewTrackPoint(newTrackPointId(kkdi.Code, "Bp", "0", ""))
	kkdiBp1 := world.NewTrackPoint(newTrackPointId(kkdi.Code, "Bp", "1", ""))

	constructCrossover(world, kkdi.Code, kkdiBp0, kkdiBp2, kkdiBp1, kkdiBp3)

	// bsTpjKkdi0 := world.NewTrackSegment("bsTpjKkdi0", units.KM(96))
	// bsKkdiTpj0 := world.NewTrackSegment("bsKkdiTpj0", units.KM(96))

	bsTpjPdkt0 := world.NewTrackSegment("bsTpjPdkt0", units.KM(55))
	bsPdktTpj0 := world.NewTrackSegment("bsPdktTpj0", units.KM(55))
	bsPdktKkdi0 := world.NewTrackSegment("bsPdktKkdi0", units.KM(41))
	bsKkdiPdkt0 := world.NewTrackSegment("bsKkdiPdkt0", units.KM(41))

	kkdiSig0 := railway.NewSignal("kkdiSig0", kkdiBp0, pdktBp2)
	tpjSig0 := railway.NewSignal("tpjSig0", tpjBp3, pdktBp1)
	world.AddSignal(tpjSig0)
	world.AddSignal(kkdiSig0)

	pdktSig0 := railway.NewSignal("pdktSig0", pdktBp0, tpjBp2)
	pdktSig1 := railway.NewSignal("pdktSig1", pdktBp3, kkdiBp1)

	world.AddSignal(pdktSig0)
	world.AddSignal(pdktSig1)

	// world.TrackGraph.AddTrack(tpjBp2, kkdiBp0, bsTpjKkdi0)
	// world.TrackGraph.AddTrack(tpjBp3, kkdiBp1, bsKkdiTpj0)

	world.TrackGraph.AddTrack(tpjBp2, pdktBp0, bsTpjPdkt0)
	world.TrackGraph.AddTrack(tpjBp3, pdktBp1, bsPdktTpj0)

	world.TrackGraph.AddTrack(pdktBp2, kkdiBp0, bsPdktKkdi0)
	world.TrackGraph.AddTrack(pdktBp3, kkdiBp1, bsKkdiPdkt0)

	fmt.Println("World creation done!")
	// connectivity check
	world.TrackGraph.BuildCacheMap()

	// paths0, _ := world.TrackGraph.GenerateCandidatePaths(tpjPf4E, kkdiPf4)

	// for i, path := range paths0 {
	// 	fmt.Printf("Path %d: ", i)
	// 	path.PPrint()
	// 	fmt.Print("Path dist: ", pathDistance(path), "m", " - ")
	// 	fmt.Print("Is Correct Way: ", IsCorrectWay(world, path, tpjPf4E))
	// 	fmt.Printf("\n")
	// }

	// paths1, _ := world.TrackGraph.GenerateCandidatePaths(tpjPf1, kkdiPf4)
	// for i, path := range paths1 {
	// 	fmt.Printf("Path %d: ", i)
	// 	path.PPrint()
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

	// railway.NewSwitch(world, "tpjSw1A", tpjBp0, tpjPf1E, tpjPf2E)
	// railway.NewSwitch(world, "tpjSw1B", tpjBp0, tpjPf2E, tpjPf3E)

	// world.TrackGraph.AddTrack(tpjPf1S, tpjPf1E, tpjPf1)
	// world.TrackGraph.AddTrack(tpjPf2S, tpjPf2E, tpjPf2)
	// world.TrackGraph.AddTrack(tpjPf3S, tpjPf3E, tpjPf3)

	// tpj.NewStationPlatform(tpjPf1, "1", PLATFORM_LENGTH)
	// tpj.NewStationPlatform(tpjPf2, "2", PLATFORM_LENGTH)
	// tpj.NewStationPlatform(tpjPf3, "3", PLATFORM_LENGTH)

	// pdkt, pdktBp0, pdktBp1 := railway.NewStationSL2PF(world, "PDKT", "Pudukkottai")

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

	// railway.NewSwitch(world, "kkdiSw1A", kkdiBp0, kkdiPf1S, kkdiPf2S)

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
