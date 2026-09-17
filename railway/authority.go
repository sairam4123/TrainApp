package railway

type MovementAuthority struct {
	path *Path

	train *Train
}

func NewMovementAuthority(path *Path, train *Train) *MovementAuthority {
	ma := &MovementAuthority{
		path:  path,
		train: train,
	}

	return ma
}
