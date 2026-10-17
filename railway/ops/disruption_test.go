package ops_test

import (
	"strings"
	"testing"
	"trainapp/railway/ops"
	"trainapp/units"
)

func plannedTrain() *ops.Train {
	tr := &ops.Train{Number: "1", MaxSpeed: units.KMPH(100), Class: "Passenger", Priority: 20}
	tr.AddSchedule(ops.NewStopPoint("A", 2, "1").SetEntryTime(10))
	tr.AddSchedule(ops.NewPassPoint("B", -1, ""))
	tr.AddSchedule(ops.NewArrDepPoint("C", 40, 45, "1"))
	return tr
}

func TestCloneIsFresh(t *testing.T) {
	tr := plannedTrain()
	s := tr.Schedule()
	s[0].RecordArrival(11)
	s[0].RecordDeparture(13)
	s[1].BeginSignalStop(20, "x")
	tr.AdvanceStop()

	c := tr.Clone()
	if c.StopIndex() != 0 || c.Number != "1" || c.Priority != 20 || c.Class != "Passenger" || c.MaxSpeed != tr.MaxSpeed {
		t.Errorf("clone %+v", c)
	}
	cs := c.Schedule()
	if len(cs) != 3 || cs[0].Arrived() || cs[0].Departed() || len(cs[1].SignalStops) != 0 {
		t.Errorf("clone keeps the run: %+v", cs[0])
	}
	if cs[0].ArrTime != 10 || cs[2].DeptTime != 45 || !cs[1].IsPass() {
		t.Errorf("clone lost the plan: %+v %+v", cs[0], cs[1])
	}
	cs[0].SetEntryTime(99)
	if s[0].ArrTime != 10 {
		t.Error("clone shares schedule points with the original")
	}
}

func TestApplyDisruptions(t *testing.T) {
	tr := plannedTrain()
	err := ops.ApplyDisruptions([]*ops.Train{tr}, []ops.Disruption{
		{Train: "1", Kind: ops.DisruptStand, Stn: "B", Min: 30},
		{Train: "1", Kind: ops.DisruptLateEntry, Min: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := tr.Schedule()
	if s[1].ExtraStand() != 30 || s[0].ExtraStand() != 0 || s[0].ArrTime != 15 {
		t.Errorf("stand %v at B, entry %v", s[1].ExtraStand(), s[0].ArrTime)
	}
	if got := s[1].ExpDwellTime(20, 1); got > 1 {
		t.Errorf("the planned dwell includes the stand: %v", got)
	}
	if c := tr.Clone(); c.Schedule()[1].ExtraStand() != 30 {
		t.Error("clone drops the stand")
	}

	err = ops.ApplyDisruptions([]*ops.Train{plannedTrain()}, []ops.Disruption{
		{Train: "2", Kind: ops.DisruptStand, Stn: "B", Min: 30},
		{Train: "1", Kind: ops.DisruptStand, Stn: "Z", Min: 30},
		{Train: "1", Kind: ops.DisruptLateEntry, Min: 0},
		{Train: "1", Kind: "flood", Min: 1},
	})
	for _, want := range []string{"no such train", `does not run through "Z"`, "min must be positive", `kind "flood"`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v, want it to say %s", err, want)
		}
	}
}

func TestReadDisruptions(t *testing.T) {
	ds, err := ops.ReadDisruptions(strings.NewReader(`{"disruptions": [{"train": "1", "kind": "stand", "stn": "B", "min": 3}]}`))
	if err != nil || len(ds) != 1 || ds[0].Stn != "B" || ds[0].Min != 3 {
		t.Errorf("%+v, %v", ds, err)
	}
	if _, err := ops.ReadDisruptions(strings.NewReader(`{"disruptions": [{"train": "1", "at": "B"}]}`)); err == nil {
		t.Error("unknown field accepted")
	}
}
