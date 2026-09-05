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

		platform := nextStn.FindAvailableStnPlatform(curSchedule.SpPfNo)
		facingPoint := tc.sim.world.TrackGraph.FindWorldBoundaryPoint(platform)
		train.FacingToward = facingPoint
		// try to reserve the track to first station
		path, ok := tc.sim.dispatcher.TryReservePathToTrack(train, platform)
		if !ok && path == nil {
			fmt.Println("Path cannot be reserved.. waiting to enter world")
			return
		}
		train.reservation = &ReservationData{
			train:   train,
			curPath: path,
			disp:    tc.sim.dispatcher,
		}

		if ma, ok := tc.sim.dispatcher.RequestToProceed(train, path); ok {
			if ok := ma.path.Edges[0].Track.Acquire(train); !ok {
				fmt.Println("Edge cannot be acquired")
				return
			}
			train.ma = ma

			train.occupation = &OccupationData{
				train:      train,
				curPathIdx: 0,
				curPath:    ma.path,
				disp:       tc.sim.dispatcher,
			}
			tc.sim.ScheduleEventNext(TrackEntered, train)
		}

	case TrackEntered:
		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
		// fmt.Println("Track Entered", curTrack.Id)
		train.FacingToward = tc.sim.world.TrackGraph.OtherEnd(curTrack, train.FacingToward.Id)
		time := curTrack.TravelTime(train.MaxSpeed)
		tc.sim.ScheduleEventAfter(time, TrackTravelEnd, train)
		// train := ev.Data.()

	case TrackTravelEnd:
		if len(train.occupation.curPath.Edges) <= train.occupation.curPathIdx+1 {
			tc.sim.ScheduleEventNext(PathCompleted, train)
			curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
			tc.sim.dispatcher.intlck.UnlockSwitchBlocks(curTrack, train)
		} else {
			// acquire next track
			nextTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx+1]
			ok := nextTrack.Track.Acquire(train)
			if ok {
				curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
				curTrack.Track.Release(train)
				tc.sim.ScheduleEventNext(TrackReleased, curTrack.Track)
				tc.sim.dispatcher.OnTrackReleased(curTrack.Track, train)
				train.occupation.curPathIdx++
				tc.sim.ScheduleEventNext(TrackEntered, train)
			}
			// s.dispatcher.sim.ScheduleEventNext(TrackExited, train)
		}

	case RouteGranted:
		reserv := data.(*ReservationData)
		train := reserv.train

		train.reservation = reserv
		path := reserv.curPath

		// TODO: RouteGrants can also happen from Home Signal Approach

		// TODO: I don't think I like this approach tbh
		if ma, ok := tc.sim.dispatcher.RequestToProceed(train, path); ok {
			train.ma = ma
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
					disp:       tc.sim.dispatcher,
				}
				tc.sim.ScheduleEventNext(TrackEntered, train)
				return
			}
			train.curSchedulePoint++
			tc.sim.ScheduleEventNext(TrainDeparted, train)
		} else {
			fmt.Println("Request to proceed failed, waiting..", train.GetFullName(), train.occupation.curPathIdx)
		}

	case MovementAuthorized:

	case MovementAuthorityEnded:
		// TODO: check if the current track is the station platform
		// TOOD: if not station platform, then pathfind to the available / preferred station platform

	case PathCompleted:
		// fmt.Println("Path completed")
		// path complete is always within the station
		tc.sim.ScheduleEventNext(TrainArrived, train)

	case TrainArrived:
		// fmt.Println("Train Arrived")
		// curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
		curSchedule := train.schedule[train.curSchedulePoint]

		tc.sim.ScheduleEventAfter(curSchedule.ExpDwellTime(tc.sim.CurTime()), TrainDwellEnd, train)

	case TrainDwellEnd:
		// fmt.Println("Train Dwell End")

		// curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
		// curSchedule := train.schedule[train.curSchedulePoint]
		// fmt.Println(len(train.schedule), train.curSchedulePoint+1)
		if len(train.schedule) <= train.curSchedulePoint+1 {
			train.reservation = nil
			tc.sim.ScheduleEventNext(TrainDeparted, train)
			return
		}
		// reserve the track to next station
		nextSchedule := train.schedule[train.curSchedulePoint+1]
		nextStn := tc.sim.world.stations[nextSchedule.StnCode]
		nextPf := nextStn.FindAvailableStnPlatform(nextSchedule.SpPfNo)
		if nextPf == nil {
			fmt.Printf("Cannot find any available platform (%s)\n", train.GetFullName())
			return
		}
		fmt.Printf("Trying reserve upto %s (by %s)\n", nextPf.Id, train.GetFullName())
		// fmt.Println("Next PF", nextPf)
		path, ok := tc.sim.dispatcher.TryReservePathToTrack(train, nextPf)
		if !ok {
			fmt.Printf("Path to %s cannot be reserved, waiting... (%s)\n", nextPf.Id, train.GetFullName())
			return
		}
		// path.PPrint()
		train.reservation = &ReservationData{
			curPath: path,
			train:   train,
			disp:    tc.sim.dispatcher,
		}
		// fmt.Println("Dispatching to station")
		if ma, ok := tc.sim.dispatcher.RequestToProceed(train, path); ok {
			train.ma = ma
			train.curSchedulePoint++
			tc.sim.ScheduleEventNext(TrainDeparted, train)
		} else {
			fmt.Println("Request to proceed failed, waiting..", train.GetFullName(), train.occupation.curPathIdx)
		}

	case TrainDeparted:

		curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
		if train.reservation == nil {
			curTrack.Track.Release(train)
			tc.sim.ScheduleEventNext(TrackReleased, curTrack.Track)
			tc.sim.dispatcher.OnTrackReleased(curTrack.Track, train)
			tc.sim.ScheduleEventNext(WorldExited, train)
			train.reservation = nil
			train.occupation = nil
			return
		}

		path := train.reservation.curPath

		// fmt.Printf("Train departed %#v\n", path)
		// s.ScheduleEventNext(TrackExited, train)

		if ok := path.Edges[0].Track.Acquire(train); !ok {
			fmt.Println("Edge cannot be acquired")
			return
		}
		curTrack.Track.Release(train)
		tc.sim.ScheduleEventNext(TrackReleased, curTrack.Track)
		tc.sim.dispatcher.OnTrackReleased(curTrack.Track, train)

		train.occupation = &OccupationData{
			train:      train,
			curPathIdx: 0,
			curPath:    path,
			disp:       tc.sim.dispatcher,
		}
		tc.sim.ScheduleEventNext(TrackEntered, train)

	}

}

func (t *Train) String() string {
	if t == nil {
		return "<nil>"
	}
	return t.GetFullName()
}
