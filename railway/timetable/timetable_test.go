package timetable_test

import (
	"fmt"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/railway/timetable"
	"trainapp/units"
	"trainapp/worlds"
)

// stop is a schedule point for a test train: a station, its platform, its
// dwell, and whether it runs through instead.
type stop struct {
	code  ids.StnCode
	pf    string
	dwell float64
	pass  bool
}

func newTrain(number string, prio int, kmph, entry float64, stops ...stop) *ops.Train {
	t := &ops.Train{Name: "T" + number, Number: number, MaxSpeed: units.KMPH(kmph), Priority: prio}
	for i, s := range stops {
		sp := ops.NewStopPoint(s.code, s.dwell, s.pf)
		if s.pass {
			sp = ops.NewPassPoint(s.code, -1, s.pf)
		}
		if i == 0 {
			sp.SetEntryTime(entry)
		}
		t.AddSchedule(sp)
	}
	return t
}

func run(t *testing.T, world *infra.World, prio *sim.PriorityConfig, trains ...*ops.Train) *sim.Sim {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: prio}
	s.SetWorld(world)
	for _, tr := range trains {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	return s
}

func singleLine() *infra.World {
	return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2})
}

func precedence() *sim.PriorityConfig {
	return &sim.PriorityConfig{Precedence: &sim.PrecedenceConfig{
		HorizonMin: 40, HeadwayMin: 1, MaxHoldMin: 30, MainLineMaxStayMin: 5,
		MinPriorityGap: 10, MinSavingMin: 1,
	}}
}

// mainTrains mirrors main.go: 6 up and 6 down trains, 20 minutes apart.
func mainTrains() []*ops.Train {
	var out []*ops.Train
	classes := []string{"Superfast Express", "Passenger", "Mail/Express"}
	for i := range 6 {
		up := newTrain(fmt.Sprintf("U%d", i), 0, 110, float64(i*20+1), stop{"TPJ", "1", 10, false}, stop{"PDKT", "1", 5, false}, stop{"KKDI", "1", 10, false})
		down := newTrain(fmt.Sprintf("D%d", i), 0, 110, float64(i*20+1), stop{"KKDI", "3", 10, false}, stop{"PDKT", "3", 5, false}, stop{"TPJ", "3", 10, false})
		up.Class, down.Class = classes[i%3], classes[(i+1)%3]
		out = append(out, up, down)
	}
	return out
}

func irProfile(t *testing.T) *sim.PriorityConfig {
	t.Helper()
	cfg, err := sim.LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// pointAt is the train's point at the station.
func pointAt(t *testing.T, tt timetable.Timetable, number, stn string) timetable.Point {
	t.Helper()
	tr, ok := tt.Train(number)
	if !ok {
		t.Fatalf("no train %s", number)
	}
	for _, p := range tr.Points {
		if p.Stn == stn {
			return p
		}
	}
	t.Fatalf("%s has no point at %s", number, stn)
	return timetable.Point{}
}

func logTimetable(t *testing.T, tt timetable.Timetable) {
	t.Helper()
	var sb strings.Builder
	if err := timetable.Print(&sb, tt); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + sb.String())
}

