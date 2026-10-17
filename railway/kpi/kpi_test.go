package kpi_test

import (
	"math"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/kpi"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/worlds"
)

// upTrain runs TPJ-PDKT-KKDI on the double line, entering at entry.
func upTrain(number string, entry float64) *ops.Train {
	t := &ops.Train{Name: "T" + number, Number: number, MaxSpeed: units.KMPH(110)}
	for i, stn := range []ids.StnCode{"TPJ", "PDKT", "KKDI"} {
		sp := ops.NewStopPoint(stn, 2, "1")
		if i == 0 {
			sp.SetEntryTime(entry)
		}
		t.AddSchedule(sp)
	}
	return t
}

// measure runs the trains, with the disruptions, and reports on the run
// against their free-running times.
func measure(t *testing.T, ds []ops.Disruption, trains ...*ops.Train) *kpi.Report {
	t.Helper()
	world := worlds.BuildTpjKkdiWorld()
	plan := make([]*ops.Train, len(trains))
	for i, tr := range trains {
		plan[i] = tr.Clone()
	}
	if err := ops.ApplyDisruptions(trains, ds); err != nil {
		t.Fatal(err)
	}
	s := &sim.Sim{OutputDir: t.TempDir()}
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
	free, err := kpi.FreeRunning(world, nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	return kpi.Compute(s, coll, kpi.Options{Config: kpi.DefaultConfig(), Free: free, Disruptions: ds})
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestLoneTrainHasNoDelay(t *testing.T) {
	r := measure(t, nil, upTrain("A", 1))
	if r.Finished != 1 || r.Delay == nil {
		t.Fatalf("finished %d, delay %+v", r.Finished, r.Delay)
	}
	if !near(r.Delay.TotalMin, 0) || r.WaitMin != 0 || r.LongestWait != nil {
		t.Errorf("delay %.3f, waits %.3f (%+v), want none", r.Delay.TotalMin, r.WaitMin, r.LongestWait)
	}
	if p := r.Delay.Punctual[0]; p.WithinMin != 1 || p.Share != 1 {
		t.Errorf("punctual %+v, want all within 1 min", p)
	}
	if r.Lateness != nil {
		t.Errorf("lateness %+v for an untimed train", r.Lateness)
	}
	tk := r.PerTrain[0]
	if !near(tk.JourneyMin, *tk.FreeRunMin) || tk.EntryMin != 1 {
		t.Errorf("journey %.3f from %.3f, free running %.3f", tk.JourneyMin, tk.EntryMin, *tk.FreeRunMin)
	}
}

func TestOccupancyAddsUp(t *testing.T) {
	r := measure(t, nil, upTrain("A", 1), upTrain("B", 1))
	u := r.Utilization
	window := u.ToMin - u.FromMin
	if !(window > 0) {
		t.Fatalf("window %.3f-%.3f", u.FromMin, u.ToMin)
	}
	blocks := 0
	for _, tu := range u.Tracks {
		if !near(tu.OccupiedMin, tu.RunningMin+tu.StandingMin) || !near(tu.Occupancy*window, tu.OccupiedMin) {
			t.Errorf("%s: occupied %.3f, running %.3f, standing %.3f, occupancy %.3f", tu.Track, tu.OccupiedMin, tu.RunningMin, tu.StandingMin, tu.Occupancy)
		}
		if tu.Occupancy > 1+1e-9 {
			t.Errorf("%s: occupied %.0f%% of the window", tu.Track, 100*tu.Occupancy)
		}
		if tu.Kind == kpi.KindBlock && tu.Entries > 0 {
			blocks++
			if tu.Entries != 2 {
				t.Errorf("%s entered %d times, want 2", tu.Track, tu.Entries)
			}
		}
	}
	if blocks == 0 || u.Blocks.Tracks == 0 {
		t.Errorf("no blocks used (%d tracks in the summary)", u.Blocks.Tracks)
	}
	if len(u.Stations) == 0 || u.Stations[0].Stn != "TPJ" {
		t.Errorf("stations %+v, want TPJ first, the way the trains run", u.Stations)
	}
}

func TestWaitingTrainIsDelayed(t *testing.T) {
	// both want TPJ pf1 at once: one waits for the other
	r := measure(t, nil, upTrain("A", 1), upTrain("B", 1))
	if r.Delay.TotalMin < 1 || r.Delay.Worst == "" {
		t.Fatalf("delay %+v, want one train late", r.Delay)
	}
	if r.LongestWait == nil || r.LongestWait.Train != r.Delay.Worst {
		t.Errorf("longest wait %+v, want one by the worst train %s", r.LongestWait, r.Delay.Worst)
	}
	// the waiting train may get another platform, whose way out runs a
	// little quicker or slower, so the two differ by that much
	if math.Abs(r.WaitMin-r.Delay.TotalMin) > 1 {
		t.Errorf("waits %.2f min, but delay %.2f", r.WaitMin, r.Delay.TotalMin)
	}
}

func TestDisruptionsArePrimaryDelay(t *testing.T) {
	ds := []ops.Disruption{
		{Train: "A", Kind: ops.DisruptStand, Stn: "PDKT", Min: 10},
		{Train: "B", Kind: ops.DisruptLateEntry, Min: 7},
	}
	// far enough apart not to meet
	r := measure(t, ds, upTrain("A", 1), upTrain("B", 200))
	if r.Disruption == nil || !near(r.Disruption.PrimaryMin, 17) || !near(r.Disruption.ReactionaryMin, 0) {
		t.Fatalf("disruption %+v, want 17 min primary and none reactionary", r.Disruption)
	}
	for _, tk := range r.PerTrain {
		want := map[string]float64{"A": 10, "B": 7}[tk.Train]
		if !near(*tk.DelayMin, want) || tk.EntryMin != map[string]float64{"A": 1, "B": 200}[tk.Train] {
			t.Errorf("%s: delay %.3f from planned entry %.3f, want %.0f", tk.Train, *tk.DelayMin, tk.EntryMin, want)
		}
		if tk.WaitMin != 0 {
			t.Errorf("%s: waits %+v; a disruption is not a wait", tk.Train, tk.Waits)
		}
	}
}

func TestLatenessAgainstTheTimetable(t *testing.T) {
	tr := &ops.Train{Name: "Timed", Number: "A", MaxSpeed: units.KMPH(110)}
	tr.AddSchedule(ops.NewStopPoint("TPJ", 2, "1").SetEntryTime(1))
	tr.AddSchedule(ops.NewArrDepPoint("PDKT", 10, 12, "1")) // too soon to get there
	tr.AddSchedule(ops.NewArrDepPoint("KKDI", 500, 502, "1"))
	r := measure(t, nil, tr)
	if r.Lateness == nil || r.Lateness.Trains != 1 {
		t.Fatalf("lateness %+v, want the timed train", r.Lateness)
	}
	// late at PDKT, but early at KKDI, its last timed point
	if r.Lateness.TotalMin != 0 {
		t.Errorf("lateness %.3f, want 0: it arrives at KKDI early", r.Lateness.TotalMin)
	}
}

func TestLoadConfig(t *testing.T) {
	cfg, err := kpi.LoadConfig(nil)
	if err != nil || len(cfg.PunctualWithinMin) != 3 {
		t.Errorf("defaults %+v, %v", cfg, err)
	}
	if _, err := kpi.LoadConfig([]byte(`{"punctual_within": [1]}`)); err == nil {
		t.Error("unknown field accepted")
	}
	if _, err := kpi.LoadConfig([]byte(`{"punctual_within_min": [-1]}`)); err == nil {
		t.Error("negative threshold accepted")
	}
}
