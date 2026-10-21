package infra

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"trainapp/railway/ids"
	"trainapp/units"
)

type WorldData struct {
	DefaultSwMaxSpeed units.MetersPerMin
	DefaultSwLength   units.Meters
	DefaultPfMaxSpeed units.MetersPerMin
	DefaultPfLength   units.Meters

	DefaultRouteSpeed units.MetersPerMin

	// Up is the way ("E" or "W") the world's UP direction runs, for labelling
	// lines; empty reads as west. The sim does not use it.
	Up string
}

// World is the static railway: topology, stations, switch blocks and signals.
// It holds no runtime state (reservations, locks, trains); see the
// interlocking and sim packages for that.
type World struct {
	switchBlocks    map[string]*SwitchBlock
	switchesOnTrack map[ids.TrackID][]*SwitchBlock // switch blocks managing each track

	stations map[ids.StnCode]*Station
	signals  map[string]*Signal

	signalsOnTrack map[ids.TrackID][]*Signal // signals on each track; built by Finalize
	platformOf     map[ids.TrackID]*Station  // station of each platform track; built by Finalize

	TrackGraph *TrackGraph
	data       WorldData

	finalized bool
}

func (w *World) Init(data WorldData) {
	w.stations = make(map[ids.StnCode]*Station)
	w.switchBlocks = make(map[string]*SwitchBlock)
	w.switchesOnTrack = make(map[ids.TrackID][]*SwitchBlock)
	w.signals = make(map[string]*Signal)
	w.signalsOnTrack = make(map[ids.TrackID][]*Signal)

	w.data = data
	w.TrackGraph = &TrackGraph{}
	w.TrackGraph.Init()
}

// Finalize builds the lookup caches and checks the world is consistent. Call
// it once the world is built and before planning or interlocking with it. It
// is safe to call again after adding more infrastructure.
func (w *World) Finalize() error {
	w.TrackGraph.BuildCacheMap()

	var errs []error
	w.signalsOnTrack = make(map[ids.TrackID][]*Signal)
	for _, sig := range w.ListSignals() {
		edge, err := w.TrackGraph.TrackBetween(sig.AtPoint, sig.FacingPoint)
		if err != nil {
			errs = append(errs, fmt.Errorf("signal %s: %w", sig.Id, err))
			continue
		}
		w.signalsOnTrack[edge.Track.Id] = append(w.signalsOnTrack[edge.Track.Id], sig)
	}
	w.platformOf = make(map[ids.TrackID]*Station)
	for _, stn := range w.ListStations() {
		for _, pf := range stn.Platforms() {
			if _, ok := w.TrackGraph.Edges[pf.Track.Id]; !ok {
				errs = append(errs, fmt.Errorf("station %s platform %s: track %s is not in the graph", stn.Code, pf.PfNo, pf.Track))
			}
			if other, ok := w.platformOf[pf.Track.Id]; ok {
				errs = append(errs, fmt.Errorf("track %s is a platform of both %s and %s", pf.Track, other.Code, stn.Code))
			}
			w.platformOf[pf.Track.Id] = stn
		}
	}

	if err := errors.Join(errs...); err != nil {
		return err
	}
	w.buildStationTracks()
	w.finalized = true
	return nil
}

// IsFinalized reports whether Finalize succeeded and nothing was added since.
func (w *World) IsFinalized() bool {
	return w.finalized && w.TrackGraph.cacheBuilt
}

// Up is the way ("E" or "W", empty for west) the world's UP direction runs
// (WorldData.Up).
func (w *World) Up() string { return w.data.Up }

// SwitchLength is the length of each leg of a switch (WorldData.DefaultSwLength).
func (w *World) SwitchLength() units.Meters { return w.data.DefaultSwLength }

func (w *World) AddSignal(sig *Signal) {
	w.signals[sig.Id] = sig
	w.finalized = false
}

func (w *World) GetStation(code ids.StnCode) (*Station, bool) {
	stn, ok := w.stations[code]
	return stn, ok
}

func (w *World) AddStation(stn *Station) {
	w.stations[stn.Code] = stn
	w.finalized = false
}

// ListStations returns every station, sorted by code.
func (w *World) ListStations() []*Station {
	return slices.SortedFunc(maps.Values(w.stations), func(a, b *Station) int { return cmp.Compare(a.Code, b.Code) })
}

func (w *World) NewTrackPoint(id ids.PointID) *TrackPoint {
	tp := &TrackPoint{
		Id:            id,
		IsDeadEnd:     false,
		IsSimBoundary: false,
	}
	return tp
}

