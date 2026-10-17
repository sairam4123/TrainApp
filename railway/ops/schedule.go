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
	halt bool // see AsHalt
	// extraStand is a disruption: minutes the train stands here on top of
	// what the schedule asks (see Disruption).
	extraStand float64

	StnCode ids.StnCode

	ArrTime   float64
	DwellTime float64
	DeptTime  float64

	// actual times the train arrived and departed, -1 until it has
	ActArrTime  float64
	ActDeptTime float64

	SpPfNo string
	// ActPfNo is the platform the train actually used, empty until it arrives.
	// It can differ from SpPfNo when the preferred one was taken.
	ActPfNo string

	// SignalStops are the stops at signals on the way to this point.
	SignalStops []SignalStop

	// WaitedFor is the train whose leaving a track let this train depart from
	// here, if it had to wait for one.
	WaitedFor ids.TrainID
}

// SignalStop is a stop at a signal short of a schedule point. To is -1 while
// the train is still standing.
type SignalStop struct {
	Track    string // the track the train stood on
	From, To float64
	// WaitedFor is the train whose leaving a track let it move on, if any.
	WaitedFor ids.TrainID
}

// RecordPlatform notes the platform the train actually used.
func (s *SchedulePoint) RecordPlatform(pfNo string) {
	s.ActPfNo = pfNo
}

// BeginSignalStop notes the train stopped at a signal on the way here.
func (s *SchedulePoint) BeginSignalStop(t float64, track string) {
	s.SignalStops = append(s.SignalStops, SignalStop{Track: track, From: t, To: -1})
}

// EndSignalStop notes the train set off again after its last signal stop,
// once waitedFor (zero if none in particular) let it.
func (s *SchedulePoint) EndSignalStop(t float64, waitedFor ids.TrainID) {
	if n := len(s.SignalStops); n > 0 && s.SignalStops[n-1].To < 0 {
		s.SignalStops[n-1].To = t
		s.SignalStops[n-1].WaitedFor = waitedFor
	}
}

// RecordDepartureAfter notes when the train departed, and the train it had to
// wait for, zero if none.
func (s *SchedulePoint) RecordDepartureAfter(t float64, waitedFor ids.TrainID) {
	s.ActDeptTime = t
	s.WaitedFor = waitedFor
}

// WithMinDwell sets the shortest dwell for a timed stop, kept even when the
// train arrives late.
func (s *SchedulePoint) WithMinDwell(dwell float64) *SchedulePoint {
	s.DwellTime = dwell
	return s
}

// AsHalt marks a timed stop as a halt: a pass the train is timed to stand at
// (e.g. for a crossing), not a call. It stands no longer than its times and
// minimum dwell ask, and is never given the minimum stop.
func (s *SchedulePoint) AsHalt() *SchedulePoint {
	s.halt = true
	return s
}

// ExtraStand is how many minutes a disruption makes the train stand here on
// top of its dwell (see Disruption). The dwell itself, ExpDwellTime, leaves
// it out, so nothing plans for it before the train has arrived.
func (s *SchedulePoint) ExtraStand() units.Minutes {
	return units.Min(s.extraStand)
}

// IsHalt reports whether the point is a halt (see AsHalt).
func (s *SchedulePoint) IsHalt() bool {
	return s.halt
}

// Kind is whether the train stops, dwells or passes here.
func (s *SchedulePoint) Kind() SchedulePointKind {
	return s.kind
}

// IsPass reports whether the train runs through the station without stopping.
func (s *SchedulePoint) IsPass() bool {
	return s.kind == SpPass
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

// ExpDwellTime is how long the train stands here if it arrives at curTime.
// At a stop it stands at least minStop, however late it runs; a pass, or a
// halt, is not a stop and is never held for it.
func (s *SchedulePoint) ExpDwellTime(curTime float64, minStop units.Minutes) units.Minutes {
	switch s.kind {
	case SpStop:
		least := units.Min(max(s.DwellTime, 0))
		if !s.halt {
			least = max(least, minStop)
		}
		if curTime > s.DeptTime {
			// late: as short a stop as it may make
			return least
		}
		// leave on time, but not before the least it must stand
		return max(units.Min(s.DeptTime-curTime), least)
	case SpPass:
		return units.Min(des.MinDeltaTime)
	case SpDwell:
		return max(units.Min(s.DwellTime), minStop)
	default:
		return max(units.Min(1), minStop)
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

// NewPassPoint is a station the train runs through without stopping. A
// negative passTime leaves the pass untimed, so it never makes the train late.
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
