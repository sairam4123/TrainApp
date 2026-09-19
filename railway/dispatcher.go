package railway

import (
	"fmt"
	"slices"
)

type Dispatcher struct {
	sim *Sim

	waitingReservationRequests []*ReservationRequest

	waitingMaRequests []*MovementAuthorityRequest

	intlck *Interlocking
}

// TODO: Move interlocking into World
func (disp *Dispatcher) Init() {
	disp.intlck = NewInterlocking(disp.sim.world)
}

// TODO: this is terrible for large scale simulations but for the time being it is fine.
func (disp *Dispatcher) OnTrackReleased(track *TrackSegment, train *Train) {

	edge := disp.sim.world.TrackGraph.Edges[track.Id]
	// TODO: for the time being just unlock the switches here
	// prevPoint := disp.pointControllers[edge.From.Id]
	// if prevPoint.lockedBy != nil && prevPoint.lockedBy.Number == train.Number {
	// 	prevPoint.UnlockPoint(train)
	// }
	// point := disp.pointControllers[edge.To.Id]
	// if point.lockedBy != nil && point.lockedBy.Number == train.Number {
	// 	point.UnlockPoint(train)
	// }
	if err := disp.intlck.UnlockSwitchBlocks(edge, train); err != nil {
		fmt.Println(err)
	}

	oldQueue := disp.waitingReservationRequests
	disp.waitingReservationRequests = make([]*ReservationRequest, 0)
	for {
		if len(oldQueue) <= 0 {
			break
		}
		elem := oldQueue[0]
		oldQueue = oldQueue[1:]
		// fmt.Printf("Trying to reserve path to %s for %s\n", elem.uptoTrack.Id, elem.train.GetFullName())
		pathRes, ok := disp.RequestRouteToStation(elem.train, elem.uptoStation, elem.prefPfNo)
		if ok && pathRes.path != nil {
			fmt.Println("Reservation successful", elem.train)
			disp.sim.ScheduleEventNext(RouteGranted, pathRes, elem.train.Number)
		} else {
			trainExists := false
			// check if the request already exists
			for _, req := range disp.waitingReservationRequests {
				if req.train.Number == elem.train.Number {
					trainExists = true
				}
			}

			fmt.Printf("Adding back the reservation request to the queue\n")
			// add it back to the queue
			if !trainExists {
				disp.waitingReservationRequests = append(disp.waitingReservationRequests, elem)
			}
		}
	}

	oldQueue2 := disp.waitingMaRequests
	disp.waitingMaRequests = make([]*MovementAuthorityRequest, 0)
	for {
		if len(oldQueue2) <= 0 {
			break
		}
		elem := oldQueue2[0]
		oldQueue2 = oldQueue2[1:]
		fmt.Printf("Trying to request for proceed for %s\n", elem.train.GetFullName())
		ma, ok := disp.RequestToProceed(elem.train, elem.path)
		if ok {
			fmt.Println("Movement authority granted successful", elem.train)
			disp.sim.ScheduleEventNext(MovementAuthorized, ma, elem.train.Number)
		} else {
			trainExists := false
			// check if the request already exists
			for _, req := range disp.waitingMaRequests {
				if req.train.Number == elem.train.Number {
					trainExists = true
				}
			}

			fmt.Printf("Adding back the movement authority request to the queue\n")
			// add it back to the queue
			if !trainExists {
				disp.waitingMaRequests = append(disp.waitingMaRequests, elem)
			}
		}
	}
}

type ReservationRequest struct {
	uptoTrack *TrackSegment
	train     *Train

	uptoStation *Station
	prefPfNo    string
}

type MovementAuthorityRequest struct {
	path  *Path
	train *Train
}

type OccupationData struct {
	train *Train

	curPathIdx int
	curPath    *Path
}

func (o *OccupationData) CurTrack() *TrackSegment {
	if o.curPath == nil {
		return nil
	}

	path := o.curPath
	if o.curPathIdx < 0 || o.curPathIdx >= len(path.Edges) {
		return nil
	}

	return path.Edges[o.curPathIdx].Track
}

type ReservationData struct {
	train *Train

	curPath *Path
}

type PathResponse struct {
	path        *Path
	nextPf      *TrackSegment // intent of the path finder (exp target)
	facingPoint *TrackPoint
}

