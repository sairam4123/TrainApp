package railway

import (
	"fmt"
	"trainapp/des"
	"trainapp/units"
)

type Sim struct {
	des   *des.DES[RailwayEvent]
	world *World

	// blockSecControllers map[string]*BlockSectionController
	// stnControllers      map[string]*StationController

	dispatcher *Dispatcher
}

func (s *Sim) SetWorld(world *World) *Sim {
	s.world = world
	return s
}

func (s *Sim) Init() {
	if s.world == nil {
		panic("s.world is nil, did you call SetWorld?")
	}
	s.des = &des.DES[RailwayEvent]{}
	s.des.Init()

	s.dispatcher = &Dispatcher{
		sim: s,
	}
	// s.blockSecControllers = make(map[string]*BlockSectionController)
	// s.stnControllers = make(map[string]*StationController)

	// for _, stn := range s.world.stations {
	// 	s.stnControllers[stn.Code] = &StationController{
	// 		station: stn,
	// 		sim:     s,
	// 		waiting: make([]*Train, 0),
	// 	}
	// }

	// for _, bsec := range s.world.bsections {
	// 	s.blockSecControllers[bsec.Id] = &BlockSectionController{
	// 		bsec:    bsec,
	// 		sim:     s,
	// 		waiting: make([]*Train, 0),
	// 	}
	// }

	for _, train := range s.world.trains {
		s.ScheduleEventAt(train.schedule[0].ArrTime-des.MinDeltaTime, WorldEntered, train)
	}
}

// func (s *Sim) stnCtrller(stnCode string) *StationController {
// 	return s.stnControllers[stnCode]
// }

// func (s *Sim) bsecCtrller(bName string) *BlockSectionController {
// 	return s.blockSecControllers[bName]
// }

func (s *Sim) NextEvent() (des.Event[RailwayEvent], bool) {
	if s.des == nil {
		panic("s.des is nil, did you call InitWorld?")
	}
	return s.des.NextEvent()
}

func (s *Sim) ScheduleEventAfter(delta units.Minutes, evtype RailwayEvent, data any) {
	s.des.Add(s.CurTime()+float64(delta), evtype, data)
}

func (s *Sim) ScheduleEventNext(evtype RailwayEvent, data any) {
	s.des.Add(s.CurTime()+des.MinDeltaTime, evtype, data)
}

func (s *Sim) ScheduleEventAt(time float64, evType RailwayEvent, data any) {
	s.des.Add(time, evType, data)
}

func (s *Sim) CurTime() float64 {
	return s.des.CurTime
}

// func (s *Sim) GetPathController() *PathController {

// }

