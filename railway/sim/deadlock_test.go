package sim_test

import (
	"cmp"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/worlds"
)

// journey is a train's calling pattern: stations in order, entering at the
// first one at the given time. A station marked with pass is run through
// without stopping.
type journey struct {
	number string
	entry  float64
	stops  []ids.StnCode
}

const passMark = "*"

// pass marks a station of a journey as one the train runs through.
func pass(code ids.StnCode) ids.StnCode { return code + passMark }

func runJourneys(t *testing.T, world *infra.World, js []journey) *sim.Sim {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(world)
	for _, j := range js {
		tr := &ops.Train{Name: "T" + j.number, Number: j.number, MaxSpeed: units.KMPH(100)}
		for i, code := range j.stops {
			sp := ops.NewStopPoint(code, 3, "1")
			if name, ok := strings.CutSuffix(string(code), passMark); ok {
				sp = ops.NewPassPoint(ids.StnCode(name), -1, "1")
			}
			if i == 0 {
				sp.SetEntryTime(j.entry)
			}
			tr.AddSchedule(sp)
		}
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
	assertAllExitedAndClean(t, s)
	return s
}

func both(n int, every float64, east, west []ids.StnCode) []journey {
	var js []journey
	for i := range n {
		js = append(js,
			journey{fmt.Sprintf("E%d", i), 1 + float64(i)*every, east},
			journey{fmt.Sprintf("W%d", i), 1 + float64(i)*every, west},
		)
	}
	return js
}

var (
	eastbound = []ids.StnCode{"WST", "LPA", "HLT", "EST"}
	westbound = []ids.StnCode{"EST", "HLT", "LPA", "WST"}
)

// the real TPJ-KKDI single line: all stations, and the expresses' calls
var (
	tpjKkdiAllStops = []ids.StnCode{"TPJ", "KRMG", "KRUR", "VEL", "PDKT", "NMN", "TYM", "CTND", "KTYR", "KKDI"}
	tpjKkdiExpress  = []ids.StnCode{"TPJ", pass("KRMG"), pass("KRUR"), pass("VEL"), "PDKT", pass("NMN"), pass("TYM"), pass("CTND"), pass("KTYR"), "KKDI"}
)

func reversed(stops []ids.StnCode) []ids.StnCode {
	out := slices.Clone(stops)
	slices.Reverse(out)
	return out
}

func TestSingleLineWithPassingLoop(t *testing.T) {
	w := worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2})
	runJourneys(t, w, both(5, 15, eastbound, westbound))
}

func TestSingleLineDenseTraffic(t *testing.T) {
	// one-platform termini fed every 5 minutes from both ends, three blocks per
	// stretch so opposing trains could meet between stations
	w := worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 1, LoopPlatforms: 2, BlocksPerLine: 3})
	runJourneys(t, w, both(6, 5, eastbound, westbound))
}

func TestSingleLineWithoutAnyPassingPlace(t *testing.T) {
	// nowhere to pass: opposing trains must take turns over the whole line
	w := worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 1, LoopPlatforms: 1, BlocksPerLine: 2})
	runJourneys(t, w, both(4, 2, eastbound, westbound))
}

func TestTpjKkdiSingleLine(t *testing.T) {
	// all-stops trains and expresses from both ends, a train every 10 minutes
	// each way, crossing at the loops
	w := worlds.BuildTpjKkdiSingleLineWorld()
	var js []journey
	for i := range 6 {
		up, down := tpjKkdiAllStops, reversed(tpjKkdiAllStops)
		if i%2 == 1 {
			up, down = tpjKkdiExpress, reversed(tpjKkdiExpress)
		}
		js = append(js,
			journey{fmt.Sprintf("U%d", i), 1 + float64(i)*10, up},
			journey{fmt.Sprintf("D%d", i), 1 + float64(i)*10, down},
		)
	}
	runJourneys(t, w, js)
}

func TestJunction(t *testing.T) {
	w := worlds.BuildJunctionWorld(1)
	var js []journey
	routes := [][]ids.StnCode{
		{"WJN", "MID", "EJN"}, {"EJN", "MID", "WJN"},
		{"WJN", "MID", "NJN"}, {"NJN", "MID", "WJN"},
	}
	for i := range 12 {
		js = append(js, journey{fmt.Sprintf("J%d", i), 1 + float64(i)*4, routes[i%len(routes)]})
	}
	runJourneys(t, w, js)
}

