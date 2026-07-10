package main

import (
	"trainapp/railway"
	"trainapp/units"
)

func buildWorld() *railway.World {

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
	pdkt := world.NewStation("PDKT", "Pudukkottai")
	kkdi := world.NewStation("KKDI", "Karaikudi Jn")

	tpjPf1S := world.NewTrackPoint("tpjPf1S").WithDeadEnd(true).WithSimLimit(true)
	tpjPf1E := world.NewTrackPoint("tpjPf1E")

	tpjPf2S := world.NewTrackPoint("tpjPf2S").WithDeadEnd(true).WithSimLimit(true)
	tpjPf2E := world.NewTrackPoint("tpjPf2E")

	tpjPf3S := world.NewTrackPoint("tpjPf3S").WithDeadEnd(true).WithSimLimit(true)
	tpjPf3E := world.NewTrackPoint("tpjPf3E")

	tpjPf1 := world.NewPlatformTrack("tpjPf1")
	tpjPf2 := world.NewPlatformTrack("tpjPf2")
	tpjPf3 := world.NewPlatformTrack("tpjPf3")

	world.TrackGraph.AddTrack(tpjPf1S, tpjPf1E, tpjPf1)
	world.TrackGraph.AddTrack(tpjPf2S, tpjPf2E, tpjPf2)
	world.TrackGraph.AddTrack(tpjPf3S, tpjPf3E, tpjPf3)

	tpj.NewStationPlatform(tpjPf1, "1", PLATFORM_LENGTH)
	tpj.NewStationPlatform(tpjPf2, "2", PLATFORM_LENGTH)
	tpj.NewStationPlatform(tpjPf3, "3", PLATFORM_LENGTH)

	pdktPf1S := world.NewTrackPoint("pdktPf1S")
	pdktPf1E := world.NewTrackPoint("pdktPf1E")

	pdktPf2S := world.NewTrackPoint("pdktPf2S")
	pdktPf2E := world.NewTrackPoint("pdktPf2E")

	pdktPf1 := world.NewPlatformTrack("pdktPf1")
	pdktPf2 := world.NewPlatformTrack("pdktPf2")

	pdkt.NewStationPlatform(pdktPf1, "1", PLATFORM_LENGTH)
	pdkt.NewStationPlatform(pdktPf2, "2", PLATFORM_LENGTH)

	kkdiPf1S := world.NewTrackPoint("kkdiPf1S")
	kkdiPf1E := world.NewTrackPoint("kkdiPf1E").WithDeadEnd(true).WithSimLimit(true)

	kkdiPf2S := world.NewTrackPoint("kkdiPf2S")
	kkdiPf2E := world.NewTrackPoint("kkdiPf2E").WithDeadEnd(true).WithSimLimit(true)

	kkdiPf1 := world.NewPlatformTrack("kkdiPf1")
	kkdiPf2 := world.NewPlatformTrack("kkdiPf2")

	kkdi.NewStationPlatform(kkdiPf1, "1", PLATFORM_LENGTH)
	kkdi.NewStationPlatform(kkdiPf2, "2", PLATFORM_LENGTH)

	world.TrackGraph.AddTrack(kkdiPf1S, kkdiPf1E, kkdiPf1)
	world.TrackGraph.AddTrack(kkdiPf2S, kkdiPf2E, kkdiPf2)

	kkdiSw1 := world.NewTrackPoint("kkdiSw1")

	kkdiSw1Pf1S := world.NewSwitchTrack("kkdiSw1Pf1S")
	kkdiSw1Pf2S := world.NewSwitchTrack("kkdiSw1Pf2S")

	world.TrackGraph.AddTrack(kkdiSw1, kkdiPf1S, kkdiSw1Pf1S)
	world.TrackGraph.AddTrack(kkdiSw1, kkdiPf2S, kkdiSw1Pf2S)
	world.TrackGraph.AddTrack(pdktPf1S, pdktPf1E, pdktPf1)
	world.TrackGraph.AddTrack(pdktPf2S, pdktPf2E, pdktPf2)

	tpjSw1 := world.NewTrackPoint("tpjSw1")
	tpjPf1ESw1 := world.NewSwitchTrack("tpjPf1ESw1")
	tpjPf2ESw1 := world.NewSwitchTrack("tpjPf2ESw1")
	tpjPf3ESw1 := world.NewSwitchTrack("tpjPf3ESw1")

	world.TrackGraph.AddTrack(tpjPf1E, tpjSw1, tpjPf1ESw1)
	world.TrackGraph.AddTrack(tpjPf2E, tpjSw1, tpjPf2ESw1)
	world.TrackGraph.AddTrack(tpjPf3E, tpjSw1, tpjPf3ESw1)

	pdktSw1 := world.NewTrackPoint("pdktSw1")
	pdktSw2 := world.NewTrackPoint("pdktSw2")

	pdktPf1SSw1 := world.NewSwitchTrack("pdktPf1SSw1")
	pdktPf1ESw2 := world.NewSwitchTrack("pdktPf1ESw2")
	pdktPf2SSw1 := world.NewSwitchTrack("pdktPf2SSw1")
	pdktPf2ESw2 := world.NewSwitchTrack("pdktPf2ESw2")

	world.TrackGraph.AddTrack(pdktPf1S, pdktSw1, pdktPf1SSw1)
	world.TrackGraph.AddTrack(pdktPf2S, pdktSw1, pdktPf2SSw1)

	world.TrackGraph.AddTrack(pdktPf1E, pdktSw2, pdktPf1ESw2)
	world.TrackGraph.AddTrack(pdktPf2E, pdktSw2, pdktPf2ESw2)

	bsecTpjPdkt := world.NewBlockSection("bsecTpjPdkt")
	bsecTpjPdkt.Init(tpj, pdkt)

	bsTpjPdkt0 := world.NewTrackSegment("bsTpjPdkt0", units.KM(5))
	bsTpjPdkt1 := world.NewTrackSegment("bsTpjPdkt1", units.KM(16))
	bsTpjPdkt2 := world.NewTrackSegment("bsTpjPdkt2", units.KM(5))

	krurCp1 := world.NewTrackPoint("krurCp1")
	tpjCp1 := world.NewTrackPoint("tpjCp1")
	pdktCp1 := world.NewTrackPoint("pdktCp1")

	bsTpjKrur0 := world.NewTrackSegment("bsTpjKrur0", units.KM(7))
	bsKrurPdkt0 := world.NewTrackSegment("bsKrurPdkt0", units.KM(7))

	world.TrackGraph.AddTrack(tpjSw1, tpjCp1, bsTpjPdkt0)
	world.TrackGraph.AddTrack(tpjCp1, krurCp1, bsTpjKrur0)
	world.TrackGraph.AddTrack(krurCp1, pdktCp1, bsKrurPdkt0)
	world.TrackGraph.AddTrack(tpjCp1, pdktCp1, bsTpjPdkt1)
	world.TrackGraph.AddTrack(pdktCp1, pdktSw1, bsTpjPdkt2)

	bsecPdktKkdi := world.NewBlockSection("bsecPdktKkdi")
	bsecPdktKkdi.Init(pdkt, kkdi)

	bsPdktKkdi0 := world.NewTrackSegment("bsPdktKkdi0", units.KM(30))
	bsecPdktKkdi.AddTrack(bsPdktKkdi0)

	world.TrackGraph.AddTrack(pdktSw2, kkdiSw1, bsPdktKkdi0)

	bsecTpjPdkt.AddTrack(bsTpjPdkt0)
	bsecTpjPdkt.AddTrack(bsTpjPdkt1)
	bsecTpjPdkt.AddTrack(bsTpjPdkt2)

	return world
}

