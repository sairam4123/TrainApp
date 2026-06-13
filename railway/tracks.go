package railway

import (
	"fmt"
	"trainapp/units"
)

type TrackDirection string

const (
	Up    TrackDirection = "UP"
	Down  TrackDirection = "DOWN"
	Bidir TrackDirection = "BIDIR"
)

type TrackSegment struct {
	Id string

	// Resource related
	ReservedBy *Train
	OccupiedBy *Train

	// Track related
	Direction TrackDirection
	Length    units.Meters
}

func (t *TrackSegment) Reserve(train *Train) bool {
	if t.IsAvailable() {
		t.ReservedBy = train
		return true
	}
	return false
}

func (t *TrackSegment) IsOccupied() bool {
	return t.OccupiedBy != nil
}

func (t *TrackSegment) IsAvailable() bool {
	return t.OccupiedBy == nil && t.ReservedBy == nil
}

func (t *TrackSegment) IsReserved() bool {
	return t.OccupiedBy == nil && t.ReservedBy != nil
}

func (t *TrackSegment) Acquire(train *Train) bool {
	if t.IsAvailable() || (t.IsReserved() && train.Number == t.ReservedBy.Number) {
		t.OccupiedBy = train
		return true
	}
	return false
}

func (t *TrackSegment) Release(train *Train) (bool, error) {
	if t.IsAvailable() {
		fmt.Printf("Cannot release an empty track\n")
		return false, fmt.Errorf("Cannot release an empty track")
	}
	if t.OccupiedBy.Number != train.Number {
		return false, fmt.Errorf("Cannot release train occupied by another track")
	}
	if t.ReservedBy.Number == train.Number {
		t.ReservedBy = nil
	}
	t.OccupiedBy = nil
	return true, nil
}

func (t *TrackSegment) TravelTime(trainMaxSpeed units.MetersPerMin) units.Minutes {
	return units.Min(float64(t.Length) / float64(trainMaxSpeed))
}
