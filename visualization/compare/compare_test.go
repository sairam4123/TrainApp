package compare

import (
	"bytes"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/kpi/report"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"
)

func world(t *testing.T, name string) *vizproto.World {
	t.Helper()
	w, err := worlds.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return vizproto.FromWorld(w)
}

func TestSameWorld(t *testing.T) {
	a, b := world(t, "tpjkkdi-sl"), world(t, "tpjkkdi-sl")
	b.Trains = []vizproto.Train{{Number: "1"}} // trains and labels don't count
	b.Tracks[0].Siding, b.Tracks[0].Name = "Gd9", "renamed"
	if d := SameWorld(a, b); len(d) != 0 {
		t.Errorf("the same world differs: %v", d)
	}
	gone := b.Tracks[3].ID
	b.Tracks = slices.Delete(b.Tracks, 3, 4)
	if d := SameWorld(a, b); len(d) != 1 || d[0] != "track "+gone+" only in A" {
		t.Errorf("a track removed: %v", d)
	}
	c := world(t, "tpjkkdi-sl")
	c.Stations[0].Platforms = append(c.Stations[0].Platforms, vizproto.Platform{No: "9", Track: c.Tracks[0].ID})
	if d := SameWorld(a, c); len(d) != 1 || !strings.HasPrefix(d[0], "station "+c.Stations[0].Code) {
		t.Errorf("a platform added: %v", d)
	}
}

func TestWorldsAndSharedStations(t *testing.T) {
	sl, dl := world(t, "tpjkkdi-sl"), world(t, "tpjkkdi-dl")
	if g := Worlds([]*vizproto.World{sl, dl, world(t, "tpjkkdi-sl")}); !slices.Equal(g, []int{1, 2, 1}) {
		t.Errorf("groups %v, want [1 2 1]", g)
	}
	shared := SharedStations([]*vizproto.World{sl, dl})
	if len(shared) == 0 || !slices.Contains(shared, "TPJ") || !slices.Contains(shared, "KKDI") {
		t.Fatalf("shared stations %v", shared)
	}
	for _, code := range shared {
		if !slices.ContainsFunc(dl.Stations, func(s vizproto.Station) bool { return s.Code == code }) {
			t.Errorf("%s is not on the double line", code)
		}
	}
	if len(shared) > len(dl.Stations) || len(shared) > len(sl.Stations) {
		t.Errorf("%d shared stations, more than a world has", len(shared))
	}
}

func f(v float64) *float64 { return &v }

// train is a finished train entering at entry, calling at the stations,
// with delay minutes lost to the causes in turn, a minute each.
func train(no string, entry, delay float64, stns ...string) report.Train {
	t := report.Train{Train: no, Finished: true, EntryMin: entry, ExitMin: entry + 60, DelayMin: f(delay)}
	for _, s := range stns {
		t.Calls = append(t.Calls, report.Call{Stn: s})
	}
	if delay > 0 {
		t.Losses = []report.Loss{
			{Cause: report.CauseHeld, Kind: "cross", Where: report.WhereStation, At: stns[0], Min: delay / 2},
			{Cause: report.CauseSignal, Where: report.WhereSection, At: stns[0] + "–" + stns[1], Min: delay / 2},
		}
		t.Waits = []report.Wait{{Train: no, Kind: report.WaitStation, At: stns[0], Min: delay / 2}}
		t.WaitMin = delay / 2
	}
	return t
}

func run(trains ...report.Train) *report.Report {
	r := &report.Report{Trains: len(trains), PerTrain: trains}
	total, worst := 0.0, ""
	for _, t := range trains {
		r.Finished++
		total += *t.DelayMin
		r.WaitMin += t.WaitMin
		if worst == "" || *t.DelayMin > r.Delay.WorstMin {
			worst = t.Train
			if r.Delay == nil {
				r.Delay = &report.Stats{}
			}
			r.Delay.Worst, r.Delay.WorstMin = t.Train, *t.DelayMin
		}
	}
	if r.Delay == nil {
		r.Delay = &report.Stats{}
	}
	r.Delay.Trains, r.Delay.TotalMin, r.Delay.MeanMin = len(trains), total, total/float64(len(trains))
	r.Delay.Punctual = []report.Punctuality{{WithinMin: 5, Share: 0.5}}
	return r
}

