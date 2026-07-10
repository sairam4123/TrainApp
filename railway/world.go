package railway

import (
	"fmt"
	"trainapp/units"
)

type WorldData struct {
	DefaultSwMaxSpeed units.MetersPerMin
	DefaultSwLength   units.Meters
	DefaultPfMaxSpeed units.MetersPerMin
	DefaultPfLength   units.Meters

	DefaultRouteSpeed units.MetersPerMin
}

type World struct {
	stations map[string]*Station
	trains   map[string]*Train

	bsections map[string]*BlockSection

	TrackGraph *TrackGraph
	data       WorldData
}

func (w *World) FindBlockBwStns(aStnCode string, bStnCode string) (*BlockSection, error) {
	for _, bsec := range w.bsections {
		if bsec.stnA.Code == aStnCode && bsec.stnB.Code == bStnCode {
			return bsec, nil
		}
		if bsec.stnA.Code == bStnCode && bsec.stnB.Code == aStnCode {
			return bsec, nil
		}
	}
	return nil, fmt.Errorf("Cannot find any block sections between aStnCode %s <-> bStnCode %s", aStnCode, bStnCode)
}

func (w *World) GetStation(code string) (*Station, bool) {
	stn, ok := w.stations[code]
	return stn, ok
}

func (w *World) AddTrain(train *Train) {
	w.trains[train.Number] = train
}
func (w *World) RemoveTrain(trainNumber string) {
	delete(w.trains, trainNumber)
}

func (w *World) AddStation(stn *Station) {
	w.stations[stn.Code] = stn
}
func (w *World) RemoveStation(stnCode string) {
	delete(w.stations, stnCode)
}

func (w *World) AddBlockSection(bsec *BlockSection) {
	w.bsections[bsec.Id] = bsec
}

func (w *World) Init(data WorldData) {
	w.stations = make(map[string]*Station)
	w.trains = make(map[string]*Train)
	w.bsections = make(map[string]*BlockSection)

	w.data = data
	w.TrackGraph = &TrackGraph{}
	w.TrackGraph.Init()
}

func (w *World) NewTrackPoint(id string) *TrackPoint {
	tp := &TrackPoint{
		Id:            id,
		IsDeadEnd:     false,
		IsSimBoundary: false,
	}
	return tp
}

func (w *World) NewStation(stnCode string, stnName string) *Station {
	stn := &Station{
		Code: stnCode,
		Name: stnName,
	}
	stn.Init()
	w.AddStation(stn)
	return stn
}

func (w *World) NewTrackSegment(id string, length units.Meters) *TrackSegment {
	ts := &TrackSegment{
		Id:       id,
		MaxSpeed: w.data.DefaultRouteSpeed,
		Length:   length,
	}
	return ts
}

func (w *World) NewSwitchTrack(id string) *TrackSegment {
	ts := w.NewTrackSegment(id, w.data.DefaultSwLength)
	ts.SetTrackAttributes(w.data.DefaultSwLength, w.data.DefaultSwMaxSpeed)
	return ts
}

func (w *World) NewPlatformTrack(id string) *TrackSegment {
	ts := w.NewTrackSegment(id, w.data.DefaultPfLength)
	ts.SetTrackAttributes(w.data.DefaultPfLength, w.data.DefaultPfMaxSpeed)
	return ts
}

func (w *World) NewBlockSection(id string) *BlockSection {
	bsec := &BlockSection{
		Id: id,
	}
	w.AddBlockSection(bsec)
	return bsec
}
