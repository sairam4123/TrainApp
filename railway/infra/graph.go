package infra

import (
	"errors"
	"fmt"
	"slices"
	"trainapp/railway/ids"
)

// MaxCandidatePaths bounds GenerateCandidatePaths so that loop-heavy layouts
// cannot blow up the search.
const MaxCandidatePaths = 256

var ErrCacheNotBuilt = errors.New("track graph cache not built, call World.Finalize (or TrackGraph.BuildCacheMap) first")

type GraphEdge struct {
	Track *TrackSegment
	From  *TrackPoint
	To    *TrackPoint
}

// Other returns the end of the edge opposite to pt, or nil if pt is not an end.
func (e *GraphEdge) Other(pt *TrackPoint) *TrackPoint {
	switch pt.Id {
	case e.From.Id:
		return e.To
	case e.To.Id:
		return e.From
	}
	return nil
}

type TrackGraph struct {
	points map[ids.PointID]*TrackPoint
	tracks map[ids.TrackID]*TrackSegment

	Edges map[ids.TrackID]*GraphEdge

	NeighborMap map[ids.PointID]map[ids.PointID]*TrackSegment

	adjacency  map[ids.PointID][]*GraphEdge               // incident edges, sorted; built by BuildCacheMap
	forbidden  map[ids.PointID]map[transitionKey]struct{} // track pairs that cannot be traversed through the point
	cacheBuilt bool
}

func (graph *TrackGraph) Init() {
	graph.points = make(map[ids.PointID]*TrackPoint)
	graph.Edges = make(map[ids.TrackID]*GraphEdge)
	graph.tracks = make(map[ids.TrackID]*TrackSegment)
	graph.NeighborMap = make(map[ids.PointID]map[ids.PointID]*TrackSegment)
	graph.adjacency = make(map[ids.PointID][]*GraphEdge)
	graph.forbidden = make(map[ids.PointID]map[transitionKey]struct{})
}

// ListTracks returns every track, sorted by id.
func (graph *TrackGraph) ListTracks() []*TrackSegment {
	tracks := make([]*TrackSegment, 0, len(graph.tracks))
	for _, trk := range graph.tracks {
		tracks = append(tracks, trk)
	}
	slices.SortFunc(tracks, func(a, b *TrackSegment) int { return a.Id.Compare(b.Id) })
	return tracks
}

func (graph *TrackGraph) GetTrack(id ids.TrackID) (*TrackSegment, bool) {
	trk, ok := graph.tracks[id]
	return trk, ok
}

func (graph *TrackGraph) GetPoint(id ids.PointID) (*TrackPoint, bool) {
	pt, ok := graph.points[id]
	return pt, ok
}

func (graph *TrackGraph) Edge(track *TrackSegment) (*GraphEdge, bool) {
	edge, ok := graph.Edges[track.Id]
	return edge, ok
}

func (graph *TrackGraph) FindWorldBoundaryPoint(segment *TrackSegment) *TrackPoint {
	edge, ok := graph.Edges[segment.Id]
	if !ok {
		return nil
	}
	if edge.From.IsSimBoundary {
		return edge.From
	}
	if edge.To.IsSimBoundary {
		return edge.To
	}
	return nil
}

// AddTrack joins two points with a track and returns the resulting edge. The
// track's id is set from the two points.
//
// Adding a second track between the same two points returns the existing edge,
// which keeps the lower of the two speed limits. This is how overlapping switch
// blocks (e.g. a scissors crossover) share edges. Using two different point
// objects with the same id, or a track already added between other points, is
// a construction bug and panics.
func (graph *TrackGraph) AddTrack(fromPoint *TrackPoint, toPoint *TrackPoint, track *TrackSegment) *GraphEdge {
	id := ids.NewTrackID(fromPoint.Id, toPoint.Id)
	if !track.Id.IsZero() && track.Id != id {
		panic(fmt.Sprintf("infra: track %s is already in the graph, cannot add it again between %s and %s", track, fromPoint.Id, toPoint.Id))
	}
	if existing, ok := graph.Edges[id]; ok {
		existing.Track.MaxSpeed = min(existing.Track.MaxSpeed, track.MaxSpeed)
		if existing.Track.Name == "" {
			existing.Track.Name = track.Name
		}
		return existing
	}

	graph.addPoint(fromPoint)
	graph.addPoint(toPoint)

	track.Id = id
	edge := &GraphEdge{
		From:  fromPoint,
		To:    toPoint,
		Track: track,
	}
	graph.tracks[id] = track
	graph.Edges[id] = edge
	graph.cacheBuilt = false
	return edge
}

