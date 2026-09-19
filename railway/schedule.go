package railway

import (
	"trainapp/des"
	"trainapp/units"
)

type SchedulePointKind string

const (
	SpStop  SchedulePointKind = "stop"
	SpPass  SchedulePointKind = "pass"
	SpDwell SchedulePointKind = "dwell"
)

type SchedulePoint struct {
	kind SchedulePointKind

	StnCode string

	ArrTime   float64
	DwellTime float64
	DeptTime  float64

	SpPfNo string
}

func (s *SchedulePoint) ExpDwellTime(curTime float64) units.Minutes {

	switch s.kind {
	case SpStop:
		if curTime < s.ArrTime {
			return units.Min(s.DeptTime - curTime)
		} else if curTime > s.DeptTime {
			return units.Min(1) // one minute stop cuz we're delayed af
		}
		return units.Min(s.DeptTime - s.ArrTime)
	case SpPass:
		return units.Min(des.MinDeltaTime)
	case SpDwell:
		return units.Min(s.DwellTime)
	default:
		return units.Min(1)
	}

}

func NewArrDepPoint(stnCode string, arrTime, deptTime float64, prefPfNo string) *SchedulePoint {
	return &SchedulePoint{
		kind: SpStop,

		// only arr and dep time is constrained
		ArrTime:   arrTime,
		DeptTime:  deptTime,
		DwellTime: -1, // we don't know the exact dwell time

		StnCode: stnCode,
		SpPfNo:  prefPfNo,
	}
}

func NewPassPoint(stnCode string, passTime float64, prefPfNo string) *SchedulePoint {
	return &SchedulePoint{
		kind: SpPass,

		// all three are constrained
		ArrTime:   passTime,
		DeptTime:  passTime,
		DwellTime: 0,

		StnCode: stnCode,
		SpPfNo:  prefPfNo,
	}
}

func (s *SchedulePoint) SetEntryTime(entryTime float64) *SchedulePoint {
	s.ArrTime = entryTime
	return s
}

func (s *SchedulePoint) SetExitTime(exitTime float64) *SchedulePoint {
	s.DeptTime = exitTime
	return s
}

func NewStopPoint(stnCode string, dwellTime float64, prefPfNo string) *SchedulePoint {
	return &SchedulePoint{
		kind: SpDwell,

		// only dwell time is constrained
		ArrTime:   -1,
		DwellTime: dwellTime,
		DeptTime:  -1,

		StnCode: stnCode,
		SpPfNo:  prefPfNo,
	}
}
