package railway

type MovementAuthority struct {
	path *Path

	limit *TrackSegment

	train *Train
}

func NewMovementAuthority(path *Path, train *Train) *MovementAuthority {
	ma := &MovementAuthority{
		path:  path,
		train: train,
	}

	return ma
}

func (ma *MovementAuthority) ExtendByPath(path *Path) {
	ma.path.Edges = append(ma.path.Edges, path.Edges...)
}
