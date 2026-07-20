package railway

import "fmt"

type Dispatcher struct {
	sim *Sim

	waitingReservationRequests []*ReservationRequest

	waitingProceedRequests []*OccupationRequest

	pointControllers map[string]*PointController
}

func (disp *Dispatcher) ReleasePoints(curTrack *GraphEdge, train *Train) {
	prevPoint := disp.pointControllers[curTrack.From.Id]
	if prevPoint.lockedBy != nil && prevPoint.lockedBy.Number == train.Number {
		prevPoint.UnlockPoint(train)
	}
	point := disp.pointControllers[curTrack.To.Id]
	if point.lockedBy != nil && point.lockedBy.Number == train.Number {
		point.UnlockPoint(train)
	}
}

func (disp *Dispatcher) Init() {
	disp.pointControllers = make(map[string]*PointController)
	for _, p := range disp.sim.world.TrackGraph.points {
		disp.pointControllers[p.Id] = &PointController{
			sim:         disp.sim,
			point:       p,
			activeRoute: nil,
			isLocked:    false,
		}
	}
}

// TODO: this is terrible for large scale simulations but for the time being it is fine.
func (disp *Dispatcher) OnTrackReleased(track *TrackSegment, train *Train) {

	edge := disp.sim.world.TrackGraph.Edges[track.Id]
	// TODO: for the time being just unlock the switches here
	prevPoint := disp.pointControllers[edge.From.Id]
	if prevPoint.lockedBy != nil && prevPoint.lockedBy.Number == train.Number {
		prevPoint.UnlockPoint(train)
	}
	point := disp.pointControllers[edge.To.Id]
	if point.lockedBy != nil && point.lockedBy.Number == train.Number {
		point.UnlockPoint(train)
	}

	oldQueue := disp.waitingReservationRequests
	disp.waitingReservationRequests = make([]*ReservationRequest, 0)
	for {
		if len(oldQueue) <= 0 {
			break
		}
		elem := oldQueue[0]
		oldQueue = oldQueue[1:]
		fmt.Printf("Trying to reserve path to %s for %s\n", elem.edge.Id, elem.train.GetFullName())
		path, ok := disp.TryReservePathToTrack(elem.train, elem.edge)
		if ok {
			fmt.Println("Reservation successful", elem.train)
			disp.sim.ScheduleEventNext(RouteGranted, &ReservationData{
				curPath: path,
				train:   elem.train,
				disp:    disp,
			})
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
}

type ReservationRequest struct {
	edge  *TrackSegment
	train *Train
}

type OccupationRequest struct {
	path  *Path
	train *Train
}

type OccupationData struct {
	train *Train

	curPathIdx int
	curPath    *Path

	disp *Dispatcher
}

type ReservationData struct {
	train *Train

	curPath *Path
	disp    *Dispatcher
}

func (disp *Dispatcher) TryReservePathToTrack(train *Train, toTrack *TrackSegment) (*Path, bool) {
	path := disp.sim.world.TrackGraph.FindPathToTrack(train.FacingToward, toTrack)
	if path == nil {
		return nil, false
	}
	// if len(path.Edges) == 1 && path.Edges[0].Track.Id == to.Id {
	// 	return path, false
	// }

	reservationFailed := false

	for i, edge := range path.Edges {
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
		prevPoint := disp.pointControllers[edge.From.Id]
		if prevPoint.lockedBy == nil || prevPoint.lockedBy.Number != train.Number {
			prevPoint.MoveSwitchState(edge.To.Id)
			prevPoint.LockPoint(train)
		}
		point := disp.pointControllers[edge.To.Id]
		if i+1 == len(path.Edges) {
			point.MoveSwitchState(edge.From.Id)
			point.LockPoint(train)
			continue
		}
		nextEdge := path.Edges[i+1]
		if err1, ok := point.MoveSwitchState(nextEdge.From.Id); !ok {
			if err2, ok := point.MoveSwitchState(nextEdge.To.Id); !ok {
				fmt.Printf("Failed to set switches %s - %s\n", err1, err2)
				reservationFailed = true
			}
		}
		if ok := point.LockPoint(train); !ok {
			fmt.Printf("Err occurred when trying to lock point - point %s\n", point.point.Id)
			reservationFailed = true
		}
	}

	if reservationFailed {
		for i, edge := range path.Edges {
			if edge.Track.IsReserved() && edge.Track.ReservedBy.Number == train.Number {
				edge.Track.ReservedBy = nil // clear the reservation
			}
			prevPoint := disp.pointControllers[edge.From.Id]
			if prevPoint.isLocked {
				prevPoint.UnlockPoint(train)
			}
			point := disp.pointControllers[edge.To.Id]
			if i+1 == len(path.Edges) {
				if ok := point.UnlockPoint(train); !ok {
					fmt.Printf("Err occurred when trying to unlock point - point %s\n", point.point.Id)
				}
				continue
			}
			nextEdge := path.Edges[i+1]
			if err1, ok := point.MoveSwitchState(nextEdge.From.Id); !ok {
				if err2, ok := point.MoveSwitchState(nextEdge.To.Id); !ok {
					fmt.Printf("Failed to set switches %s - %s\n", err1, err2)
				}
			}
			if ok := point.UnlockPoint(train); !ok {
				fmt.Printf("Err occurred when trying to unlock point - point %s\n", point.point.Id)
			}
		}
		disp.waitingReservationRequests = append(disp.waitingReservationRequests, &ReservationRequest{
			edge:  toTrack,
			train: train,
		})
		return nil, false
	}

	return path, true
}

func (disp *Dispatcher) RequestToProceed(train *Train, path *Path) bool {
	ok := path.EnsureAllEdgesAreReserved(train)
	if !ok {
		fmt.Println("Request to Proceed failed. Reason: All Edges are not reserved")
		disp.waitingProceedRequests = append(disp.waitingProceedRequests, &OccupationRequest{
			path:  path,
			train: train,
		})
		return ok
	}
	ok = disp.EnsureAllSwitchesSet(train, path)
	if !ok {
		fmt.Println("Request to Proceed failed. Reason: All switches are not locked.")
		disp.waitingProceedRequests = append(disp.waitingProceedRequests, &OccupationRequest{
			path:  path,
			train: train,
		})
	}

	return ok
}

func (disp *Dispatcher) EnsureAllSwitchesSet(train *Train, path *Path) bool {
	for _, edge := range path.Edges {
		fmt.Println("Switching check", edge.Track.Id, edge.Track.IsOccupied(), edge.Track.IsReserved(), edge.Track.OccupiedBy, edge.Track.ReservedBy, train)
		if !disp.pointControllers[edge.From.Id].isLocked || !disp.pointControllers[edge.To.Id].isLocked {
			fmt.Println(disp.pointControllers[edge.From.Id].isLocked, disp.pointControllers[edge.To.Id].isLocked)
			return false
		}
		if disp.pointControllers[edge.From.Id].lockedBy.Number != train.Number || disp.pointControllers[edge.To.Id].lockedBy.Number != train.Number {
			return false
		}
	}
	return true
}