func TestSchedules(t *testing.T) {
	base := run(train("1U", 0, 4, "TPJ", "PDKT"), train("2D", 10, 0, "PDKT", "TPJ"))
	same := run(train("1U", 0, 8, "TPJ", "PDKT"), train("2D", 10, 1, "PDKT", "TPJ"))
	later := run(train("1U", 5, 4, "TPJ", "PDKT"), train("2D", 10, 0, "PDKT", "TPJ"))
	other := run(train("1U", 0, 4, "TPJ", "PDKT"), train("3D", 10, 0, "PDKT", "TPJ"))
	for _, c := range []struct {
		run  *report.Report
		want Kind
	}{{same, SameSchedule}, {later, Retimed}, {other, OtherSchedule}, {run(train("1U", 0, 0, "TPJ", "PDKT")), OtherSchedule}} {
		if got := Schedules(base, c.run); got != c.want {
			t.Errorf("got %s, want %s", got, c.want)
		}
	}
	if n := RetimedTrains(base, later); n != 1 {
		t.Errorf("%d retimed trains, want 1", n)
	}
	if k := Kinds([]*report.Report{base, same, same}, []int{1, 1, 2}); !slices.Equal(k, []Kind{SameSchedule, SameSchedule, OtherWorld}) {
		t.Errorf("kinds %v", k)
	}
}

func TestTrains(t *testing.T) {
	a := run(train("1U", 0, 4, "TPJ", "PDKT"), train("2D", 10, 0, "PDKT", "TPJ"))
	b := run(train("1U", 5, 2, "TPJ", "PDKT"))
	c := run(train("2D", 10, 1, "PDKT", "TPJ"), train("3U", 20, 0, "TPJ", "PDKT"))
	rows := Trains([]*report.Report{a, b, c})
	if got := []string{rows[0].Train, rows[1].Train, rows[2].Train}; len(rows) != 3 || !slices.Equal(got, []string{"1U", "2D", "3U"}) {
		t.Fatalf("rows %v", got)
	}
	if rows[0].Runs[2] != nil || rows[1].Runs[1] != nil || rows[2].Runs[0] != nil {
		t.Error("a train missing from a run has a row there")
	}
	if !rows[0].Retimed[1] || rows[1].Retimed[2] {
		t.Errorf("retimed %v %v", rows[0].Retimed, rows[1].Retimed)
	}
}

func TestFigures(t *testing.T) {
	base := run(train("1U", 0, 4, "TPJ", "PDKT"), train("2D", 10, 0, "PDKT", "TPJ"))
	better := run(train("1U", 0, 2, "TPJ", "PDKT"), train("2D", 10, 0, "PDKT", "TPJ"))
	more := run(train("1U", 0, 2, "TPJ", "PDKT"), train("2D", 10, 0, "PDKT", "TPJ"), train("3U", 20, 3, "TPJ", "PDKT"))
	better.Holds, more.Holds = 3, 3
	figs := Figures([]*report.Report{base, better, more}, []Kind{SameSchedule, SameSchedule, OtherSchedule})

	mean, _ := Find(figs, "delay_mean")
	if d, ok := mean.Delta(1); !ok || d != -1 || mean.Verdict(1) != 1 || mean.DeltaText(1) != "-1.0" {
		t.Errorf("mean delay Δ %v %v, verdict %d, %q", d, ok, mean.Verdict(1), mean.DeltaText(1))
	}
	if _, ok := mean.Delta(2); !ok {
		t.Error("a mean does not compare across schedules")
	}
	total, _ := Find(figs, "delay_total")
	if _, ok := total.Delta(2); ok || total.Values[2].Fair || total.DeltaText(2) != "—" {
		t.Error("a total compares across schedules")
	}
	if _, ok := total.Delta(1); !ok {
		t.Error("a total does not compare over the same trains")
	}
	holds, _ := Find(figs, "holds")
	if holds.Verdict(1) != 0 {
		t.Error("more holds are called better or worse")
	}
	worst, _ := Find(figs, "delay_worst")
	if worst.Text(0) != "4.0 1U" {
		t.Errorf("worst %q", worst.Text(0))
	}
	fin, _ := Find(figs, "finished")
	if fin.Text(2) != "3/3" {
		t.Errorf("finished %q", fin.Text(2))
	}
	if _, ok := Find(figs, "primary"); ok {
		t.Error("disruption figures without disruptions")
	}
	ot, _ := Find(figs, "on_time_5")
	if ot.Text(0) != "50%" || ot.DeltaText(1) != "0" {
		t.Errorf("on time %q, Δ %q", ot.Text(0), ot.DeltaText(1))
	}
}

