package infra

import (
	"slices"
)

// Signal stands at AtPoint and is read by trains approaching it from
// FacingPoint, i.e. it governs movement FacingPoint -> AtPoint.
type Signal struct {
	Id string

	AtPoint     *TrackPoint
	FacingPoint *TrackPoint
}

func NewSignal(id string, atPoint *TrackPoint, facingPoint *TrackPoint) *Signal {
	sig1 := &Signal{
		Id:          id,
		AtPoint:     atPoint,
		FacingPoint: facingPoint,
	}
	return sig1
}

// Protects returns the tracks beyond the signal, sorted by id.
func (sig *Signal) Protects(world *World) []*TrackSegment {
	segments := make([]*TrackSegment, 0)
	for key, edge := range world.TrackGraph.NeighborMap[sig.AtPoint.Id] {
		if key == sig.FacingPoint.Id {
			continue
		}
		segments = append(segments, edge)
	}
	slices.SortFunc(segments, func(a, b *TrackSegment) int { return a.Id.Compare(b.Id) })
	return segments
}

func (sig *Signal) FacesMovement(from, to *TrackPoint) bool {
	return sig.AtPoint.Id == to.Id && sig.FacingPoint.Id == from.Id
}