func TestHaltReasons(t *testing.T) {
	cases := []struct {
		name    string
		prio    *sim.PriorityConfig
		trains  []*ops.Train
		train   string
		stn     string
		kind    string
		reason  string
		waitFor string
	}{
		{
			// X waits to leave EST until P, from the other way, is off the line
			name: "crossing, no precedence", train: "X", stn: "EST", kind: timetable.KindStop, reason: timetable.ReasonCross, waitFor: "P",
			trains: []*ops.Train{
				newTrain("P", 20, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
				newTrain("X", 50, 100, 17, stop{"EST", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"LPA", "2", 0, true}, stop{"WST", "2", 2, false}),
			},
		},
		{
			name: "crossing, precedence hold", prio: precedence(), train: "P", stn: "LPA", kind: timetable.KindStop, reason: timetable.ReasonCross, waitFor: "X",
			trains: []*ops.Train{
				newTrain("P", 20, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
				newTrain("X", 50, 100, 17, stop{"EST", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"LPA", "1", 0, true}, stop{"WST", "2", 2, false}),
			},
		},
		{
			name: "overtake, precedence hold", prio: precedence(), train: "P", stn: "LPA", kind: timetable.KindStop, reason: timetable.ReasonOvertake, waitFor: "X",
			trains: []*ops.Train{
				newTrain("P", 20, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
				newTrain("X", 50, 100, 12, stop{"WST", "2", 2, false}, stop{"LPA", "1", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "2", 2, false}),
			},
		},
		{
			// S catches F up and stands at HLT, a pass point, until F is clear
			name: "following train", train: "S", stn: "HLT", kind: timetable.KindHalt, reason: timetable.ReasonTechnical, waitFor: "F",
			trains: []*ops.Train{
				newTrain("F", 0, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 1, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
				newTrain("S", 0, 110, 4, stop{"WST", "2", 2, false}, stop{"LPA", "2", 1, false}, stop{"HLT", "1", 0, true}, stop{"EST", "2", 2, false}),
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := run(t, singleLine(), c.prio, c.trains...)
			tt := timetable.FromRun(s, timetable.DefaultConfig())
			logTimetable(t, tt)
			p := pointAt(t, tt, c.train, c.stn)
			if p.Kind != c.kind {
				t.Errorf("%s at %s is a %s, want %s", c.train, c.stn, p.Kind, c.kind)
			}
			if p.Halt == nil {
				t.Fatalf("%s at %s: no halt", c.train, c.stn)
			}
			if p.Halt.Reason != c.reason || p.Halt.For != c.waitFor {
				t.Errorf("%s at %s: halt %+v, want %s for %s", c.train, c.stn, *p.Halt, c.reason, c.waitFor)
			}
			if p.Halt.Min <= 0.5 {
				t.Errorf("%s at %s: halt of %.2f min", c.train, c.stn, p.Halt.Min)
			}
		})
	}
}

func TestOnlyRealSignalStopsAreKept(t *testing.T) {
	s := run(t, singleLine(), nil,
		newTrain("F", 0, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 1, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
		newTrain("S", 0, 110, 4, stop{"WST", "2", 2, false}, stop{"LPA", "2", 1, false}, stop{"HLT", "1", 0, true}, stop{"EST", "2", 2, false}),
	)
	tt := timetable.FromRun(s, timetable.DefaultConfig())
	for _, tr := range tt.Trains {
		for _, p := range tr.Points {
			for _, ss := range p.SignalStops {
				if tr.Number == "F" {
					t.Errorf("F ran free but has a signal stop before %s: %+v", p.Stn, ss)
				}
				if ss.Min <= 0.5 || ss.For != "F" {
					t.Errorf("S before %s: signal stop %+v", p.Stn, ss)
				}
			}
		}
	}
	if p := pointAt(t, tt, "S", "EST"); len(p.SignalStops) == 0 {
		t.Error("S has no signal stop before EST")
	}
}

func TestLateEntryIsAConflict(t *testing.T) {
	// TPJ has four boundary platforms; the fifth train cannot enter on time
	var trains []*ops.Train
	for i := range 5 {
		trains = append(trains, newTrain(fmt.Sprintf("E%d", i), 0, 110, 1, stop{"TPJ", "1", 10, false}, stop{"PDKT", "1", 2, false}))
	}
	s := run(t, worlds.BuildTpjKkdiWorld(), nil, trains...)
	tt := timetable.FromRun(s, timetable.DefaultConfig())
	late := 0
	for _, tr := range tt.Trains {
		for _, w := range tr.Warnings {
			if strings.HasPrefix(w, "entered ") && strings.Contains(w, "late") {
				late++
			}
		}
	}
	if late != 1 {
		logTimetable(t, tt)
		t.Errorf("%d trains warned of a late entry, want 1", late)
	}
}

func TestMainScenarioTimetableHolds(t *testing.T) {
	prio := irProfile(t)
	cfg, err := timetable.LoadConfig(prio.Timetable)
	if err != nil {
		t.Fatal(err)
	}
	world := worlds.BuildTpjKkdiWorld()
	trains := mainTrains()
	s := run(t, world, prio, trains...)
	raw := timetable.FromRun(s, cfg)
	tt := timetable.Round(raw, cfg)
	rep, err := timetable.Check(world, prio, tt, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tt.Check = rep
	logTimetable(t, tt)
	if !rep.OK() {
		t.Errorf("check run failed: %+v", *rep)
	}
	for i, tr := range tt.Trains {
		if tr.Points[0].Arr != trains[i].Schedule()[0].ArrTime {
			t.Errorf("%s enters at %v, want its entry time %v", tr.Number, tr.Points[0].Arr, trains[i].Schedule()[0].ArrTime)
		}
		if len(tr.Warnings) > 0 {
			t.Errorf("%s: %v", tr.Number, tr.Warnings)
		}
	}

	// a timetable the trains cannot keep fails the check
	bad := timetable.Round(raw, cfg)
	p := &bad.Trains[0].Points[1]
	p.Arr -= 5
	p.Dep -= 5
	rep, err = timetable.Check(world, prio, bad, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() || len(rep.Late) == 0 || rep.Late[0].Train != bad.Trains[0].Number {
		t.Errorf("check of an impossible timetable: %+v", *rep)
	}
}