// TestRandomTrafficNeverDeadlocks throws random traffic at every world shape.
// Any run that ends with a train stuck is a deadlock the safety check missed.
func TestRandomTrafficNeverDeadlocks(t *testing.T) {
	type shape struct {
		name   string
		build  func() *infra.World
		routes [][]ids.StnCode
	}
	slRoutes := [][]ids.StnCode{eastbound, westbound, {"WST", "LPA"}, {"EST", "HLT", "LPA"}, {"LPA", "HLT", "EST"}}
	shapes := []shape{
		{"single-line 1pf", func() *infra.World {
			return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 1, LoopPlatforms: 2, BlocksPerLine: 2})
		}, slRoutes[:2]},
		{"single-line 2pf", func() *infra.World {
			return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 3, BlocksPerLine: 3})
		}, slRoutes[:2]},
		{"junction", func() *infra.World { return worlds.BuildJunctionWorld(2) }, [][]ids.StnCode{
			{"WJN", "MID", "EJN"}, {"EJN", "MID", "WJN"}, {"WJN", pass("MID"), "NJN"}, {"NJN", "MID", "WJN"},
		}},
		{"tpj-kkdi", worlds.BuildTpjKkdiWorld, [][]ids.StnCode{
			{"TPJ", "PDKT", "KKDI"}, {"KKDI", "PDKT", "TPJ"}, {"TPJ", pass("PDKT"), "KKDI"}, {"KKDI", pass("PDKT"), "TPJ"},
		}},
		{"tpj-kkdi single line", worlds.BuildTpjKkdiSingleLineWorld, [][]ids.StnCode{
			tpjKkdiAllStops, reversed(tpjKkdiAllStops), tpjKkdiExpress, reversed(tpjKkdiExpress),
		}},
	}
	for _, sh := range shapes {
		for seed := range uint64(8) {
			t.Run(fmt.Sprintf("%s/seed%d", sh.name, seed), func(t *testing.T) {
				rng := rand.New(rand.NewPCG(seed, 99))
				var js []journey
				for i := range 16 {
					js = append(js, journey{
						number: fmt.Sprintf("R%02d", i),
						entry:  1 + float64(rng.IntN(90)),
						stops:  sh.routes[rng.IntN(len(sh.routes))],
					})
				}
				runJourneys(t, sh.build(), js)
			})
		}
	}
}

// runScenario runs a scenario file as trainapp does, under the precedence
// rule ("" for the profile's), and returns the sim and Run's error.
func runScenario(t *testing.T, path, rule string) (*sim.Sim, error) {
	t.Helper()
	sc, err := scenario.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := worlds.Load(sc.WorldName())
	if err != nil {
		t.Fatal(err)
	}
	trains, err := sc.Trains()
	if err != nil {
		t.Fatal(err)
	}
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(w)
	sc.Configure(s)
	if p := sc.ProfilePath(); p != "" {
		if s.Priorities, err = sim.LoadPriorityConfig(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := scenario.ApplyRule(s.Priorities, rule); err != nil {
		t.Fatal(err)
	}
	for _, tr := range trains {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	return s, s.Run()
}

func TestDeadlockDemo(t *testing.T) {
	// the demo shown in traindemo: without the safe-state check the trains
	// lock up, with it they all finish
	t.Run("off", func(t *testing.T) {
		_, err := runScenario(t, "../../scenarios/deadlock-off.json", "")
		var dl *sim.Deadlock
		if !errors.As(err, &dl) {
			t.Fatalf("want a deadlock, got %v", err)
		}
		got := fmt.Sprint(dl.Trains())
		if got != "[W1 E1]" {
			t.Errorf("cycle %s, want W1 and E1: %v", got, dl)
		}
	})
	t.Run("on", func(t *testing.T) {
		s, err := runScenario(t, "../../scenarios/deadlock-on.json", "")
		if err != nil {
			t.Fatal(err)
		}
		assertAllExitedAndClean(t, s)
	})
}

func TestOvertakeDemo(t *testing.T) {
	// the overtake shown in traindemo: under the profile's rule the
	// Passenger is held in L's loop and the Superfast passes it; without
	// precedence the Superfast trails it to B
	arrival := func(s *sim.Sim, number string) float64 {
		for _, st := range s.Status() {
			if st.Train.Number == number {
				sched := st.Train.Schedule()
				return sched[len(sched)-1].ActArrTime
			}
		}
		t.Fatalf("no train %s", number)
		return 0
	}
	for _, c := range []struct {
		rule     string
		overtake bool
	}{{"", true}, {scenario.RuleOff, false}} {
		t.Run(cmp.Or(c.rule, "profile"), func(t *testing.T) {
			s, err := runScenario(t, "../../scenarios/overtake.json", c.rule)
			if err != nil {
				t.Fatal(err)
			}
			assertAllExitedAndClean(t, s)
			held := slices.ContainsFunc(s.Decisions(), func(d sim.Decision) bool {
				return d.Action == sim.ActionHold && d.Train.Number == "P1" && d.For.Number == "S1" && d.At == "L"
			})
			first := arrival(s, "S1") < arrival(s, "P1")
			if held != c.overtake || first != c.overtake {
				t.Errorf("P1 held at L for S1: %v, S1 at B first: %v; want %v (decisions %+v)", held, first, c.overtake, s.Decisions())
			}
		})
	}
}
