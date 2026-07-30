package railway

import (
	"errors"
	"fmt"
)

type SlipOrientation int

const (
	FromAtoB SlipOrientation = iota
	FromBtoA
)

type SwitchBlock struct {
	Id string

	activeEdge *GraphEdge

	managedEdges []*GraphEdge

	isLocked bool
	lockedBy *Train
}

func NewSwitch(world *World, id string, from, a, b *TrackPoint) *SwitchBlock {
	// create two edges
	// from -> a
	// from -> b

	fromA := world.NewSwitchTrack(NewTrackID(from, a))
	fromB := world.NewSwitchTrack(NewTrackID(from, b))

	world.TrackGraph.AddTrack(from, a, fromA)
	world.TrackGraph.AddTrack(from, b, fromB)

	fromAEdge := world.TrackGraph.Edges[fromA.Id]
	fromBEdge := world.TrackGraph.Edges[fromB.Id]

	managedEdges := []*GraphEdge{fromAEdge, fromBEdge}
	swBlk := world.newSwitchBlock(id, managedEdges, fromAEdge)
	return swBlk
}

func NewDiamondCrossing(world *World, id string, fromA, toA, fromB, toB *TrackPoint) *SwitchBlock {
	// create two edges
	// fromA -> toA
	// fromB -> toB

	fromATrk := world.NewSwitchTrack(NewTrackID(fromA, toA))
	fromBTrk := world.NewSwitchTrack(NewTrackID(fromB, toB))

	world.TrackGraph.AddTrack(fromA, toA, fromATrk)
	world.TrackGraph.AddTrack(fromB, toB, fromBTrk)

	fromAEdge := world.TrackGraph.Edges[fromATrk.Id]
	fromBEdge := world.TrackGraph.Edges[fromBTrk.Id]

	managedEdges := []*GraphEdge{fromAEdge, fromBEdge}

	swBlk := world.newSwitchBlock(id, managedEdges, fromAEdge)

	return swBlk

}

func newSlipSwitch(world *World, id string, fromA, toA, fromB, toB *TrackPoint, isDoubleSlip bool, orientation SlipOrientation) (*SwitchBlock, error) {

	if orientation > 1 {
		return nil, errors.New("Orientation possible values: 0, 1")
	}
	if orientation != 0 && isDoubleSlip {
		return nil, errors.New("Orientation must be 0 for double slips. ")
	}

	fromATrk := world.NewSwitchTrack(NewTrackID(fromA, toA))
	fromBTrk := world.NewSwitchTrack(NewTrackID(fromB, toB))

	world.TrackGraph.AddTrack(fromA, toA, fromATrk)
	world.TrackGraph.AddTrack(fromB, toB, fromBTrk)

	fromAEdge := world.TrackGraph.Edges[fromA.Id]
	fromBEdge := world.TrackGraph.Edges[fromB.Id]
	managedEdges := []*GraphEdge{fromAEdge, fromBEdge}

	if isDoubleSlip {
		fromAtoB := world.NewSwitchTrack(NewTrackID(fromA, toB))
		fromBtoA := world.NewSwitchTrack(NewTrackID(fromB, toA))

		world.TrackGraph.AddTrack(fromA, toB, fromAtoB)
		world.TrackGraph.AddTrack(fromB, toA, fromBtoA)

		fromAtoBEdge := world.TrackGraph.Edges[fromAtoB.Id]
		fromBtoAEdge := world.TrackGraph.Edges[fromBtoA.Id]

		managedEdges = append(managedEdges, fromAtoBEdge, fromBtoAEdge)
	} else {
		switch orientation {
		case FromAtoB:
			fromAtoB := world.NewSwitchTrack(NewTrackID(fromA, toB))
			world.TrackGraph.AddTrack(fromA, toB, fromAtoB)
			fromAtoBEdge := world.TrackGraph.Edges[fromAtoB.Id]
			managedEdges = append(managedEdges, fromAtoBEdge)

		case FromBtoA:
			fromBtoA := world.NewSwitchTrack(NewTrackID(fromB, toA))
			world.TrackGraph.AddTrack(fromB, toA, fromBtoA)
			fromBtoAEdge := world.TrackGraph.Edges[fromBtoA.Id]
			managedEdges = append(managedEdges, fromBtoAEdge)

		default:
			return nil, fmt.Errorf("Orientation possible values: 0, 1, received: %d", orientation)
		}
	}

	swBlk := world.newSwitchBlock(id, managedEdges, fromAEdge)

	return swBlk, nil
}

func NewSingleSlip(world *World, id string, fromA, toA, fromB, toB *TrackPoint, orientation SlipOrientation) (*SwitchBlock, error) {
	return newSlipSwitch(world, id, fromA, toA, fromB, toB, false, orientation)
}

func NewDoubleSlip(world *World, id string, fromA, toA, fromB, toB *TrackPoint) (*SwitchBlock, error) {
	return newSlipSwitch(world, id, fromA, toA, fromB, toB, true, FromAtoB)
}

func (sb *SwitchBlock) SetActiveEdge(edge *GraphEdge) error {
	if sb.isLocked {
		return errors.New("Switch block is locked")
	}
	sb.activeEdge = edge
	return nil
}

func (sb *SwitchBlock) LockSwitchFor(train *Train) error {
	if sb.isLocked && sb.lockedBy.Number != train.Number {
		return fmt.Errorf("Switch already locked by train %s, cannot lock switch %s", sb.lockedBy, sb.Id)
	}
	if sb.activeEdge == nil {
		return fmt.Errorf("Switch does not have active edge, cannot lock switch %s", sb.Id)
	}

	sb.isLocked = true
	sb.lockedBy = train

	return nil
}

func (sb *SwitchBlock) UnlockSwitchFor(train *Train) error {
	if !sb.isLocked {
		return fmt.Errorf("Switch not locked, cannot unlock switch %s", sb.Id)
	}
	if sb.lockedBy != nil && sb.lockedBy.Number != train.Number {
		return fmt.Errorf("Switch locked by train %s, cannot unlock switch %s", sb.lockedBy, sb.Id)
	}

	sb.isLocked = false
	sb.lockedBy = nil
	return nil
}
