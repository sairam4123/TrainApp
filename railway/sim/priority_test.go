package sim_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/worlds"
)

// retryLog records which trains got a route after waiting, in order.
type retryLog struct{ trains []string }

func (r *retryLog) Logged(_ slog.Level, event string, attrs []slog.Attr) {
	if event != "ROUTE_RETRY_OK" {
		return
	}
	for _, a := range attrs {
		if a.Key == "train" {
			r.trains = append(r.trains, a.Value.String())
		}
	}
}

func (r *retryLog) AfterEvent(des.Event[ops.RailwayEvent]) {}

// entryQueue fills TPJ's four boundary platforms at t=1, so E4 and E5, added
// last in that order, both wait to enter. setup can adjust the sim before Run.
func entryQueue(t *testing.T, setup func(s *sim.Sim, trains []*ops.Train)) (*sim.Sim, []string) {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	var trains []*ops.Train
	for i := range 6 {
		tr := newTrain(fmt.Sprintf("E%d", i), 1, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})
		trains = append(trains, tr)
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	rec := &retryLog{}
	s.Observer = rec
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(s, trains)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	assertAllExitedAndClean(t, s)
	return s, rec.trains
}

// firstOf returns whichever of a and b appears first in order.
func firstOf(order []string, a, b string) string {
	for _, n := range order {
		if n == a || n == b {
			return n
		}
	}
	return ""
}

func TestEqualPriorityIsFirstComeFirstServed(t *testing.T) {
	_, order := entryQueue(t, nil)
	if got := firstOf(order, "E4", "E5"); got != "E4" {
		t.Fatalf("retries %v: %s entered first, want E4 (queued first)", order, got)
	}
}

func TestHigherPriorityIsServedFirst(t *testing.T) {
	_, order := entryQueue(t, func(_ *sim.Sim, trains []*ops.Train) {
		trains[5].Priority = 10
	})
	if got := firstOf(order, "E4", "E5"); got != "E5" {
		t.Fatalf("retries %v: %s entered first, want E5 (higher priority)", order, got)
	}
}

func TestSetPriorityAtChangesTheOrder(t *testing.T) {
	_, order := entryQueue(t, func(s *sim.Sim, _ []*ops.Train) {
		// both are already waiting by t=2
		if err := s.SetPriorityAt(2, ids.TrainID{Number: "E5"}, 10); err != nil {
			t.Fatal(err)
		}
	})
	if got := firstOf(order, "E4", "E5"); got != "E5" {
		t.Fatalf("retries %v: %s entered first, want E5 (raised at t=2)", order, got)
	}
}

func TestSetPriorityAtRejectsBadCalls(t *testing.T) {
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	if err := s.AddTrain(newTrain("A", 1, [2]string{"TPJ", "1"})); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPriorityAt(5, ids.TrainID{Number: "A"}, 1); err == nil {
		t.Error("SetPriorityAt before Init accepted")
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPriorityAt(5, ids.TrainID{Number: "NOPE"}, 1); err == nil {
		t.Error("SetPriorityAt for an unknown train accepted")
	}
}

func TestWaitingToEnterMakesATrainLate(t *testing.T) {
	s, _ := entryQueue(t, nil)
	for _, st := range s.Status() {
		late := st.Delay > 1
		if waited := st.Train.Number == "E4" || st.Train.Number == "E5"; waited != late {
			t.Errorf("%s ended %.2f min late", st.Train, st.Delay)
		}
	}
}

func TestClassesSetBasePriority(t *testing.T) {
	cfg := &sim.PriorityConfig{Classes: map[string]int{"Fast": 50}}

	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	fast := newTrain("A", 1, [2]string{"TPJ", "1"})
	fast.Class = "Fast"
	plain := newTrain("B", 1, [2]string{"TPJ", "2"})
	plain.Priority = 7
	for _, tr := range []*ops.Train{fast, plain} {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if fast.Priority != 50 || plain.Priority != 7 {
		t.Errorf("priorities = %d, %d, want 50, 7", fast.Priority, plain.Priority)
	}

	s = &sim.Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	odd := newTrain("C", 1, [2]string{"TPJ", "1"})
	odd.Class = "Slow"
	if err := s.AddTrain(odd); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err == nil {
		t.Error("train of an unknown class accepted")
	}
}

func TestLoadPriorityConfig(t *testing.T) {
	cfg, err := sim.LoadPriorityConfig(filepath.Join("..", "..", "profiles", "ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Classes["VB Express"] <= cfg.Classes["Rajdhani"] || len(cfg.DelayBands) == 0 {
		t.Errorf("unexpected IR config: %+v", cfg)
	}

	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown field":  `{"classes": {}, "bands": []}`,
		"duplicate band": `{"delay_bands": [{"over_min": 15, "adjust": 1}, {"over_min": 15, "adjust": 2}]}`,
	} {
		path := filepath.Join(dir, "cfg.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := sim.LoadPriorityConfig(path); err == nil {
			t.Errorf("%s: config accepted", name)
		}
	}
}

func TestActualTimesAreRecorded(t *testing.T) {
	s := mainScenario(t)
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	for _, st := range s.Status() {
		prev := 0.0
		for _, sp := range st.Train.Schedule() {
			if !sp.Arrived() || !sp.Departed() {
				t.Fatalf("%s at %s: arrival %v departure %v not recorded", st.Train, sp.StnCode, sp.ActArrTime, sp.ActDeptTime)
			}
			if sp.ActArrTime < prev || sp.ActDeptTime < sp.ActArrTime {
				t.Fatalf("%s at %s: times out of order (prev %v, arr %v, dep %v)", st.Train, sp.StnCode, prev, sp.ActArrTime, sp.ActDeptTime)
			}
			prev = sp.ActDeptTime
		}
	}
}

// ageingQueue has C take the TPJ-PDKT block at t=7, A (priority 0) queue for
// it at t=8 and B (priority 20) at t=26: both wait until C leaves it at t=37.
func ageingQueue(t *testing.T, ageing *sim.Ageing) []string {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: &sim.PriorityConfig{Ageing: ageing}}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	c := newTrain("C", 1, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})
	a := newTrain("A", 2, [2]string{"TPJ", "2"}, [2]string{"PDKT", "1"})
	b := newTrain("B", 20, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})
	b.Priority = 20
	for _, tr := range []*ops.Train{c, a, b} {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	rec := &retryLog{}
	s.Observer = rec
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	assertAllExitedAndClean(t, s)
	return rec.trains
}

func TestWithoutAgeingPriorityWins(t *testing.T) {
	if got := firstOf(ageingQueue(t, nil), "A", "B"); got != "B" {
		t.Fatalf("%s got the block first, want B (higher priority)", got)
	}
}

func TestAgeingServesALongWaitFirst(t *testing.T) {
	// at t=37 A has waited 29 min, B 11: 0 + 2x25 beats 20 + 1x25
	if got := firstOf(ageingQueue(t, &sim.Ageing{EveryMin: 10, Adjust: 25}), "A", "B"); got != "A" {
		t.Fatalf("%s got the block first, want A (waited longest)", got)
	}
}

func TestAgeingConfig(t *testing.T) {
	for name, a := range map[string]sim.Ageing{"no step": {Adjust: 5}, "negative adjust": {EveryMin: 10, Adjust: -1}} {
		if err := (&sim.PriorityConfig{Ageing: &a}).Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
