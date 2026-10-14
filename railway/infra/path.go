package infra

import (
	"fmt"
	"slices"
	"strings"
	"trainapp/units"
)

type Path struct {
	InitPoint *TrackPoint

	Edges []*GraphEdge
}

// Step is one edge of a path together with the direction it is run in.
type Step struct {
	Edge *GraphEdge
	From *TrackPoint
	To   *TrackPoint
}

func (path *Path) Length() units.Meters {
	length := units.Meters(0)
	for _, edge := range path.Edges {
		length += edge.Track.Length
	}
	return length
}

// Steps walks the path from InitPoint and returns each edge with its direction
// of travel. It fails if an edge does not continue from the previous one.
func (path *Path) Steps() ([]Step, error) {
	if path.InitPoint == nil {
		return nil, fmt.Errorf("path has no initial point")
	}
	steps := make([]Step, 0, len(path.Edges))
	cur := path.InitPoint
	for i, edge := range path.Edges {
		next := edge.Other(cur)
		if next == nil {
			return nil, fmt.Errorf("path is broken at edge %d (%s): it does not touch %s", i, edge.Track, cur.Id)
		}
		steps = append(steps, Step{Edge: edge, From: cur, To: next})
		cur = next
	}
	return steps, nil
}

// EndPoint is the point the path finishes at, or nil if the path is broken.
func (path *Path) EndPoint() *TrackPoint {
	steps, err := path.Steps()
	if err != nil {
		return nil
	}
	if len(steps) == 0 {
		return path.InitPoint
	}
	return steps[len(steps)-1].To
}

// Prefix returns a new path made of the first n edges.
func (path *Path) Prefix(n int) *Path {
	return &Path{InitPoint: path.InitPoint, Edges: slices.Clone(path.Edges[:n])}
}

// String lists the path's edges in the direction of travel.
func (path *Path) String() string {
	steps, err := path.Steps()
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	var sb strings.Builder
	for i, s := range steps {
		if i > 0 {
			sb.WriteString(" --> ")
		}
		fmt.Fprintf(&sb, "%s -> %s (%s - %.2fm)", s.From.Id, s.To.Id, s.Edge.Track, float64(s.Edge.Track.Length))
	}
	return sb.String()
}

func (path *Path) PPrint() {
	fmt.Println(path.String())
}

func (path *Path) ExtendPath(toTrack *GraphEdge) {
	path.Edges = append(path.Edges, toTrack)
}

func (path *Path) Contains(track *GraphEdge) bool {
	return slices.Contains(path.Edges, track)
}
