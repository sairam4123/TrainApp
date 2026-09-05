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

	s.trainCtrllers = make(map[string]*TrainController)

	s.des = &des.DES[RailwayEvent]{}
	s.des.Init()

	s.dispatcher = &Dispatcher{
		sim: s,
	}
	s.dispatcher.Init()

	for _, train := range s.world.trains {
		s.trainCtrllers[train.Number] = &TrainController{
			trainId: train.Number,
			sim:     s,
		}
		s.ScheduleEventAt(train.schedule[0].ArrTime-des.MinDeltaTime, WorldEntered, train)
	}
}

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