func (w *World) NewStation(stnCode ids.StnCode, stnName string) *Station {
	stn := &Station{
		Code: stnCode,
		Name: stnName,
	}
	stn.Init()
	w.AddStation(stn)
	return stn
}

// NewTrackSegment creates a plain track. Its id is assigned when it is added to
// the graph; name is an optional display name.
func (w *World) NewTrackSegment(name string, length units.Meters) *TrackSegment {
	ts := &TrackSegment{
		Name:     name,
		MaxSpeed: w.data.DefaultRouteSpeed,
		Length:   length,
	}
	return ts
}

func (w *World) NewSwitchTrack(name string) *TrackSegment {
	ts := w.NewTrackSegment(name, w.data.DefaultSwLength)
	ts.SetTrackAttributes(w.data.DefaultSwLength, w.data.DefaultSwMaxSpeed)
	return ts
}

func (w *World) NewPlatformTrack(name string) *TrackSegment {
	ts := w.NewTrackSegment(name, w.data.DefaultPfLength)
	ts.SetTrackAttributes(w.data.DefaultPfLength, w.data.DefaultPfMaxSpeed)
	return ts
}

// newSwitchBlock registers a switch block, indexes it by the tracks it manages
// and forbids running from one of its edges onto another through a point they
// share (e.g. from one switch leg straight onto the other).
func (w *World) newSwitchBlock(id string, managedEdges []*GraphEdge, defaultEdge *GraphEdge) *SwitchBlock {
	if _, ok := w.switchBlocks[id]; ok {
		panic(fmt.Sprintf("infra: switch block id %q is already used", id))
	}
	swBlk := &SwitchBlock{
		Id: id,

		defaultEdge:  defaultEdge,
		managedEdges: managedEdges,
		seq:          len(w.switchBlocks),
	}
	w.switchBlocks[swBlk.Id] = swBlk

	for _, edge := range managedEdges {
		w.switchesOnTrack[edge.Track.Id] = append(w.switchesOnTrack[edge.Track.Id], swBlk)
	}
	for i, a := range managedEdges {
		for _, b := range managedEdges[i+1:] {
			for _, pt := range []*TrackPoint{a.From, a.To} {
				if b.Other(pt) != nil {
					w.TrackGraph.forbidTransition(pt, a, b)
				}
			}
		}
	}

	return swBlk
}

// ListSwitchBlocks returns every switch block, sorted by id.
func (w *World) ListSwitchBlocks() []*SwitchBlock {
	return slices.SortedFunc(maps.Values(w.switchBlocks), func(a, b *SwitchBlock) int { return strings.Compare(a.Id, b.Id) })
}

func (w *World) GetSwitchBlock(id string) (*SwitchBlock, bool) {
	sb, ok := w.switchBlocks[id]
	return sb, ok
}

// SwitchBlocksOn returns the switch blocks that must be set and locked for a
// train to use the track. Empty for plain track.
func (w *World) SwitchBlocksOn(track *TrackSegment) []*SwitchBlock {
	return slices.Clone(w.switchesOnTrack[track.Id])
}

// SignalsOn returns the signals standing at either end of the track, facing
// along it. Requires Finalize.
func (w *World) SignalsOn(track *TrackSegment) []*Signal {
	return slices.Clone(w.signalsOnTrack[track.Id])
}

func (w *World) IsStationPlatform(track *TrackSegment) bool {
	_, ok := w.StationOfPlatform(track)
	return ok
}

// StationOfPlatform returns the station the platform track belongs to.
func (w *World) StationOfPlatform(track *TrackSegment) (*Station, bool) {
	if w.IsFinalized() {
		stn, ok := w.platformOf[track.Id]
		return stn, ok
	}
	for _, stn := range w.stations {
		if stn.IsPlatform(track.Id) {
			return stn, true
		}
	}
	return nil, false
}

// ListSignals returns every signal, sorted by id.
func (w *World) ListSignals() []*Signal {
	return slices.SortedFunc(maps.Values(w.signals), func(a, b *Signal) int { return strings.Compare(a.Id, b.Id) })
}

func (w *World) GetSignal(signalId string) (*Signal, bool) {
	sig, ok := w.signals[signalId]
	return sig, ok
}

// ListSimPts returns the sim boundary points, sorted by id.
func (w *World) ListSimPts() []*TrackPoint {
	pts := make([]*TrackPoint, 0)
	for _, pt := range w.TrackGraph.points {
		if pt.IsSimBoundary {
			pts = append(pts, pt)
		}
	}
	slices.SortFunc(pts, func(a, b *TrackPoint) int { return a.Id.Compare(b.Id) })
	return pts
}
