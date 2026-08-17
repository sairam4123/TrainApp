package railway

import (
	"fmt"
	"slices"
)

// import "fmt"

type PathController struct {
	Id string

	path *Path

	train *Train
	sim   *Sim
}

type Path struct {
	initPoint *TrackPoint

	Edges []*GraphEdge

	length uint64

	curPoint *TrackPoint

	IncludesReserved bool
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

func (path *Path) EnsureAllEdgesAreReserved(train *Train) bool {
	for _, edge := range path.Edges {
		fmt.Println("Pathing check", edge.Track.Id, edge.Track.IsOccupied(), edge.Track.IsReserved(), edge.Track.OccupiedBy, edge.Track.ReservedBy, train)
		if !edge.Track.IsReserved() || edge.Track.ReservedBy.Number != train.Number {
			return false
		}
		if edge.Track.IsOccupied() && edge.Track.OccupiedBy.Number != train.Number {
			return false
		}
	}

	return true
}
