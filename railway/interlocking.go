package railway

import "fmt"

type Interlocking struct {
	world *World

	trackSwitchMap map[string]string // trackId -> switchBlockId / "" (if no sw is present)
}

func NewInterlocking(world *World) *Interlocking {
	ilk := Interlocking{
		world:          world,
		trackSwitchMap: map[string]string{},
	}

	for _, trk := range world.TrackGraph.tracks {
		ilk.trackSwitchMap[trk.Id] = ""
	}
	for _, sb := range world.switchBlocks {
		for _, trk := range sb.managedEdges {
			ilk.trackSwitchMap[trk.Track.Id] = sb.Id
		}
	}

	return &ilk
}

func (ilck *Interlocking) TryReservePathToTrack(train *Train, toTrack *TrackSegment) (*Path, bool) {
	path := ilck.world.TrackGraph.FindPathToTrack(train.FacingToward, toTrack)
	if path == nil {
		return nil, false
	}
	// if len(path.Edges) == 1 && path.Edges[0].Track.Id == to.Id {
	// 	return path, false
	// }

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

		// prevPoint := ilck.trackSwitchMap[edge.Track.Id]
		// if prevPoint.lockedBy == nil || prevPoint.lockedBy.Number != train.Number {
		// 	prevPoint.MoveSwitchState(edge.To.Id)
		// 	prevPoint.LockPoint(train)
		// }
		// point := ilck.switchBlocks[edge.To.Id]
		// if i+1 == len(path.Edges) {
		// 	point.MoveSwitchState(edge.From.Id)
		// 	point.LockPoint(train)
		// 	continue
		// }
		// nextEdge := path.Edges[i+1]
		// if err1, ok := point.MoveSwitchState(nextEdge.From.Id); !ok {
		// 	if err2, ok := point.MoveSwitchState(nextEdge.To.Id); !ok {
		// 		fmt.Printf("Failed to set switches %s - %s\n", err1, err2)
		// 		reservationFailed = true
		// 	}
		// }
		// if ok := point.LockPoint(train); !ok {
		// 	fmt.Printf("Err occurred when trying to lock point - point %s\n", point.point.Id)
		// 	reservationFailed = true
		// }
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

			// prevPoint := ilck.pointControllers[edge.From.Id]
			// if prevPoint.isLocked {
			// 	prevPoint.UnlockPoint(train)
			// }
			// point := ilck.pointControllers[edge.To.Id]
			// if i+1 == len(path.Edges) {
			// 	if ok := point.UnlockPoint(train); !ok {
			// 		fmt.Printf("Err occurred when trying to unlock point - point %s\n", point.point.Id)
			// 	}
			// 	continue
			// }
			// nextEdge := path.Edges[i+1]
			// if err1, ok := point.MoveSwitchState(nextEdge.From.Id); !ok {
			// 	if err2, ok := point.MoveSwitchState(nextEdge.To.Id); !ok {
			// 		fmt.Printf("Failed to set switches %s - %s\n", err1, err2)
			// 	}
			// }
			// if ok := point.UnlockPoint(train); !ok {
			// 	fmt.Printf("Err occurred when trying to unlock point - point %s\n", point.point.Id)
			// }
		}
		// ilck.waitingReservationRequests = append(ilck.waitingReservationRequests, &ReservationRequest{
		// 	uptoTrack: toTrack,
		// 	train:     train,
		// })
		return nil, false
	}

	return path, true
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