func (s *Sim) Run() {
	for {
		ev, ok := s.NextEvent()
		if !ok {
			break
		}
		train, ok := ev.Data.(*Train)
		if ok && train.occupation != nil {
			curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
			fmt.Printf("[%.2f] %s - %s (Track %s - %dm)\n", ev.Time, ev.Type, train.Name, curTrack.Track.Id, int(curTrack.Track.Length))
		} else if ok {
			fmt.Printf("[%.2f] %s - %s\n", ev.Time, ev.Type, train.Name)
		} else if track, ok := ev.Data.(*TrackSegment); ok {
			fmt.Printf("[%.2f] %s - %s\n", ev.Time, ev.Type, track.Id)
		}
		switch RailwayEvent(ev.Type) {
		case WorldEntered:
			train := ev.Data.(*Train)
			train.curSchedulePoint = 0
			curSchedule := train.schedule[train.curSchedulePoint]
			nextStn, ok := s.world.stations[curSchedule.StnCode]
			if !ok {
				fmt.Println("Something went wrong, cannot find station required for schedule")
			}

			platform := nextStn.StationPlatform(curSchedule.SpPfNo)
			facingPoint := s.world.TrackGraph.FindWorldBoundaryPoint(platform)
			train.FacingToward = facingPoint
			// try to reserve the track to first station
			path, ok := s.dispatcher.TryReservePathToEdge(train, platform)
			if !ok && path == nil {
				fmt.Println("Path cannot be reserved")
				continue
			}
			train.reservation = &ReservationData{
				train:   train,
				curPath: path,
				disp:    s.dispatcher,
			}
			// if !ok && path != nil {
			// 	fmt.Println("Path cannot be reserved - only one")
			// 	// it is the platform only.. in this case.. just occupy the platform directly and switch facing to other direction
			// 	train.FacingToward = s.world.TrackGraph.OtherEnd(platform, train.FacingToward.Id)
			// 	train.occupation = &OccupationData{
			// 		train:      train,
			// 		curPathIdx: 0,
			// 		curPath: &Path{
			// 			Edges: append(make([]*GraphEdge, 0), s.dispatcher.sim.world.TrackGraph.Edges[platform.Id]), // it's messy ik
			// 		},
			// 	}
			// 	s.ScheduleEventNext(TrainArrived, train)
			// 	return
			// }

			if ok := s.dispatcher.RequestToProceed(train, path); ok {
				if ok := path.Edges[0].Track.Acquire(train); !ok {
					fmt.Println("Edge cannot be acquired")
					return
				}

				train.occupation = &OccupationData{
					train:      train,
					curPathIdx: 0,
					curPath:    path,
					disp:       s.dispatcher,
				}
				s.ScheduleEventNext(TrackEntered, train)
			}

		case TrackEntered:
			train := ev.Data.(*Train)
			curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
			// fmt.Println("Track Entered", curTrack.Id)
			train.FacingToward = s.world.TrackGraph.OtherEnd(curTrack, train.FacingToward.Id)
			time := curTrack.TravelTime(train.MaxSpeed)
			s.ScheduleEventAfter(time, TrackTravelEnd, train)

		case TrackReleased:
			track := ev.Data.(*TrackSegment)
			s.dispatcher.OnTrackReleased(track)

		case TrackTravelEnd:
			train := ev.Data.(*Train)
			if len(train.occupation.curPath.Edges) <= train.occupation.curPathIdx+1 {
				s.ScheduleEventNext(PathCompleted, train)
			} else {
				// acquire next track
				nextTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx+1]
				ok := nextTrack.Track.Acquire(train)
				if ok {
					curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
					curTrack.Track.Release(train)
					s.ScheduleEventNext(TrackReleased, curTrack.Track)
					train.occupation.curPathIdx++
				}
				// s.dispatcher.sim.ScheduleEventNext(TrackExited, train)
				s.ScheduleEventNext(TrackEntered, train)
			}

		case RouteGranted:
			reserv := ev.Data.(*ReservationData)
			train := reserv.train

			train.reservation = reserv
			path := reserv.curPath

			// TODO: RouteGrants can also happen from Home Signal Approach

			// TODO: I don't think I like this approach tbh
			if ok := s.dispatcher.RequestToProceed(train, path); ok {
				train.curSchedulePoint++
				s.ScheduleEventNext(TrainDeparted, train)
			} else {
				fmt.Println("Request to proceed failed, waiting..")
			}

		case PathCompleted:
			// fmt.Println("Path completed")
			// path complete is always within the station
			s.ScheduleEventNext(TrainArrived, ev.Data)

		case TrainArrived:
			// fmt.Println("Train Arrived")
			train := ev.Data.(*Train)
			// curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
			curSchedule := train.schedule[train.curSchedulePoint]

			s.ScheduleEventAfter(curSchedule.ExpDwellTime(s.CurTime()), TrainDwellEnd, train)

		case TrainDwellEnd:
			// fmt.Println("Train Dwell End")

			train := ev.Data.(*Train)
			// curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx].Track
			// curSchedule := train.schedule[train.curSchedulePoint]
			// fmt.Println(len(train.schedule), train.curSchedulePoint+1)
			if len(train.schedule) <= train.curSchedulePoint+1 {
				train.reservation = nil
				s.ScheduleEventNext(TrainDeparted, train)
				continue
			}
			// reserve the track to next station
			nextSchedule := train.schedule[train.curSchedulePoint+1]
			nextStn := s.world.stations[nextSchedule.StnCode]
			nextPf := nextStn.StationPlatform(nextSchedule.SpPfNo)

			// fmt.Println("Next PF", nextPf)
			path, ok := s.dispatcher.TryReservePathToEdge(train, nextPf)
			if !ok {
				fmt.Printf("Path to %s cannot be reserved, waiting...\n", nextPf.Id)
				continue
			}
			// path.PPrint()
			train.reservation = &ReservationData{
				curPath: path,
				train:   train,
				disp:    s.dispatcher,
			}
			// fmt.Println("Dispatching to station")
			if ok := s.dispatcher.RequestToProceed(train, path); ok {
				train.curSchedulePoint++
				s.ScheduleEventNext(TrainDeparted, train)
			} else {
				fmt.Println("Request to proceed failed, waiting..")
			}

		case TrainDeparted:
			train := ev.Data.(*Train)

			curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
			if train.reservation == nil {
				curTrack.Track.Release(train)
				s.ScheduleEventNext(TrackReleased, curTrack.Track)
				s.ScheduleEventNext(WorldExited, train)
				continue
			}

			path := train.reservation.curPath

			// fmt.Printf("Train departed %#v\n", path)
			// s.ScheduleEventNext(TrackExited, train)

			if ok := path.Edges[0].Track.Acquire(train); !ok {
				fmt.Println("Edge cannot be acquired")
				return
			}
			curTrack.Track.Release(train)
			s.ScheduleEventNext(TrackReleased, curTrack.Track)

			train.occupation = &OccupationData{
				train:      train,
				curPathIdx: 0,
				curPath:    path,
				disp:       s.dispatcher,
			}
			s.ScheduleEventNext(TrackEntered, train)

		}
	}
}

