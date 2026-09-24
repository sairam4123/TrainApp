package infra

import (
	"fmt"
	"slices"
)

type Path struct {
	initPoint *TrackPoint

	Edges []*GraphEdge

	length uint64 //TODO: do we really need it? Can we compute it before?

	curPoint *TrackPoint
}

func (path *Path) Length() uint64 {
	if path.length != uint64(0) {
		return path.length
	}

	path.length = 0
	for _, edge := range path.Edges {
		path.length += uint64(edge.Track.Length)
	}

	return path.length
}

func (path *Path) PPrint() {
	for _, edge := range path.Edges {
		fmt.Printf("%s -> %s (%s - %.2fm) --> ", edge.From.Id, edge.To.Id, edge.Track.Id, float64(edge.Track.Length))
	}
}

func (path *Path) ExtendPath(toTrack *GraphEdge) {
	path.Edges = append(path.Edges, toTrack)
}

func (path *Path) Contains(track *GraphEdge) bool {
	return slices.Contains(path.Edges, track)
}

