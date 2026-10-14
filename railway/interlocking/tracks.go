package interlocking

import (
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

// trackClaim is the runtime state of one track. A track is free, reserved by
// a train, or reserved and occupied by that same train; it is never occupied
// without being reserved by the occupier.
type trackClaim struct {
	reservedBy ids.TrainID
	occupiedBy ids.TrainID
}

type trackClaims struct {
	claims map[ids.TrackID]*trackClaim
	tracks map[ids.TrackID]*infra.TrackSegment

	byTrain map[ids.TrainID]map[ids.TrackID]struct{} // tracks each train has reserved or occupied
}

func newTrackClaims(world *infra.World) *trackClaims {
	tc := &trackClaims{
		claims:  map[ids.TrackID]*trackClaim{},
		tracks:  map[ids.TrackID]*infra.TrackSegment{},
		byTrain: map[ids.TrainID]map[ids.TrackID]struct{}{},
	}
	for _, trk := range world.TrackGraph.ListTracks() {
		tc.claims[trk.Id] = &trackClaim{}
	}
	return tc
}

func (tc *trackClaims) get(track *infra.TrackSegment) *trackClaim {
	c, ok := tc.claims[track.Id]
	if !ok {
		c = &trackClaim{}
		tc.claims[track.Id] = c
	}
	tc.tracks[track.Id] = track
	return c
}

func (tc *trackClaims) index(track *infra.TrackSegment, train ids.TrainID) {
	if tc.byTrain[train] == nil {
		tc.byTrain[train] = map[ids.TrackID]struct{}{}
	}
	tc.byTrain[train][track.Id] = struct{}{}
}

func (tc *trackClaims) unindex(track *infra.TrackSegment, train ids.TrainID) {
	delete(tc.byTrain[train], track.Id)
	if len(tc.byTrain[train]) == 0 {
		delete(tc.byTrain, train)
	}
}

func (tc *trackClaims) holdings(train ids.TrainID) []*infra.TrackSegment {
	out := make([]*infra.TrackSegment, 0, len(tc.byTrain[train]))
	for id := range tc.byTrain[train] {
		out = append(out, tc.tracks[id])
	}
	slices.SortFunc(out, func(a, b *infra.TrackSegment) int { return a.Id.Compare(b.Id) })
	return out
}

// reserve claims a free track. A track already reserved, even by the same
// train, is a conflict: one route per track.
func (tc *trackClaims) reserve(edge *infra.GraphEdge, train ids.TrainID) error {
	c := tc.get(edge.Track)
	if !c.reservedBy.IsZero() {
		return &ConflictError{Edge: edge, Train: train, HeldBy: c.reservedBy, Reason: "track reserved"}
	}
	if !c.occupiedBy.IsZero() {
		return &ConflictError{Edge: edge, Train: train, HeldBy: c.occupiedBy, Reason: "track occupied"}
	}
	c.reservedBy = train
	tc.index(edge.Track, train)
	return nil
}

// release drops a reservation the train holds but has not used.
func (tc *trackClaims) release(edge *infra.GraphEdge, train ids.TrainID) error {
	c := tc.get(edge.Track)
	if c.reservedBy != train || !c.occupiedBy.IsZero() {
		return &StateError{Edge: edge, Train: train, Op: "release", ReservedBy: c.reservedBy, OccupiedBy: c.occupiedBy}
	}
	c.reservedBy = ids.TrainID{}
	tc.unindex(edge.Track, train)
	return nil
}

// occupy marks a track the train has reserved as physically occupied by it.
func (tc *trackClaims) occupy(edge *infra.GraphEdge, train ids.TrainID) error {
	c := tc.get(edge.Track)
	if c.reservedBy != train || !c.occupiedBy.IsZero() {
		return &StateError{Edge: edge, Train: train, Op: "occupy", ReservedBy: c.reservedBy, OccupiedBy: c.occupiedBy}
	}
	c.occupiedBy = train
	return nil
}

// vacate frees a track the train occupied: the train has passed, so both the
// occupation and the reservation end.
func (tc *trackClaims) vacate(edge *infra.GraphEdge, train ids.TrainID) error {
	c := tc.get(edge.Track)
	if c.occupiedBy != train {
		return &StateError{Edge: edge, Train: train, Op: "vacate", ReservedBy: c.reservedBy, OccupiedBy: c.occupiedBy}
	}
	c.occupiedBy = ids.TrainID{}
	c.reservedBy = ids.TrainID{}
	tc.unindex(edge.Track, train)
	return nil
}

func (tc *trackClaims) holder(track *infra.TrackSegment) (reservedBy, occupiedBy ids.TrainID) {
	c := tc.get(track)
	return c.reservedBy, c.occupiedBy
}
