package railway

import (
	"fmt"
	"trainapp/units"
)

type Train struct {
	Name   string
	Number string

	curSchedulePoint int
	schedule         []*SchedulePoint

	FacingToward *TrackPoint
	MaxSpeed     units.MetersPerMin

	occupation *OccupationData

	ma *MovementAuthority

	reservation *ReservationData
}

func (t *Train) GetFullName() string {
	return t.Number + " - " + t.Name
}

func (t *Train) AddSchedule(sp *SchedulePoint) {
	t.schedule = append(t.schedule, sp)
}

type TrainController struct {
	sim     *Sim
	trainId string
}

func (tc *TrainController) OnEvent(event RailwayEvent, data any) {
	train := tc.sim.world.trains[tc.trainId]

	switch RailwayEvent(event) {
	case WorldEntered:
		train.curSchedulePoint = 0
		curSchedule := train.schedule[train.curSchedulePoint]
		nextStn, ok := tc.sim.world.stations[curSchedule.StnCode]
		if !ok {
			fmt.Println("Something went wrong, cannot find station required for schedule")
		}

		pathRes, ok := tc.sim.dispatcher.RequestRouteToPlatform(train, nextStn, curSchedule.SpPfNo)
		if !ok {
			fmt.Println("Path cannot be reserved.. waiting to enter world")
			return
		}

		tc.sim.ScheduleEventNext(RouteGranted, pathRes, train.Number)

	case TrackEntered:
		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur track is nil")
			return
		}
		train.FacingToward = tc.sim.world.TrackGraph.OtherEnd(curTrack, train.FacingToward.Id)
		time := curTrack.TravelTime(train.MaxSpeed)
		tc.sim.ScheduleEventAfter(time, TrackTravelEnd, train, train.Number)

	case TrackTravelEnd:
		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur track is nil")
			return
		}
		nextTrack := train.occupation.NextTrack()
		if nextTrack == nil {
			tc.sim.ScheduleEventNext(PathCompleted, train, train.Number)
			tc.sim.dispatcher.intlck.UnlockSwitchBlocks(curTrack, train)
		} else {
			// acquire next track
			ok := nextTrack.Acquire(train)
			if !ok {
				fmt.Println("Failed to acquire track, bailing...")
				return
			}
			curTrack.Release(train)
			tc.sim.ScheduleEventNext(TrackReleased, curTrack, train.Number)
			tc.sim.dispatcher.OnTrackReleased(curTrack, train)
			train.occupation.curPathIdx++
			tc.sim.ScheduleEventNext(TrackEntered, train, train.Number)
			// s.dispatcher.sim.ScheduleEventNext(TrackExited, train)
		}

	case RouteGranted:
		reserv := data.(*PathResponse)

		train.reservation = &ReservationData{
			train:   train,
			curPath: reserv.path,
		}
		path := reserv.path
		// TEMP: Put facing toward here...
		train.FacingToward = reserv.facingPoint

		// TODO: RouteGrants can also happen from Home Signal Approach
		// RouteGrant, grants the route, it must be checked first before proceeding.
		if ma, ok := tc.sim.dispatcher.RequestToProceed(train, path); ok {
			tc.sim.ScheduleEventNext(MovementAuthorized, ma, train.Number)
		}

	case MovementAuthorized:
		ma := data.(*MovementAuthority)
		train.ma = ma

		// TODO: Rework this slightly well
		if train.occupation == nil {
			if ok := ma.path.Edges[0].Track.Acquire(train); !ok {
				fmt.Println("Edge cannot be acquired")
				return
			}
			// train was waiting to enter the world
			train.occupation = &OccupationData{
				train:      train,
				curPathIdx: 0,
				curPath:    ma.path,
			}
			tc.sim.ScheduleEventNext(TrackEntered, train, train.Number)
			return
		}

		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur Track is nil")
			return
		}
		if tc.sim.world.IsStationPlatform(curTrack) {
			fmt.Println("Incrementing schedule point", train.curSchedulePoint, train.curSchedulePoint+1)
			train.curSchedulePoint++
			train.ma = ma
			tc.sim.ScheduleEventNext(TrainDeparted, train, train.Number)
		} else {
			if ok := ma.path.Edges[0].Track.Acquire(train); !ok {
				fmt.Println("Edge cannot be acquired")
				return
			}
			train.occupation = &OccupationData{
				train:      train,
				curPathIdx: 0,
				curPath:    ma.path,
			}
			tc.sim.ScheduleEventNext(TrackEntered, train, train.Number)
			curTrack.Release(train)
			tc.sim.ScheduleEventNext(TrackReleased, curTrack, train.Number)
			tc.sim.dispatcher.OnTrackReleased(curTrack, train)
		}

	case MovementAuthorityEnded:
		// TODO: check if the current track is the station platform
		// TOOD: if not station platform, then pathfind to the available / preferred station platform
		curSchedule := train.schedule[train.curSchedulePoint]
		curStn, ok := tc.sim.world.GetStation(curSchedule.StnCode)
		if !ok {
			fmt.Println("Cannot find station.. impossible")
			return
		}

		path, ok := tc.sim.dispatcher.RequestRouteToStation(train, curStn, curSchedule.SpPfNo)
		if !ok {
			fmt.Println("Reservation failed, ma ended.. waiting for ma")
			return
		}
		tc.sim.ScheduleEventNext(RouteGranted, path, train.Number)

	case PathCompleted:
		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur Track is nil")
			return
		}

		if tc.sim.world.IsStationPlatform(curTrack) {
			tc.sim.ScheduleEventNext(TrainArrived, train, train.Number)
		} else {
			tc.sim.ScheduleEventNext(MovementAuthorityEnded, train, train.Number)
		}

	case TrainArrived:
		// fmt.Println("Train Arrived")
		curSchedule := train.schedule[train.curSchedulePoint]

		tc.sim.ScheduleEventAfter(curSchedule.ExpDwellTime(tc.sim.CurTime()), TrainDwellEnd, train, train.Number)

	case TrainDwellEnd:
		// fmt.Println("Train Dwell End")

		if len(train.schedule) <= train.curSchedulePoint+1 {
			train.curSchedulePoint++
			tc.sim.ScheduleEventNext(TrainDeparted, train, train.Number)
			return
		}
		// reserve the track to next station
		nextSchedule := train.schedule[train.curSchedulePoint+1]
		nextStn := tc.sim.world.stations[nextSchedule.StnCode]
		fmt.Printf("Trying reserve upto %s (by %s)\n", nextStn.Code, train.GetFullName())
		path, ok := tc.sim.dispatcher.RequestRouteToStation(train, nextStn, nextSchedule.SpPfNo)
		if !ok {
			fmt.Println("Reservation failed.")
			return
		}
		tc.sim.ScheduleEventNext(RouteGranted, path, train.Number)

	case TrainDeparted:

		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
		if train.curSchedulePoint >= len(train.schedule) {
			tc.sim.ScheduleEventNext(ScheduleEnd, train, train.Number)
			return
		}

		path := train.ma.path

		if ok := path.Edges[0].Track.Acquire(train); !ok {
			fmt.Println("Edge cannot be acquired")
			return
		}
		curTrack.Track.Release(train)
		tc.sim.ScheduleEventNext(TrackReleased, curTrack.Track, train.Number)
		tc.sim.dispatcher.OnTrackReleased(curTrack.Track, train)

		train.occupation = &OccupationData{
			train:      train,
			curPathIdx: 0,
			curPath:    path,
		}
		tc.sim.ScheduleEventNext(TrackEntered, train, train.Number)

	case ScheduleEnd:
		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur Track is nil")
			return
		}

		curTrack.Release(train)
		tc.sim.ScheduleEventNext(TrackReleased, curTrack, train.Number)
		tc.sim.dispatcher.OnTrackReleased(curTrack, train)
		tc.sim.ScheduleEventNext(WorldExited, train, train.Number)
		train.reservation = nil
		train.occupation = nil

	}

}

func (t *Train) String() string {
	if t == nil {
		return "<nil>"
	}
	return t.GetFullName()
}