// RequestRouteToPlatform is to be used only during World Entry or when MovementAuthority ended and train is not in station platform.
// See also [[RequestRouteToStation]]
func (disp *Dispatcher) RequestRouteToPlatform(train *Train, toStn *Station, prefPfNo string) (*PathResponse, bool) {
	platform := toStn.FindAvailableStnPlatform(prefPfNo)
	if platform == nil {
		disp.waitingReservationRequests = append(disp.waitingReservationRequests,
			&ReservationRequest{
				train:       train,
				uptoStation: toStn,
				prefPfNo:    prefPfNo,
			})
		return nil, false
	}
	facingPoint := disp.sim.world.TrackGraph.FindWorldBoundaryPoint(platform)
	if facingPoint == nil {
		return nil, false
	}
	path, ok := disp.intlck.TryReservePathTo(train, platform, facingPoint)
	if ok {
		return &PathResponse{
			path:        path,
			nextPf:      platform,
			facingPoint: facingPoint,
		}, true
	}
	disp.waitingReservationRequests = append(disp.waitingReservationRequests,
		&ReservationRequest{
			train:       train,
			uptoStation: toStn,
			prefPfNo:    prefPfNo,
		})

	return nil, false
}

// RequestRouteToStation is to be used for path finding to next station. Train must be in a station platform for it to work properly.
// See also [[RequestRouteToPlatform]]
func (disp *Dispatcher) RequestRouteToStation(train *Train, toStn *Station, prefPfNo string) (*PathResponse, bool) {
	platform := toStn.FindAvailableStnPlatform(prefPfNo)
	if platform == nil {
		disp.waitingReservationRequests = append(disp.waitingReservationRequests,
			&ReservationRequest{
				train:       train,
				uptoStation: toStn,
				prefPfNo:    prefPfNo,
			})
		return nil, false
	}
	path, ok := disp.intlck.TryReservePathTo(train, platform, train.FacingToward)
	if ok {
		return &PathResponse{
			path:        path,
			nextPf:      platform,
			facingPoint: train.FacingToward,
		}, true
	}

	// find the preferred platform
	platform = toStn.FindStnPlatform(prefPfNo)
	if platform == nil { // platform doesn't even exist
		return nil, false
	}

	// find the best path to station incase we can't find
	bestPath, ok := disp.intlck.BestPathToTrack(train.FacingToward, platform)
	if !ok {
		return nil, false
	}

	slices.Reverse(bestPath.Edges)
	var reservedPath *Path
	for _, edge := range bestPath.Edges {
		sigId, ok := disp.intlck.trackSigMap[edge.Track.Id]
		if !ok {
			continue
		}
		sig, ok := disp.intlck.world.GetSignal(sigId)
		if !ok {
			continue
		}
		if sig.FacesMovement(edge.From, edge.To) {
			reservedPath, ok = disp.intlck.TryReservePathTo(train, edge.Track, train.FacingToward)
			if !ok {
				continue
			} else {
				reservedPath.PPrint()
				break
			}
		}
	}

	if reservedPath == nil {
		disp.waitingReservationRequests = append(disp.waitingReservationRequests,
			&ReservationRequest{
				train:       train,
				uptoStation: toStn,
				prefPfNo:    prefPfNo,
			})
	}

	return &PathResponse{
		path:        reservedPath,
		nextPf:      platform,
		facingPoint: train.FacingToward,
	}, reservedPath != nil

	// TODO: try reserving upto a last signal if station platform reservation fails
	// TODO: (only if another pathway exists for trains leaving the platform, or another platform exists)
	// TODO: if no pathway for exit exists, don't reserve and keep the train waiting..

}

// @Deprecated TryReservePathToTrack is deprecated, see [[RequestRouteToStation]]
func (disp *Dispatcher) TryReservePathToTrack(train *Train, toTrack *TrackSegment) (*Path, bool) {
	path, ok := disp.intlck.TryReservePathTo(train, toTrack, train.FacingToward)
	if !ok {
		disp.waitingReservationRequests = append(disp.waitingReservationRequests, &ReservationRequest{
			uptoTrack: toTrack,
			train:     train,
		})
		return nil, false
	}
	// disp.sim.ScheduleEventNext(RouteGranted, &ReservationData{
	// 	curPath: path,
	// 	train:   train,
	// }, train.Number)
	return path, true
}

// func (disp *Dispatcher) TryReservePathToTrack(train *Train, toTrack *TrackSegment) (*Path, bool) {
// 	path := disp.sim.world.TrackGraph.FindPathToTrack(train.FacingToward, toTrack)
// 	if path == nil {
// 		return nil, false
// 	}
// 	// if len(path.Edges) == 1 && path.Edges[0].Track.Id == to.Id {
// 	// 	return path, false
// 	// }

