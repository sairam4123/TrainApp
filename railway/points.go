package railway

type TrackPoint struct {
	Id string

	IsDeadEnd     bool
	IsSimBoundary bool
}

// Changes inplace
func (pt *TrackPoint) WithDeadEnd(isDeadEnd bool) *TrackPoint {
	pt.IsDeadEnd = isDeadEnd
	return pt
}

// Changes inplace
func (pt *TrackPoint) WithSimLimit(isSimBdary bool) *TrackPoint {
	pt.IsSimBoundary = isSimBdary
	return pt
}
