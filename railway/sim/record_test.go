package sim_test

import (
	"fmt"
	"testing"
	"trainapp/railway/sim"
	"trainapp/worlds"
)

func TestActualPlatformIsRecorded(t *testing.T) {
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	// all want TPJ platform 1, so all but one enter on another
	for i := range 3 {
		if err := s.AddTrain(newTrain(fmt.Sprintf("E%d", i), 1, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	for _, st := range s.Status() {
		for _, sp := range st.Train.Schedule() {
			if sp.ActPfNo == "" {
				t.Errorf("%s at %s: no platform recorded", st.Train, sp.StnCode)
			}
		}
		used[st.Train.Schedule()[0].ActPfNo] = true
	}
	if len(used) != 3 {
		t.Errorf("entry platforms used = %v, want three different ones", used)
	}
}

func TestSignalStopsAreRecorded(t *testing.T) {
	// a fast train right behind a slow one on a single line with intermediate
	// block signals has to stop at one of them
	trains := overtakeTrains()
	trains[1].Priority = 0 // no precedence: it just follows
	s, _ := runWith(t, singleLine(), nil, nil, trains...)
	n := 0
	for _, st := range s.Status() {
		for _, sp := range st.Train.Schedule() {
			for _, ss := range sp.SignalStops {
				n++
				if ss.Track == "" || ss.To < ss.From {
					t.Errorf("%s before %s: bad signal stop %+v", st.Train, sp.StnCode, ss)
				}
				if sp.Arrived() && ss.To > sp.ActArrTime {
					t.Errorf("%s before %s: signal stop %+v ends after arrival %.3f", st.Train, sp.StnCode, ss, sp.ActArrTime)
				}
			}
		}
	}
	if n == 0 {
		t.Error("no signal stops recorded")
	}
}
