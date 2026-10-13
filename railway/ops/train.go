package ops

import (
	"trainapp/railway/infra"
	"trainapp/units"
)

type Train struct {
	Name   string
	Number string

	curSchedulePoint int
	schedule         []*SchedulePoint

	FacingToward *infra.TrackPoint
	MaxSpeed     units.MetersPerMin

	occupation *OccupationData

	ma *MovementAuthority

	reservation *ReservationData
}

func (t *Train) GetFullName() string {
	return t.Number + " - " + t.Name
}

func (t *Train) AddSchedule(sp *SchedulePoint) {
	t.schedule = append(t.schedule, sp)
}

func (t *Train) String() string {
	if t == nil {
		return "<nil>"
	}
	return t.GetFullName()
}
