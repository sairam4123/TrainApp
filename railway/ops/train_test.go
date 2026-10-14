package ops_test

import (
	"testing"
	"trainapp/railway/ops"
	"trainapp/units"
)

func TestDelay(t *testing.T) {
	newTrain := func() *ops.Train {
		tr := &ops.Train{Number: "1"}
		tr.AddSchedule(ops.NewArrDepPoint("A", 10, 15, "1"))
		tr.AddSchedule(ops.NewStopPoint("B", 2, "1")) // untimed
		tr.AddSchedule(ops.NewArrDepPoint("C", 40, 45, "1"))
		return tr
	}

	cases := []struct {
		name   string
		record func(s []*ops.SchedulePoint)
		now    float64
		want   units.Minutes
	}{
		{"not entered, not due", func([]*ops.SchedulePoint) {}, 5, 0},
		{"not entered, overdue", func([]*ops.SchedulePoint) {}, 18, 8},
		{"arrived early", func(s []*ops.SchedulePoint) { s[0].RecordArrival(8) }, 9, -2},
		{"arrived late, departure overdue by more", func(s []*ops.SchedulePoint) { s[0].RecordArrival(12) }, 20, 5},
		{"departed late, next due later", func(s []*ops.SchedulePoint) {
			s[0].RecordArrival(12)
			s[0].RecordDeparture(19)
		}, 30, 4},
		{"untimed stop skipped", func(s []*ops.SchedulePoint) {
			s[0].RecordArrival(12)
			s[0].RecordDeparture(19)
			s[1].RecordArrival(25)
			s[1].RecordDeparture(27)
		}, 50, 10},
		{"made up time at the last stop", func(s []*ops.SchedulePoint) {
			s[0].RecordArrival(12)
			s[0].RecordDeparture(19)
			s[1].RecordArrival(25)
			s[1].RecordDeparture(27)
			s[2].RecordArrival(41)
			s[2].RecordDeparture(46)
		}, 60, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := newTrain()
			c.record(tr.Schedule())
			if got := tr.Delay(c.now); got != c.want {
				t.Errorf("Delay(%v) = %v, want %v", c.now, got, c.want)
			}
		})
	}
}
