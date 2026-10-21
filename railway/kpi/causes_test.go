package kpi_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/kpi"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/worlds"
)

// downTrain runs KKDI-PDKT-TPJ on the double line, entering at entry.
func downTrain(number string, entry float64) *ops.Train {
	t := &ops.Train{Name: "T" + number, Number: number, MaxSpeed: units.KMPH(110)}
	for i, stn := range []ids.StnCode{"KKDI", "PDKT", "TPJ"} {
		sp := ops.NewStopPoint(stn, 2, "3")
		if i == 0 {
			sp.SetEntryTime(entry)
		}
		t.AddSchedule(sp)
	}
	return t
}

// slTrain runs the single-line world through stops, each a station and
// its platform; a dwell of -1 runs through.
func slTrain(number string, prio int, kmph, entry float64, stops ...string) *ops.Train {
	t := &ops.Train{Name: "T" + number, Number: number, MaxSpeed: units.KMPH(kmph), Priority: prio}
	for i, s := range stops {
		code, pf, _ := strings.Cut(s, ":")
		sp := ops.NewStopPoint(ids.StnCode(code), 2, pf)
		if i > 0 && i < len(stops)-1 && strings.HasSuffix(pf, "!") {
			sp = ops.NewPassPoint(ids.StnCode(code), -1, strings.TrimSuffix(pf, "!"))
		}
		if i == 0 {
			sp.SetEntryTime(entry)
		}
		t.AddSchedule(sp)
	}
	return t
}

func singleLineWorld() *infra.World {
	return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2})
}

// precedence holds trains for faster ones and for crossings.
func precedence() *sim.PriorityConfig {
	return &sim.PriorityConfig{Precedence: &sim.PrecedenceConfig{
		HorizonMin: 40, HeadwayMin: 1, MaxHoldMin: 30, MainLineMaxStayMin: 5, MinPriorityGap: 10, MinSavingMin: 1,
	}}
}

// measureOn is measureWith on another world, with a priority config.
func measureOn(t *testing.T, world *infra.World, prio *sim.PriorityConfig, ds []ops.Disruption, trains ...*ops.Train) *kpi.Report {
	t.Helper()
	plan := make([]*ops.Train, len(trains))
	for i, tr := range trains {
		plan[i] = tr.Clone()
	}
	if err := ops.ApplyDisruptions(trains, ds); err != nil {
		t.Fatal(err)
	}
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
	coll := kpi.NewCollector()
	s.Observer = coll
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	free, err := kpi.FreeRunning(world, prio, plan)
	if err != nil {
		t.Fatal(err)
	}
	return kpi.Compute(s, coll, kpi.Options{Config: kpi.DefaultConfig(), Free: free, Disruptions: ds})
}

// overtakeAndCross has P held at LPA for the express X behind it, and for
// Y coming the other way.
func overtakeAndCross(t *testing.T) *kpi.Report {
	return measureOn(t, singleLineWorld(), precedence(), nil,
		slTrain("P", 20, 60, 1, "WST:1", "LPA:1", "HLT:1!", "EST:1"),
		slTrain("X", 50, 100, 12, "WST:2", "LPA:1!", "HLT:1!", "EST:2"),
		slTrain("Y", 50, 100, 30, "EST:1", "HLT:1!", "LPA:2!", "WST:2"))
}

// checkSums checks every finished train's losses add up to its delay, and
// every track's split to its occupied minutes.
func checkSums(t *testing.T, r *kpi.Report) {
	t.Helper()
	for _, tk := range r.PerTrain {
		if tk.DelayMin == nil {
			continue
		}
		if len(tk.Losses) == 0 && math.Abs(*tk.DelayMin) > 1e-6 {
			t.Errorf("%s: %.3f min late, no losses", tk.Train, *tk.DelayMin)
		}
		sum := 0.0
		for _, l := range tk.Losses {
			sum += l.Min
			if l.Where != kpi.WhereEntry && l.Track == "" {
				t.Errorf("%s: loss without a track: %+v", tk.Train, l)
			}
		}
		if math.Abs(sum-*tk.DelayMin) > 1e-6 {
			t.Errorf("%s: losses sum to %.6f, delay %.6f: %+v", tk.Train, sum, *tk.DelayMin, tk.Losses)
		}
	}
	for _, tu := range r.Utilization.Tracks {
		sum := 0.0
		for _, s := range tu.Split {
			sum += s.Min
		}
		if math.Abs(sum-tu.OccupiedMin) > 1e-6 {
			t.Errorf("%s: split sums to %.6f, occupied %.6f: %+v", tu.Track, sum, tu.OccupiedMin, tu.Split)
		}
	}
}

func TestLossesAddUp(t *testing.T) {
	slow := upTrain("S", 1)
	slow.MaxSpeed = units.KMPH(50)
	checkSums(t, measure(t, []ops.Disruption{{Train: "S", Kind: "stand", Stn: "PDKT", Min: 6}, {Train: "D", Kind: "late_entry", Min: 4}},
		slow, upTrain("F", 3), downTrain("D", 2), downTrain("E", 3)))
	checkSums(t, overtakeAndCross(t))
	checkSums(t, measureOn(t, singleLineWorld(), nil, nil,
		slTrain("A", 0, 100, 1, "WST:1", "LPA:1", "HLT:1", "EST:1"),
		slTrain("B", 0, 100, 2, "EST:1", "HLT:1", "LPA:2", "WST:2")))
}

func TestLoneTrainLosesNothing(t *testing.T) {
	r := measure(t, nil, upTrain("A", 1))
	for _, l := range r.PerTrain[0].Losses {
		if math.Abs(l.Min) > 0.01 {
			t.Errorf("loss %+v", l)
		}
	}
	for _, tu := range r.Utilization.Tracks {
		for _, s := range tu.Split {
			if s.Cause != kpi.CauseFree && s.Cause != kpi.CauseDwell && s.Min > 0.01 {
				t.Errorf("%s: %+v", tu.Track, s)
			}
		}
	}
}

