package sim_test

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/worlds"
)

// entry is one log entry, its attributes as strings.
type entry struct {
	event string
	attrs map[string]string
}

// eventLog records the precedence entries of a run.
type eventLog struct{ entries []entry }

func (l *eventLog) Logged(_ slog.Level, event string, attrs []slog.Attr) {
	if !strings.HasPrefix(event, "PRECEDENCE") && event != "PLATFORM_CHANGE" && event != "TRAIN_ARRIVE" {
		return
	}
	e := entry{event: event, attrs: map[string]string{}}
	for _, a := range attrs {
		e.attrs[a.Key] = a.Value.String()
	}
	l.entries = append(l.entries, e)
}

func (l *eventLog) AfterEvent(des.Event[ops.RailwayEvent]) {}

func (l *eventLog) find(event string, match map[string]string) []entry {
	var out []entry
next:
	for _, e := range l.entries {
		if e.event != event {
			continue
		}
		for k, v := range match {
			if e.attrs[k] != v {
				continue next
			}
		}
		out = append(out, e)
	}
	return out
}

func precedenceConfig() *sim.PriorityConfig {
	return &sim.PriorityConfig{Precedence: &sim.PrecedenceConfig{
		HorizonMin: 40, HeadwayMin: 1, MaxHoldMin: 30, MainLineMaxStayMin: 5,
		MinPriorityGap: 10, MinSavingMin: 1,
	}}
}

// stop is a schedule point for a test train: a station, its platform, how
// long it stands there, and whether it runs through instead.
type stop struct {
	code  ids.StnCode
	pf    string
	dwell float64
	pass  bool
}

func testTrain(number string, prio int, kmph float64, entry float64, stops ...stop) *ops.Train {
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

// runWith runs the trains over the world with the given priority config,
// returning the sim and its precedence log.
func runWith(t *testing.T, world *infra.World, cfg *sim.PriorityConfig, setup func(*sim.Sim), trains ...*ops.Train) (*sim.Sim, *eventLog) {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(world)
	for _, tr := range trains {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	log := &eventLog{}
	s.Observer = log
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(s)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	assertAllExitedAndClean(t, s)
	assertDecisionsEnded(t, s)
	return s, log
}

func assertDecisionsEnded(t *testing.T, s *sim.Sim) {
	t.Helper()
	for _, d := range s.Decisions() {
		if d.Status == sim.StatusActive {
			t.Errorf("decision %s still active at the end", d.ID)
		}
	}
}

func singleLine() *infra.World {
	return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2})
}