// TODO: move to TrainController, work out a way for it
// func (s *Sim) Run() {
// 	for {
// 		ev, ok := s.NextEvent()
// 		if !ok {
// 			break
// 		}
// 		switch RailwayEvent(ev.Type) {
// 		case TrainEntered:
// 			train := ev.Data.(*Train)
// 			train.curSchedulePoint = 0 // enters the sim

// 			s.des.Add(s.des.CurTime+1.0, train.Name, TrainArrived, train)
// 			fmt.Printf("[%.2f] %s entered the simulation\n", s.des.CurTime, train.Name)
// 			// pf.track.OccupiedBy = train // occupy

// 		case TrainArrived:
// 			train := ev.Data.(*Train)
// 			trainSchedule := train.schedule
// 			curSchedule := trainSchedule[train.curSchedulePoint]

// 			curOccup := train.occupation

// 			curStn, ok := s.world.GetStation(curSchedule.StnCode)
// 			if !ok {
// 				panic("Station seems to be missing")
// 			}
// 			ctrller := s.stnCtrller(curStn.Code)
// 			_, ok = ctrller.Acquire(train, "0")
// 			if !ok {
// 				fmt.Printf("[%.2f] %s waiting to enter station %s - %s\n", s.des.CurTime, train.Name, curStn.Name, curStn.Code)
// 				break
// 			}
// 			if curOccup != nil {
// 				curOccup.ctrller.Release(curOccup)
// 			}

// 			dwellTime := curSchedule.ExpDwellTime()

// 			fmt.Printf("[%.2f] %s arrived at %s - %s\n", s.des.CurTime, train.Name, curStn.Name, curStn.Code)
// 			s.des.Add(s.des.CurTime+dwellTime, train.Name, TrainDwellEnd, train)

// 		case TrainDwellEnd:
// 			train := ev.Data.(*Train)
// 			s.des.Add(s.des.CurTime+1.0, train.Name, TrainDeparted, train)

// 		case TrackReleased:
// 			train := ev.Data.(*Train)
// 			// train is waked up here..
// 			// find the state
// 			fmt.Printf("[%.2f] %s track release received, waking up...\n", s.des.CurTime, train.Name)
// 			if _, ok := train.occupation.ctrller.(*BlockSectionController); ok {
// 				// so we found that we are waiting to enter station.
// 				s.des.Add(s.des.CurTime+1.0, train.Name, TrainArrived, train)
// 			} else if _, ok := train.occupation.ctrller.(*StationController); ok {
// 				// we are watiing to enter block section, just go back to traindeparting state
// 				s.des.Add(s.des.CurTime+1.0, train.Name, TrainDeparted, train)
// 			}

// 		case TrainDeparted:
// 			train := ev.Data.(*Train)
// 			curSchedule := train.schedule[train.curSchedulePoint]
// 			curStn, ok := s.world.GetStation(curSchedule.StnCode)
// 			curOccp := train.occupation
// 			if !ok {
// 				panic("Station seems to be missing")
// 			}
// 			fmt.Printf("[%.2f] %s departed from %s - %s\n", s.des.CurTime, train.Name, curStn.Name, curStn.Code)
// 			if train.curSchedulePoint+1 >= len(train.schedule) {
// 				train.curSchedulePoint += 1
// 				curOccp.ctrller.Release(curOccp)
// 				s.des.Add(s.des.CurTime+1.0, train.Name, TrainExited, train)
// 			} else {
// 				nextSchedule := train.schedule[train.curSchedulePoint+1]

// 				bSec, err := s.world.FindBlockBwStns(curSchedule.StnCode, nextSchedule.StnCode)
// 				if err != nil {
// 					panic(err)
// 				}
// 				ctrller := s.bsecCtrller(bSec.Id)
// 				if ctrller == nil {
// 					panic("Controller is not available for bsec.id")
// 				}
// 				_, ok := ctrller.Acquire(train, "0")
// 				if !ok {
// 					fmt.Printf("[%.2f] %s waiting for free track\n", s.des.CurTime, train.Name)
// 					// don't bother scheduling anything
// 					break
// 				}
// 				curOccp.ctrller.Release(curOccp)
// 				train.curSchedulePoint += 1
// 				s.des.Add(s.des.CurTime+100.0, train.Name, TrainArrived, train)
// 			}
// 		case TrainExited:
// 			train := ev.Data.(*Train)
// 			fmt.Printf("[%.2f] %s exited simulation\n", s.des.CurTime, train.Name)
// 		}
// 	}
// }
