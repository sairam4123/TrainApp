package infra

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"trainapp/railway/ids"
	"trainapp/units"
)

// World snapshots.
//
// A Snapshot is a world as plain data, for saving to and loading from a file
// (JSON). It is low level: every point, track, switch block, signal and
// platform is listed as built, so a world comes back from its snapshot
// unchanged, ids and all. Points are named by their id's string (e.g.
// "tpjPf1S") and tracks by theirs (their two point ids, sorted, joined by a
// "-"), as in the logs. Lengths are in meters and speeds in km/h.

// SnapshotVersion is the snapshot format Snapshot writes and Build reads.
const SnapshotVersion = 1

type Snapshot struct {
	Version  int               `json:"version"`
	Defaults SnapshotDefaults  `json:"defaults"`
	Points   []SnapshotPoint   `json:"points"`
	Tracks   []SnapshotTrack   `json:"tracks"`
	Switches []SnapshotSwitch  `json:"switches"`
	Signals  []SnapshotSignal  `json:"signals"`
	Stations []SnapshotStation `json:"stations"`
}

// SnapshotDefaults is the world's WorldData: what tracks built onto the world
// after it is loaded get.
type SnapshotDefaults struct {
	SwitchKmph      float64 `json:"switch_kmph"`
	SwitchLengthM   float64 `json:"switch_length_m"`
	PlatformKmph    float64 `json:"platform_kmph"`
	PlatformLengthM float64 `json:"platform_length_m"`
	RouteKmph       float64 `json:"route_kmph"`
	// Up is the way ("E" or "W") the UP direction runs, which trainedit
	// labels one-way lines by (SnapshotTrack.Direction); empty reads as west.
	// The sim does not use it.
	Up string `json:"up,omitempty"`
}

// SnapshotPoint is a track point, given by the parts of its id. Stn puts the
// point in that station's area (see station_tracks.go). ID is the parts
// joined; Build works it out if it is left out, and checks it if not.
type SnapshotPoint struct {
	ID       string `json:"id,omitempty"`
	Stn      string `json:"stn,omitempty"`
	Type     string `json:"type,omitempty"`
	Seq      string `json:"seq,omitempty"`
	Other    string `json:"other,omitempty"`
	DeadEnd  bool   `json:"dead_end,omitempty"`
	Boundary bool   `json:"boundary,omitempty"`
}

