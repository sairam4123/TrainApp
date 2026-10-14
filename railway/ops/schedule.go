package ops

import (
	"trainapp/des"
	"trainapp/railway/ids"
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

	StnCode ids.StnCode

	ArrTime   float64
	DwellTime float64
	DeptTime  float64

	// actual times the train arrived and departed, -1 until it has
	ActArrTime  float64
	ActDeptTime float64

	SpPfNo string
}

// RecordArrival notes when the train actually arrived.
func (s *SchedulePoint) RecordArrival(t float64) {
	s.ActArrTime = t
}

// RecordDeparture notes when the train actually departed.
func (s *SchedulePoint) RecordDeparture(t float64) {
	s.ActDeptTime = t
}

func (s *SchedulePoint) Arrived() bool {
	return s.ActArrTime >= 0
}

func (s *SchedulePoint) Departed() bool {
	return s.ActDeptTime >= 0
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

func NewArrDepPoint(stnCode ids.StnCode, arrTime, deptTime float64, prefPfNo string) *SchedulePoint {
	return &SchedulePoint{
		kind: SpStop,

		// only arr and dep time is constrained
		ArrTime:   arrTime,
		DeptTime:  deptTime,
		DwellTime: -1, // we don't know the exact dwell time

		ActArrTime:  -1,
		ActDeptTime: -1,

		StnCode: stnCode,
		SpPfNo:  prefPfNo,
	}
}

func NewPassPoint(stnCode ids.StnCode, passTime float64, prefPfNo string) *SchedulePoint {
	return &SchedulePoint{
		kind: SpPass,

		// all three are constrained
		ArrTime:   passTime,
		DeptTime:  passTime,
		DwellTime: 0,

		ActArrTime:  -1,
		ActDeptTime: -1,

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

func NewStopPoint(stnCode ids.StnCode, dwellTime float64, prefPfNo string) *SchedulePoint {
	return &SchedulePoint{
		kind: SpDwell,

		// only dwell time is constrained
		ArrTime:   -1,
		DwellTime: dwellTime,
		DeptTime:  -1,

		ActArrTime:  -1,
		ActDeptTime: -1,

		StnCode: stnCode,
		SpPfNo:  prefPfNo,
	}
}
