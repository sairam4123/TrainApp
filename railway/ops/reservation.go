package ops

import "trainapp/railway/infra"

type ReservationBook struct {
	reservations map[string]string // trackId -> trainId / "" (if no train has reserved)

	switchLockMap map[string]string // switchId -> trainId / "" (if no train has locked)

}

func NewReservationBook() *ReservationBook {
	return &ReservationBook{
		reservations:  map[string]string{},
		switchLockMap: map[string]string{},
	}
}

func (rb *ReservationBook) ReserveTrack(track *infra.TrackSegment, train *Train) bool {
	if rb.reservations[track.Id] != "" {
		return false
	}
	rb.reservations[track.Id] = train.Number
	return true
}

func (rb *ReservationBook) ReleaseTrack(track *infra.TrackSegment, train *Train) bool {
	if rb.reservations[track.Id] != train.Number {
		return false
	}
	rb.reservations[track.Id] = ""
	return true
}

type OccupationMap struct {
	occupiedTrackMap map[string]string // trackId -> trainId / "" (if no train has occupied)
}

func NewOccupationMap() *OccupationMap {
	return &OccupationMap{
		occupiedTrackMap: map[string]string{},
	}
}

func (om *OccupationMap) IsTrackOccupied(track *infra.TrackSegment) bool {
	return om.occupiedTrackMap[track.Id] != ""
}

func (rb *ReservationBook) IsTrackReservedByTrain(track *infra.TrackSegment, train *Train) bool {
	return rb.reservations[track.Id] != "" && rb.reservations[track.Id] != train.Number
}

func (om *OccupationMap) AcquireTrack(track *infra.TrackSegment, train *Train) bool {
	if om.occupiedTrackMap[track.Id] != "" {
		return false
	}

	om.occupiedTrackMap[track.Id] = train.Number
	return true
}

func (om *OccupationMap) ReleaseTrack(track *infra.TrackSegment, train *Train) bool {
	if om.occupiedTrackMap[track.Id] != train.Number {
		return false
	}

	om.occupiedTrackMap[track.Id] = ""
	return true
}

type OccupationData struct {
	train *Train

	curPathIdx int
	curPath    *infra.Path
}

func (o *OccupationData) CurTrack() *infra.TrackSegment {
	if o.curPath == nil {
		return nil
	}

	path := o.curPath
	if o.curPathIdx < 0 || o.curPathIdx >= len(path.Edges) {
		return nil
	}

	return path.Edges[o.curPathIdx].Track
}

func (o *OccupationData) NextTrack() *infra.TrackSegment {
	if o.curPath == nil {
		return nil
	}

	path := o.curPath
	if o.curPathIdx+1 < 0 || o.curPathIdx+1 >= len(path.Edges) {
		return nil
	}

	return path.Edges[o.curPathIdx+1].Track
}

type ReservationData struct {
	train *Train

	curPath *infra.Path
}
