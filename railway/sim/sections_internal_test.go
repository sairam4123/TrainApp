package sim

import (
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/interlocking"
	"trainapp/railway/ops"
	"trainapp/units"
	"trainapp/worlds"
)

// Every route the dispatcher grants is a single signal section: no step before
// the last one ends at a signal or platform end, so nothing beyond the next
// signal is locked for the train.
func TestRoutesAreSingleSignalSections(t *testing.T) {
	s := &Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	for i, sched := range [][]string{{"TPJ", "PDKT", "KKDI"}, {"KKDI", "PDKT", "TPJ"}} {
		tr := &ops.Train{Name: sched[0], Number: sched[0], MaxSpeed: units.KMPH(110)}
		for j, code := range sched {
			sp := ops.NewStopPoint(stn(code), 5, "1")
			if j == 0 {
				sp.SetEntryTime(float64(1 + i))
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

	granted := 0
	for {
		ev, ok := s.NextEvent()
		if !ok {
			break
		}
		if route, ok := ev.Data.(*interlocking.Route); ok && ev.Type == ops.RouteGranted {
			granted++
			for i := range route.End() - 1 {
				if s.world.EndsAtSignal(route.Step(i)) {
					t.Errorf("route %s runs past a signal at edge %d", route.Path(), i)
				}
			}
		}
		if ev.TrainID != "" {
			s.trainCtrllers[trainID(ev.TrainID)].OnEvent(ev.Type, ev.Data)
		}
		if s.err != nil {
			t.Fatal(s.err)
		}
	}
	if granted == 0 {
		t.Fatal("no routes granted")
	}
	for _, st := range s.Status() {
		if st.Phase != phaseExited.String() {
			t.Errorf("%s ended %s", st.Train, st.Phase)
		}
	}
}

func stn(code string) ids.StnCode       { return ids.StnCode(code) }
func trainID(number string) ids.TrainID { return ids.TrainID{Number: number} }