func TestHeldTrainLosesToTheHold(t *testing.T) {
	r := overtakeAndCross(t)
	held := map[string]float64{} // kind for train at place
	for _, tk := range r.PerTrain {
		for _, l := range tk.Losses {
			if l.Cause == kpi.CauseHeld {
				held[tk.Train+" "+l.Kind+" for "+l.For+" at "+l.At] += l.Min
			}
		}
	}
	forX := 0.0
	for k, m := range held {
		if strings.HasPrefix(k, "P ") && strings.HasSuffix(k, " for X at LPA") {
			forX += m
		}
	}
	if forX <= 0 {
		t.Errorf("P lost nothing to its holds for X at LPA: %v", held)
	}
	if held["P cross for Y at LPA"] <= 0 {
		t.Errorf("P lost nothing to its hold for Y at LPA: %v", held)
	}
	for _, tk := range r.PerTrain {
		for _, w := range tk.Waits {
			if tk.Train == "P" && w.At == "LPA" && (w.Cause != kpi.CauseHeld || w.For == "") {
				t.Errorf("P's wait at LPA: %+v, want held for a train", w)
			}
		}
	}
}

func TestSignalStopsNameWhoTheyWaitedFor(t *testing.T) {
	// without precedence, B follows A over the single line and stops at its
	// signals
	r := measureOn(t, singleLineWorld(), nil, nil,
		slTrain("A", 0, 50, 1, "WST:1", "LPA:1!", "HLT:1!", "EST:1"),
		slTrain("B", 0, 120, 3, "WST:2", "LPA:2!", "HLT:1!", "EST:2"))
	stops := 0
	for _, tk := range r.PerTrain {
		for _, w := range tk.Waits {
			if w.Kind != kpi.WaitSignal {
				continue
			}
			stops++
			found := false
			for _, l := range tk.Losses {
				if l.Cause == kpi.CauseSignal && l.Track == w.At && l.For == w.For && l.Where == kpi.WhereSection {
					found = true
				}
			}
			if !found || w.Cause != kpi.CauseSignal {
				t.Errorf("%s's signal stop %+v has no loss: %+v", tk.Train, w, tk.Losses)
			}
		}
	}
	if stops == 0 {
		t.Fatalf("no signal stops: %+v", r.PerTrain)
	}
}

func TestDisruptionsAreTheirOwnCause(t *testing.T) {
	r := measure(t, []ops.Disruption{
		{Train: "A", Kind: "stand", Stn: "PDKT", Min: 10},
		{Train: "B", Kind: "late_entry", Min: 5},
	}, upTrain("A", 1), upTrain("B", 200))
	got := map[string]float64{}
	for _, tk := range r.PerTrain {
		for _, l := range tk.Losses {
			if l.Cause == kpi.CauseDisruption {
				got[tk.Train+" "+l.Kind+" "+l.Where+" "+l.At] += l.Min
			}
		}
	}
	if !near(got["A stand station PDKT"], 10) {
		t.Errorf("A's stand: %v", got)
	}
	if !near(got["B late_entry entry TPJ"], 5) {
		t.Errorf("B's late entry: %v", got)
	}
}

func TestSectionsFollowTheLine(t *testing.T) {
	sections := func(r *kpi.Report) map[string]bool {
		out := map[string]bool{}
		for _, tu := range r.Utilization.Tracks {
			if tu.Kind == kpi.KindBlock && tu.Entries > 0 {
				out[tu.Section] = true
			}
		}
		return out
	}
	double := sections(measure(t, nil, upTrain("A", 1), downTrain("B", 1)))
	for _, want := range []string{"TPJ→PDKT", "PDKT→KKDI", "KKDI→PDKT", "PDKT→TPJ"} {
		if !double[want] {
			t.Errorf("double line sections %v, want %s", double, want)
		}
	}
	single := sections(measureOn(t, singleLineWorld(), nil, nil,
		slTrain("A", 0, 100, 1, "WST:1", "LPA:1", "HLT:1", "EST:1"),
		slTrain("B", 0, 100, 60, "EST:1", "HLT:1", "LPA:2", "WST:2")))
	for _, want := range []string{"WST–LPA", "LPA–HLT", "HLT–EST"} {
		if !single[want] {
			t.Errorf("single line sections %v, want %s", single, want)
		}
	}
	for s := range single {
		if strings.Contains(s, "→") {
			t.Errorf("single line section %s has a direction", s)
		}
	}
}

func TestParetoPct(t *testing.T) {
	if got := kpi.DefaultConfig().ParetoPct; got != 80 {
		t.Errorf("default pareto_pct %g, want 80", got)
	}
	for raw, ok := range map[string]bool{`{"pareto_pct": 50}`: true, `{}`: true, `{"pareto_pct": 0}`: false, `{"pareto_pct": 101}`: false} {
		cfg, err := kpi.LoadConfig(json.RawMessage(raw))
		if (err == nil) != ok {
			t.Errorf("%s: %v", raw, err)
		}
		if ok && raw == `{}` && cfg.ParetoPct != 80 {
			t.Errorf("%s: pareto_pct %g", raw, cfg.ParetoPct)
		}
	}
	cfg := kpi.DefaultConfig()
	cfg.ParetoPct = 70
	if r := measureWith(t, cfg, nil, upTrain("A", 1)); r.ParetoPct != 70 || r.Pareto() != 70 {
		t.Errorf("report pareto_pct %g", r.ParetoPct)
	}
}
