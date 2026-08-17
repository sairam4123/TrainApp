package railway

import (
	"cmp"
	"fmt"
	"slices"
)

type Interlocking struct {
	world *World

	trackSwitchMap map[string]string // trackId -> switchBlockId / "" (if no sw is present)
	trackSigMap    map[string]string // trackId -> signalId / "" (if no sig is present)
}

func NewInterlocking(world *World) *Interlocking {
	ilk := Interlocking{
		world:          world,
		trackSwitchMap: map[string]string{},
		trackSigMap:    map[string]string{},
	}

	for _, trk := range world.TrackGraph.tracks {
		ilk.trackSwitchMap[trk.Id] = ""
		ilk.trackSigMap[trk.Id] = ""
	}
	for _, sb := range world.switchBlocks {
		for _, trk := range sb.managedEdges {
			ilk.trackSwitchMap[trk.Track.Id] = sb.Id
		}
	}

	for _, sig := range world.signals {
		atPMap := world.TrackGraph.NeighborMap[sig.AtPoint.Id]
		if atPMap != nil {
			trk := atPMap[sig.FacingPoint.Id]
			ilk.trackSigMap[trk.Id] = sig.Id
		}
	}

	return &ilk
}

func (ilk *Interlocking) NavigatesCorrectly(path *Path) bool {
	curPoint := path.initPoint
	curPtIdx := 0

	facesRight := true

	for {
		if curPtIdx >= len(path.Edges) {
			break
		}
		edge := path.Edges[curPtIdx]
		prevPoint := curPoint
		curPoint = ilk.world.TrackGraph.OtherEnd(edge.Track, curPoint.Id)
		sigId := ilk.trackSigMap[edge.Track.Id]
		if sigId == "" {
			curPtIdx++
			continue
		}
		sig := ilk.world.signals[sigId]
		if sig == nil {
			curPtIdx++
			continue
		}
		if !sig.FacesMovement(prevPoint, curPoint) {
			facesRight = false
		}
		curPtIdx++
	}
	return facesRight
}

func (ilck *Interlocking) TryReservePathTo(train *Train, toTrack *TrackSegment) (*Path, bool) {

	// var path *Path
	// if toTrack != nil {
	// 	path = ilck.world.TrackGraph.FindPathToTrack(train.FacingToward, toTrack)
	// } else if toSignal != nil {
	// 	path = ilck.world.TrackGraph.FindPath(train.FacingToward, toSignal.AtPoint)
	// }

	// if path == nil {
	// 	return nil, false
	// }

	// generate candidate paths
	paths, err := ilck.world.TrackGraph.GenerateCandidatePaths(train.FacingToward, toTrack)
	if err != nil {
		return nil, false
	}
	slices.SortStableFunc(paths, func(a, b *Path) int {

		// check if it's correct way
		aCorrect := ilck.NavigatesCorrectly(a)
		bCorrect := ilck.NavigatesCorrectly(b)

		if aCorrect == bCorrect {
			// check the path distance
			return cmp.Compare(a.Length(), b.Length())
		} else {
			if aCorrect {
				return -1
			}
			return 1
		}
	})
	for _, path := range paths {

		if ilck.TryReservePath(path, train) {
			return path, true
		}
	}

	return nil, false
}

func (ilck *Interlocking) TryReservePath(path *Path, train *Train) bool {

	reservationFailed := false

	for _, edge := range path.Edges {
		// fmt.Printf("Edge - %s - %v - %v\n", edge.Track.Id, edge.Track.ReservedBy == nil, edge.Track.OccupiedBy == nil)
		if edge.Track.ReservedBy == nil && edge.Track.OccupiedBy == nil {
			edge.Track.Reserve(train)
		} else {
			reservationFailed = true
			// TODO: save the edge and use it for resource based queuing
			// fail the reservation
			break
		}
		if edge.Track.IsReserved() && edge.Track.ReservedBy.Number != train.Number {
			reservationFailed = true
			break
		}

		swBlkId := ilck.trackSwitchMap[edge.Track.Id]
		if swBlkId == "" {
			continue
		}

		swBlk := ilck.world.switchBlocks[swBlkId]
		if swBlk.lockedBy != nil {
			fmt.Printf("Failed to set switches %s\n", swBlkId)
			reservationFailed = true
		}

		if err := swBlk.SetActiveEdge(edge); err != nil {
			fmt.Printf("Failed to set active edge on switch %s\n", swBlkId)
		}
		if err := swBlk.LockSwitchFor(train); err != nil {
			fmt.Printf("Failed to lock switch %s\n", swBlkId)
		}
	}

	if reservationFailed {
		for _, edge := range path.Edges {
			if edge.Track.IsReserved() && edge.Track.ReservedBy.Number == train.Number {
				edge.Track.ReservedBy = nil // clear the reservation
			}

			swBlkId := ilck.trackSwitchMap[edge.Track.Id]
			if swBlkId == "" {
				continue
			}

			swBlk := ilck.world.switchBlocks[swBlkId]
			if swBlk.lockedBy != nil {
				fmt.Printf("Failed to set switches %s\n", swBlkId)
				reservationFailed = true
			}

			if err := swBlk.SetActiveEdge(edge); err != nil {
				fmt.Printf("Failed to set active edge on switch %s\n", swBlkId)
			}

			if err := swBlk.UnlockSwitchFor(train); err != nil {
				fmt.Printf("Failed to lock switch %s\n", swBlkId)
			}

		}

		return false
	}
	return true

}

func (ilck *Interlocking) EnsureAllSwitchesLocked(train *Train, path *Path) bool {
	for _, edge := range path.Edges {
		fmt.Println("Switching check", edge.Track.Id, edge.Track.IsOccupied(), edge.Track.IsReserved(), edge.Track.OccupiedBy, edge.Track.ReservedBy, train)

		swId := ilck.trackSwitchMap[edge.Track.Id]
		if swId == "" {
			continue
		}

		swBlk := ilck.world.switchBlocks[swId]

		if !swBlk.isLocked {
			fmt.Println(swId, swBlk.isLocked)
			return false
		}
		if swBlk.lockedBy.Number != train.Number {
			return false
		}
	}
	return true
}

func (ilck *Interlocking) ReleaseSwitch(curTrack *GraphEdge, train *Train) error {
	swId := ilck.trackSwitchMap[curTrack.Track.Id]
	if swId == "" {
		return nil
	}

	swBlk := ilck.world.switchBlocks[swId]
	if err := swBlk.UnlockSwitchFor(train); err != nil {
		return err
	}

	return nil
}
