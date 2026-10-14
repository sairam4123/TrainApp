package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/visualization/record"
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
	world := worlds.BuildTpjKkdiWorld()
	logFormat := flag.String("log", string(sim.LogText), "log format: text (logfmt) or json (one object per line)")
	recordPath := flag.String("record", "", "also write the run to this file for trainviz replay")
	prioritiesPath := flag.String("priorities", "profiles/ir.json", "train class priorities and delay bands (JSON); empty for none")
	flag.Parse()

	s := &sim.Sim{LogFormat: sim.LogFormat(*logFormat)}
	s.SetWorld(world)
	if *prioritiesPath != "" {
		cfg, err := sim.LoadPriorityConfig(*prioritiesPath)
		if err != nil {
			log.Fatal(err)
		}
		s.Priorities = cfg
	}

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

	var upInterval = 20
	var downInterval = 20

	// class names must match the priority config's classes
	classes := []string{"Superfast Express", "Passenger", "Mail/Express"}

	for i := range 6 {
		train1 := ops.Train{
			Name:     fmt.Sprintf("Train%dUp", i+1),
			Number:   fmt.Sprintf("045%dU", i+1),
			MaxSpeed: units.KMPH(110),
		}

		train1.AddSchedule(
			ops.NewStopPoint(
				tpj.Code, 10, "1",
			).SetEntryTime(
				float64(i*upInterval + 1),
			),
		)

		train1.AddSchedule(ops.NewStopPoint(
			pdkt.Code, 5, "1",
		))

		train1.AddSchedule(ops.NewStopPoint(
			kkdi.Code, 10, "1",
		))

		train2 := ops.Train{
			Name:     fmt.Sprintf("Train%dDown", i+1),
			Number:   fmt.Sprintf("045%dD", i+1),
			MaxSpeed: units.KMPH(110),
		}

		train2.AddSchedule(
			ops.NewStopPoint(
				kkdi.Code, 10, "3",
			).SetEntryTime(
				float64(i*downInterval + 1),
			),
		)

		train2.AddSchedule(ops.NewStopPoint(
			pdkt.Code, 5, "3",
		))

		train2.AddSchedule(ops.NewStopPoint(
			tpj.Code, 10, "3",
		))

		for j, t := range []*ops.Train{&train1, &train2} {
			if *prioritiesPath != "" {
				t.Class = classes[(i+j)%len(classes)]
			}
			if err := s.AddTrain(t); err != nil {
				log.Fatal(err)
			}
		}
	}

	if err := s.Init(); err != nil {
		log.Fatal(err)
	}

	var rec *record.Recorder
	var recFile *os.File
	if *recordPath != "" {
		f, err := os.Create(*recordPath)
		if err != nil {
			log.Fatal(err)
		}
		recFile = f
		if rec, err = record.New(f, s); err != nil {
			log.Fatal(err)
		}
		s.Observer = rec
	}

	runErr := s.Run()

	if rec != nil {
		if err := errors.Join(rec.Close(runErr), recFile.Close()); err != nil {
			log.Fatal(err)
		}
	}

	exited := 0
	for _, st := range s.Status() {
		if st.Phase == "exited" {
			exited++
			continue
		}
		fmt.Printf("%s did not finish: %s, priority %d, %.0f min late", st.Train, st.Phase, st.Priority, st.Delay)
		if st.Waiting != nil {
			fmt.Printf(", waiting since %.2f for %s pf%s: %v", st.Waiting.Since, st.Waiting.Station.Code, st.Waiting.PrefPfNo, st.Waiting.LastErr)
		}
		fmt.Println()
	}
	fmt.Printf("%d/%d trains completed their schedule by t=%.2f (log: %s)\n", exited, len(s.Status()), s.CurTime(), s.LogPath())
	if runErr != nil {
		log.Fatal(runErr)
	}
}

// func main() {
// 	world := worlds.BuildTpjKkdiWorld()
// 	visualization.BuildViz(world)
// }
