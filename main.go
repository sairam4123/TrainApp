package main

import (
	"fmt"
	"trainapp/railway"
	"trainapp/units"
	"trainapp/worlds"
)

// func main() {

// 	sim := railway.Sim{}

// 	world := worlds.BuildTestWorld()
// 	sim.SetWorld(world)
// 	// this is a temp call -> TODO: Move it to Graph.FindPath() call instead or something
// 	world.TrackGraph.BuildCacheMap()

// 	tpj, ok := world.GetStation("TPJ")

// 	if !ok {
// 		panic("Something went wrong while fetching stations")
// 	}

// 	pdkt, ok := world.GetStation("PDKT")

// 	if !ok {
// 		panic("Something went wrong while fetching stations")
// 	}

// 	kkdi, ok := world.GetStation("KKDI")

// 	if !ok {
// 		panic("Something went wrong while fetching stations")
// 	}

// 	var upInterval = 40
// 	var downInterval = 40

// 	for i := range 6 {
// 		train1 := railway.Train{
// 			Name:     fmt.Sprintf("Train%dUp", i+1),
// 			Number:   fmt.Sprintf("045%dU", i+1),
// 			MaxSpeed: units.KMPH(110),
// 		}

// 		train1.AddSchedule(&railway.SchedulePoint{
// 			StnCode:  tpj.Code,
// 			ArrTime:  float64(10 + i*upInterval),
// 			DeptTime: float64(20 + i*upInterval),
// 			SpPfNo:   "1",
// 		})

// 		train1.AddSchedule(&railway.SchedulePoint{
// 			StnCode:  pdkt.Code,
// 			ArrTime:  float64(30 + i*upInterval),
// 			DeptTime: float64(40 + i*upInterval),
// 			SpPfNo:   "1",
// 		})

// 		train1.AddSchedule(&railway.SchedulePoint{
// 			StnCode:  kkdi.Code,
// 			ArrTime:  float64(60 + i*upInterval),
// 			DeptTime: float64(70 + i*upInterval),
// 			SpPfNo:   "1",
// 		})

// 		train2 := railway.Train{
// 			Name:     fmt.Sprintf("Train%dDown", i+1),
// 			Number:   fmt.Sprintf("045%dD", i+1),
// 			MaxSpeed: units.KMPH(110),
// 		}

// 		train2.AddSchedule(&railway.SchedulePoint{
// 			StnCode:  kkdi.Code,
// 			ArrTime:  float64(30 + i*downInterval),
// 			DeptTime: float64(40 + i*downInterval),
// 			SpPfNo:   "1",
// 		})

// 		train2.AddSchedule(&railway.SchedulePoint{
// 			StnCode:  pdkt.Code,
// 			ArrTime:  float64(70 + i*downInterval),
// 			DeptTime: float64(80 + i*downInterval),
// 			SpPfNo:   "2",
// 		})

// 		train2.AddSchedule(&railway.SchedulePoint{
// 			StnCode:  tpj.Code,
// 			ArrTime:  float64(90 + i*25),
// 			DeptTime: float64(100 + i*25),
// 			SpPfNo:   "1",
// 		})

// 		world.AddTrain(&train1)
// 		// if i%2 == 0 {
// 		world.AddTrain(&train2)
// 		// }
// 	}
// 	sim.Init()
// 	sim.Run()
// }

func main() {
	sim := railway.Sim{}
	world := worlds.BuildTpjKkdiWorld()
	sim.SetWorld(world)

	tpj, ok := world.GetStation("TPJ")

	if !ok {
		panic("Something went wrong while fetching stations")
	}

	pdkt, ok := world.GetStation("PDKT")

	if !ok {
		panic("Something went wrong while fetching stations")
	}

	kkdi, ok := world.GetStation("KKDI")

	if !ok {
		panic("Something went wrong while fetching stations")
	}

	var upInterval = 40
	var downInterval = 40

	for i := range 2 {
		train1 := railway.Train{
			Name:     fmt.Sprintf("Train%dUp", i+1),
			Number:   fmt.Sprintf("045%dU", i+1),
			MaxSpeed: units.KMPH(110),
		}

		train1.AddSchedule(&railway.SchedulePoint{
			StnCode:  tpj.Code,
			ArrTime:  float64(10 + i*upInterval),
			DeptTime: float64(20 + i*upInterval),
			SpPfNo:   "1",
		})

		train1.AddSchedule(&railway.SchedulePoint{
			StnCode:  pdkt.Code,
			ArrTime:  float64(60 + i*upInterval),
			DeptTime: float64(65 + i*upInterval),
			SpPfNo:   "1",
		})

		train1.AddSchedule(&railway.SchedulePoint{
			StnCode:  kkdi.Code,
			ArrTime:  float64(95 + i*upInterval),
			DeptTime: float64(100 + i*upInterval),
			SpPfNo:   "1",
		})

		train2 := railway.Train{
			Name:     fmt.Sprintf("Train%dDown", i+1),
			Number:   fmt.Sprintf("045%dD", i+1),
			MaxSpeed: units.KMPH(110),
		}

		train2.AddSchedule(&railway.SchedulePoint{
			StnCode:  kkdi.Code,
			ArrTime:  float64(10 + i*downInterval),
			DeptTime: float64(20 + i*downInterval),
			SpPfNo:   "2",
		})

		train2.AddSchedule(&railway.SchedulePoint{
			StnCode:  pdkt.Code,
			ArrTime:  float64(45 + i*downInterval),
			DeptTime: float64(50 + i*downInterval),
			SpPfNo:   "3",
		})

		train2.AddSchedule(&railway.SchedulePoint{
			StnCode:  tpj.Code,
			ArrTime:  float64(60 + i*25),
			DeptTime: float64(70 + i*25),
			SpPfNo:   "1",
		})

		world.AddTrain(&train1)
		// if i%2 == 0 {
		world.AddTrain(&train2)
		// }
	}

	sim.Init()
	sim.Run()

}
