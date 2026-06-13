package main

import (
	"trainapp/railway"
	"trainapp/units"
)

func buildWorld() *railway.World {

	PLATFORM_TRACK_LENGTH := 800.0
	PLATFORM_LENGTH := 600.0

	world := &railway.World{}
	world.Init()

	tpj := world.NewStation("TPJ", "Tiruchy Jn")
	pdkt := world.NewStation("PDKT", "Pudukkottai")
	kkdi := world.NewStation("KKDI", "Karaikudi Jn")

	tpjPf1S := world.NewTrackPoint("tpjPf1S").WithDeadEnd(true).ConfigureSimBoundary(true)
	tpjPf1E := world.NewTrackPoint("tpjPf1E")

	tpjPf2S := world.NewTrackPoint("tpjPf2S").WithDeadEnd(true).ConfigureSimBoundary(true)
	tpjPf2E := world.NewTrackPoint("tpjPf2E")

	tpjPf3S := world.NewTrackPoint("tpjPf3S").WithDeadEnd(true).ConfigureSimBoundary(true)
	tpjPf3E := world.NewTrackPoint("tpjPf3E")

	tpjPf1 := &railway.TrackSegment{
		Id:     "tpjPf1",
		Length: units.M(PLATFORM_TRACK_LENGTH),
	}

	tpjPf2 := &railway.TrackSegment{
		Id:     "tpjPf2",
		Length: units.M(PLATFORM_TRACK_LENGTH),
	}

	tpjPf3 := &railway.TrackSegment{
		Id:     "tpjPf3",
		Length: units.M(PLATFORM_TRACK_LENGTH),
	}

	world.TrackGraph.AddTrack(tpjPf1S, tpjPf1E, tpjPf1)
	world.TrackGraph.AddTrack(tpjPf2S, tpjPf2E, tpjPf2)
	world.TrackGraph.AddTrack(tpjPf3S, tpjPf3E, tpjPf3)

	tpj.AddPlatform(&railway.Platform{
		Track:  tpjPf1,
		PfNo:   "1",
		Length: units.M(PLATFORM_LENGTH),
	})

	tpj.AddPlatform(&railway.Platform{
		Track:  tpjPf2,
		PfNo:   "2",
		Length: units.M(PLATFORM_LENGTH),
	})

	tpj.AddPlatform(&railway.Platform{
		Track:  tpjPf3,
		PfNo:   "3",
		Length: units.M(PLATFORM_LENGTH),
	})

	pdktPf1S := world.NewTrackPoint("pdktPf1S")
	pdktPf1E := world.NewTrackPoint("pdktPf1E")

	pdktPf2S := world.NewTrackPoint("pdktPf2S")
	pdktPf2E := world.NewTrackPoint("pdktPf2E")

	pdktPf1 := railway.TrackSegment{
		Id:     "pdktPf1",
		Length: units.M(PLATFORM_TRACK_LENGTH),
	}

	pdktPf2 := railway.TrackSegment{
		Id:     "pdktPf2",
		Length: units.M(PLATFORM_TRACK_LENGTH),
	}

	pdkt.AddPlatform(&railway.Platform{
		Track:  &pdktPf1,
		Length: units.M(PLATFORM_LENGTH),
		PfNo:   "1",
	})
	pdkt.AddPlatform(&railway.Platform{
		Track:  &pdktPf2,
		Length: units.M(PLATFORM_LENGTH),
		PfNo:   "2",
	})

	kkdiPf1S := world.NewTrackPoint("kkdiPf1S")
	kkdiPf1E := world.NewTrackPoint("kkdiPf1E").WithDeadEnd(true).ConfigureSimBoundary(true)

	kkdiPf2S := world.NewTrackPoint("kkdiPf2S")
	kkdiPf2E := world.NewTrackPoint("kkdiPf2E").WithDeadEnd(true).ConfigureSimBoundary(true)

	kkdiPf1 := railway.TrackSegment{
		Id:     "kkdiPf1",
		Length: units.M(PLATFORM_TRACK_LENGTH),
	}
	kkdiPf2 := railway.TrackSegment{
		Id:     "kkdiPf2",
		Length: units.M(PLATFORM_TRACK_LENGTH),
	}

	kkdi.AddPlatform(&railway.Platform{
		PfNo:   "1",
		Track:  &kkdiPf1,
		Length: units.M(PLATFORM_LENGTH),
	})

	kkdi.AddPlatform(&railway.Platform{
		PfNo:   "2",
		Track:  &kkdiPf2,
		Length: units.M(PLATFORM_LENGTH),
	})

	world.TrackGraph.AddTrack(kkdiPf1S, kkdiPf1E, &kkdiPf1)
	world.TrackGraph.AddTrack(kkdiPf2S, kkdiPf2E, &kkdiPf2)

	kkdiSw1 := world.NewTrackPoint("kkdiSw1")
	kkdiSw1Pf1S := railway.TrackSegment{
		Id:     "kkdiSw1Pf1S",
		Length: units.M(100),
	}
	kkdiSw1Pf2S := railway.TrackSegment{
		Id:     "kkdiSw1Pf2S",
		Length: units.M(100),
	}

	world.TrackGraph.AddTrack(kkdiSw1, kkdiPf1S, &kkdiSw1Pf1S)
	world.TrackGraph.AddTrack(kkdiSw1, kkdiPf2S, &kkdiSw1Pf2S)
	world.TrackGraph.AddTrack(pdktPf1S, pdktPf1E, &pdktPf1)
	world.TrackGraph.AddTrack(pdktPf2S, pdktPf2E, &pdktPf2)

	tpjSw1 := world.NewTrackPoint("tpjSw1")
	tpjPf1ESw1 := railway.TrackSegment{
		Id:     "tpjPf1ESw1",
		Length: units.M(100),
	}
	tpjPf2ESw1 := railway.TrackSegment{
		Id:     "tpjPf2ESw1",
		Length: units.M(100),
	}

	tpjPf3ESw1 := railway.TrackSegment{
		Id:     "tpjPf3ESw1",
		Length: units.M(100),
	}

	world.TrackGraph.AddTrack(tpjPf1E, tpjSw1, &tpjPf1ESw1)
	world.TrackGraph.AddTrack(tpjPf2E, tpjSw1, &tpjPf2ESw1)
	world.TrackGraph.AddTrack(tpjPf3E, tpjSw1, &tpjPf3ESw1)

	pdktSw1 := world.NewTrackPoint("pdktSw1")
	pdktSw2 := world.NewTrackPoint("pdktSw2")

	pdktPf1SSw1 := railway.TrackSegment{
		Id:     "pdktPf1SSw1",
		Length: units.M(100),
	}

	pdktPf2SSw1 := railway.TrackSegment{
		Id:     "pdktPf2SSw1",
		Length: units.M(100),
	}

	pdktPf1ESw2 := railway.TrackSegment{
		Id:     "pdktPf1ESw2",
		Length: units.M(100),
	}
	pdktPf2ESw2 := railway.TrackSegment{
		Id:     "pdktPf2ESw2",
		Length: units.M(100),
	}

	world.TrackGraph.AddTrack(pdktPf1S, pdktSw1, &pdktPf1SSw1)
	world.TrackGraph.AddTrack(pdktPf2S, pdktSw1, &pdktPf2SSw1)

	world.TrackGraph.AddTrack(pdktPf1E, pdktSw2, &pdktPf1ESw2)
	world.TrackGraph.AddTrack(pdktPf2E, pdktSw2, &pdktPf2ESw2)

	bsecTpjPdkt := world.NewBlockSection("bsecTpjPdkt")
	bsecTpjPdkt.Init(tpj, pdkt)
	bsTpjPdkt0 := railway.TrackSegment{
		Id:     "bsTpjPdkt0",
		Length: units.KM(5),
	}
	bsTpjPdkt1 := railway.TrackSegment{
		Id:     "bsTpjPdkt1",
		Length: units.KM(16),
	}
	bsTpjPdkt2 := railway.TrackSegment{
		Id:     "bsTpjPdkt2",
		Length: units.KM(5),
	}
	krurCp1 := world.NewTrackPoint("krurCp1")
	tpjCp1 := world.NewTrackPoint("tpjCp1")
	pdktCp1 := world.NewTrackPoint("pdktCp1")

	bsTpjKrur0 := railway.TrackSegment{
		Id:     "bsTpjKrur",
		Length: units.KM(7),
	}
	bsKrurPdkt0 := railway.TrackSegment{
		Id:     "bsKrurPdkt0",
		Length: units.KM(7),
	}

	world.TrackGraph.AddTrack(tpjSw1, tpjCp1, &bsTpjPdkt0)
	world.TrackGraph.AddTrack(tpjCp1, krurCp1, &bsTpjKrur0)
	world.TrackGraph.AddTrack(krurCp1, pdktCp1, &bsKrurPdkt0)
	world.TrackGraph.AddTrack(tpjCp1, pdktCp1, &bsTpjPdkt1)
	world.TrackGraph.AddTrack(pdktCp1, pdktSw1, &bsTpjPdkt2)

	bsecPdktKkdi := world.NewBlockSection("bsecPdktKkdi")
	bsecPdktKkdi.Init(pdkt, kkdi)

	bsPdktKkdi0 := railway.TrackSegment{
		Id:     "bsPdktKkdi0",
		Length: units.KM(30),
	}
	bsecPdktKkdi.AddTrack(&bsPdktKkdi0)

	world.TrackGraph.AddTrack(pdktSw2, kkdiSw1, &bsPdktKkdi0)

	bsecTpjPdkt.AddTrack(&bsTpjPdkt0)
	bsecTpjPdkt.AddTrack(&bsTpjPdkt1)
	bsecTpjPdkt.AddTrack(&bsTpjPdkt2)

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
