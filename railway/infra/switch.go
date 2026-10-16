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

	seq int // order the block was built in; SwitchBlocksOn lists them so
}

// Heading is a direction along the schematic: East is left to right.
type Heading int

const (
	East Heading = iota
	West
)

// SwitchLayout says how a switch is drawn: which way its legs leave the toe,
// and how many lanes the diverging leg ends up from the toe (positive is
// down). The straight leg keeps whatever lanes it already has, 0 if it is new.
type SwitchLayout struct {
	Legs  Heading
	Lanes int
}

// NewSwitch joins toe to straight and to div; only one of the two legs can be
// in use at a time.
func NewSwitch(world *World, id string, toe, straight, div *TrackPoint, layout SwitchLayout) *SwitchBlock {
	if layout.Lanes == 0 {
		panic(fmt.Sprintf("infra: switch %s: the diverging leg must move at least one lane", id))
	}
	var straightEdge, divEdge *GraphEdge
	switch layout.Legs {
	case East:
		straightEdge = world.TrackGraph.AddTrack(toe, straight, world.NewTrackSegment("", world.data.DefaultSwLength))
		divEdge = world.TrackGraph.AddTrack(toe, div, world.NewSwitchTrack("")).SetLanes(layout.Lanes)
	case West:
		straightEdge = world.TrackGraph.AddTrack(straight, toe, world.NewTrackSegment("", world.data.DefaultSwLength))
		divEdge = world.TrackGraph.AddTrack(div, toe, world.NewSwitchTrack("")).SetLanes(-layout.Lanes)
	default:
		panic(fmt.Sprintf("infra: switch %s: unknown heading %d", id, layout.Legs))
	}

	return world.newSwitchBlock(id, []*GraphEdge{straightEdge, divEdge}, straightEdge)
}

// NewDiamondCrossing creates two tracks that cross each other, fromA -> toA
// and fromB -> toB, with the from points at the west end. A moves lanes lanes
// and B moves them back, so the two cross on the schematic.
func NewDiamondCrossing(world *World, id string, fromA, toA, fromB, toB *TrackPoint, lanes int) *SwitchBlock {
	if lanes == 0 {
		panic(fmt.Sprintf("infra: diamond %s: the tracks must move at least one lane to cross", id))
	}
	fromAEdge := world.TrackGraph.AddTrack(fromA, toA, world.NewSwitchTrack("")).SetLanes(lanes)
	fromBEdge := world.TrackGraph.AddTrack(fromB, toB, world.NewSwitchTrack("")).SetLanes(-lanes)

	return world.newSwitchBlock(id, []*GraphEdge{fromAEdge, fromBEdge}, fromAEdge)
}

// newSlipSwitch is a diamond (fromA -> toA, fromB -> toB, laid out as in
// NewDiamondCrossing) with slip routes fromA -> toB and/or fromB -> toA.
func newSlipSwitch(world *World, id string, fromA, toA, fromB, toB *TrackPoint, lanes int, isDoubleSlip bool, orientation SlipOrientation) (*SwitchBlock, error) {

	if orientation != FromAtoB && orientation != FromBtoA {
		return nil, fmt.Errorf("Orientation possible values: 0, 1, received: %d", orientation)
	}
	if orientation != FromAtoB && isDoubleSlip {
		return nil, errors.New("Orientation must be 0 for double slips. ")
	}
	if lanes == 0 {
		return nil, errors.New("the crossing tracks must move at least one lane")
	}

	fromAEdge := world.TrackGraph.AddTrack(fromA, toA, world.NewTrackSegment("", world.data.DefaultSwLength)).SetLanes(lanes)
	fromBEdge := world.TrackGraph.AddTrack(fromB, toB, world.NewTrackSegment("", world.data.DefaultSwLength)).SetLanes(-lanes)
	managedEdges := []*GraphEdge{fromAEdge, fromBEdge}

	if isDoubleSlip || orientation == FromAtoB {
		managedEdges = append(managedEdges, world.TrackGraph.AddTrack(fromA, toB, world.NewSwitchTrack("")))
	}
	if isDoubleSlip || orientation == FromBtoA {
		managedEdges = append(managedEdges, world.TrackGraph.AddTrack(fromB, toA, world.NewSwitchTrack("")))
	}

	return world.newSwitchBlock(id, managedEdges, fromAEdge), nil
}

func NewSingleSlip(world *World, id string, fromA, toA, fromB, toB *TrackPoint, lanes int, orientation SlipOrientation) (*SwitchBlock, error) {
	return newSlipSwitch(world, id, fromA, toA, fromB, toB, lanes, false, orientation)
}

func NewDoubleSlip(world *World, id string, fromA, toA, fromB, toB *TrackPoint, lanes int) (*SwitchBlock, error) {
	return newSlipSwitch(world, id, fromA, toA, fromB, toB, lanes, true, FromAtoB)
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
