package ops

import "trainapp/railway/infra"

type SwitchLock struct {
	train       *Train
	holdingEdge *infra.GraphEdge
}

type ReservationBook struct {
	reservations map[string]string // trackId -> trainId / "" (if no train has reserved)

}

type SwitchLockMap struct {
	switchLockMap map[string]*SwitchLock // switchId -> trainId / "" (if no train has locked)
}

func NewReservationBook(world *infra.World) *ReservationBook {
	rb := &ReservationBook{
		reservations: map[string]string{},
	}
	for _, trck := range world.TrackGraph.ListTracks() {
		rb.reservations[trck.Id] = ""
	}

	return rb
}

func NewSwitchLockMap(world *infra.World) *SwitchLockMap {
	sw := &SwitchLockMap{
		switchLockMap: map[string]*SwitchLock{},
	}

	for _, swBlck := range world.ListSwitchBlocks() {
		sw.switchLockMap[swBlck.Id] = &SwitchLock{
			holdingEdge: swBlck.ListManagedEdges()[0],
			train:       nil,
		}
	}
	return sw
}

func (sw *SwitchLockMap) IsLocked(swBlck *infra.SwitchBlock) bool {
	return sw.switchLockMap[swBlck.Id].train != nil
}

func (sw *SwitchLockMap) IsLockedByTrain(swBlck *infra.SwitchBlock, train *Train) bool {
	return sw.switchLockMap[swBlck.Id].train != nil && sw.switchLockMap[swBlck.Id].train.Number == train.Number
}

func (sw *SwitchLockMap) LockSwitch(swBlck *infra.SwitchBlock, train *Train) bool {
	if sw.IsLocked(swBlck) && !sw.IsLockedByTrain(swBlck, train) {
		return false
	}

	sw.switchLockMap[swBlck.Id].train = train
	return true
}

func (sw *SwitchLockMap) SetActiveEdge(swBlck *infra.SwitchBlock, edge *infra.GraphEdge) bool {
	if sw.IsLocked(swBlck) {
		return false
	}

	sw.switchLockMap[swBlck.Id] = &SwitchLock{
		holdingEdge: edge,
		train:       nil, // set later once lock confirmed
	}
	return true
}

func (sw *SwitchLockMap) UnlockSwitch(swBlck *infra.SwitchBlock, train *Train) bool {
	if !sw.IsLocked(swBlck) || !sw.IsLockedByTrain(swBlck, train) {
		return false
	}

	sw.switchLockMap[swBlck.Id].train = nil
	return true
}

func (rb *ReservationBook) ReserveTrack(track *infra.TrackSegment, train *Train) bool {
	if rb.reservations[track.Id] != "" {
		return false
	}
	rb.reservations[track.Id] = train.Number
	return true
}

func (rb *ReservationBook) IsTrackReserved(track *infra.TrackSegment) bool {
	return rb.reservations[track.Id] != ""
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

func NewOccupationMap(world *infra.World) *OccupationMap {
	occ := &OccupationMap{
		occupiedTrackMap: map[string]string{},
	}

	for _, trk := range world.TrackGraph.ListTracks() {
		occ.occupiedTrackMap[trk.Id] = ""
	}
	return occ
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

func (om *OccupationMap) IsTrackOccupiedByTrain(track *infra.TrackSegment, train *Train) bool {
	return om.occupiedTrackMap[track.Id] != "" && om.occupiedTrackMap[track.Id] != train.Number
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
