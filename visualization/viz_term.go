package visualization

import (
	"fmt"
	"maps"
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

func PrintEdge(edge *infra.GraphEdge) {
	if edge.From.IsSimBoundary {
		fmt.Print("←")
	}
	fmt.Print(edge.Track.Id)
	if edge.To.IsSimBoundary {
		fmt.Print("→")
	}
}

func BuildViz(world *infra.World) {
	for _, pt := range world.ListSimPts() {
		for _, pt2 := range slices.SortedFunc(maps.Keys(world.TrackGraph.NeighborMap[pt.Id]), ids.PointID.Compare) {
			seg := world.TrackGraph.NeighborMap[pt.Id][pt2]
			edge := world.TrackGraph.Edges[seg.Id]
			PrintEdge(edge)
			fmt.Println()
		}
	}
}
