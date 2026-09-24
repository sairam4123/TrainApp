package ops

import "trainapp/railway/infra"

type MovementAuthority struct {
	path *infra.Path

	train *Train
}

func NewMovementAuthority(path *infra.Path, train *Train) *MovementAuthority {
	ma := &MovementAuthority{
		path:  path,
		train: train,
	}

	return ma
}
