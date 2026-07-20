package railway

import "fmt"

// import "fmt"

type PathController struct {
	Id string

	path *Path

	train *Train
	sim   *Sim
}

type Path struct {
	Edges []*GraphEdge

	IncludesReserved bool
}

func (path *Path) PPrint() {
	for _, edge := range path.Edges {
		fmt.Printf("%s -> %s (%s) --> ", edge.From.Id, edge.To.Id, edge.Track.Id)
	}
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
