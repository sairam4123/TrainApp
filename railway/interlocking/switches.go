package interlocking

import (
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

// switchState is the runtime state of one switch block: which of its edges it
// is set for and which train, if any, has it locked in that position.
type switchState struct {
	active   *infra.GraphEdge
	lockedBy ids.TrainID
}

// switchControl sets and locks switch blocks. A track can be managed by several
// blocks (e.g. a crossover diagonal belongs to a diamond and two switches);
// claiming the track sets and locks all of them, or none.
type switchControl struct {
	world  *infra.World
	states map[string]*switchState // switchBlockId -> state
}

func newSwitchControl(world *infra.World) *switchControl {
	sc := &switchControl{world: world, states: map[string]*switchState{}}
	for _, sb := range world.ListSwitchBlocks() {
		sc.states[sb.Id] = &switchState{active: sb.DefaultEdge()}
	}
	return sc
}

func (sc *switchControl) state(sb *infra.SwitchBlock) *switchState {
	st, ok := sc.states[sb.Id]
	if !ok {
		st = &switchState{active: sb.DefaultEdge()}
		sc.states[sb.Id] = st
	}
	return st
}

// claim sets every block on the edge to the edge and locks it for the train.
// It fails without changing anything if any block is already locked, including
// by the same train: a block can only be locked for one edge at a time.
func (sc *switchControl) claim(edge *infra.GraphEdge, train ids.TrainID) error {
	blocks := sc.world.SwitchBlocksOn(edge.Track)
	for _, sb := range blocks {
		if st := sc.state(sb); !st.lockedBy.IsZero() {
			return &ConflictError{Edge: edge, Train: train, HeldBy: st.lockedBy, Switch: sb, Reason: "switch " + sb.Id + " locked"}
		}
	}
	for _, sb := range blocks {
		st := sc.state(sb)
		st.active = edge
		st.lockedBy = train
	}
	return nil
}

// release unlocks the blocks on the edge that the train locked for it. The
// blocks stay set to the edge.
func (sc *switchControl) release(edge *infra.GraphEdge, train ids.TrainID) {
	for _, sb := range sc.world.SwitchBlocksOn(edge.Track) {
		if st := sc.state(sb); st.lockedBy == train && st.active == edge {
			st.lockedBy = ids.TrainID{}
		}
	}
}
