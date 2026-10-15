package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/railway/timetable"
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
	prioritiesPath := flag.String("priorities", "profiles/ir.json", "train class priorities, delay bands and precedence settings (JSON); empty for none")
	overridesPath := flag.String("overrides", "", "precedence overrides to apply (JSON), e.g. to release a hold from an earlier run")
	timetablePath := flag.String("timetable", "", "generate a timetable from the run, check it with a second run, and write it to this file (JSON)")
	checkRecordPath := flag.String("check-record", "", "with -timetable, also write the check run to this file for trainviz replay")
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
	if *overridesPath != "" {
		overrides, err := sim.LoadOverrides(*overridesPath)
		if err != nil {
			log.Fatal(err)
		}
		if err := s.ApplyOverrides(overrides); err != nil {
			log.Fatal(err)
		}
	}

	runErr, err := runRecorded(s, *recordPath)
	if err != nil {
		log.Fatal(err)
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
	if ds := s.Decisions(); len(ds) > 0 {
		holds := 0
		for _, d := range ds {
			if d.Action == sim.ActionHold {
				holds++
			}
		}
		fmt.Printf("precedence: %d holds, %d platform changes (PRECEDENCE_* and PLATFORM_CHANGE in the log)\n", holds, len(ds)-holds)
	}
	if runErr != nil {
		log.Fatal(runErr)
	}

	if *timetablePath != "" {
		if err := writeTimetable(s, *timetablePath, *checkRecordPath); err != nil {
			log.Fatal(err)
		}
	}
}

// runRecorded runs the sim, writing it to path for trainviz if path is set.
// It returns the run's error and, separately, any error recording it.
func runRecorded(s *sim.Sim, path string) (runErr, err error) {
	if path == "" {
		return s.Run(), nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	rec, err := record.New(f, s)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	s.Observer = rec
	runErr = s.Run()
	return runErr, errors.Join(rec.Close(runErr), f.Close())
}

// writeTimetable turns the finished run into a timetable, rounds it, runs it
// again as timed trains to check it holds, prints it and writes it to path.
func writeTimetable(s *sim.Sim, path, checkRecordPath string) error {
	var raw json.RawMessage
	if s.Priorities != nil {
		raw = s.Priorities.Timetable
	}
	cfg, err := timetable.LoadConfig(raw)
	if err != nil {
		return err
	}
	tt := timetable.Round(timetable.FromRun(s, cfg), cfg)

	check, err := timetable.NewCheckSim(s.World(), s.Priorities, tt, filepath.Join("logs", "check"))
	if err != nil {
		return fmt.Errorf("check run: %w", err)
	}
	check.LogFormat = s.LogFormat
	runErr, err := runRecorded(check, checkRecordPath)
	if err != nil {
		return fmt.Errorf("check run: %w", err)
	}
	tt.Check = timetable.Report(check, runErr, cfg.CheckToleranceMin)

	fmt.Println()
	if err := timetable.Print(os.Stdout, tt); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := errors.Join(timetable.WriteJSON(f, tt), f.Close()); err != nil {
		return err
	}
	fmt.Printf("timetable written to %s (check run log: %s)\n", path, check.LogPath())
	return nil
}

// func main() {
// 	world := worlds.BuildTpjKkdiWorld()
// 	visualization.BuildViz(world)
// }
