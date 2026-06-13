package railway

import "fmt"

// import "fmt"

type PathController struct {
	Id string

	path *Path

	train *Train
	sim   *Sim
}

func (path *Path) EnsureAllEdgesAreReserved(train *Train) bool {
	for _, edge := range path.Edges {
		fmt.Println(edge.Track.Id, edge.Track.IsOccupied(), edge.Track.IsReserved(), edge.Track.OccupiedBy, edge.Track.ReservedBy, train)
		if !edge.Track.IsReserved() || edge.Track.ReservedBy.Number != train.Number {
			return false
		}
		if edge.Track.IsOccupied() && edge.Track.OccupiedBy.Number != train.Number {
			return false
		}
	}

	return true
}