func (graph *TrackGraph) addPoint(pt *TrackPoint) {
	if existing, ok := graph.points[pt.Id]; ok && existing != pt {
		panic(fmt.Sprintf("infra: two different track points share the id %s", pt.Id))
	}
	graph.points[pt.Id] = pt
}

func (graph *TrackGraph) TrackBetween(fromPoint *TrackPoint, toPoint *TrackPoint) (*GraphEdge, error) {
	if !graph.cacheBuilt {
		return nil, ErrCacheNotBuilt
	}
	edge, ok := graph.Edges[ids.NewTrackID(fromPoint.Id, toPoint.Id)]
	if !ok {
		return nil, fmt.Errorf("no track between %s and %s", fromPoint.Id, toPoint.Id)
	}
	return edge, nil
}

func (graph *TrackGraph) BuildCacheMap() {
	graph.NeighborMap = make(map[ids.PointID]map[ids.PointID]*TrackSegment)
	graph.adjacency = make(map[ids.PointID][]*GraphEdge)
	for _, edge := range graph.Edges {
		if graph.NeighborMap[edge.From.Id] == nil {
			graph.NeighborMap[edge.From.Id] = make(map[ids.PointID]*TrackSegment)
		}
		if graph.NeighborMap[edge.To.Id] == nil {
			graph.NeighborMap[edge.To.Id] = make(map[ids.PointID]*TrackSegment)
		}

		graph.NeighborMap[edge.From.Id][edge.To.Id] = edge.Track
		graph.NeighborMap[edge.To.Id][edge.From.Id] = edge.Track

		graph.adjacency[edge.From.Id] = append(graph.adjacency[edge.From.Id], edge)
		graph.adjacency[edge.To.Id] = append(graph.adjacency[edge.To.Id], edge)
	}
	// map iteration order is random; sort so path search is deterministic
	for _, edges := range graph.adjacency {
		slices.SortFunc(edges, func(a, b *GraphEdge) int { return a.Track.Id.Compare(b.Track.Id) })
	}
	graph.cacheBuilt = true
}

// transitionKey is an unordered pair of tracks.
type transitionKey struct{ a, b ids.TrackID }

func newTransitionKey(x, y *GraphEdge) transitionKey {
	a, b := x.Track.Id, y.Track.Id
	if a.Compare(b) > 0 {
		a, b = b, a
	}
	return transitionKey{a, b}
}

// forbidTransition records that a train cannot pass through `at` going from
// edge a to edge b (or back), e.g. between the two legs of a switch.
func (graph *TrackGraph) forbidTransition(at *TrackPoint, a, b *GraphEdge) {
	if graph.forbidden[at.Id] == nil {
		graph.forbidden[at.Id] = make(map[transitionKey]struct{})
	}
	graph.forbidden[at.Id][newTransitionKey(a, b)] = struct{}{}
}

// TransitionAllowed reports whether a train can pass through `at` from edge
// `from` onto edge `to`.
func (graph *TrackGraph) TransitionAllowed(at *TrackPoint, from, to *GraphEdge) bool {
	if from == to {
		return false // reversing onto the same track
	}
	_, banned := graph.forbidden[at.Id][newTransitionKey(from, to)]
	return !banned
}

// GenerateCandidatePaths returns every path from fromTp that ends by running
// over toTrack, in breadth-first (fewest tracks first) order. A path never
// visits a point twice and never makes a transition the layout forbids (such
// as from one switch leg to the other). At most MaxCandidatePaths are returned.
func (graph *TrackGraph) GenerateCandidatePaths(fromTp *TrackPoint, toTrack *TrackSegment) ([]*Path, error) {
	return graph.GenerateCandidatePathsVia(nil, fromTp, toTrack)
}

