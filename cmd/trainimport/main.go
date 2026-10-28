// Command trainimport makes a day of a real schedule, as the public
// timetable gives it, a scenario:
//
//	trainimport -name sl-irl-wed docs/real_schedule/schedules_3.json
//
// writes scenarios/sl-irl-wed.json and scenarios/schedules/sl-irl-wed.json.
// What the schedule leaves out about each train, its name, class and top
// speed, comes from a train list, scenarios/sl-irl-wed/trains.json unless
// -trains says otherwise (see trainList).
//
// The day runs from -from to the same time the next day: a train that
// enters before -from runs at the end of the day, so every train runs once
// and whole. Each train stops where it calls and passes every other station
// on its way through the world; one that comes from beyond the world without
// calling where it enters is run on in from the world's end (see extend),
// with a warning. Nothing is written if anything is wrong.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/railway/sim"
	"trainapp/worlds"
)

func main() {
	log.SetFlags(0)
	name := flag.String("name", "", "the scenario's name, and its files' (required)")
	worldName := flag.String("world", "tpjkkdi-sl", "the world: tpjkkdi, tpjkkdi-sl, tpjkkdi-dl or a world snapshot (.json)")
	trainsPath := flag.String("trains", "", "the train list (default <dir>/<name>/trains.json)")
	dir := flag.String("dir", "scenarios", "the scenarios directory to write into")
	profile := flag.String("profile", "../profiles/ir.json", "the profile, as the scenario names it, relative to the scenario file")
	from := flag.String("from", "05:00", "when the day starts, HH:MM")
	sourceMin := flag.Float64("source-min", 5, "min a train stands at its Source before it leaves")
	destMin := flag.Float64("dest-min", 5, "min a train stands at its Destination after it arrives")
	force := flag.Bool("f", false, "overwrite the scenario's files if they are there")
	flag.Parse()
	if *name == "" || flag.NArg() != 1 {
		log.Fatal("usage: trainimport -name scenario [flags] real-schedule.json")
	}
	start, err := parseClock(*from)
	if err != nil {
		log.Fatalf("-from: %v", err)
	}
	if *sourceMin < 0 || *destMin < 0 {
		log.Fatal("-source-min and -dest-min must not be negative")
	}
	if *trainsPath == "" {
		*trainsPath = filepath.Join(*dir, *name, "trains.json")
	}

	var sched realSchedule
	if err := readFile(flag.Arg(0), &sched); err != nil {
		log.Fatal(err)
	}
	var list trainList
	if err := readFile(*trainsPath, &list); err != nil {
		log.Fatal(err)
	}
	cfg, err := sim.LoadPriorityConfig(filepath.Join(*dir, *profile))
	if err != nil {
		log.Fatal(err)
	}
	w, err := worlds.Load(*worldName)
	if err != nil {
		log.Fatal(err)
	}
	snap, err := w.Snapshot()
	if err != nil {
		log.Fatal(err)
	}

	trains, warnings, err := convert(snap, sched, list, cfg.Classes, options{from: start, sourceMin: *sourceMin, destMin: *destMin})
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		log.Fatalf("%s: nothing written:\n%v", flag.Arg(0), err)
	}

	sc := &scenario.Scenario{
		Name: *name,
		Description: fmt.Sprintf("A day of the real schedule in %s, %s to %s the next day: %d trains. Times are minutes after midnight",
			flag.Arg(0), *from, *from, len(trains)),
		World:    *worldName,
		Schedule: "schedules/" + *name + ".json",
		Profile:  *profile,
		Path:     filepath.Join(*dir, *name+".json"),
	}
	schedPath := sc.SchedulePath()
	for _, p := range []string{sc.Path, schedPath} {
		if _, err := os.Stat(p); err == nil && !*force {
			log.Fatalf("%s is there already: -f overwrites it", p)
		}
	}
	if err := os.MkdirAll(filepath.Dir(schedPath), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(schedPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := ops.WriteSchedule(f, ops.ScheduleFile{Trains: trains}); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	if err := sc.Save(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d trains: wrote %s and %s\n", len(trains), sc.Path, schedPath)
}

func readFile(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := readJSON(f, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
