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

type SchedulePoint struct {
	TrainNumber string
	StnCode     string
	ArrTime     float64
	DeptTime    float64
	SpPfNo      string
}

func (t *Train) AddSchedule(sp *SchedulePoint) {
	t.schedule = append(t.schedule, sp)
}

func (s *SchedulePoint) ExpDwellTime(curTime float64) units.Minutes {
	if curTime < s.ArrTime {
		return units.Min(s.DeptTime - curTime)
	} else if curTime > s.DeptTime {
		return units.Min(1) // one minute stop cuz we're delayed af
	}
	return units.Min(s.DeptTime - s.ArrTime)
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
		} else {
			tc.sim.ScheduleEventNext(RouteGranted, pathRes, train.Number)
			return
		}

		// platform := nextStn.FindAvailableStnPlatform(curSchedule.SpPfNo)
		// facingPoint := tc.sim.world.TrackGraph.FindWorldBoundaryPoint(platform)
		// train.FacingToward = facingPoint
		// // try to reserve the track to first station
		// path, ok := tc.sim.dispatcher.TryReservePathToTrack(train, platform)
		// if !ok && path == nil {
		// 	fmt.Println("Path cannot be reserved.. waiting to enter world")
		// 	return
		// }
		// if pathRes.facingPoint != nil {
		// 	train.FacingToward = pathRes.facingPoint
		// }
		// train.reservation = &ReservationData{
		// 	train:   train,
		// 	curPath: path,
		// }
		// if ma, ok := tc.sim.dispatcher.RequestToProceed(train, path); ok {
		// fmt.Println("World entering REQUEST PROCEED (WORLD_ENTERED)")
		// tc.sim.ScheduleEventNext(MovementAuthorized, ma, train.Number)
		// if ok := ma.path.Edges[0].Track.Acquire(train); !ok {
		// 	fmt.Println("Edge cannot be acquired")
		// 	return
		// }
		// train.ma = ma

		// train.occupation = &OccupationData{
		// 	train:      train,
		// 	curPathIdx: 0,
		// 	curPath:    ma.path,
		// }
		// tc.sim.ScheduleEventNext(TrackEntered, train)
		// }

	case TrackEntered:
		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
		// fmt.Println("Track Entered", curTrack.Id)
		train.FacingToward = tc.sim.world.TrackGraph.OtherEnd(curTrack, train.FacingToward.Id)
		time := curTrack.TravelTime(train.MaxSpeed)
		tc.sim.ScheduleEventAfter(time, TrackTravelEnd, train, train.Number)
		// train := ev.Data.()

	case TrackTravelEnd:
		if len(train.occupation.curPath.Edges) <= train.occupation.curPathIdx+1 {
			tc.sim.ScheduleEventNext(PathCompleted, train, train.Number)
			curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
			tc.sim.dispatcher.intlck.UnlockSwitchBlocks(curTrack, train)
		} else {
			// acquire next track
			nextTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx+1]
			ok := nextTrack.Track.Acquire(train)
			if ok {
				curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
				curTrack.Track.Release(train)
				tc.sim.ScheduleEventNext(TrackReleased, curTrack.Track, train.Number)
				tc.sim.dispatcher.OnTrackReleased(curTrack.Track, train)
				train.occupation.curPathIdx++
				tc.sim.ScheduleEventNext(TrackEntered, train, train.Number)
			}
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

		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
		if !tc.sim.world.IsStationPlatform(curTrack.Track) {
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
			curTrack.Track.Release(train)
			tc.sim.ScheduleEventNext(TrackReleased, curTrack.Track, train.Number)
			tc.sim.dispatcher.OnTrackReleased(curTrack.Track, train)

			return
		}

		fmt.Println("Incrementing schedule point", train.curSchedulePoint, train.curSchedulePoint+1)
		train.curSchedulePoint++
		train.ma = ma
		tc.sim.ScheduleEventNext(TrainDeparted, train, train.Number)

	case MovementAuthorityEnded:
		// TODO: check if the current track is the station platform
		// TOOD: if not station platform, then pathfind to the available / preferred station platform
		curSchedule := train.schedule[train.curSchedulePoint]
		curStn, ok := tc.sim.world.GetStation(curSchedule.StnCode)
		if !ok {
			panic("Cannot find station.. impossible")
		}

		path, ok := tc.sim.dispatcher.RequestRouteToStation(train, curStn, curSchedule.SpPfNo)
		if ok {
			tc.sim.ScheduleEventNext(RouteGranted, path, train.Number)
		}

	case PathCompleted:
		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
		// fmt.Println("Path completed")
		if tc.sim.world.IsStationPlatform(curTrack.Track) {
			tc.sim.ScheduleEventNext(TrainArrived, train, train.Number)
		} else {
			tc.sim.ScheduleEventNext(MovementAuthorityEnded, train, train.Number)
		}

	case TrainArrived:
		// fmt.Println("Train Arrived")
		// curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
		curSchedule := train.schedule[train.curSchedulePoint]

		tc.sim.ScheduleEventAfter(curSchedule.ExpDwellTime(tc.sim.CurTime()), TrainDwellEnd, train, train.Number)

	case TrainDwellEnd:
		// fmt.Println("Train Dwell End")

		// curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
		// curSchedule := train.schedule[train.curSchedulePoint]
		// fmt.Println(len(train.schedule), train.curSchedulePoint+1)
		if len(train.schedule) <= train.curSchedulePoint+1 {
			train.curSchedulePoint++
			tc.sim.ScheduleEventNext(TrainDeparted, train, train.Number)
			return
		}
		// reserve the track to next station
		nextSchedule := train.schedule[train.curSchedulePoint+1]
		nextStn := tc.sim.world.stations[nextSchedule.StnCode]
		// nextPf := nextStn.FindAvailableStnPlatform(nextSchedule.SpPfNo)
		// if nextPf == nil { // it always returns something, stil best to keep tho..
		// fmt.Printf("Cannot find any available platform (%s)\n", train.GetFullName())
		// return
		// }
		fmt.Printf("Trying reserve upto %s (by %s)\n", nextStn.Code, train.GetFullName())
		// fmt.Println("Next PF", nextPf)
		// path, ok := tc.sim.dispatcher.TryReservePathToTrack(train, nextPf)
		// if !ok {
		// fmt.Printf("Path to %s cannot be reserved, waiting... (%s)\n", nextPf.Id, train.GetFullName())
		// return
		// }
		path, ok := tc.sim.dispatcher.RequestRouteToStation(train, nextStn, nextSchedule.SpPfNo)
		if ok {
			tc.sim.ScheduleEventNext(RouteGranted, path, train.Number)
			return
		}

		// path.PPrint()
		// train.reservation = &ReservationData{
		// 	curPath: path,
		// 	train:   train,
		// }
		// // fmt.Println("Dispatching to station")
		// if ma, ok := tc.sim.dispatcher.RequestToProceed(train, path); ok {
		// 	tc.sim.ScheduleEventNext(MovementAuthorized, ma, train.Number)
		// }

	case TrainDeparted:

		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
		if train.curSchedulePoint >= len(train.schedule) {
			tc.sim.ScheduleEventNext(ScheduleEnd, train, train.Number)
			return
		}

		path := train.ma.path

		// fmt.Printf("Train departed %#v\n", path)
		// s.ScheduleEventNext(TrackExited, train)

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
		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]

		curTrack.Track.Release(train)
		tc.sim.ScheduleEventNext(TrackReleased, curTrack.Track, train.Number)
		tc.sim.dispatcher.OnTrackReleased(curTrack.Track, train)
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