// overtake: a slow passenger calls at LPA while an express catches it up.
func overtakeTrains() []*ops.Train {
	return []*ops.Train{
		testTrain("P", 20, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
		testTrain("X", 50, 100, 12, stop{"WST", "2", 2, false}, stop{"LPA", "1", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "2", 2, false}),
	}
}

func arrivalAt(t *ops.Train, code ids.StnCode) float64 {
	for _, sp := range t.Schedule() {
		if sp.StnCode == code {
			return sp.ActArrTime
		}
	}
	return -1
}

func TestExpressOvertakesAtALoop(t *testing.T) {
	off := overtakeTrains()
	runWith(t, singleLine(), nil, nil, off...)
	on := overtakeTrains()
	_, log := runWith(t, singleLine(), precedenceConfig(), nil, on...)

	holds := log.find("PRECEDENCE_HOLD", map[string]string{"train": "P", "for": "X", "at": "LPA"})
	if len(holds) == 0 {
		t.Fatalf("P was not held at LPA for X; log: %+v", log.entries)
	}
	if k := holds[0].attrs["kind"]; k != "overtake" {
		t.Errorf("hold kind %s, want overtake", k)
	}
	if got := log.find("PRECEDENCE_RELEASE", map[string]string{"decision": holds[0].attrs["decision"]}); len(got) == 0 {
		t.Errorf("hold never released")
	}
	if arrivalAt(on[1], "EST") >= arrivalAt(on[0], "EST") {
		t.Errorf("X reached EST at %.2f, after P at %.2f", arrivalAt(on[1], "EST"), arrivalAt(on[0], "EST"))
	}
	if arrivalAt(on[1], "EST") >= arrivalAt(off[1], "EST") {
		t.Errorf("X reached EST at %.2f with precedence, no earlier than %.2f without", arrivalAt(on[1], "EST"), arrivalAt(off[1], "EST"))
	}
	for _, e := range log.entries {
		t.Logf("%s %v", e.event, e.attrs)
	}
}

func TestTrainsCrossAtALoop(t *testing.T) {
	trains := []*ops.Train{
		testTrain("P", 20, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
		testTrain("X", 50, 100, 17, stop{"EST", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"LPA", "1", 0, true}, stop{"WST", "2", 2, false}),
	}
	_, log := runWith(t, singleLine(), precedenceConfig(), nil, trains...)
	holds := log.find("PRECEDENCE_HOLD", map[string]string{"train": "P", "for": "X", "at": "LPA", "kind": "cross"})
	if len(holds) == 0 {
		t.Fatalf("P was not held at LPA to cross X: %+v", log.entries)
	}
	if x, p := arrivalAt(trains[1], "LPA"), arrivalAt(trains[0], "HLT"); x >= p {
		t.Errorf("X reached LPA at %.2f, after P reached HLT at %.2f: they did not cross at LPA", x, p)
	}
}

func TestStoppingTrainsKeepOffThroughTracks(t *testing.T) {
	w := worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2, ThroughTrack: true})
	trains := []*ops.Train{
		testTrain("P", 20, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 2, false}, stop{"HLT", "1", 1, false}, stop{"EST", "1", 2, false}),
		testTrain("X", 50, 100, 30, stop{"WST", "2", 2, false}, stop{"LPA", "2", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "2", 2, false}),
	}
	_, log := runWith(t, w, precedenceConfig(), nil, trains...)
	if got := log.find("PLATFORM_CHANGE", map[string]string{"train": "P", "station": "LPA", "pf_from": "1", "reason": "through_track"}); len(got) == 0 {
		t.Errorf("P was not moved off LPA's through track: %+v", log.entries)
	}
	if got := log.find("TRAIN_ARRIVE", map[string]string{"train": "X", "track": "lpaPf1"}); len(got) == 0 {
		t.Errorf("X did not run through LPA on its through track: %+v", log.find("TRAIN_ARRIVE", map[string]string{"train": "X"}))
	}
}

func TestAHigherTrainTakesTheMainLine(t *testing.T) {
	// H stops on LPA's main line for a short stay; VH runs through it the
	// other way meanwhile, so H moves to the loop
	trains := []*ops.Train{
		testTrain("H", 50, 100, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 4, false}, stop{"HLT", "1", 1, false}, stop{"EST", "1", 2, false}),
		testTrain("VH", 100, 100, 3, stop{"EST", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"LPA", "1", 0, true}, stop{"WST", "2", 2, false}),
	}
	_, log := runWith(t, singleLine(), precedenceConfig(), nil, trains...)
	if got := log.find("PLATFORM_CHANGE", map[string]string{"train": "H", "station": "LPA", "pf_to": "2", "reason": "booked", "for": "VH"}); len(got) == 0 {
		t.Errorf("H was not moved to the loop for VH: %+v", log.entries)
	}
	if got := log.find("TRAIN_ARRIVE", map[string]string{"train": "VH", "track": "lpaPf1"}); len(got) == 0 {
		t.Errorf("VH did not run through LPA on the main line")
	}
}

func TestHoldsAreOnlyPlacedAtRefuges(t *testing.T) {
	// a slow train ahead keeps P at signals out on the line, where the express
	// behind could not get past it
	trains := []*ops.Train{
		testTrain("M", 20, 40, 1, stop{"WST", "1", 1, false}, stop{"LPA", "1", 1, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 1, false}),
		testTrain("P", 20, 100, 2, stop{"WST", "2", 1, false}, stop{"LPA", "1", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "2", 1, false}),
		testTrain("X", 50, 100, 6, stop{"WST", "1", 1, false}, stop{"LPA", "1", 0, true}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 1, false}),
	}
	_, log := runWith(t, singleLine(), precedenceConfig(), nil, trains...)
	for _, h := range log.find("PRECEDENCE_HOLD", nil) {
		if at := h.attrs["at"]; at != "WST" && at != "LPA" && at != "EST" {
			t.Errorf("%s held at %s, where nothing can pass it", h.attrs["train"], at)
		}
	}
}

