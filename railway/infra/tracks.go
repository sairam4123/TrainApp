package infra

import (
	"fmt"
	"math"
	"trainapp/railway/ids"
	"trainapp/units"
)

type TrackDirection string

const (
	Up    TrackDirection = "UP"
	Down  TrackDirection = "DOWN"
	Bidir TrackDirection = "BIDIR"
)

// TrackSegment is a piece of static infrastructure. Who has reserved or
// occupied it is tracked by the interlocking, not here.
type TrackSegment struct {
	Id   ids.TrackID // set when the track is added to the graph
	Name string      // optional display name, e.g. "bsTpjPdkt0"

	// Track related
	Direction TrackDirection
	Length    units.Meters
	MaxSpeed  units.MetersPerMin
}

// TravelTime is the time to run the whole segment at the lower of the train's
// and the track's speed limit. It panics if that speed is not positive or the
// length is negative, since either means the world or train was set up wrong.
// A train's run time comes from motion.Plan, which uses this.
func (t *TrackSegment) TravelTime(trainMaxSpeed units.MetersPerMin) units.Minutes {
	maxSpeed := math.Min(float64(trainMaxSpeed), float64(t.MaxSpeed))
	if !(maxSpeed > 0) || t.Length < 0 {
		panic(fmt.Sprintf("infra: track %s has no valid travel time (length %v m, speed %v m/min)", t, t.Length, maxSpeed))
	}
	return units.Min(float64(t.Length) / maxSpeed)
}

// String is the display name if there is one, otherwise the id.
func (t *TrackSegment) String() string {
	if t == nil {
		return "<nil>"
	}
	if t.Name != "" {
		return t.Name
	}
	return t.Id.String()
}

func (t *TrackSegment) SetTrackAttributes(length units.Meters, speed units.MetersPerMin) *TrackSegment {
	t.Length = length
	t.MaxSpeed = speed
	return t
}
