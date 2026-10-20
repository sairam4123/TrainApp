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
	return measureWith(t, kpi.DefaultConfig(), ds, trains...)
}

// measureWith is measure with the report's settings.
func measureWith(t *testing.T, cfg kpi.Config, ds []ops.Disruption, trains ...*ops.Train) *kpi.Report {
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
	return kpi.Compute(s, coll, kpi.Options{Config: cfg, Free: free, Disruptions: ds})
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

func TestCapacityColorBand(t *testing.T) {
	for _, bad := range []string{
		`{"capacity_color_band": [{"over_pct": -5, "name": "x"}]}`,
		`{"capacity_color_band": [{"over_pct": 0, "name": "a"}, {"over_pct": 0, "name": "b"}]}`,
		`{"capacity_color_band": [{"over_pct": 0}]}`,
		`{"capacity_color_band": [{"over_pct": 0, "name": "a", "color": "red"}]}`,
		`{"capacity_color_band": [{"over_pct": 0, "name": "a", "color": "256"}]}`,
		`{"delay_color_band": [{"over_min": -1, "name": "early"}]}`,
		`{"delay_color_band": [{"over_min": 5}]}`,
		`{"delay_color_band": [{"over_pct": 5, "name": "late"}]}`,
	} {
		if _, err := kpi.LoadConfig([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	cfg, err := kpi.LoadConfig([]byte(`{"capacity_color_band": [
		{"over_pct": 60, "name": "heavy", "color": "214"},
		{"over_pct": 10, "name": "light"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PunctualWithinMin) != 3 {
		t.Errorf("punctual_within_min %v, want the defaults kept", cfg.PunctualWithinMin)
	}

	// the report carries the bands, lowest first
	u := measureWith(t, cfg, nil, upTrain("A", 1)).Utilization
	if len(u.Bands) != 2 || u.Bands[0].Name != "light" || u.Bands[1].Color != "214" {
		t.Errorf("bands %+v, want light then heavy", u.Bands)
	}
	u.Bands = nil
	if u.Band(0.5) != -1 {
		t.Error("a band without any bands")
	}
	u.Bands = []kpi.CapacityBand{{OverPct: 10, Name: "light"}, {OverPct: 60, Name: "heavy"}}
	for share, want := range map[float64]int{0.05: -1, 0.1: 0, 0.59: 0, 0.6: 1, 1: 1} {
		if got := u.Band(share); got != want {
			t.Errorf("band of %.0f%% is %d, want %d", 100*share, got, want)
		}
	}
}

func TestDelayColorBand(t *testing.T) {
	cfg, err := kpi.LoadConfig([]byte(`{"delay_color_band": [
		{"over_min": 15, "name": "late", "color": "203"},
		{"over_min": 0, "name": "on time"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := measureWith(t, cfg, nil, upTrain("A", 1))
	if len(r.DelayBands) != 2 || r.DelayBands[0].Name != "on time" {
		t.Fatalf("bands %+v, want on time then late", r.DelayBands)
	}
	for min, want := range map[float64]int{0: 0, 14.9: 0, 15: 1, 90: 1} {
		if got := r.DelayBand(min); got != want {
			t.Errorf("band of %g min is %d, want %d", min, got, want)
		}
	}
}

// callGains is what a train gained on the way to each point and standing
// there, in all.
func callGains(tk kpi.Train) float64 {
	sum := 0.0
	for _, c := range tk.Calls {
		if c.ApproachMin != nil {
			sum += *c.ApproachMin
		}
		if c.StationMin != nil {
			sum += *c.StationMin
		}
	}
	return sum
}

func TestLoneTrainIsOnTimeEverywhere(t *testing.T) {
	r := measure(t, nil, upTrain("A", 1))
	tk := r.PerTrain[0]
	if len(tk.Calls) != 3 {
		t.Fatalf("%d calls, want one per point", len(tk.Calls))
	}
	for _, c := range tk.Calls {
		if c.DelayMin == nil || !near(*c.DelayMin, 0) || c.LateMin != nil {
			t.Errorf("call at %s: delay %v, late %v; want on time and untimed", c.Stn, c.DelayMin, c.LateMin)
		}
	}
	if len(r.StationDelays) != 3 || r.StationDelays[0].Stn != "TPJ" {
		t.Fatalf("stations %+v, want TPJ, PDKT, KKDI", r.StationDelays)
	}
	for _, sd := range r.StationDelays {
		if sd.Calls != 1 || sd.Punctual[0].Share != 1 {
			t.Errorf("%s: %d calls, %+v", sd.Stn, sd.Calls, sd.Punctual)
		}
	}
	if len(r.BlockDelays) == 0 {
		t.Fatal("no block delays")
	}
	for _, bd := range r.BlockDelays {
		if bd.Trains != 1 || !near(bd.GainedMin, 0) {
			t.Errorf("block %s: %+v, want one train gaining nothing", bd.Track, bd)
		}
	}
}

// The delay a train gains call by call adds up to its delay.
func TestCallGainsAddUpToTheDelay(t *testing.T) {
	r := measure(t, nil, upTrain("A", 1), upTrain("B", 1))
	for _, tk := range r.PerTrain {
		if !near(callGains(tk), *tk.DelayMin) {
			t.Errorf("%s gained %.3f call by call, but is %.3f late", tk.Train, callGains(tk), *tk.DelayMin)
		}
	}
	late := r.Delay.Worst
	for _, sd := range r.StationDelays {
		if sd.Calls != 2 {
			t.Errorf("%s: %d calls, want both trains", sd.Stn, sd.Calls)
		}
		if sd.Stn == "KKDI" && (sd.Worst != late || sd.Punctual[0].Trains != 1) {
			t.Errorf("KKDI: worst %q, %+v; want %s late and the other on time", sd.Worst, sd.Punctual[0], late)
		}
	}
}

func TestDisruptionsShowWhereTheyHappen(t *testing.T) {
	ds := []ops.Disruption{
		{Train: "A", Kind: ops.DisruptStand, Stn: "PDKT", Min: 10},
		{Train: "B", Kind: ops.DisruptLateEntry, Min: 7},
	}
	r := measure(t, ds, upTrain("A", 1), upTrain("B", 200))
	for _, tk := range r.PerTrain {
		c := map[string]kpi.Call{}
		for _, call := range tk.Calls {
			c[call.Stn] = call
		}
		switch tk.Train {
		case "A": // stands 10 min longer at PDKT
			if !near(*c["PDKT"].StationMin, 10) || !near(*c["PDKT"].DelayMin, 0) || !near(*c["KKDI"].DelayMin, 10) {
				t.Errorf("A at PDKT gained %.2f (arrived %.2f late), at KKDI %.2f late; want 10 gained at PDKT", *c["PDKT"].StationMin, *c["PDKT"].DelayMin, *c["KKDI"].DelayMin)
			}
		case "B": // enters 7 min late
			if !near(*c["TPJ"].ApproachMin, 7) || !near(*c["KKDI"].DelayMin, 7) {
				t.Errorf("B gained %.2f getting onto the line, is %.2f late at KKDI; want 7", *c["TPJ"].ApproachMin, *c["KKDI"].DelayMin)
			}
		}
	}
	for _, sd := range r.StationDelays {
		if sd.Stn == "PDKT" && !near(sd.StationMin, 10) {
			t.Errorf("PDKT: %.2f gained standing, want 10", sd.StationMin)
		}
	}
}

func TestLatenessAtEachStation(t *testing.T) {
	tr := &ops.Train{Name: "Timed", Number: "A", MaxSpeed: units.KMPH(110)}
	tr.AddSchedule(ops.NewStopPoint("TPJ", 2, "1").SetEntryTime(1))
	tr.AddSchedule(ops.NewArrDepPoint("PDKT", 10, 12, "1")) // too soon to get there
	tr.AddSchedule(ops.NewArrDepPoint("KKDI", 500, 502, "1"))
	r := measure(t, nil, tr)
	calls := r.PerTrain[0].Calls
	if calls[1].LateMin == nil || *calls[1].LateMin <= 0 || calls[2].LateMin == nil || *calls[2].LateMin != 0 {
		t.Errorf("late at PDKT %v, at KKDI %v; want late, then on time", calls[1].LateMin, calls[2].LateMin)
	}
	for _, sd := range r.StationDelays {
		if want := map[string]int{"TPJ": 0, "PDKT": 1, "KKDI": 1}[sd.Stn]; sd.LateCalls != want {
			t.Errorf("%s: %d timed calls, want %d", sd.Stn, sd.LateCalls, want)
		}
	}
}
