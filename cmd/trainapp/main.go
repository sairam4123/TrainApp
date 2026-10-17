package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/kpi"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
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
	worldName := flag.String("world", "tpjkkdi", "world to run: tpjkkdi (the double line), tpjkkdi-sl (the real single line, with every station) or a world snapshot file (.json)")
	schedulePath := flag.String("schedule", "", "trains to run (JSON); empty for the built-in demo trains, which need the tpjkkdi or tpjkkdi-sl world")
	saveWorldPath := flag.String("save-world", "", "also write the world to this file as a snapshot (JSON), which -world can load")
	saveSchedulePath := flag.String("save-schedule", "", "also write the trains' schedules to this file (JSON), which -schedule can load")
	logFormat := flag.String("log", string(sim.LogText), "log format: text (logfmt) or json (one object per line)")
	recordPath := flag.String("record", "", "also write the run to this file for trainviz replay")
	prioritiesPath := flag.String("priorities", "profiles/ir.json", "train class priorities, delay bands and precedence settings (JSON); empty for none")
	overridesPath := flag.String("overrides", "", "precedence overrides to apply (JSON), e.g. to release a hold from an earlier run")
	timetablePath := flag.String("timetable", "", "generate a timetable from the run, check it with a second run, and write it to this file (JSON)")
	checkRecordPath := flag.String("check-record", "", "with -timetable, also write the check run to this file for trainviz replay")
	checkPrecedence := flag.String("check-precedence", "", "with -timetable, how the check run decides precedence: plan (keep to the timetable's plan) or auto (decide afresh); empty for the profile's check_precedence")
	showProgress := flag.Bool("progress", isTerminal(os.Stderr), "draw a progress bar on stderr while the sim runs (default on for a terminal)")
	demoPerWay := flag.Int("demo", 3, "with no -schedule, how many demo trains to run each way")
	disruptionsPath := flag.String("disruptions", "", "disruptions to apply to the trains (JSON): extra stands at stations, late entries")
	kpiPath := flag.String("kpi", "", "also write the KPI report to this file (JSON)")
	scenarioPath := flag.String("scenario", "", "run a scenario file (JSON): its world, schedule, profile and disruptions, in place of -world, -schedule, -priorities and -disruptions")
	rule := flag.String("rule", "", "precedence rule in place of the profile's: a hold rule's name (thresholds, weighted), or off to run without precedence")
	flag.Parse()

	var disruptions []ops.Disruption
	if *scenarioPath != "" {
		var clash []string
		flag.Visit(func(f *flag.Flag) {
			if slices.Contains([]string{"world", "schedule", "priorities", "disruptions"}, f.Name) {
				clash = append(clash, "-"+f.Name)
			}
		})
		if len(clash) > 0 {
			log.Fatalf("-scenario sets the world, schedule, profile and disruptions: drop %s", strings.Join(clash, ", "))
		}
		sc, err := scenario.Load(*scenarioPath)
		if err != nil {
			log.Fatal(err)
		}
		*worldName, *schedulePath, *prioritiesPath = sc.WorldName(), sc.SchedulePath(), sc.ProfilePath()
		disruptions = sc.Disruptions
	}

	// between lists the stations the demo trains run through between their
	// calls, TPJ-PDKT and PDKT-KKDI, from TPJ
	var world *infra.World
	var between [2][]ids.StnCode
	builtin := true
	switch *worldName {
	case "tpjkkdi":
		world = worlds.BuildTpjKkdiWorld()
	case "tpjkkdi-sl":
		world = worlds.BuildTpjKkdiSingleLineWorld()
		between = [2][]ids.StnCode{{"KRMG", "KRUR", "VEL"}, {"NMN", "TYM", "CTND", "KTYR"}}
	default:
		var err error
		if world, err = worlds.Load(*worldName); err != nil {
			log.Fatal(err)
		}
		builtin = false
	}

	var trains []*ops.Train
	if *schedulePath != "" {
		var err error
		if trains, err = ops.LoadTrains(*schedulePath); err != nil {
			log.Fatal(err)
		}
	} else {
		if !builtin {
			log.Fatal("the demo trains need the tpjkkdi or tpjkkdi-sl world: give a world snapshot a -schedule")
		}
		var err error
		if trains, err = demoTrains(world, between, *demoPerWay, *prioritiesPath != ""); err != nil {
			log.Fatal(err)
		}
	}

	if *saveWorldPath != "" {
		if err := infra.SaveWorld(*saveWorldPath, world); err != nil {
			log.Fatal(err)
		}
	}
	if *saveSchedulePath != "" {
		if err := saveTrains(*saveSchedulePath, trains); err != nil {
			log.Fatal(err)
		}
	}

	// the trains as planned, for their free-running times
	plan := make([]*ops.Train, len(trains))
	for i, t := range trains {
		plan[i] = t.Clone()
	}
	if *disruptionsPath != "" {
		var err error
		if disruptions, err = ops.LoadDisruptions(*disruptionsPath); err != nil {
			log.Fatal(err)
		}
	}
	if err := ops.ApplyDisruptions(trains, disruptions); err != nil {
		log.Fatal(err)
	}

	s := &sim.Sim{LogFormat: sim.LogFormat(*logFormat)}
	s.SetWorld(world)
	if *prioritiesPath != "" {
		cfg, err := sim.LoadPriorityConfig(*prioritiesPath)
		if err != nil {
			log.Fatal(err)
		}
		s.Priorities = cfg
	}
	if err := scenario.ApplyRule(s.Priorities, *rule); err != nil {
		log.Fatal(err)
	}
	for _, t := range trains {
		if err := s.AddTrain(t); err != nil {
			log.Fatal(err)
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

	var prog *progress
	if *showProgress {
		prog = newProgress(*worldName, s)
	}
	coll := kpi.NewCollector()
	runErr, err := runRecorded(s, *recordPath, prog, coll)
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

	kpiCfg, err := kpi.ConfigOf(s.Priorities)
	if err != nil {
		log.Fatal(err)
	}
	free, err := kpi.FreeRunning(world, s.Priorities, plan)
	if err != nil {
		fmt.Printf("free running: %v\n", err)
	}
	reports := map[string]*kpi.Report{"run": kpi.Compute(s, coll, kpi.Options{Config: kpiCfg, Free: free, Disruptions: disruptions})}
	fmt.Println()
	if err := kpi.Print(os.Stdout, "run", reports["run"]); err != nil {
		log.Fatal(err)
	}

	if *timetablePath != "" {
		check, err := writeTimetable(s, *timetablePath, *checkRecordPath, *checkPrecedence, *showProgress)
		if err != nil {
			log.Fatal(err)
		}
		reports["check"] = kpi.Compute(check.sim, check.coll, kpi.Options{Config: kpiCfg})
		fmt.Println()
		if err := kpi.Print(os.Stdout, "check run", reports["check"]); err != nil {
			log.Fatal(err)
		}
	}

	if *kpiPath != "" {
		f, err := os.Create(*kpiPath)
		if err != nil {
			log.Fatal(err)
		}
		if err := errors.Join(kpi.WriteJSON(f, reports), f.Close()); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("KPIs written to %s\n", *kpiPath)
	}
}

// demoTrains is perWay trains each way between TPJ and KKDI, calling at PDKT
// and running through the stations in between. With classes they cycle
// through three classes, whose names must match the priority config's.
func demoTrains(world *infra.World, between [2][]ids.StnCode, perWay int, classes bool) ([]*ops.Train, error) {
	var stns []*infra.Station
	for _, code := range []ids.StnCode{"TPJ", "PDKT", "KKDI"} {
		stn, ok := world.GetStation(code)
		if !ok {
			return nil, fmt.Errorf("demo trains: the world has no station %s", code)
		}
		stns = append(stns, stn)
	}
	tpj, pdkt, kkdi := stns[0], stns[1], stns[2]

	var upInterval = 10
	var downInterval = 10

	classNames := []string{"Superfast Express", "Passenger", "Mail/Express"}

	var trains []*ops.Train
	for i := range perWay {
		train1 := ops.Train{
			Name:     fmt.Sprintf("Train%dUp", i+1),
			Number:   fmt.Sprintf("045%dU", i+1),
			MaxSpeed: units.KMPH(110),
		}

		train1.AddSchedule(
			ops.NewStopPoint(
				tpj.Code, 5, "1",
			).SetEntryTime(
				float64(i*upInterval + 1),
			),
		)

		addPasses(&train1, between[0], false)
		train1.AddSchedule(ops.NewStopPoint(
			pdkt.Code, 2, "1",
		))

		addPasses(&train1, between[1], false)
		train1.AddSchedule(ops.NewStopPoint(
			kkdi.Code, 5, "1",
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

		addPasses(&train2, between[1], true)
		train2.AddSchedule(ops.NewStopPoint(
			pdkt.Code, 5, "3",
		))

		addPasses(&train2, between[0], true)
		train2.AddSchedule(ops.NewStopPoint(
			tpj.Code, 10, "3",
		))

		for j, t := range []*ops.Train{&train1, &train2} {
			if classes {
				t.Class = classNames[(i+j)%len(classNames)]
			}
			trains = append(trains, t)
		}
	}
	return trains, nil
}

// saveTrains writes the trains' schedules to path as a schedule file.
func saveTrains(path string, trains []*ops.Train) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return errors.Join(ops.WriteTrains(f, trains), f.Close())
}

// addPasses adds a pass point at each of the stations, in reverse order for a
// train running towards TPJ. No platform is preferred: the dispatcher picks.
func addPasses(t *ops.Train, stations []ids.StnCode, reverse bool) {
	if reverse {
		stations = slices.Clone(stations)
		slices.Reverse(stations)
	}
	for _, code := range stations {
		t.AddSchedule(ops.NewPassPoint(code, -1, ""))
	}
}

// runRecorded runs the sim, writing it to path for trainviz if path is set
// and drawing prog if it isn't nil; coll watches it too. It returns the run's
// error and, separately, any error recording it.
func runRecorded(s *sim.Sim, path string, prog *progress, coll *kpi.Collector) (runErr, err error) {
	obs := sim.Observers{coll}
	if prog != nil {
		obs = append(obs, prog)
		defer prog.clear()
	}
	if path == "" {
		s.Observer = obs
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
	s.Observer = append(obs, rec)
	runErr = s.Run()
	return runErr, errors.Join(rec.Close(runErr), f.Close())
}

// checkRun is the check run of a timetable, and what watched it.
type checkRun struct {
	sim  *sim.Sim
	coll *kpi.Collector
}

// writeTimetable turns the finished run into a timetable, rounds it, runs it
// again as timed trains to check it holds, prints it and writes it to path.
// checkPrecedence, if set, replaces the profile's check_precedence.
func writeTimetable(s *sim.Sim, path, checkRecordPath, checkPrecedence string, showProgress bool) (*checkRun, error) {
	var raw json.RawMessage
	if s.Priorities != nil {
		raw = s.Priorities.Timetable
	}
	cfg, err := timetable.LoadConfig(raw)
	if err != nil {
		return nil, err
	}
	if checkPrecedence != "" {
		cfg.CheckPrecedence = checkPrecedence
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
	}
	tt := timetable.Round(timetable.FromRun(s, cfg), cfg)

	check, err := timetable.NewCheckSim(s.World(), s.Priorities, tt, cfg.CheckPrecedence, filepath.Join("logs", "check"))
	if err != nil {
		return nil, fmt.Errorf("check run: %w", err)
	}
	check.LogFormat = s.LogFormat
	var prog *progress
	if showProgress {
		prog = newProgress("check run", check)
	}
	coll := kpi.NewCollector()
	runErr, err := runRecorded(check, checkRecordPath, prog, coll)
	if err != nil {
		return nil, fmt.Errorf("check run: %w", err)
	}
	tt.Check = timetable.Report(check, runErr, cfg)

	fmt.Println()
	if err := timetable.Print(os.Stdout, tt); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(timetable.WriteJSON(f, tt), f.Close()); err != nil {
		return nil, err
	}
	fmt.Printf("timetable written to %s (check run log: %s)\n", path, check.LogPath())
	return &checkRun{sim: check, coll: coll}, nil
}

// func main() {
// 	world := worlds.BuildTpjKkdiWorld()
// 	visualization.BuildViz(world)
// }