func TestCausesSumToDelay(t *testing.T) {
	a := run(train("1U", 0, 4, "TPJ", "PDKT"), train("2D", 10, 0, "PDKT", "TPJ"))
	b := run(train("1U", 0, 1, "TPJ", "PDKT"), train("2D", 10, 6, "PDKT", "TPJ"))
	reps := []*report.Report{a, b}
	for _, rows := range [][]CauseRow{Causes(reps), CausesByPlace(reps)} {
		sum := 0.0
		for _, r := range rows {
			sum += r.Min[1] - r.Min[0]
		}
		if want := b.Delay.TotalMin - a.Delay.TotalMin; math.Abs(sum-want) > 1e-9 {
			t.Errorf("the Δs sum to %.2f, the total delay's Δ is %.2f", sum, want)
		}
	}
	places := WaitPlaces(reps)
	if len(places) != 2 || places[0].At != "TPJ" || places[0].Min[0] != 2 || places[0].Trains[1] != 1 {
		t.Errorf("wait places %+v", places[0])
	}
}

func TestBench(t *testing.T) {
	a := run(train("1U", 0, 4, "TPJ", "PDKT"))
	b := run(train("1U", 0, 2, "TPJ", "PDKT"))
	c := run(train("9U", 0, 2, "TPJ", "PDKT"))
	old := []report.BenchRun{{Scenario: "sl-6", Rule: "", Report: a, WallSec: 2}, {Scenario: "sl-6", Rule: "off", Report: a}, {Scenario: "gone", Rule: "off", Report: a}}
	var buf bytes.Buffer
	if err := report.WriteBench(&buf, old); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/old.json"
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := report.ReadBench(path)
	if err != nil || len(old) != 3 || old[0].Report.PerTrain[0].Train != "1U" {
		t.Fatalf("read back %v, %v", old, err)
	}
	d := Bench(old, []report.BenchRun{{Scenario: "sl-6", Rule: "profile", Report: b, WallSec: 1}, {Scenario: "sl-6", Rule: "off", Report: c}, {Scenario: "new", Rule: "off", Report: c}})
	if len(d.Pairs) != 2 || len(d.OnlyOld) != 1 || d.OnlyOld[0].Scenario != "gone" || len(d.OnlyNew) != 1 || d.OnlyNew[0].Scenario != "new" {
		t.Fatalf("pairs %d, only old %v, only new %v", len(d.Pairs), d.OnlyOld, d.OnlyNew)
	}
	if w, ok := Find(d.Pairs[0].Figures, "wall"); !ok || w.DeltaText(1) != "-1.0s" {
		t.Error("no wall time Δ for the profile rule, though both files have one")
	}
	if d.Pairs[1].Kind != OtherSchedule {
		t.Errorf("kind %s for changed trains", d.Pairs[1].Kind)
	}
	if total, _ := Find(d.Pairs[1].Figures, "delay_total"); total.DeltaText(1) != "—" {
		t.Error("a total compares though the trains changed")
	}
}
