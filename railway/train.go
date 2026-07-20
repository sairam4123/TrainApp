package railway

import "trainapp/units"

type Train struct {
	Name   string
	Number string

	curSchedulePoint int
	schedule         []*SchedulePoint

	FacingToward *TrackPoint
	MaxSpeed     units.MetersPerMin

	occupation *OccupationData

	reservation *ReservationData
}

func (t *Train) GetFullName() string {
	return t.Number + " - " + t.Name
}

type SchedulePoint struct {
	TrainNumber string
	StnCode     string
	ArrTime     float64
	DeptTime    float64
	SpPfNo      string
}

func (t *Train) AddSchedule(sp *SchedulePoint) {
	t.schedule = append(t.schedule, sp)
}

func (s *SchedulePoint) ExpDwellTime(curTime float64) units.Minutes {
	if curTime < s.ArrTime {
		return units.Min(s.DeptTime - curTime)
	} else if curTime > s.DeptTime {
		return units.Min(1) // one minute stop cuz we're delayed af
	}
	return units.Min(s.DeptTime - s.ArrTime)
}

type TrainController struct {
	sim   *Sim
	train *Train
}

func (t *Train) String() string {
	if t == nil {
		return "<nil>"
	}
	return t.GetFullName()
}
