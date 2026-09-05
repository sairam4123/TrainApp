package railway

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"trainapp/des"
	"trainapp/units"
)

type Sim struct {
	des   *des.DES[RailwayEvent]
	world *World

	// blockSecControllers map[string]*BlockSectionController
	// stnControllers      map[string]*StationController

	dispatcher *Dispatcher

	trainCtrllers map[string]*TrainController

	dCount int // temp dump count
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
	s.dispatcher.Init()
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

func (s *Sim) DumpSim() {
	os.MkdirAll("dumps/", 0755)
	dump, err := os.OpenFile(fmt.Sprintf("dumps/dump%d.txt", int(s.des.CurTime*100)), os.O_APPEND|os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic("Error opening dump file")
	}
	defer dump.Close()

	trains := slices.SortedStableFunc(maps.Values(s.world.trains), func(t1, t2 *Train) int {
		return strings.Compare(t1.Number, t2.Number)
	})
	for _, train := range trains {
		fmt.Fprintf(dump, "Train %s", train)
		if train.occupation != nil {
			curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
			fmt.Fprintf(dump, " - Track %s", curTrack.Track.Id)
		}
		fmt.Fprintln(dump)
	}
	fmt.Fprintln(dump)

	tracks := slices.SortedStableFunc(maps.Values(s.world.TrackGraph.tracks), func(trk1, trk2 *TrackSegment) int {
		return strings.Compare(trk1.Id, trk2.Id)
	})

	for _, track := range tracks {
		fmt.Fprintf(dump, "Track %s - Reserved %s - Occupied %s\n", track.Id, track.ReservedBy, track.OccupiedBy)
	}
	fmt.Fprintln(dump)
}

func (s *Sim) InitLogger() *os.File {
	os.MkdirAll("logs/", 0755)
	logger, err := os.OpenFile("logs/logs.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic("Error opening logging files")
	}
	return logger
}

func (s *Sim) Run() {
	logger := s.InitLogger()
	os.RemoveAll("dumps/")
	defer logger.Close()

	for {
		ev, ok := s.NextEvent()
		if !ok {
			s.DumpSim()
			break
		}
		train, ok := ev.Data.(*Train)
		if ok && train.occupation != nil {
			curTrack := train.occupation.curPath.Edges[train.occupation.curPathIdx]
			fmt.Fprintf(logger, "[%.2f] %s - %s (Track %s - %dm)\n", ev.Time, ev.Type, train.Name, curTrack.Track.Id, int(curTrack.Track.Length))
		} else if ok {
			fmt.Fprintf(logger, "[%.2f] %s - %s\n", ev.Time, ev.Type, train.Name)
		} else if track, ok := ev.Data.(*TrackSegment); ok {
			fmt.Fprintf(logger, "[%.2f] %s - %s\n", ev.Time, ev.Type, track.Id)
		}

		if ok {
			tc := s.trainCtrllers[train.Number]
			tc.OnEvent(ev.Type, ev.Data)
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
