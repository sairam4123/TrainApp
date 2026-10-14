package sim_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/worlds"
)

// newTrain builds a train with stops (station, platform) and dwell; it enters
// the world at entry.
func newTrain(number string, entry float64, stops ...[2]string) *ops.Train {
	t := &ops.Train{Name: "T" + number, Number: number, MaxSpeed: units.KMPH(110)}
	for i, s := range stops {
		sp := ops.NewStopPoint(ids.StnCode(s[0]), 5, s[1])
		if i == 0 {
			sp.SetEntryTime(entry)
		}
		t.AddSchedule(sp)
	}
	return t
}

// mainScenario mirrors main.go: 6 up and 6 down trains, 20 minutes apart.
func mainScenario(t *testing.T) *sim.Sim {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	for i := range 6 {
		up := newTrain(fmt.Sprintf("U%d", i), float64(i*20+1), [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"}, [2]string{"KKDI", "1"})
		down := newTrain(fmt.Sprintf("D%d", i), float64(i*20+1), [2]string{"KKDI", "3"}, [2]string{"PDKT", "3"}, [2]string{"TPJ", "3"})
		for _, tr := range []*ops.Train{up, down} {
			if err := s.AddTrain(tr); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	return s
}

func assertAllExitedAndClean(t *testing.T, s *sim.Sim) {
	t.Helper()
	for _, st := range s.Status() {
		if st.Phase != "exited" {
			t.Errorf("%s ended %s (waiting: %+v)", st.Train, st.Phase, st.Waiting)
		}
	}
	ilk := s.Interlocking()
	for _, trk := range s.World().TrackGraph.ListTracks() {
		if !ilk.IsTrackFree(trk) {
			r, o := ilk.TrackHolder(trk)
			t.Errorf("track %s still held at the end: reserved %v occupied %v", trk, r, o)
		}
	}
	if n := len(s.Dispatcher().Waiting()); n != 0 {
		t.Errorf("%d requests still queued", n)
	}
}

func TestMainScenarioCompletes(t *testing.T) {
	s := mainScenario(t)
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	assertAllExitedAndClean(t, s)
}

func TestRunsAreDeterministic(t *testing.T) {
	read := func() string {
		s := mainScenario(t)
		if err := s.Run(); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(s.OutputDir, "logs", "logs.log"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	first := read()
	for range 3 {
		if read() != first {
			t.Fatal("two runs of the same scenario produced different logs")
		}
	}
}

func TestEntryWaitsForAFreePlatform(t *testing.T) {
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	// TPJ has four boundary platforms; the fifth train must wait to enter
	for i := range 5 {
		tr := newTrain(fmt.Sprintf("E%d", i), 1, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})
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
}

func TestInvalidTrainsAreRejected(t *testing.T) {
	s := &sim.Sim{}
	s.SetWorld(worlds.BuildTpjKkdiWorld())

	noSchedule := &ops.Train{Number: "X", MaxSpeed: 100}
	if err := s.AddTrain(noSchedule); err == nil {
		t.Error("train without schedule accepted")
	}
	noSpeed := newTrain("Y", 1, [2]string{"TPJ", "1"})
	noSpeed.MaxSpeed = 0
	if err := s.AddTrain(noSpeed); err == nil {
		t.Error("train without speed accepted")
	}
	noEntry := &ops.Train{Number: "Z", MaxSpeed: 100}
	noEntry.AddSchedule(ops.NewStopPoint("TPJ", 5, "1"))
	if err := s.AddTrain(noEntry); err == nil {
		t.Error("train without entry time accepted")
	}

	ok := newTrain("A", 1, [2]string{"TPJ", "1"})
	if err := s.AddTrain(ok); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTrain(newTrain("A", 2, [2]string{"TPJ", "2"})); err == nil {
		t.Error("duplicate train number accepted")
	}
}

func TestUnknownStationFailsTheRun(t *testing.T) {
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	if err := s.AddTrain(newTrain("A", 1, [2]string{"NOPE", "1"})); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(); err == nil {
		t.Fatal("run with an unknown station succeeded")
	}
}
