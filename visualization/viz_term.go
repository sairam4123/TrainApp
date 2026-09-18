package visualization

import (
	"fmt"
	"trainapp/railway"
)

type TrackPointViz struct {
	x int
	y int
}

type TrackSegmentViz struct {
	length int
}

func PrintEdge(edge *railway.GraphEdge) {
	if edge.From.IsSimBoundary {
		fmt.Print("←")
	}
	fmt.Print(edge.Track.Id)
	if edge.To.IsSimBoundary {
		fmt.Print("→")
	}
}

func BuildViz(world *railway.World) {
	for _, pt := range world.ListSimPts() {
		for pt2 := range world.TrackGraph.NeighborMap[pt.Id] {
			seg := world.TrackGraph.NeighborMap[pt.Id][pt2]
			edge := world.TrackGraph.Edges[seg.Id]
			PrintEdge(edge)
			fmt.Println()
		}
	}
}