// SnapshotTrack joins two points, From (the west end) to To. Lanes is how
// many schematic lanes it moves west to east. ID is worked out from the two
// points if it is left out, and checked if not.
type SnapshotTrack struct {
	ID        string  `json:"id,omitempty"`
	Name      string  `json:"name,omitempty"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	LengthM   float64 `json:"length_m"`
	MaxKmph   float64 `json:"max_kmph"`
	Lanes     int     `json:"lanes,omitempty"`
	Direction string  `json:"direction,omitempty"`
}

// SnapshotSwitch is a switch block: only one of its tracks can be in use at
// a time, and no train runs from one of them onto another through a point
// they share. It starts set to Default. Switches are listed in the order they
// were built, which is the order the interlocking checks them in.
type SnapshotSwitch struct {
	ID      string   `json:"id"`
	Tracks  []string `json:"tracks"`
	Default string   `json:"default"`
}

// SnapshotSignal stands at At and governs movement from Facing towards At.
type SnapshotSignal struct {
	ID     string `json:"id"`
	At     string `json:"at"`
	Facing string `json:"facing"`
}

type SnapshotStation struct {
	Code      string             `json:"code"`
	Name      string             `json:"name"`
	Platforms []SnapshotPlatform `json:"platforms"`
}

// SnapshotPlatform is platform No on Track; a LengthM of 0 makes it a
// through track.
type SnapshotPlatform struct {
	No      string  `json:"no"`
	Track   string  `json:"track"`
	LengthM float64 `json:"length_m"`
}

// Snapshot describes the world as plain data. It fails if two points' ids
// read the same, as a snapshot could not tell them apart.
func (w *World) Snapshot() (*Snapshot, error) {
	d := w.data
	s := &Snapshot{
		Version: SnapshotVersion,
		Defaults: SnapshotDefaults{
			SwitchKmph:      d.DefaultSwMaxSpeed.InKMPH(),
			SwitchLengthM:   float64(d.DefaultSwLength),
			PlatformKmph:    d.DefaultPfMaxSpeed.InKMPH(),
			PlatformLengthM: float64(d.DefaultPfLength),
			RouteKmph:       d.DefaultRouteSpeed.InKMPH(),
			Up:              d.Up,
		},
		Points:   []SnapshotPoint{},
		Tracks:   []SnapshotTrack{},
		Switches: []SnapshotSwitch{},
		Signals:  []SnapshotSignal{},
		Stations: []SnapshotStation{},
	}

	seen := map[string]ids.PointID{}
	for _, id := range slices.SortedFunc(maps.Keys(w.TrackGraph.points), ids.PointID.Compare) {
		pt := w.TrackGraph.points[id]
		if other, ok := seen[id.String()]; ok {
			return nil, fmt.Errorf("points %+v and %+v both have the id %q", other, id, id.String())
		}
		seen[id.String()] = id
		s.Points = append(s.Points, SnapshotPoint{
			ID: id.String(), Stn: string(id.StnCode), Type: id.Type, Seq: id.Seq, Other: id.Other,
			DeadEnd: pt.IsDeadEnd, Boundary: pt.IsSimBoundary,
		})
	}
	for _, trk := range w.TrackGraph.ListTracks() {
		e := w.TrackGraph.Edges[trk.Id]
		s.Tracks = append(s.Tracks, SnapshotTrack{
			ID: trk.Id.String(), Name: trk.Name, From: e.From.Id.String(), To: e.To.Id.String(),
			LengthM: float64(trk.Length), MaxKmph: trk.MaxSpeed.InKMPH(), Lanes: e.Lanes, Direction: string(trk.Direction),
		})
	}
	blocks := slices.SortedFunc(maps.Values(w.switchBlocks), func(a, b *SwitchBlock) int { return cmp.Compare(a.seq, b.seq) })
	for _, sb := range blocks {
		sw := SnapshotSwitch{ID: sb.Id, Default: sb.defaultEdge.Track.Id.String()}
		for _, e := range sb.managedEdges {
			sw.Tracks = append(sw.Tracks, e.Track.Id.String())
		}
		s.Switches = append(s.Switches, sw)
	}
	for _, sig := range w.ListSignals() {
		s.Signals = append(s.Signals, SnapshotSignal{ID: sig.Id, At: sig.AtPoint.Id.String(), Facing: sig.FacingPoint.Id.String()})
	}
	for _, stn := range w.ListStations() {
		st := SnapshotStation{Code: string(stn.Code), Name: stn.Name, Platforms: []SnapshotPlatform{}}
		for _, pf := range stn.Platforms() {
			st.Platforms = append(st.Platforms, SnapshotPlatform{No: pf.PfNo, Track: pf.Track.Id.String(), LengthM: float64(pf.Length)})
		}
		s.Stations = append(s.Stations, st)
	}
	return s, nil
}

// Build makes the world the snapshot describes and finalizes it. It reports
// everything wrong with the snapshot it can find, rather than stopping at the
// first.
func (s *Snapshot) Build() (*World, error) {
	if s.Version != SnapshotVersion {
		return nil, fmt.Errorf("snapshot version %d, want %d", s.Version, SnapshotVersion)
	}
	d := s.Defaults
	w := &World{}
	w.Init(WorldData{
		DefaultSwMaxSpeed: units.KMPH(d.SwitchKmph),
		DefaultSwLength:   units.M(d.SwitchLengthM),
		DefaultPfMaxSpeed: units.KMPH(d.PlatformKmph),
		DefaultPfLength:   units.M(d.PlatformLengthM),
		DefaultRouteSpeed: units.KMPH(d.RouteKmph),
		Up:                d.Up,
	})

	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	points := map[string]*TrackPoint{}
	for i, p := range s.Points {
		id := ids.PointID{StnCode: ids.StnCode(p.Stn), Type: p.Type, Seq: p.Seq, Other: p.Other}
		switch {
		case id.IsZero():
			fail("point %d: no stn, type, seq or other", i)
		case p.ID != "" && p.ID != id.String():
			fail("point %d: id %q, but its parts make %q", i, p.ID, id.String())
		case points[id.String()] != nil:
			fail("point %s: listed twice", id)
		default:
			points[id.String()] = w.NewTrackPoint(id).WithDeadEnd(p.DeadEnd).WithSimLimit(p.Boundary)
		}
	}
	point := func(what, id string) *TrackPoint {
		pt, ok := points[id]
		if !ok {
			fail("%s: no point %q", what, id)
		}
		return pt
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	edges := map[string]*GraphEdge{}
	for i, t := range s.Tracks {
		what := fmt.Sprintf("track %d (%s)", i, cmp.Or(t.ID, t.From+"-"+t.To))
		from, to := point(what, t.From), point(what, t.To)
		if from == nil || to == nil {
			continue
		}
		id := ids.NewTrackID(from.Id, to.Id).String()
		switch dir := TrackDirection(t.Direction); {
		case from == to:
			fail("%s: runs from a point to itself", what)
		case t.ID != "" && t.ID != id:
			fail("%s: id %q, but its points make %q", what, t.ID, id)
		case edges[id] != nil:
			fail("%s: listed twice", what)
		case !(t.LengthM >= 0):
			fail("%s: length_m must not be negative", what)
		case !(t.MaxKmph > 0):
			fail("%s: max_kmph must be positive", what)
		case dir != "" && dir != Up && dir != Down && dir != Bidir:
			fail("%s: direction %q, want %s, %s or %s", what, t.Direction, Up, Down, Bidir)
		default:
			seg := w.NewTrackSegment(t.Name, units.M(t.LengthM))
			seg.MaxSpeed = units.KMPH(t.MaxKmph)
			seg.Direction = dir
			e := w.TrackGraph.AddTrack(from, to, seg)
			e.Lanes = t.Lanes
			edges[id] = e
		}
	}
	edge := func(what, id string) *GraphEdge {
		e, ok := edges[id]
		if !ok {
			fail("%s: no track %q", what, id)
		}
		return e
	}

	for _, sw := range s.Switches {
		what := "switch " + sw.ID
		if sw.ID == "" {
			fail("switch with no id")
			continue
		}
		if _, ok := w.switchBlocks[sw.ID]; ok {
			fail("%s: listed twice", what)
			continue
		}
		if len(sw.Tracks) < 2 {
			fail("%s: needs at least two tracks", what)
			continue
		}
		var managed []*GraphEdge
		ok := true
		for _, id := range sw.Tracks {
			e := edge(what, id)
			if e != nil && slices.Contains(managed, e) {
				fail("%s: track %s listed twice", what, id)
			}
			ok = ok && e != nil
			managed = append(managed, e)
		}
		def := edge(what+" default", sw.Default)
		if def != nil && ok && !slices.Contains(managed, def) {
			fail("%s: default %s is not one of its tracks", what, sw.Default)
			continue
		}
		if ok && def != nil {
			w.newSwitchBlock(sw.ID, managed, def)
		}
	}

	for _, sig := range s.Signals {
		what := "signal " + sig.ID
		if sig.ID == "" {
			fail("signal with no id")
			continue
		}
		if _, ok := w.signals[sig.ID]; ok {
			fail("%s: listed twice", what)
			continue
		}
		at, facing := point(what, sig.At), point(what, sig.Facing)
		if at != nil && facing != nil {
			w.AddSignal(NewSignal(sig.ID, at, facing))
		}
	}

	for _, st := range s.Stations {
		what := "station " + st.Code
		if st.Code == "" {
			fail("station with no code")
			continue
		}
		if _, ok := w.stations[ids.StnCode(st.Code)]; ok {
			fail("%s: listed twice", what)
			continue
		}
		stn := w.NewStation(ids.StnCode(st.Code), st.Name)
		for _, pf := range st.Platforms {
			what := fmt.Sprintf("%s platform %s", what, pf.No)
			e := edge(what, pf.Track)
			switch {
			case pf.No == "":
				fail("%s: platform with no number", what)
			case e == nil:
			case stn.FindStnPlatform(pf.No) != nil:
				fail("%s: listed twice", what)
			case stn.IsPlatform(e.Track.Id):
				fail("%s: track %s is already one of its platforms", what, pf.Track)
			case !(pf.LengthM >= 0):
				fail("%s: length_m must not be negative", what)
			default:
				stn.NewStationPlatform(e.Track, pf.No, units.M(pf.LengthM))
			}
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if err := w.Finalize(); err != nil {
		return nil, err
	}
	return w, nil
}

// LoadWorld reads a world snapshot from a JSON file and builds it.
func LoadWorld(path string) (*World, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var s Snapshot
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	w, err := s.Build()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return w, nil
}

// SaveWorld writes the world's snapshot to a JSON file.
func SaveWorld(path string, w *World) error {
	s, err := w.Snapshot()
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return errors.Join(enc.Encode(s), f.Close())
}
