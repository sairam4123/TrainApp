package railway

import (
	"fmt"
	"math"
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
	MaxSpeed  units.MetersPerMin
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
	fmt.Printf("is avbl: %v, is rsvd: %v, train 1: %s train 2: %s\n", t.IsAvailable(), t.IsReserved(), train.GetFullName(), t.ReservedBy.GetFullName())
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
	maxSpeed := math.Min(float64(trainMaxSpeed), float64(t.MaxSpeed))
	return units.Min(float64(t.Length) / maxSpeed)
}

func (t *TrackSegment) SetTrackAttributes(length units.Meters, speed units.MetersPerMin) *TrackSegment {
	t.Length = length
	t.MaxSpeed = speed
	return t
}
