package sim_test

import (
	"math"
	"strings"
	"testing"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/worlds"
)

// stockSim starts a sim on the IR profile with the given trains.
func stockSim(t *testing.T, trains ...*ops.Train) error {
	t.Helper()
	cfg, err := sim.LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	for _, tr := range trains {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	return s.Init()
}

func TestInitWorksOutStock(t *testing.T) {
	express := newTrain("S1", 1, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})
	express.Class = "Superfast Express"
	vb := newTrain("V1", 30, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})
	vb.Class = "VB Express"
	long := newTrain("L1", 60, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})
	long.Class = "Mail/Express"
	coaches := 24
	long.Stock = &ops.Stock{Coaches: &coaches}
	plain := newTrain("P1", 90, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"}) // no class

	if err := stockSim(t, express, vb, long, plain); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		tr           *ops.Train
		length       float64
		accel, brake float64 // m/s²
	}{
		{express, 491, 0.159, 0.5},
		{vb, 384, 0.198, 0.5},
		{long, 585, 0.132, 0.5},
		{plain, 0, 0, 0},
	} {
		if float64(c.tr.Length) != c.length ||
			math.Abs(c.tr.Accel.InMS2()-c.accel) > 0.001 || math.Abs(c.tr.Brake.InMS2()-c.brake) > 1e-9 {
			t.Errorf("%s: length %v, accel %.3f, brake %v; want %v, %.3f, %v",
				c.tr.Number, c.tr.Length, c.tr.Accel.InMS2(), c.tr.Brake.InMS2(), c.length, c.accel, c.brake)
		}
	}
}

func TestInitRejectsBadStock(t *testing.T) {
	tr := newTrain("B1", 1, [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"})
	locoLen := 21.0
	tr.Stock = &ops.Stock{LocoLengthM: &locoLen} // a class-less train with half a makeup
	err := stockSim(t, tr)
	if err == nil || !strings.Contains(err.Error(), "train B1: stock: length needs") {
		t.Fatalf("Init = %v, want a stock error for B1", err)
	}
}
