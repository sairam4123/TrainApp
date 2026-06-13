package railway

type Dispatcher struct {
	sim *Sim

	waitingTrains []*Train
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
		if edge.Track.IsAvailable() {
			edge.Track.Reserve(train)
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

		return nil, false
	}

	return path, true
}

func (disp *Dispatcher) RequestToProceed(train *Train, path *Path) bool {
	ok := path.EnsureAllEdgesAreReserved(train)

	return ok
}

// import "fmt"

// type OccupationData struct {
// 	track   *TrackSegment
// 	ctrller IController
// 	train   *Train
// }

// type IController interface {
// 	FindAvailableTrack() (*TrackSegment, error)
// 	Acquire(train *Train, expectedTrackId string) (*OccupationData, bool)
// 	Release(occup *OccupationData)
// }

// type StationController struct {
// 	station *Station
// 	sim     *Sim

// 	waiting []*Train // Must be FIFO
// }

// type BlockSectionController struct {
// 	bsec *BlockSection
// 	sim  *Sim

// 	waiting []*Train
// }

// func (ctrl *StationController) FindAvailableTrack() (*TrackSegment, error) {
// 	for _, pf := range ctrl.station.Platforms {
// 		// ignore direction for now
// 		// fmt.Printf("Track Available %s -> %t\n", pf.track.Id, pf.track.IsAvailable())
// 		if pf.Track.IsAvailable() {
// 			return pf.Track, nil
// 		}
// 	}
// 	return nil, fmt.Errorf("Cannot find any available tracks")
// }

// func (ctrl *StationController) Acquire(train *Train, expectedTrackId string) (*OccupationData, bool) {
// 	// ignore expectedTrackId for now
// 	track, err := ctrl.FindAvailableTrack()
// 	if err != nil {
// 		// queue the train to the block section
// 		ctrl.waiting = append(ctrl.waiting, train)
// 		return nil, false
// 	}

// 	// create an occupation object
// 	occup := OccupationData{
// 		track:   track,
// 		ctrller: ctrl,
// 		train:   train,
// 	}

// 	track.Request(train)
// 	train.occupation = &occup

// 	return &occup, true
// }

// func (ctrl *StationController) Release(occup *OccupationData) {
// 	if ctrl != occup.ctrller {
// 		return
// 	}

// 	occup.track.Release()
// 	if occup == occup.train.occupation {
// 		occup.train.occupation = nil
// 	}
// 	if len(ctrl.waiting) > 0 {
// 		waitingTrain := ctrl.waiting[0]
// 		ctrl.waiting = ctrl.waiting[1:]

// 		ctrl.sim.des.Add(ctrl.sim.des.CurTime+1, waitingTrain.Name, TrackReleased, waitingTrain)
// 	}
// }

// func (ctrl *BlockSectionController) FindAvailableTrack() (*TrackSegment, error) {
// 	for _, trk := range ctrl.bsec.tracks {
// 		// fmt.Printf("Track Available %s -> %t\n", trk.Id, trk.IsAvailable())
// 		// ignore direction for now
// 		if trk.IsAvailable() {
// 			return trk, nil
// 		}
// 	}
// 	return nil, fmt.Errorf("Cannot find any available tracks")
// }

// func (ctrl *BlockSectionController) Acquire(train *Train, expectedTrackId string) (*OccupationData, bool) {
// 	// ignore expectedTrackId for now
// 	track, err := ctrl.FindAvailableTrack()
// 	if err != nil {
// 		// fmt.Println("Waiting Len", len(ctrl.waiting))
// 		// queue the train to the block section
// 		ctrl.waiting = append(ctrl.waiting, train)
// 		return nil, false
// 	}

// 	// create an occupation object
// 	occup := OccupationData{
// 		track:   track,
// 		ctrller: ctrl,
// 		train:   train,
// 	}

// 	track.Request(train)
// 	train.occupation = &occup

// 	return &occup, true
// }

// func (ctrl *BlockSectionController) Release(occup *OccupationData) {
// 	if ctrl != occup.ctrller {
// 		return
// 	}

// 	// fmt.Println("Releasing block section")

// 	occup.track.Release()
// 	if occup == occup.train.occupation {
// 		occup.train.occupation = nil
// 	}

// 	// fmt.Println("Waiting Len", len(ctrl.waiting))
// 	if len(ctrl.waiting) > 0 {

// 		waitingTrain := ctrl.waiting[0]
// 		ctrl.waiting = ctrl.waiting[1:]

// 		ctrl.sim.des.Add(ctrl.sim.des.CurTime+1, waitingTrain.Name, TrackReleased, waitingTrain)
// 	}
// }
