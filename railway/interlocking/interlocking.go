// Package interlocking owns the runtime safety state of the railway: which
// train has reserved or occupied each track, and how each switch block is set
// and locked. Callers never touch that state directly; they reserve a Route and
// then Enter and Leave its edges as the train moves.
//
// Trains are identified by ids.TrainID, so this package does not depend on how
// trains or schedules are modelled.
package interlocking

import (
	"errors"
	"fmt"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

var ErrIllegalPath = errors.New("path is broken or makes a forbidden transition")

// ConflictError reports a resource held by another route that stopped a
// reservation.
type ConflictError struct {
	Edge   *infra.GraphEdge
	Train  ids.TrainID        // the train that asked
	HeldBy ids.TrainID        // the train holding the resource
	Switch *infra.SwitchBlock // the locked switch, if that was the conflict; nil if it was the track
	Reason string
}

// Blockers returns the tracks whose release could clear the conflict: the
// track itself, or for a locked switch every track the switch manages (it is
// unlocked when its holder leaves whichever of them it is using).
func (e *ConflictError) Blockers() []*infra.TrackSegment {
	if e.Switch == nil {
		return []*infra.TrackSegment{e.Edge.Track}
	}
	edges := e.Switch.ListManagedEdges()
	tracks := make([]*infra.TrackSegment, len(edges))
	for i, edge := range edges {
		tracks[i] = edge.Track
	}
	return tracks
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s cannot use %s: %s by %s", e.Train, e.Edge.Track, e.Reason, e.HeldBy)
}

// StateError reports an operation that does not match the state of a track,
// which means the caller's bookkeeping is wrong.
type StateError struct {
	Edge       *infra.GraphEdge
	Train      ids.TrainID
	Op         string
	ReservedBy ids.TrainID
	OccupiedBy ids.TrainID
}

func (e *StateError) Error() string {
	return fmt.Sprintf("%s cannot %s %s (reserved by %q, occupied by %q)", e.Train, e.Op, e.Edge.Track, e.ReservedBy, e.OccupiedBy)
}

type Interlocking struct {
	world *infra.World

	tracks   *trackClaims
	switches *switchControl
}

// New creates an interlocking for a finalized world with everything free.
func New(world *infra.World) (*Interlocking, error) {
	if !world.IsFinalized() {
		return nil, errors.New("interlocking: world is not finalized, call World.Finalize first")
	}
	return &Interlocking{
		world:    world,
		tracks:   newTrackClaims(world),
		switches: newSwitchControl(world),
	}, nil
}

// Reserve reserves every track on the path and sets and locks every switch on
// it, for the train. It is all or nothing: on failure nothing this call
// claimed is kept, and nothing the train already held is touched. A
// *ConflictError names the first resource in the way.
func (ilk *Interlocking) Reserve(path *infra.Path, train ids.TrainID) (*Route, error) {
	if train.IsZero() {
		return nil, errors.New("interlocking: empty train id")
	}
	if !ilk.world.IsLegalPath(path) {
		return nil, ErrIllegalPath
	}
	steps, _ := path.Steps()

	reserved := 0
	switched := 0
	undo := func() {
		for _, s := range steps[:switched] {
			ilk.switches.release(s.Edge, train)
		}
		for _, s := range steps[:reserved] {
			ilk.tracks.release(s.Edge, train)
		}
	}

	for _, s := range steps {
		if err := ilk.tracks.reserve(s.Edge, train); err != nil {
			undo()
			return nil, err
		}
		reserved++
		if err := ilk.switches.claim(s.Edge, train); err != nil {
			undo()
			return nil, err
		}
		switched++
	}

	return &Route{train: train, path: path, steps: steps, end: len(steps)}, nil
}

// ReserveFirst tries the paths in order and reserves the first that succeeds.
// If none can be reserved it returns the error from the first path, which is
// usually the most useful one to wait on.
func (ilk *Interlocking) ReserveFirst(paths []*infra.Path, train ids.TrainID) (*Route, error) {
	if len(paths) == 0 {
		return nil, errors.New("interlocking: no candidate paths")
	}
	var firstErr error
	for _, p := range paths {
		route, err := ilk.Reserve(p, train)
		if err == nil {
			return route, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

// Enter moves the front of the train onto the next edge of the route and
// returns it.
func (ilk *Interlocking) Enter(route *Route) (infra.Step, error) {
	if route.entered >= route.end {
		return infra.Step{}, fmt.Errorf("interlocking: %s has no reserved edge left to enter", route.train)
	}
	step := route.steps[route.entered]
	if err := ilk.tracks.occupy(step.Edge, route.train); err != nil {
		return infra.Step{}, err
	}
	route.entered++
	return step, nil
}

// Leave moves the rear of the train off the oldest edge it occupies on the
// route, freeing the track and unlocking its switches, and returns it.
func (ilk *Interlocking) Leave(route *Route) (infra.Step, error) {
	if route.left >= route.entered {
		return infra.Step{}, fmt.Errorf("interlocking: %s occupies no edge of the route", route.train)
	}
	step := route.steps[route.left]
	if err := ilk.tracks.vacate(step.Edge, route.train); err != nil {
		return infra.Step{}, err
	}
	ilk.switches.release(step.Edge, route.train)
	route.left++
	return step, nil
}

// Release gives up the part of the route the train has not entered yet. Edges
// the train occupies stay occupied until it Leaves them. It returns the
// tracks freed.
func (ilk *Interlocking) Release(route *Route) []*infra.TrackSegment {
	freed := make([]*infra.TrackSegment, 0, route.end-route.entered)
	for i := route.entered; i < route.end; i++ {
		step := route.steps[i]
		ilk.switches.release(step.Edge, route.train)
		if err := ilk.tracks.release(step.Edge, route.train); err == nil {
			freed = append(freed, step.Edge.Track)
		}
	}
	route.end = route.entered
	return freed
}

// TrackHolder reports which train has reserved and which has occupied the
// track (zero for none).
func (ilk *Interlocking) TrackHolder(track *infra.TrackSegment) (reservedBy, occupiedBy ids.TrainID) {
	return ilk.tracks.holder(track)
}

// Holdings returns the tracks the train has reserved or occupied, sorted.
func (ilk *Interlocking) Holdings(train ids.TrainID) []*infra.TrackSegment {
	return ilk.tracks.holdings(train)
}

func (ilk *Interlocking) IsTrackFree(track *infra.TrackSegment) bool {
	r, o := ilk.tracks.holder(track)
	return r.IsZero() && o.IsZero()
}

// SwitchState reports the edge a switch block is set for and the train that
// has it locked (zero for none).
func (ilk *Interlocking) SwitchState(sb *infra.SwitchBlock) (active *infra.GraphEdge, lockedBy ids.TrainID) {
	st := ilk.switches.state(sb)
	return st.active, st.lockedBy
}
