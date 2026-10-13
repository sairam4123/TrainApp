package sim

import (
	"fmt"
	"trainapp/railway/ops"
)

type TrainController struct {
	sim     *Sim
	trainId string
}

func (tc *TrainController) OnEvent(event ops.RailwayEvent, data any) {
	train := tc.sim.world.trains[tc.trainId]

	switch ops.RailwayEvent(event) {
	case ops.WorldEntered:
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

		tc.sim.ScheduleEventNext(ops.RouteGranted, pathRes, train.Number)

	case ops.TrackEntered:
		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur track is nil")
			return
		}
		train.FacingToward = tc.sim.world.TrackGraph.OtherEnd(curTrack, train.FacingToward.Id)
		time := curTrack.TravelTime(train.MaxSpeed)
		tc.sim.ScheduleEventAfter(time, ops.TrackTravelEnd, train, train.Number)

	case ops.TrackTravelEnd:
		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur track is nil")
			return
		}
		nextTrack := train.occupation.NextTrack()
		if nextTrack == nil {
			tc.sim.ScheduleEventNext(ops.PathCompleted, train, train.Number)
			tc.sim.dispatcher.intlck.UnlockSwitchBlocks(curTrack, train)
		} else {
			// acquire next track
			ok := nextTrack.Acquire(train)
			if !ok {
				fmt.Println("Failed to acquire track, bailing...")
				return
			}
			curTrack.Release(train)
			tc.sim.ScheduleEventNext(ops.TrackReleased, curTrack, train.Number)
			tc.sim.dispatcher.OnTrackReleased(curTrack, train)
			train.occupation.curPathIdx++
			tc.sim.ScheduleEventNext(ops.TrackEntered, train, train.Number)
			// s.dispatcher.sim.ScheduleEventNext(TrackExited, train)
		}

	case ops.RouteGranted:
		reserv := data.(*PathResponse)

		train.reservation = &ops.ReservationData{
			train:   train,
			curPath: reserv.path,
		}
		path := reserv.path
		// TEMP: Put facing toward here...
		train.FacingToward = reserv.facingPoint

		// TODO: RouteGrants can also happen from Home Signal Approach
		// RouteGrant, grants the route, it must be checked first before proceeding.
		if ma, ok := tc.sim.dispatcher.RequestToProceed(train, path); ok {
			tc.sim.ScheduleEventNext(ops.MovementAuthorized, ma, train.Number)
		}

	case ops.MovementAuthorized:
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
			tc.sim.ScheduleEventNext(ops.TrackEntered, train, train.Number)
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
			tc.sim.ScheduleEventNext(ops.TrainDeparted, train, train.Number)
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
			tc.sim.ScheduleEventNext(ops.TrackEntered, train, train.Number)
			curTrack.Release(train)
			tc.sim.ScheduleEventNext(ops.TrackReleased, curTrack, train.Number)
			tc.sim.dispatcher.OnTrackReleased(curTrack, train)
		}

	case ops.MovementAuthorityEnded:
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
		tc.sim.ScheduleEventNext(ops.RouteGranted, path, train.Number)

	case ops.PathCompleted:
		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur Track is nil")
			return
		}

		if tc.sim.world.IsStationPlatform(curTrack) {
			tc.sim.ScheduleEventNext(ops.TrainArrived, train, train.Number)
		} else {
			tc.sim.ScheduleEventNext(ops.MovementAuthorityEnded, train, train.Number)
		}

	case ops.TrainArrived:
		// fmt.Println("Train Arrived")
		curSchedule := train.schedule[train.curSchedulePoint]

		tc.sim.ScheduleEventAfter(curSchedule.ExpDwellTime(tc.sim.CurTime()), ops.TrainDwellEnd, train, train.Number)

	case ops.TrainDwellEnd:
		// fmt.Println("Train Dwell End")

		if len(train.schedule) <= train.curSchedulePoint+1 {
			train.curSchedulePoint++
			tc.sim.ScheduleEventNext(ops.TrainDeparted, train, train.Number)
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
		tc.sim.ScheduleEventNext(ops.RouteGranted, path, train.Number)

	case ops.TrainDeparted:

		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
		if train.curSchedulePoint >= len(train.schedule) {
			tc.sim.ScheduleEventNext(ops.ScheduleEnd, train, train.Number)
			return
		}

		path := train.ma.path

		if ok := path.Edges[0].Track.Acquire(train); !ok {
			fmt.Println("Edge cannot be acquired")
			return
		}
		curTrack.Track.Release(train)
		tc.sim.ScheduleEventNext(ops.TrackReleased, curTrack.Track, train.Number)
		tc.sim.dispatcher.OnTrackReleased(curTrack.Track, train)

		train.occupation = &OccupationData{
			train:      train,
			curPathIdx: 0,
			curPath:    path,
		}
		tc.sim.ScheduleEventNext(ops.TrackEntered, train, train.Number)

	case ops.ScheduleEnd:
		curTrack := train.occupation.CurTrack()
		if curTrack == nil {
			fmt.Println("Cur Track is nil")
			return
		}

		curTrack.Release(train)
		tc.sim.ScheduleEventNext(ops.TrackReleased, curTrack, train.Number)
		tc.sim.dispatcher.OnTrackReleased(curTrack, train)
		tc.sim.ScheduleEventNext(ops.WorldExited, train, train.Number)
		train.reservation = nil
		train.occupation = nil

	}

}
