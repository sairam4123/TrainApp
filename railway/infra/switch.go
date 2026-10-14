package infra

import (
	"errors"
	"fmt"
	"slices"
)

type SlipOrientation int

const (
	FromAtoB SlipOrientation = iota
	FromBtoA
)

// SwitchBlock is a set of edges of which only one can be in use at a time
// (the legs of a switch, the two routes over a diamond, ...). Its position and
// lock are runtime state owned by the interlocking.
type SwitchBlock struct {
	Id string

	defaultEdge *GraphEdge

	managedEdges []*GraphEdge
}

func NewSwitch(world *World, id string, from, to, div *TrackPoint) *SwitchBlock {
	// create two edges
	// from -> to  (straight)
	// from -> div (diverging)

	fromAEdge := world.TrackGraph.AddTrack(from, to, world.NewTrackSegment("", world.data.DefaultSwLength))
	fromBEdge := world.TrackGraph.AddTrack(from, div, world.NewSwitchTrack(""))

	return world.newSwitchBlock(id, []*GraphEdge{fromAEdge, fromBEdge}, fromAEdge)
}

func NewDiamondCrossing(world *World, id string, fromA, toA, fromB, toB *TrackPoint) *SwitchBlock {
	// create two edges that cross each other
	// fromA -> toA
	// fromB -> toB

	fromAEdge := world.TrackGraph.AddTrack(fromA, toA, world.NewSwitchTrack(""))
	fromBEdge := world.TrackGraph.AddTrack(fromB, toB, world.NewSwitchTrack(""))

	return world.newSwitchBlock(id, []*GraphEdge{fromAEdge, fromBEdge}, fromAEdge)
}

func newSlipSwitch(world *World, id string, fromA, toA, fromB, toB *TrackPoint, isDoubleSlip bool, orientation SlipOrientation) (*SwitchBlock, error) {

	if orientation != FromAtoB && orientation != FromBtoA {
		return nil, fmt.Errorf("Orientation possible values: 0, 1, received: %d", orientation)
	}
	if orientation != FromAtoB && isDoubleSlip {
		return nil, errors.New("Orientation must be 0 for double slips. ")
	}

	fromAEdge := world.TrackGraph.AddTrack(fromA, toA, world.NewTrackSegment("", world.data.DefaultSwLength))
	fromBEdge := world.TrackGraph.AddTrack(fromB, toB, world.NewTrackSegment("", world.data.DefaultSwLength))
	managedEdges := []*GraphEdge{fromAEdge, fromBEdge}

	if isDoubleSlip || orientation == FromAtoB {
		managedEdges = append(managedEdges, world.TrackGraph.AddTrack(fromA, toB, world.NewSwitchTrack("")))
	}
	if isDoubleSlip || orientation == FromBtoA {
		managedEdges = append(managedEdges, world.TrackGraph.AddTrack(fromB, toA, world.NewSwitchTrack("")))
	}

	return world.newSwitchBlock(id, managedEdges, fromAEdge), nil
}

func NewSingleSlip(world *World, id string, fromA, toA, fromB, toB *TrackPoint, orientation SlipOrientation) (*SwitchBlock, error) {
	return newSlipSwitch(world, id, fromA, toA, fromB, toB, false, orientation)
}

func NewDoubleSlip(world *World, id string, fromA, toA, fromB, toB *TrackPoint) (*SwitchBlock, error) {
	return newSlipSwitch(world, id, fromA, toA, fromB, toB, true, FromAtoB)
}

func (sb *SwitchBlock) SetDefaultEdge(edge *GraphEdge) error {
	if !slices.Contains(sb.managedEdges, edge) {
		return fmt.Errorf("edge %s is not managed by switch block %s", edge.Track, sb.Id)
	}
	sb.defaultEdge = edge
	return nil
}

func (sb *SwitchBlock) DefaultEdge() *GraphEdge {
	return sb.defaultEdge
}

func (sb *SwitchBlock) ListManagedEdges() []*GraphEdge {
	return slices.Clone(sb.managedEdges)
}

func (sb *SwitchBlock) Manages(edge *GraphEdge) bool {
	return slices.Contains(sb.managedEdges, edge)
}