// GenerateCandidatePathsVia is GenerateCandidatePaths for a train that reached
// fromTp over the edge via: paths do not reverse onto via, and their first
// transition out of via must be allowed. A nil via means no constraint.
func (graph *TrackGraph) GenerateCandidatePathsVia(via *GraphEdge, fromTp *TrackPoint, toTrack *TrackSegment) ([]*Path, error) {
	if !graph.cacheBuilt {
		return nil, ErrCacheNotBuilt
	}
	if fromTp == nil || toTrack == nil {
		return nil, errors.New("GenerateCandidatePaths: nil start point or target track")
	}
	if via != nil && via.Other(fromTp) == nil {
		return nil, fmt.Errorf("GenerateCandidatePaths: %s does not end at %s", via.Track, fromTp.Id)
	}
	if _, ok := graph.points[fromTp.Id]; !ok {
		return nil, fmt.Errorf("GenerateCandidatePaths: unknown point %s", fromTp.Id)
	}
	if _, ok := graph.tracks[toTrack.Id]; !ok {
		return nil, fmt.Errorf("GenerateCandidatePaths: unknown track %s", toTrack)
	}

	type partial struct {
		edges  []*GraphEdge
		points []*TrackPoint // points[0] is fromTp, points[i+1] is the end of edges[i]
	}

	queue := []partial{{points: []*TrackPoint{fromTp}}}
	allPaths := make([]*Path, 0)

	for head := 0; head < len(queue) && len(allPaths) < MaxCandidatePaths; head++ {
		cur := queue[head]
		queue[head] = partial{}

		curPoint := cur.points[len(cur.points)-1]
		lastEdge := via
		if len(cur.edges) > 0 {
			lastEdge = cur.edges[len(cur.edges)-1]
		}

		for _, edge := range graph.adjacency[curPoint.Id] {
			if lastEdge != nil && !graph.TransitionAllowed(curPoint, lastEdge, edge) {
				continue
			}
			nextPoint := edge.Other(curPoint)
			if slices.ContainsFunc(cur.points, func(p *TrackPoint) bool { return p.Id == nextPoint.Id }) {
				continue
			}

			edges := append(slices.Clip(cur.edges), edge)
			if edge.Track.Id == toTrack.Id {
				allPaths = append(allPaths, &Path{InitPoint: fromTp, Edges: edges})
				continue
			}
			queue = append(queue, partial{edges: edges, points: append(slices.Clip(cur.points), nextPoint)})
		}
	}

	return allPaths, nil
}

func (graph *TrackGraph) OtherEnd(track *TrackSegment, pointId ids.PointID) *TrackPoint {
	edge, ok := graph.Edges[track.Id]
	if !ok {
		return nil
	}
	if edge.From.Id == pointId {
		return edge.To
	}
	if edge.To.Id == pointId {
		return edge.From
	}
	return nil
}

// Position is where a train stands for planning: at a point, having arrived
// over an edge. Via is nil for a train that has not moved yet (no constraint
// on its first edge).
type Position struct {
	At  *TrackPoint
	Via *GraphEdge
}

func (p Position) key() positionKey {
	k := positionKey{at: p.At.Id}
	if p.Via != nil {
		k.via = p.Via.Track.Id
	}
	return k
}

type positionKey struct {
	at  ids.PointID
	via ids.TrackID
}

// Reach searches outward from the start positions, honouring the layout's
// forbidden transitions and never reversing onto the edge just run. A step is
// only taken if allow returns true for it (nil allows everything). When a step
// satisfies isTarget the search records the position after it and does not go
// further along that branch. It returns those positions, deduplicated, in a
// deterministic order. Requires the cache to be built.
func (graph *TrackGraph) Reach(starts []Position, isTarget, allow func(Step) bool) ([]Position, error) {
	if !graph.cacheBuilt {
		return nil, ErrCacheNotBuilt
	}
	seen := make(map[positionKey]struct{})
	found := make(map[positionKey]struct{})
	var out []Position

	queue := make([]Position, 0, len(starts))
	for _, s := range starts {
		if _, ok := seen[s.key()]; !ok {
			seen[s.key()] = struct{}{}
			queue = append(queue, s)
		}
	}
	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		for _, edge := range graph.adjacency[cur.At.Id] {
			if cur.Via != nil && !graph.TransitionAllowed(cur.At, cur.Via, edge) {
				continue
			}
			step := Step{Edge: edge, From: cur.At, To: edge.Other(cur.At)}
			if allow != nil && !allow(step) {
				continue
			}
			next := Position{At: step.To, Via: edge}
			if isTarget(step) {
				if _, ok := found[next.key()]; !ok {
					found[next.key()] = struct{}{}
					out = append(out, next)
				}
				continue
			}
			if _, ok := seen[next.key()]; ok {
				continue
			}
			seen[next.key()] = struct{}{}
			queue = append(queue, next)
		}
	}
	return out, nil
}