func main() {

	sim := railway.Sim{}

	world := buildWorld()
	sim.SetWorld(world)
	// this is a temp call -> TODO: Move it to Graph.FindPath() call instead or something
	world.TrackGraph.BuildCacheMap()

	// path := world.TrackGraph.FindPathToTrack(tpjPf1E, &pdktPf1)
	// for i, edge := range path.Edges {
	// 	fmt.Printf("%d. %s -> %s (%s)\n", i+1, edge.From.Id, edge.To.Id, edge.Track.Id)
	// }

	tpj, ok := world.GetStation("TPJ")
	pdkt, ok := world.GetStation("PDKT")
	kkdi, ok := world.GetStation("KKDI")

	if !ok {
		panic("Something went wrong")
	}

	train1 := railway.Train{
		Name:     "Train1Up",
		Number:   "0456U",
		MaxSpeed: units.KMPH(110),
	}

	train1.AddSchedule(&railway.SchedulePoint{
		StnCode:  "TPJ",
		ArrTime:  10,
		DeptTime: 20,
		SpPfNo:   "1",
	})

	train1.AddSchedule(&railway.SchedulePoint{
		StnCode:  pdkt.Code,
		ArrTime:  30,
		DeptTime: 40,
		SpPfNo:   "1",
	})

	train1.AddSchedule(&railway.SchedulePoint{
		StnCode:  kkdi.Code,
		ArrTime:  60,
		DeptTime: 70,
		SpPfNo:   "1",
	})

	train2 := railway.Train{
		Name:     "Train2Down",
		Number:   "0457D",
		MaxSpeed: units.KMPH(110),
	}

	train2.AddSchedule(&railway.SchedulePoint{
		StnCode:  kkdi.Code,
		ArrTime:  30,
		DeptTime: 40,
		SpPfNo:   "1",
	})

	train2.AddSchedule(&railway.SchedulePoint{
		StnCode:  pdkt.Code,
		ArrTime:  70,
		DeptTime: 80,
		SpPfNo:   "2",
	})

	train2.AddSchedule(&railway.SchedulePoint{
		StnCode:  tpj.Code,
		ArrTime:  90,
		DeptTime: 100,
		SpPfNo:   "1",
	})

	world.AddTrain(&train1)
	world.AddTrain(&train2)
	sim.Init()
	sim.Run()

}
