package railway

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

func (sig *Signal) Protects(world *World) []*TrackSegment {
	segments := make([]*TrackSegment, 0)
	for key, edge := range world.TrackGraph.NeighborMap[sig.AtPoint.Id] {
		if key == sig.FacingPoint.Id {
			continue
		}
		segments = append(segments, edge)
	}
	return segments
}

func (sig *Signal) FacesMovement(from, to *TrackPoint) bool {
	return sig.AtPoint.Id == to.Id && sig.FacingPoint.Id == from.Id
}