func TestOverrides(t *testing.T) {
	const id = "hold:P>X@LPA"
	ptr := func(f float64) *float64 { return &f }

	t.Run("release vetoes a hold", func(t *testing.T) {
		_, log := runWith(t, singleLine(), precedenceConfig(), func(s *sim.Sim) {
			if err := s.ApplyOverrides([]sim.Override{{Action: sim.OverrideRelease, Decision: id}}); err != nil {
				t.Fatal(err)
			}
		}, overtakeTrains()...)
		if got := log.find("PRECEDENCE_HOLD", map[string]string{"decision": id}); len(got) != 0 {
			t.Errorf("vetoed hold placed: %v", got)
		}
		if got := log.find("PRECEDENCE_OVERRIDE", map[string]string{"action": "release", "status": "applied"}); len(got) != 1 {
			t.Errorf("override not logged: %+v", log.entries)
		}
	})

	t.Run("release ends a hold", func(t *testing.T) {
		s, _ := runWith(t, singleLine(), precedenceConfig(), func(s *sim.Sim) {
			if err := s.ApplyOverrides([]sim.Override{{T: ptr(22), Action: sim.OverrideRelease, Decision: id}}); err != nil {
				t.Fatal(err)
			}
		}, overtakeTrains()...)
		found := false
		for _, d := range s.Decisions() {
			if d.ID == id {
				found = true
				if d.Status != sim.StatusOverridden || d.Ended != 22 {
					t.Errorf("hold ended %s at %.2f, want overridden at 22", d.Status, d.Ended)
				}
			}
		}
		if !found {
			t.Fatal("no hold to release")
		}
	})

	t.Run("disable", func(t *testing.T) {
		_, log := runWith(t, singleLine(), precedenceConfig(), func(s *sim.Sim) {
			if err := s.Override(sim.Override{Action: sim.OverrideDisable, Train: "P"}); err != nil {
				t.Fatal(err)
			}
		}, overtakeTrains()...)
		if got := log.find("PRECEDENCE_HOLD", nil); len(got) != 0 {
			t.Errorf("disabled train held: %v", got)
		}
		if got := log.find("PLATFORM_CHANGE", nil); len(got) != 0 {
			t.Errorf("disabled train moved: %v", got)
		}
	})

	t.Run("platform pin", func(t *testing.T) {
		_, log := runWith(t, singleLine(), precedenceConfig(), func(s *sim.Sim) {
			if err := s.Override(sim.Override{Action: sim.OverridePlatform, Train: "X", Station: "EST", Pf: "1"}); err != nil {
				t.Fatal(err)
			}
		}, overtakeTrains()...)
		if got := log.find("TRAIN_ARRIVE", map[string]string{"train": "X", "track": "estPf1"}); len(got) == 0 {
			t.Errorf("X not on its pinned platform: %+v", log.find("TRAIN_ARRIVE", map[string]string{"train": "X"}))
		}
		if got := log.find("PLATFORM_CHANGE", map[string]string{"train": "X", "reason": "override", "source": "manual"}); len(got) == 0 {
			t.Errorf("pinned platform not logged as a decision")
		}
	})

	t.Run("manual hold", func(t *testing.T) {
		trains := overtakeTrains()
		_, log := runWith(t, singleLine(), precedenceConfig(), func(s *sim.Sim) {
			if err := s.Override(sim.Override{Action: sim.OverrideHold, Train: "P", For: "X", Station: "WST"}); err != nil {
				t.Fatal(err)
			}
		}, trains...)
		if got := log.find("PRECEDENCE_HOLD", map[string]string{"decision": "hold:P>X@WST", "source": "manual"}); len(got) == 0 {
			t.Fatalf("manual hold not placed: %+v", log.entries)
		}
		if p, x := trains[0].Schedule()[0].ActDeptTime, trains[1].Schedule()[0].ActDeptTime; p < x {
			t.Errorf("P left WST at %.2f, before X at %.2f", p, x)
		}
	})

	t.Run("bad overrides", func(t *testing.T) {
		s := &sim.Sim{OutputDir: t.TempDir(), Priorities: precedenceConfig()}
		s.SetWorld(singleLine())
		for _, tr := range overtakeTrains() {
			if err := s.AddTrain(tr); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Override(sim.Override{Action: sim.OverrideDisable, Train: "P"}); err == nil {
			t.Error("override before Init accepted")
		}
		if err := s.Init(); err != nil {
			t.Fatal(err)
		}
		for name, o := range map[string]sim.Override{
			"unknown action":   {Action: "bump", Train: "P"},
			"unknown train":    {Action: sim.OverrideDisable, Train: "NOPE"},
			"no decision":      {Action: sim.OverrideRelease},
			"unknown platform": {Action: sim.OverridePlatform, Train: "P", Station: "LPA", Pf: "9"},
			"not calling":      {Action: sim.OverrideHold, Train: "P", For: "X", Station: "NOPE"},
			"held for itself":  {Action: sim.OverrideHold, Train: "P", For: "P", Station: "LPA"},
		} {
			if err := s.Override(o); err == nil {
				t.Errorf("%s: accepted", name)
			}
		}

		off := &sim.Sim{OutputDir: t.TempDir()}
		off.SetWorld(singleLine())
		if err := off.Init(); err != nil {
			t.Fatal(err)
		}
		if err := off.Override(sim.Override{Action: sim.OverrideDisable, Train: "P"}); err == nil {
			t.Error("override accepted with precedence off")
		}
	})
}

func TestLoadOverrides(t *testing.T) {
	path := t.TempDir() + "/o.json"
	body := `{"overrides": [{"t": 40, "action": "release", "decision": "hold:A>B@LPA"}, {"action": "disable", "train": "A"}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := sim.LoadOverrides(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].T == nil || *got[0].T != 40 || got[1].T != nil || got[1].Train != "A" {
		t.Errorf("loaded %+v", got)
	}
}

func TestPrecedenceConfig(t *testing.T) {
	for name, cfg := range map[string]sim.PrecedenceConfig{
		"unknown rule":            {Rule: "coin-toss"},
		"negative horizon":        {HorizonMin: -1},
		"negative reception hold": {MaxReceptionHoldMin: -1},
		"negative ratio":          {MaxWaitRatio: -1},
	} {
		if err := (&sim.PriorityConfig{Precedence: &cfg}).Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := (&sim.PriorityConfig{Precedence: &sim.PrecedenceConfig{Rule: "weighted"}}).Validate(); err != nil {
		t.Errorf("weighted rule rejected: %v", err)
	}
}

func TestWeightedRuleHoldsToo(t *testing.T) {
	cfg := precedenceConfig()
	cfg.Precedence.Rule = "weighted"
	_, log := runWith(t, singleLine(), cfg, nil, overtakeTrains()...)
	holds := log.find("PRECEDENCE_HOLD", map[string]string{"train": "P", "for": "X", "rule": "weighted"})
	if len(holds) == 0 {
		t.Fatalf("weighted rule did not hold P for X: %+v", log.entries)
	}
}

func TestScheduleMustNameEveryStation(t *testing.T) {
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	if err := s.AddTrain(newTrain("A", 1, [2]string{"TPJ", "1"}, [2]string{"KKDI", "1"})); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err == nil || !strings.Contains(err.Error(), "pass point") {
		t.Fatalf("schedule skipping PDKT accepted (err %v)", err)
	}
}

// TestRandomTrafficWithPrecedence throws random traffic of mixed priorities
// and speeds at every world shape with precedence on. Every train must finish,
// every decision must have ended, and no hold may outlast max_hold_min.
func TestRandomTrafficWithPrecedence(t *testing.T) {
	type shape struct {
		name   string
		build  func() *infra.World
		routes [][]ids.StnCode
	}
	shapes := []shape{
		{"single-line", singleLine, [][]ids.StnCode{eastbound, westbound}},
		{"single-line through", func() *infra.World {
			return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 3, ThroughTrack: true})
		}, [][]ids.StnCode{eastbound, westbound}},
		{"junction", func() *infra.World { return worlds.BuildJunctionWorld(2) }, [][]ids.StnCode{
			{"WJN", "MID", "EJN"}, {"EJN", "MID", "WJN"}, {"WJN", "MID", "NJN"}, {"NJN", "MID", "WJN"},
		}},
		{"tpj-kkdi", worlds.BuildTpjKkdiWorld, [][]ids.StnCode{{"TPJ", "PDKT", "KKDI"}, {"KKDI", "PDKT", "TPJ"}}},
	}
	cfg := precedenceConfig()
	maxHold := float64(cfg.Precedence.MaxHoldMin)
	for _, sh := range shapes {
		for seed := range uint64(seeds()) {
			t.Run(fmt.Sprintf("%s/seed%d", sh.name, seed), func(t *testing.T) {
				trains := randomTraffic(sh.routes, seed)
				s, _ := runWith(t, sh.build(), cfg, nil, trains...)
				for _, d := range s.Decisions() {
					if d.Action == sim.ActionHold && d.Ended-d.Since > maxHold+2 {
						t.Errorf("%s held for %.2f min", d.ID, d.Ended-d.Since)
					}
				}
			})
		}
	}
}

// randomTraffic is 12 trains of mixed priorities, speeds and dwells over the
// routes, entering within 90 minutes, with some intermediate stations run
// through.
func randomTraffic(routes [][]ids.StnCode, seed uint64) []*ops.Train {
	rng := rand.New(rand.NewPCG(seed, 7))
	var trains []*ops.Train
	for i := range 12 {
		route := routes[rng.IntN(len(routes))]
		var stops []stop
		for j, code := range route {
			pass := j > 0 && j < len(route)-1 && rng.IntN(2) == 0
			stops = append(stops, stop{code, fmt.Sprint(1 + rng.IntN(2)), float64(1 + rng.IntN(4)), pass})
		}
		prio := []int{20, 40, 50, 100}[rng.IntN(4)]
		trains = append(trains, testTrain(fmt.Sprintf("R%02d", i), prio, float64(50+rng.IntN(60)), 1+float64(rng.IntN(90)), stops...))
	}
	return trains
}

// seeds is how many random runs per world: 8, or PREC_SEEDS for a longer
// soak, e.g. PREC_SEEDS=250 go test ./railway/sim -run RandomTrafficWithPrecedence.
func seeds() int {
	if n, err := strconv.Atoi(os.Getenv("PREC_SEEDS")); err == nil && n > 0 {
		return n
	}
	return 8
}

// TestTrainsKeepToTheirOwnSide runs the main scenario with precedence on.
// TPJ and KKDI are termini, so nobody runs through them and the main-line
// stay limit does not apply: down trains keep pf3, the down main, there. When
// a train cannot have its main line it takes the loop on its own side, never
// one across the other line.
func TestTrainsKeepToTheirOwnSide(t *testing.T) {
	var trains []*ops.Train
	for i := range 6 {
		trains = append(trains,
			newTrain(fmt.Sprintf("U%d", i), float64(i*20+1), [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"}, [2]string{"KKDI", "1"}),
			newTrain(fmt.Sprintf("D%d", i), float64(i*20+1), [2]string{"KKDI", "3"}, [2]string{"PDKT", "3"}, [2]string{"TPJ", "3"}))
	}
	for i, tr := range trains {
		tr.Priority = []int{50, 20, 40}[i%3]
	}
	_, log := runWith(t, worlds.BuildTpjKkdiWorld(), precedenceConfig(), nil, trains...)
	for _, e := range log.find("PLATFORM_CHANGE", map[string]string{"reason": "main_line_stay"}) {
		if st := e.attrs["station"]; st == "TPJ" || st == "KKDI" {
			t.Errorf("%s moved off the main line at terminus %s", e.attrs["train"], st)
		}
	}
	for _, e := range log.find("TRAIN_ARRIVE", nil) {
		pf := e.attrs["track"][len(e.attrs["track"])-1:]
		up := strings.HasPrefix(e.attrs["train"], "U")
		if up != (pf == "1" || pf == "2") {
			t.Errorf("%s used %s, on the other line's side", e.attrs["train"], e.attrs["track"])
		}
	}
}

// receptionTrains fill EST's two platforms with A and B, which stand there
// until t=40, while L runs from WST to EST, calling at LPA. All have the same
// priority, so no hold rule comes into it.
func receptionTrains() []*ops.Train {
	return []*ops.Train{
		testTrain("A", 20, 100, 1, stop{"EST", "1", 39, false}),
		testTrain("B", 20, 100, 1, stop{"EST", "2", 39, false}),
		testTrain("L", 20, 100, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
	}
}

// longestLineStop is the longest the train stood at a signal on its way to
// the station, and on which track.
func longestLineStop(tr *ops.Train, code ids.StnCode) (float64, string) {
	longest, on := 0.0, ""
	for _, sp := range tr.Schedule() {
		if sp.StnCode != code {
			continue
		}
		for _, ss := range sp.SignalStops {
			if d := ss.To - ss.From; d > longest {
				longest, on = d, ss.Track
			}
		}
	}
	return longest, on
}

func TestReceptionHoldKeepsATrainOffTheLine(t *testing.T) {
	// EST is full until t=40: L waits at LPA, not at EST's home signal
	trains := receptionTrains()
	_, log := runWith(t, singleLine(), precedenceConfig(), nil, trains...)
	if got := log.find("PRECEDENCE_HOLD", map[string]string{"train": "L", "at": "LPA", "kind": "reception"}); len(got) == 0 {
		t.Fatalf("L was not held at LPA for want of a track at EST: %+v", log.entries)
	}
	if got := log.find("PRECEDENCE_HOLD", map[string]string{"kind": "reception"}); got[0].attrs["rule"] != "" {
		t.Errorf("a reception hold names rule %q, which it does not ask", got[0].attrs["rule"])
	}
	if d, on := longestLineStop(trains[2], "EST"); d > 0.5 {
		t.Errorf("L stood %.2f min at a signal on %s on the way to EST", d, on)
	}

	// with the hold capped near nothing, it goes and stands out on the line
	cfg := precedenceConfig()
	cfg.Precedence.MaxReceptionHoldMin = 0.01
	trains = receptionTrains()
	runWith(t, singleLine(), cfg, nil, trains...)
	if d, _ := longestLineStop(trains[2], "EST"); d < 5 {
		t.Errorf("without the hold L stood only %.2f min on the way to EST: the scenario does not test it", d)
	}
}

func TestManualPrecedenceMakesNoDecisionsOfItsOwn(t *testing.T) {
	_, log := runWith(t, singleLine(), precedenceConfig(), func(s *sim.Sim) { s.ManualPrecedence = true }, overtakeTrains()...)
	if got := log.find("PRECEDENCE_HOLD", nil); len(got) > 0 {
		t.Errorf("held with ManualPrecedence: %+v", got)
	}
}

func TestHoldUntilArrived(t *testing.T) {
	// X stops at LPA for 10 min on its way west; P waits there for it
	trains := func() []*ops.Train {
		return []*ops.Train{
			testTrain("P", 20, 60, 1, stop{"WST", "1", 2, false}, stop{"LPA", "1", 1, false}, stop{"HLT", "1", 0, true}, stop{"EST", "1", 2, false}),
			testTrain("X", 50, 100, 5, stop{"EST", "1", 2, false}, stop{"HLT", "1", 0, true}, stop{"LPA", "2", 10, false}, stop{"WST", "2", 2, false}),
		}
	}
	at := func(tr *ops.Train, code ids.StnCode) *ops.SchedulePoint {
		for _, sp := range tr.Schedule() {
			if sp.StnCode == code {
				return sp
			}
		}
		t.Fatalf("%s has no stop at %s", tr.Number, code)
		return nil
	}
	for _, until := range []string{sim.HoldUntilArrived, sim.HoldUntilLeft} {
		t.Run(until, func(t *testing.T) {
			ts := trains()
			runWith(t, singleLine(), precedenceConfig(), func(s *sim.Sim) {
				s.ManualPrecedence = true
				if err := s.Override(sim.Override{Action: sim.OverrideHold, Train: "P", For: "X", Station: "LPA", Until: until}); err != nil {
					t.Fatal(err)
				}
			}, ts...)
			p, x := at(ts[0], "LPA"), at(ts[1], "LPA")
			if p.ActDeptTime < x.ActArrTime-1 {
				t.Errorf("P left LPA at %.2f, before X got there at %.2f", p.ActDeptTime, x.ActArrTime)
			}
			if left := p.ActDeptTime >= x.ActDeptTime; left != (until == sim.HoldUntilLeft) {
				t.Errorf("P left LPA at %.2f, X at %.2f: wrong for until %s", p.ActDeptTime, x.ActDeptTime, until)
			}
		})
	}
	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: precedenceConfig()}
	s.SetWorld(singleLine())
	for _, tr := range trains() {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Override(sim.Override{Action: sim.OverrideHold, Train: "P", For: "X", Station: "LPA", Until: "soon"}); err == nil {
		t.Error("unknown until accepted")
	}
}