// 	reservationFailed := false

// 	for i, edge := range path.Edges {
// 		// fmt.Printf("Edge - %s - %v - %v\n", edge.Track.Id, edge.Track.ReservedBy == nil, edge.Track.OccupiedBy == nil)
// 		if edge.Track.ReservedBy == nil && edge.Track.OccupiedBy == nil {
// 			edge.Track.Reserve(train)
// 		} else {
// 			reservationFailed = true
// 			// TODO: save the edge and use it for resource based queuing
// 			// fail the reservation
// 			break
// 		}
// 		if edge.Track.IsReserved() && edge.Track.ReservedBy.Number != train.Number {
// 			reservationFailed = true
// 			break
// 		}
// 		prevPoint := disp.pointControllers[edge.From.Id]
// 		if prevPoint.lockedBy == nil || prevPoint.lockedBy.Number != train.Number {
// 			prevPoint.MoveSwitchState(edge.To.Id)
// 			prevPoint.LockPoint(train)
// 		}
// 		point := disp.pointControllers[edge.To.Id]
// 		if i+1 == len(path.Edges) {
// 			point.MoveSwitchState(edge.From.Id)
// 			point.LockPoint(train)
// 			continue
// 		}
// 		nextEdge := path.Edges[i+1]
// 		if err1, ok := point.MoveSwitchState(nextEdge.From.Id); !ok {
// 			if err2, ok := point.MoveSwitchState(nextEdge.To.Id); !ok {
// 				fmt.Printf("Failed to set switches %s - %s\n", err1, err2)
// 				reservationFailed = true
// 			}
// 		}
// 		if ok := point.LockPoint(train); !ok {
// 			fmt.Printf("Err occurred when trying to lock point - point %s\n", point.point.Id)
// 			reservationFailed = true
// 		}
// 	}

// 	if reservationFailed {
// 		for i, edge := range path.Edges {
// 			if edge.Track.IsReserved() && edge.Track.ReservedBy.Number == train.Number {
// 				edge.Track.ReservedBy = nil // clear the reservation
// 			}
// 			prevPoint := disp.pointControllers[edge.From.Id]
// 			if prevPoint.isLocked {
// 				prevPoint.UnlockPoint(train)
// 			}
// 			point := disp.pointControllers[edge.To.Id]
// 			if i+1 == len(path.Edges) {
// 				if ok := point.UnlockPoint(train); !ok {
// 					fmt.Printf("Err occurred when trying to unlock point - point %s\n", point.point.Id)
// 				}
// 				continue
// 			}
// 			nextEdge := path.Edges[i+1]
// 			if err1, ok := point.MoveSwitchState(nextEdge.From.Id); !ok {
// 				if err2, ok := point.MoveSwitchState(nextEdge.To.Id); !ok {
// 					fmt.Printf("Failed to set switches %s - %s\n", err1, err2)
// 				}
// 			}
// 			if ok := point.UnlockPoint(train); !ok {
// 				fmt.Printf("Err occurred when trying to unlock point - point %s\n", point.point.Id)
// 			}
// 		}
// 		disp.waitingReservationRequests = append(disp.waitingReservationRequests, &ReservationRequest{
// 			uptoTrack: toTrack,
// 			train:     train,
// 		})
// 		return nil, false
// 	}

// 	return path, true
// }

func (disp *Dispatcher) RequestToProceed(train *Train, path *Path) (*MovementAuthority, bool) {
	ok := path.EnsureAllEdgesAreReserved(train)
	if !ok {
		fmt.Println("Request to Proceed failed. Reason: All tracks are not reserved")
		disp.waitingMaRequests = append(disp.waitingMaRequests, &MovementAuthorityRequest{
			path:  path,
			train: train,
		})
		return nil, ok
	}
	ok = disp.intlck.EnsureAllSwitchesLocked(train, path)
	if !ok {
		fmt.Println("Request to Proceed failed. Reason: All switches are not locked.")
		disp.waitingMaRequests = append(disp.waitingMaRequests, &MovementAuthorityRequest{
			path:  path,
			train: train,
		})
		return nil, ok
	}

	// TODO - Depending on the availability, ma can only include path upto a certain track only (upto the last available signal) -- Sairam, 21-08-2026
	ma := &MovementAuthority{
		path:  path,
		train: train,
	}
	return ma, ok
}
