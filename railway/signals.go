package railway

type Signal struct {
	atPoint     *TrackPoint
	facingPoint *TrackPoint
}

func NewSignal(atPoint *TrackPoint, facingPoint *TrackPoint) *Signal {
	sig1 := &Signal{
		atPoint:     atPoint,
		facingPoint: facingPoint,
	}
	return sig1
}
