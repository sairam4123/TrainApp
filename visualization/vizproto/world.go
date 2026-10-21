package vizproto

import (
	"maps"
	"slices"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

// World is the static railway as plain data, enough to lay it out and label
// it. Points and tracks are listed in id order.
type World struct {
	Points   []Point       `json:"points"`
	Tracks   []Track       `json:"tracks"`
	Switches []SwitchBlock `json:"switches"`
	Signals  []Signal      `json:"signals"`
	Stations []Station     `json:"stations"`
	Trains   []Train       `json:"trains"`
}

type Point struct {
	ID       string `json:"id"`
	DeadEnd  bool   `json:"deadEnd,omitempty"`
	Boundary bool   `json:"boundary,omitempty"`
}

const (
	KindLine     = "line"
	KindPlatform = "platform"
	KindSwitch   = "switch"
)

// Track runs west to east, From to To, moving Lanes schematic lanes.
type Track struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"` // display name, the id if the track has none
	From   string  `json:"from"`
	To     string  `json:"to"`
	Lanes  int     `json:"lanes,omitempty"`
	Kind   string  `json:"kind"`
	Length float64 `json:"length"` // meters
	// Direction is the line's UP or DOWN label, if it has one; only a label,
	// the way trains run on it is its signals'.
	Direction string `json:"direction,omitempty"`
}

type SwitchBlock struct {
	ID      string   `json:"id"`
	Tracks  []string `json:"tracks"`
	Default string   `json:"default"`
}

// Signal stands at At and governs movement from Facing towards At.
type Signal struct {
	ID     string `json:"id"`
	At     string `json:"at"`
	Facing string `json:"facing"`
}

type Station struct {
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Platforms []Platform `json:"platforms"`
}

type Platform struct {
	No    string `json:"no"`
	Track string `json:"track"`
	// Class is main, through or loop (see infra.TrackClass); MainFor lists
	// the running lines it is the main line for. Empty in version 1.
	Class   string   `json:"class,omitempty"`
	MainFor []string `json:"mainFor,omitempty"`
}

type Train struct {
	Number string `json:"number"`
	Name   string `json:"name"`
	// Class and Schedule are empty before version 3.
	Class    string          `json:"class,omitempty"`
	Schedule []SchedulePoint `json:"schedule,omitempty"`
	// LengthM is the train's length in metres; 0 before version 5, or for a
	// train with no length.
	LengthM float64 `json:"length_m,omitempty"`
}

// SchedulePoint is a station in a train's schedule as planned. Arr and Dep
// are -1 where the schedule gives no time.
type SchedulePoint struct {
	Stn  string  `json:"stn"`
	Kind string  `json:"kind"` // stop, dwell or pass, as ops.SchedulePointKind
	Pf   string  `json:"pf,omitempty"`
	Arr  float64 `json:"arr"`
	Dep  float64 `json:"dep"`
}

// FromWorld describes a finalized world. Trains are left for the caller.
func FromWorld(w *infra.World) *World {
	out := &World{}
	pts := make(map[ids.PointID]*infra.TrackPoint)
	for _, trk := range w.TrackGraph.ListTracks() {
		edge, ok := w.TrackGraph.Edge(trk)
		if !ok {
			continue
		}
		pts[edge.From.Id] = edge.From
		pts[edge.To.Id] = edge.To
		kind := KindLine
		if w.IsStationPlatform(trk) {
			kind = KindPlatform
		} else if len(w.SwitchBlocksOn(trk)) > 0 {
			kind = KindSwitch
		}
		out.Tracks = append(out.Tracks, Track{
			ID:     trk.Id.String(),
			Name:   trk.String(),
			From:   edge.From.Id.String(),
			To:     edge.To.Id.String(),
			Lanes:  edge.Lanes,
			Kind:   kind,
			Length: float64(trk.Length),

			Direction: string(trk.Direction),
		})
	}
	// in the world's id order, which the layout uses to break ties
	for _, id := range slices.SortedFunc(maps.Keys(pts), ids.PointID.Compare) {
		pt := pts[id]
		out.Points = append(out.Points, Point{ID: id.String(), DeadEnd: pt.IsDeadEnd, Boundary: pt.IsSimBoundary})
	}

	for _, sb := range w.ListSwitchBlocks() {
		blk := SwitchBlock{ID: sb.Id}
		for _, e := range sb.ListManagedEdges() {
			blk.Tracks = append(blk.Tracks, e.Track.Id.String())
		}
		if d := sb.DefaultEdge(); d != nil {
			blk.Default = d.Track.Id.String()
		}
		out.Switches = append(out.Switches, blk)
	}
	for _, sig := range w.ListSignals() {
		out.Signals = append(out.Signals, Signal{ID: sig.Id, At: sig.AtPoint.Id.String(), Facing: sig.FacingPoint.Id.String()})
	}
	for _, stn := range w.ListStations() {
		s := Station{Code: string(stn.Code), Name: stn.Name}
		for _, pf := range stn.Platforms() {
			p := Platform{No: pf.PfNo, Track: pf.Track.Id.String(), Class: stn.TrackClass(pf.Id).String()}
			for _, line := range stn.MainLinesOf(pf.Id) {
				p.MainFor = append(p.MainFor, line.String())
			}
			s.Platforms = append(s.Platforms, p)
		}
		out.Stations = append(out.Stations, s)
	}
	return out
}
