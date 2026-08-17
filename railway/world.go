package railway

import (
	"maps"
	"slices"
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
	switchBlocks map[string]*SwitchBlock

	stations map[string]*Station
	trains   map[string]*Train

	bsections map[string]*BlockSection
	signals   map[string]*Signal

	TrackGraph *TrackGraph
	data       WorldData
}

func (w *World) AddSignal(sig *Signal) {
	w.signals[sig.Id] = sig
}

// func (w *World) FindBlockBwStns(aStnCode string, bStnCode string) (*BlockSection, error) {
// 	for _, bsec := range w.bsections {
// 		if bsec.stnA.Code == aStnCode && bsec.stnB.Code == bStnCode {
// 			return bsec, nil
// 		}
// 		if bsec.stnA.Code == bStnCode && bsec.stnB.Code == aStnCode {
// 			return bsec, nil
// 		}
// 	}
// 	return nil, fmt.Errorf("Cannot find any block sections between aStnCode %s <-> bStnCode %s", aStnCode, bStnCode)
// }

func (w *World) GetStation(code string) (*Station, bool) {
	stn, ok := w.stations[code]
	return stn, ok
}

func (w *World) AddTrain(train *Train) {
	w.trains[train.Number] = train
}
func (w *World) AddStation(stn *Station) {
	w.stations[stn.Code] = stn
}

func (w *World) Init(data WorldData) {
	w.stations = make(map[string]*Station)
	w.trains = make(map[string]*Train)
	w.bsections = make(map[string]*BlockSection)
	w.switchBlocks = make(map[string]*SwitchBlock)
	w.signals = make(map[string]*Signal)

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
	w.bsections[bsec.Id] = bsec
	return bsec
}

func (w *World) newSwitchBlock(id string, managedEdges []*GraphEdge, activeEdge *GraphEdge) *SwitchBlock {
	swBlk := &SwitchBlock{
		Id: id,

		activeEdge:   activeEdge,
		managedEdges: managedEdges,
	}
	w.switchBlocks[swBlk.Id] = swBlk

	return swBlk
}

// TEMP code -> to be removed
func (w *World) ListSignals() []*Signal {
	return slices.Collect(maps.Values(w.signals))
}

func (w *World) GetSignal(signalId string) *Signal {
	return w.signals[signalId]
}
