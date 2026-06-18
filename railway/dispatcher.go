package railway

type Dispatcher struct {
	sim *Sim

	waitingReservationRequests []*ReservationRequest

	waitingProceedRequests []*OccupationRequest

	pointControllers map[string]*PointController
}

func (disp *Dispatcher) Init() {
	for _, p := range disp.sim.world.TrackGraph.points {
		disp.pointControllers[p.Id] = &PointController{
			sim: disp.sim,
			point: p,
			activeRoute: nil,
			isLocked: false,
		}
	}
}

// TODO: this is terrible for large scale simulations but for the time being it is fine.
func (disp *Dispatcher) OnTrackReleased(track *TrackSegment) {
	oldQueue := disp.waitingReservationRequests
	disp.waitingReservationRequests = make([]*ReservationRequest, 0)
	for {
		if len(oldQueue) <= 0 {
			break
		}
		elem := oldQueue[0]
		oldQueue = oldQueue[1:]

		path, ok := disp.TryReservePathToEdge(elem.train, elem.edge)
		if ok {
			disp.sim.ScheduleEventNext(RouteGranted, &ReservationData{
				curPath: path,
				train:   elem.train,
				disp:    disp,
			})
		} else {

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

func (disp *Dispatcher) TryReservePathToEdge(train *Train, to *TrackSegment) (*Path, bool) {
	path := disp.sim.world.TrackGraph.FindPathToTrack(train.FacingToward, to)
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
			// fail the reservation
			break
		}
		if edge.Track.IsReserved() && edge.Track.ReservedBy.Number != train.Number {
			reservationFailed = true
			break
		}
	}

	if reservationFailed {
		for _, edge := range path.Edges {
			if edge.Track.IsReserved() && edge.Track.ReservedBy.Number == train.Number {
				edge.Track.ReservedBy = nil // clear the reservation
			}
		}
		disp.waitingReservationRequests = append(disp.waitingReservationRequests, &ReservationRequest{
			edge:  to,
			train: train,
		})
		return nil, false
	}

	return path, true
}

func (disp *Dispatcher) RequestToProceed(train *Train, path *Path) bool {
	ok := path.EnsureAllEdgesAreReserved(train)
	if !ok {
		disp.waitingProceedRequests = append(disp.waitingProceedRequests, &OccupationRequest{
			path:  path,
			train: train,
		})
		return ok
	}
	ok = path.EnsureAllSwitchesSet(train)
	if !ok {
		disp.waitingProceedRequests = append(disp.waitingProceedRequests, &OccupationRequest{
			path:  path,
			train: train,
		})
	}

	return ok
}
