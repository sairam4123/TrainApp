package railway

import (
	"container/heap"
	"fmt"
	"math"
	"slices"
)

type GraphEdge struct {
	Track *TrackSegment
	From  *TrackPoint
	To    *TrackPoint
}

type TrackGraph struct {
	points map[string]*TrackPoint
	tracks map[string]*TrackSegment

	Edges map[string]*GraphEdge

	NeighborMap map[string]map[string]*TrackSegment
}

func (graph *TrackGraph) FindWorldBoundaryPoint(segment *TrackSegment) *TrackPoint {
	if from := graph.Edges[segment.Id].From; from.IsSimBoundary {
		return from
	}
	if to := graph.Edges[segment.Id].To; to.IsSimBoundary {
		return to
	}

	return nil
}

func (graph *TrackGraph) Init() {
	graph.points = make(map[string]*TrackPoint)
	graph.Edges = make(map[string]*GraphEdge)
	graph.tracks = make(map[string]*TrackSegment)
	graph.NeighborMap = make(map[string]map[string]*TrackSegment)
}

func (graph *TrackGraph) AddTrack(fromPoint *TrackPoint, toPoint *TrackPoint, track *TrackSegment) bool {
	if _, ok := graph.Edges[track.Id]; ok {
		return false
	}

	edge := &GraphEdge{
		From:  fromPoint,
		To:    toPoint,
		Track: track,
	}

	graph.points[fromPoint.Id] = fromPoint
	graph.points[toPoint.Id] = toPoint
	graph.tracks[track.Id] = track

	graph.Edges[track.Id] = edge
	return true
}

func (graph *TrackGraph) TrackBetween(fromPoint *TrackPoint, toPoint *TrackPoint) (*GraphEdge, error) {
	if graph.NeighborMap[fromPoint.Id] == nil {
		return nil, fmt.Errorf("Cache seems to be empty, did you call BuildCacheMap()?")
	}
	return graph.Edges[graph.NeighborMap[fromPoint.Id][toPoint.Id].Id], nil
}

func (graph *TrackGraph) BuildCacheMap() {
	graph.NeighborMap = make(map[string]map[string]*TrackSegment)
	for _, edge := range graph.Edges {
		if graph.NeighborMap[edge.From.Id] == nil {
			graph.NeighborMap[edge.From.Id] = make(map[string]*TrackSegment)
		}
		if graph.NeighborMap[edge.To.Id] == nil {
			graph.NeighborMap[edge.To.Id] = make(map[string]*TrackSegment)
		}

		graph.NeighborMap[edge.From.Id][edge.To.Id] = edge.Track
		graph.NeighborMap[edge.To.Id][edge.From.Id] = edge.Track
	}
}

type PathNode struct {
	Id     string
	Value  *TrackPoint
	Cost   float64
	Parent *PathNode
}

type PathQueue []*PathNode

func (pq PathQueue) Len() int {
	return len(pq)
}

func (pq PathQueue) Less(i, j int) bool {
	return pq[i].Cost < pq[j].Cost
}

func (pq PathQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *PathQueue) Push(x any) {
	*pq = append(*pq, x.(*PathNode))
}

func (pq *PathQueue) Pop() any {
	old := *pq
	n := len(old)

	item := old[n-1]
	*pq = old[:n-1]

	return item
}

func (graph *TrackGraph) FindPath(fromPoint *TrackPoint, toPoint *TrackPoint) Path {
	queue := make(PathQueue, 0)
	nodes := make(map[string]*PathNode, 0)
	for _, vert := range graph.points {
		nodes[vert.Id] = &PathNode{
			Id:     vert.Id,
			Value:  vert,
			Cost:   math.MaxInt,
			Parent: nil,
		}
	}

	// set initial node cost 0
	nodes[fromPoint.Id].Cost = 0
	queue = append(queue, nodes[fromPoint.Id])
	heap.Init(&queue)

	// path := make([]*GraphEdge, 0)
	visited := make(map[string]struct{})

	for {
		if len(queue) <= 0 {
			cur := nodes[toPoint.Id]
			seq := make([]*GraphEdge, 0)
			for {
				parent := cur.Parent
				if parent == nil {
					slices.Reverse(seq)
					path := Path{
						Edges: seq,
					}
					return path
				}
				track, err := graph.TrackBetween(cur.Value, parent.Value)
				if err != nil {
					fmt.Println(err)
					return Path{}
				}

				seq = append(seq, track)
				cur = parent
			}
		}
		// fetch element from priority queue
		elem := heap.Pop(&queue).(*PathNode)
		if elem.Value.IsDeadEnd {
			continue
		}
		if _, ok := visited[elem.Id]; !ok {
			visited[elem.Id] = struct{}{}
			for key, edge := range graph.NeighborMap[elem.Id] {
				newCost := elem.Cost + float64(edge.Length)
				v := nodes[key]
				if newCost < v.Cost {
					v.Cost = newCost
					v.Parent = elem
				}
				heap.Push(&queue, v)
			}
		}

	}
}

func (graph *TrackGraph) OtherEnd(track *TrackSegment, pointId string) *TrackPoint {
	edge := graph.Edges[track.Id]
	if edge.From.Id == pointId {
		return edge.To
	}
	if edge.To.Id == pointId {
		return edge.From
	}
	return nil
}

func (graph *TrackGraph) FindPathToTrack(fromPoint *TrackPoint, toSegment *TrackSegment) *Path {
	queue := make(PathQueue, 0)
	nodes := make(map[string]*PathNode, 0)
	for _, vert := range graph.points {
		nodes[vert.Id] = &PathNode{
			Id:     vert.Id,
			Value:  vert,
			Cost:   math.MaxInt,
			Parent: nil,
		}
	}

	// set initial node cost 0
	nodes[fromPoint.Id].Cost = 0
	queue = append(queue, nodes[fromPoint.Id])
	heap.Init(&queue)

	var approachedPoint *TrackPoint
	var targetPoint *TrackPoint

	found := false

	// path := make([]*GraphEdge, 0)
	visited := make(map[string]struct{})
	for {
		if len(queue) == 0 {
			break
		}
		if found {
			break
		}
		// fetch element from priority queue
		elem := heap.Pop(&queue).(*PathNode)

		if _, ok := visited[elem.Id]; !ok {
			visited[elem.Id] = struct{}{}
			for key, edge := range graph.NeighborMap[elem.Id] {
				newCost := elem.Cost + float64(edge.Length)
				v := nodes[key]
				if newCost < v.Cost {
					v.Cost = newCost
					v.Parent = elem
				}
				if edge.Id == toSegment.Id {
					graphEdge := graph.Edges[edge.Id]
					approachedPoint = elem.Value
					targetPoint = graph.OtherEnd(graphEdge.Track, approachedPoint.Id)
					found = true
					break
				}

				if v.Value.IsDeadEnd {
					continue
				}

				heap.Push(&queue, v)
			}
		}

	}

	if targetPoint == nil {
		fmt.Println("Target point is empty?!")
		return nil
	}

	cur := nodes[targetPoint.Id]
	seq := make([]*GraphEdge, 0)
	for {
		parent := cur.Parent
		if parent == nil {
			slices.Reverse(seq)
			path := &Path{
				Edges: seq,
			}
			return path
		}
		track, err := graph.TrackBetween(cur.Value, parent.Value)
		if err != nil {
			fmt.Println(err)
			return nil
		}

		seq = append(seq, track)
		cur = parent
	}
}
