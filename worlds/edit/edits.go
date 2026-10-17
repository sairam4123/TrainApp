package edit

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/visualization/schematic"
)

// The edits. Each takes the snapshot and what is selected, changes the
// snapshot in place and returns an error, changing nothing, if it cannot be
// done. Run them through Doc.Do, which also refuses one that leaves a world
// that cannot be drawn.
//
// New points are named {code, "Ed", n}: the code of the station whose area
// they join, or none for a point out on the line. A track's id comes from
// its points, so a point that changes code (see GatherStation) changes the
// ids of its tracks, and everything naming them follows.

// Ends of the world a point can be (End).
const (
	EndOpen     = "open"     // track may go on
	EndBuffer   = "buffer"   // a buffer stop: a dead end
	EndBoundary = "boundary" // the edge of the simulated world: trains enter and leave here
)

// Extend adds a track and a new point to the east of at (or the west),
// lengthM long at kmph (0 for the world's route speed), the new point lanes
// lanes below at (negative is above). The point must have no track that way
// already: a second one is a switch (AddSwitch). An end of the world there
// is opened up. It returns the new point.
func Extend(s *infra.Snapshot, at string, east bool, lengthM, kmph float64, lanes int) (string, error) {
	i := pointIndex(s, at)
	if i < 0 {
		return "", fmt.Errorf("no point %s", at)
	}
	if west, eastward := tracksAt(s, at); (east && len(eastward) > 0) || (!east && len(west) > 0) {
		return "", fmt.Errorf("%s already has a track %s: add a switch to branch off it", at, side(east))
	}
	if !(lengthM > 0) {
		return "", errors.New("a track needs a length")
	}
	s.Points[i].DeadEnd, s.Points[i].Boundary = false, false
	np := newPoint(s, "", "Ed")
	t := infra.SnapshotTrack{LengthM: lengthM, MaxKmph: cmp.Or(kmph, s.Defaults.RouteKmph), From: at, To: np, Lanes: lanes}
	if !east {
		t.From, t.To, t.Lanes = np, at, -lanes
	}
	addTrack(s, t)
	return np, nil
}

// Split puts a new point in a track, westM from its west end, and returns
// it. The halves share the length; the west half keeps the track's name and
// any lane change, and signals at either end stay where they are. A switch
// leg or a platform cannot be split.
func Split(s *infra.Snapshot, track string, westM float64) (string, error) {
	i := trackIndex(s, track)
	if i < 0 {
		return "", fmt.Errorf("no track %s", track)
	}
	t := s.Tracks[i]
	switch {
	case inSwitch(s, track):
		return "", fmt.Errorf("%s is a switch leg: delete the switch first", trackName(t))
	case platformOf(s, track) != "":
		return "", fmt.Errorf("%s is a platform of %s: take the platform off first", trackName(t), platformOf(s, track))
	case !(westM > 0 && westM < t.LengthM):
		return "", fmt.Errorf("split %s between 0 and %g m from its west end", trackName(t), t.LengthM)
	}
	np := newPoint(s, commonCode(s, t.From, t.To), "Ed")
	west, east := t, t
	west.To, west.LengthM = np, westM
	east.From, east.LengthM, east.Lanes = np, t.LengthM-westM, 0
	if t.Name != "" {
		east.Name = t.Name + "b"
	}
	s.Tracks = slices.Delete(s.Tracks, i, i+1)
	addTrack(s, west)
	addTrack(s, east)
	for k := range s.Signals {
		sig := &s.Signals[k]
		switch {
		case sig.At == t.To && sig.Facing == t.From:
			sig.Facing = np
		case sig.At == t.From && sig.Facing == t.To:
			sig.Facing = np
		}
	}
	return np, nil
}

// Join takes a point out of the line it is on: it must have one track each
// side, neither a switch leg nor a platform. The two become one, their
// lengths added and lane changes kept, at the lower of their speeds.
// Signals standing at the point go with it.
func Join(s *infra.Snapshot, at string) error {
	west, east := tracksAt(s, at)
	if len(west) != 1 || len(east) != 1 {
		return fmt.Errorf("%s needs one track each side to join them", at)
	}
	w, e := s.Tracks[west[0]], s.Tracks[east[0]]
	for _, t := range []infra.SnapshotTrack{w, e} {
		if inSwitch(s, t.ID) || platformOf(s, t.ID) != "" {
			return fmt.Errorf("%s is a switch leg or a platform", trackName(t))
		}
	}
	joined := w
	joined.To, joined.LengthM, joined.Lanes = e.To, w.LengthM+e.LengthM, w.Lanes+e.Lanes
	joined.MaxKmph = min(w.MaxKmph, e.MaxKmph)
	joined.Name = cmp.Or(w.Name, e.Name)
	removeTracks(s, w.ID, e.ID)
	addTrack(s, joined)
	s.Signals = slices.DeleteFunc(s.Signals, func(sig infra.SnapshotSignal) bool { return sig.At == at })
	for k := range s.Signals {
		sig := &s.Signals[k]
		if sig.Facing == at && sig.At == w.From {
			sig.Facing = e.To
		}
		if sig.Facing == at && sig.At == e.To {
			sig.Facing = w.From
		}
	}
	removePoint(s, at)
	return nil
}

// AddSwitch makes toe a switch: a diverging leg leaves it to the east (or
// the west) for a new point lanes lanes below (negative is above), beside
// the one track already going that way. That track is the straight leg; a
// long one is split, so the leg is the world's switch length. It returns the
// diverging leg's new point.
func AddSwitch(s *infra.Snapshot, toe string, east bool, lanes int) (string, error) {
	if pointIndex(s, toe) < 0 {
		return "", fmt.Errorf("no point %s", toe)
	}
	if lanes == 0 {
		return "", errors.New("the diverging leg must move at least one lane")
	}
	west, eastward := tracksAt(s, toe)
	on := eastward
	if !east {
		on = west
	}
	if len(on) != 1 {
		return "", fmt.Errorf("%s needs exactly one track %s to be the switch's straight leg (it has %d)", toe, side(east), len(on))
	}
	straight := s.Tracks[on[0]]
	if platformOf(s, straight.ID) != "" {
		return "", fmt.Errorf("the track %s of %s is a platform: extend a short track first", side(east), toe)
	}
	swLen := cmp.Or(s.Defaults.SwitchLengthM, 100)
	if !inSwitch(s, straight.ID) && straight.LengthM > 2*swLen {
		at := swLen
		if !east {
			at = straight.LengthM - swLen
		}
		np, err := Split(s, straight.ID, at)
		if err != nil {
			return "", err
		}
		if east {
			straight = s.Tracks[trackBetween(s, toe, np)]
		} else {
			straight = s.Tracks[trackBetween(s, np, toe)]
		}
	}
	code := pointCode(s, toe)
	div := newPoint(s, code, "Ed")
	leg := infra.SnapshotTrack{From: toe, To: div, Lanes: lanes, LengthM: swLen, MaxKmph: cmp.Or(s.Defaults.SwitchKmph, 30)}
	if !east {
		leg.From, leg.To, leg.Lanes = div, toe, -lanes
	}
	addTrack(s, leg)
	leg = s.Tracks[len(s.Tracks)-1]
	s.Switches = append(s.Switches, infra.SnapshotSwitch{
		ID: newID(s, ids.PointID{StnCode: ids.StnCode(code), Type: "EdSw"}), Tracks: []string{straight.ID, leg.ID}, Default: straight.ID,
	})
	return div, nil
}

// Crossover lays a crossover between two parallel plain tracks on different
// lanes: each is split in the middle, a diagonal joins the two new points
// and a switch at each end of it lets a train over. With down the diagonal
// runs from the upper track down to the lower one going east, else up.
func Crossover(s *infra.Snapshot, a, b string, down bool) error {
	if a == b {
		return errors.New("a crossover needs two tracks")
	}
	d, err := schematic.FromSnapshot(s)
	if err != nil {
		return err
	}
	ta, tb := trackIndex(s, a), trackIndex(s, b)
	if ta < 0 || tb < 0 {
		return errors.New("a crossover needs two tracks")
	}
	upper, lower := s.Tracks[ta], s.Tracks[tb]
	for _, t := range []infra.SnapshotTrack{upper, lower} {
		if t.Lanes != 0 || inSwitch(s, t.ID) || platformOf(s, t.ID) != "" {
			return fmt.Errorf("%s must be a plain level track, not a switch leg, a platform or a diagonal", trackName(t))
		}
	}
	la, _ := d.Lane(upper.From)
	lb, _ := d.Lane(lower.From)
	if la == lb {
		return errors.New("the two tracks are on the same lane")
	}
	if la > lb {
		upper, lower, la, lb = lower, upper, lb, la
	}
	pu, err := Split(s, upper.ID, upper.LengthM/2)
	if err != nil {
		return err
	}
	pl, err := Split(s, lower.ID, lower.LengthM/2)
	if err != nil {
		return err
	}
	from, to, lanes := pu, pl, lb-la
	if !down {
		from, to, lanes = pl, pu, la-lb
	}
	swLen := cmp.Or(s.Defaults.SwitchLengthM, 100)
	addTrack(s, infra.SnapshotTrack{From: from, To: to, Lanes: lanes, LengthM: swLen, MaxKmph: cmp.Or(s.Defaults.SwitchKmph, 30)})
	diag := s.Tracks[len(s.Tracks)-1].ID
	// at the west end the legs leave eastwards, at the east end they arrive
	_, eastOfFrom := tracksAt(s, from)
	westOfTo, _ := tracksAt(s, to)
	var legW, legE string
	for _, k := range eastOfFrom {
		if s.Tracks[k].ID != diag {
			legW = s.Tracks[k].ID
		}
	}
	for _, k := range westOfTo {
		if s.Tracks[k].ID != diag {
			legE = s.Tracks[k].ID
		}
	}
	code := commonCode(s, from, to)
	for _, leg := range []string{legW, legE} {
		s.Switches = append(s.Switches, infra.SnapshotSwitch{
			ID: newID(s, ids.PointID{StnCode: ids.StnCode(code), Type: "EdSw"}), Tracks: []string{leg, diag}, Default: leg,
		})
	}
	return nil
}

// ToggleSignal adds a signal at the east end of a track for trains running
// east on it (or at the west end for trains running west), or takes away
// the one there.
func ToggleSignal(s *infra.Snapshot, track string, east bool) error {
	i := trackIndex(s, track)
	if i < 0 {
		return fmt.Errorf("no track %s", track)
	}
	t := s.Tracks[i]
	at, facing, dir := t.To, t.From, "E"
	if !east {
		at, facing, dir = t.From, t.To, "W"
	}
	for k, sig := range s.Signals {
		if sig.At == at && sig.Facing == facing {
			s.Signals = slices.Delete(s.Signals, k, k+1)
			return nil
		}
	}
	id := at + "Sig" + dir
	for n := 2; signalIndex(s, id) >= 0; n++ {
		id = at + "Sig" + dir + strconv.Itoa(n)
	}
	s.Signals = append(s.Signals, infra.SnapshotSignal{ID: id, At: at, Facing: facing})
	return nil
}

// BlockSection joins two open points with a single line lengthM long, split
// into blocks equal blocks, signalled both ways at every block boundary and
// both ends, as worlds/templates.SingleLine builds it: points {name, Bk, i},
// tracks name0, name1, ... and signals nameSig0F, nameSig0R, ... a and b may
// be given either way round; the line runs east from whichever of them has
// no track east. Ends of the world at either are opened up.
func BlockSection(s *infra.Snapshot, a, b, name string, lengthM float64, blocks int, kmph float64) error {
	if pointIndex(s, a) < 0 || pointIndex(s, b) < 0 || a == b {
		return errors.New("a block section joins two points")
	}
	if name == "" {
		return errors.New("a block section needs a name")
	}
	if !(lengthM > 0) || blocks < 1 {
		return errors.New("a block section needs a length and at least one block")
	}
	from, to := a, b
	switch {
	case open(s, a, true) && open(s, b, false):
	case open(s, b, true) && open(s, a, false):
		from, to = b, a
	default:
		return fmt.Errorf("one of %s and %s needs no track east, the other no track west", a, b)
	}
	for _, sig := range s.Signals {
		if sig.ID == name+"Sig0F" {
			return fmt.Errorf("the name %s is taken", name)
		}
	}
	lanes := 0
	if d, err := schematic.FromSnapshot(s); err == nil && connected(s, from, to) {
		lf, _ := d.Lane(from)
		lt, _ := d.Lane(to)
		lanes = lt - lf
	}
	for _, p := range []string{from, to} {
		k := pointIndex(s, p)
		s.Points[k].DeadEnd, s.Points[k].Boundary = false, false
	}
	pts := []string{from}
	for i := 1; i < blocks; i++ {
		p := infra.SnapshotPoint{Stn: name, Type: "Bk", Seq: strconv.Itoa(i)}
		if pointIndex(s, pointID(p).String()) >= 0 {
			return fmt.Errorf("the name %s is taken", name)
		}
		p.ID = pointID(p).String()
		s.Points = append(s.Points, p)
		pts = append(pts, p.ID)
	}
	pts = append(pts, to)
	kmph = cmp.Or(kmph, s.Defaults.RouteKmph)
	for i := 0; i+1 < len(pts); i++ {
		t := infra.SnapshotTrack{Name: fmt.Sprintf("%s%d", name, i), From: pts[i], To: pts[i+1], LengthM: lengthM / float64(blocks), MaxKmph: kmph}
		if i == 0 {
			t.Lanes = lanes
		}
		addTrack(s, t)
		s.Signals = append(s.Signals,
			infra.SnapshotSignal{ID: fmt.Sprintf("%sSig%dF", name, i), At: pts[i+1], Facing: pts[i]},
			infra.SnapshotSignal{ID: fmt.Sprintf("%sSig%dR", name, i), At: pts[i], Facing: pts[i+1]},
		)
	}
	return nil
}

// SetEnd makes a point an end of the world (EndBuffer, EndBoundary) or not
// (EndOpen).
func SetEnd(s *infra.Snapshot, at, kind string) error {
	i := pointIndex(s, at)
	if i < 0 {
		return fmt.Errorf("no point %s", at)
	}
	p := &s.Points[i]
	switch kind {
	case EndOpen:
		p.DeadEnd, p.Boundary = false, false
	case EndBuffer:
		p.DeadEnd, p.Boundary = true, false
	case EndBoundary:
		p.DeadEnd, p.Boundary = true, true
	default:
		return fmt.Errorf("end %q, want %s, %s or %s", kind, EndOpen, EndBuffer, EndBoundary)
	}
	return nil
}

// SetPlatform makes a track platform no of a station, lengthM long (0 for a
// through track), moving it from any other station; an empty no takes the
// platform off. The track's ends, and the throats reached from them over
// switches, are taken into the station's area (GatherStation).
func SetPlatform(s *infra.Snapshot, track, code, no string, lengthM float64) error {
	if trackIndex(s, track) < 0 {
		return fmt.Errorf("no track %s", track)
	}
	for i := range s.Stations {
		s.Stations[i].Platforms = slices.DeleteFunc(s.Stations[i].Platforms, func(p infra.SnapshotPlatform) bool { return p.Track == track })
	}
	if no == "" {
		return nil
	}
	if inSwitch(s, track) {
		return errors.New("a switch leg cannot be a platform")
	}
	if !(lengthM >= 0) {
		return errors.New("a platform's length must not be negative")
	}
	k := stationIndex(s, code)
	if k < 0 {
		return fmt.Errorf("no station %s: add it first", code)
	}
	for _, p := range s.Stations[k].Platforms {
		if p.No == no {
			return fmt.Errorf("%s already has a platform %s", code, no)
		}
	}
	s.Stations[k].Platforms = append(s.Stations[k].Platforms, infra.SnapshotPlatform{No: no, Track: track, LengthM: lengthM})
	return GatherStation(s, code)
}

// SetStation adds a station, or renames one.
func SetStation(s *infra.Snapshot, code, name string) error {
	if code == "" {
		return errors.New("a station needs a code")
	}
	if k := stationIndex(s, code); k >= 0 {
		s.Stations[k].Name = name
		return nil
	}
	s.Stations = append(s.Stations, infra.SnapshotStation{Code: code, Name: name, Platforms: []infra.SnapshotPlatform{}})
	slices.SortFunc(s.Stations, func(a, b infra.SnapshotStation) int { return cmp.Compare(a.Code, b.Code) })
	return nil
}

// GatherStation takes into the station's area every point its platform
// tracks reach over platform tracks and switch legs: the platforms' ends and
// the throats. A station's area is the tracks both of whose points carry its
// code (infra/station_tracks.go), so each such point is renamed to carry it.
func GatherStation(s *infra.Snapshot, code string) error {
	k := stationIndex(s, code)
	if k < 0 {
		return fmt.Errorf("no station %s", code)
	}
	via := map[string]bool{}
	var todo []string
	for _, p := range s.Stations[k].Platforms {
		via[p.Track] = true
		if i := trackIndex(s, p.Track); i >= 0 {
			todo = append(todo, s.Tracks[i].From, s.Tracks[i].To)
		}
	}
	for _, sw := range s.Switches {
		for _, t := range sw.Tracks {
			via[t] = true
		}
	}
	seen := map[string]bool{}
	var area []string
	for len(todo) > 0 {
		p := todo[0]
		todo = todo[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		area = append(area, p)
		for _, t := range s.Tracks {
			if via[t.ID] && (t.From == p || t.To == p) {
				todo = append(todo, t.From, t.To)
			}
		}
	}
	for _, p := range area {
		if pointCode(s, p) == code {
			continue
		}
		if _, err := Recode(s, p, code); err != nil {
			return err
		}
	}
	return nil
}

// Recode gives a point another station code, keeping the rest of its id
// (with another seq if that id is taken), and returns its new id. Its
// tracks' ids change with it, and everything naming them follows.
func Recode(s *infra.Snapshot, at, code string) (string, error) {
	i := pointIndex(s, at)
	if i < 0 {
		return "", fmt.Errorf("no point %s", at)
	}
	p := s.Points[i]
	p.Stn = code
	seq := p.Seq
	for n := 1; pointID(p).String() != at && pointIndex(s, pointID(p).String()) >= 0; n++ {
		p.Seq = seq + "x" + strconv.Itoa(n)
	}
	p.ID = pointID(p).String()
	renamePoint(s, at, p)
	return p.ID, nil
}

// Delete takes a piece out with whatever hangs off it alone: a point with
// its tracks; a track with the switches it is a leg of, the signals on it
// and its platform, and either end it leaves with no track; a signal; a
// station (its tracks stay).
func Delete(s *infra.Snapshot, p schematic.Piece) error {
	switch p.Kind {
	case schematic.PiecePoint:
		if pointIndex(s, p.ID) < 0 {
			return fmt.Errorf("no point %s", p.ID)
		}
		w, e := tracksAt(s, p.ID)
		var gone []string
		for _, k := range append(w, e...) {
			gone = append(gone, s.Tracks[k].ID)
		}
		for _, t := range gone {
			deleteTrack(s, t)
		}
		removePoint(s, p.ID)
	case schematic.PieceTrack:
		if trackIndex(s, p.ID) < 0 {
			return fmt.Errorf("no track %s", p.ID)
		}
		deleteTrack(s, p.ID)
	case schematic.PieceSignal:
		k := signalIndex(s, p.ID)
		if k < 0 {
			return fmt.Errorf("no signal %s", p.ID)
		}
		s.Signals = slices.Delete(s.Signals, k, k+1)
	case schematic.PieceStation:
		k := stationIndex(s, p.ID)
		if k < 0 {
			return fmt.Errorf("no station %s", p.ID)
		}
		s.Stations = slices.Delete(s.Stations, k, k+1)
	default:
		return errors.New("nothing to delete")
	}
	return nil
}

// TrackAttrs are what can be set on a track.
type TrackAttrs struct {
	Name      string
	LengthM   float64
	MaxKmph   float64
	Direction string // empty, UP, DOWN or BIDIR
	Lanes     int
}

// SetTrack sets a track's attributes.
func SetTrack(s *infra.Snapshot, track string, a TrackAttrs) error {
	i := trackIndex(s, track)
	if i < 0 {
		return fmt.Errorf("no track %s", track)
	}
	switch d := infra.TrackDirection(a.Direction); {
	case !(a.LengthM >= 0):
		return errors.New("length must not be negative")
	case !(a.MaxKmph > 0):
		return errors.New("speed must be positive")
	case d != "" && d != infra.Up && d != infra.Down && d != infra.Bidir:
		return fmt.Errorf("direction %q, want %s, %s, %s or none", a.Direction, infra.Up, infra.Down, infra.Bidir)
	}
	t := &s.Tracks[i]
	t.Name, t.LengthM, t.MaxKmph, t.Direction, t.Lanes = a.Name, a.LengthM, a.MaxKmph, a.Direction, a.Lanes
	return nil
}

// SetDefaults sets the world's defaults.
func SetDefaults(s *infra.Snapshot, d infra.SnapshotDefaults) error {
	if !(d.SwitchKmph > 0 && d.PlatformKmph > 0 && d.RouteKmph > 0) {
		return errors.New("speeds must be positive")
	}
	if !(d.SwitchLengthM > 0 && d.PlatformLengthM > 0) {
		return errors.New("lengths must be positive")
	}
	s.Defaults = d
	return nil
}

// AddPoint adds a point with no track, e.g. to start a world from; it
// returns its id.
func AddPoint(s *infra.Snapshot) string { return newPoint(s, "", "Ed") }

// helpers

func pointIndex(s *infra.Snapshot, id string) int {
	return slices.IndexFunc(s.Points, func(p infra.SnapshotPoint) bool { return pointID(p).String() == id })
}

func trackIndex(s *infra.Snapshot, id string) int {
	return slices.IndexFunc(s.Tracks, func(t infra.SnapshotTrack) bool { return t.ID == id })
}

func signalIndex(s *infra.Snapshot, id string) int {
	return slices.IndexFunc(s.Signals, func(sig infra.SnapshotSignal) bool { return sig.ID == id })
}

func stationIndex(s *infra.Snapshot, code string) int {
	return slices.IndexFunc(s.Stations, func(st infra.SnapshotStation) bool { return st.Code == code })
}

func trackBetween(s *infra.Snapshot, from, to string) int {
	return slices.IndexFunc(s.Tracks, func(t infra.SnapshotTrack) bool { return t.From == from && t.To == to })
}

// tracksAt lists the tracks that end at a point from the west (they run to
// it) and those that leave it to the east, by index.
func tracksAt(s *infra.Snapshot, at string) (west, east []int) {
	for i, t := range s.Tracks {
		if t.To == at {
			west = append(west, i)
		}
		if t.From == at {
			east = append(east, i)
		}
	}
	return west, east
}

// open reports whether a point has no track to the east (or west).
func open(s *infra.Snapshot, at string, east bool) bool {
	w, e := tracksAt(s, at)
	if east {
		return len(e) == 0
	}
	return len(w) == 0
}

// connected reports whether two points are joined by track.
func connected(s *infra.Snapshot, a, b string) bool {
	seen := map[string]bool{a: true}
	for todo := []string{a}; len(todo) > 0; todo = todo[1:] {
		for _, t := range s.Tracks {
			for _, pair := range [][2]string{{t.From, t.To}, {t.To, t.From}} {
				if pair[0] == todo[0] && !seen[pair[1]] {
					if pair[1] == b {
						return true
					}
					seen[pair[1]] = true
					todo = append(todo, pair[1])
				}
			}
		}
	}
	return false
}

func inSwitch(s *infra.Snapshot, track string) bool {
	for _, sw := range s.Switches {
		if slices.Contains(sw.Tracks, track) {
			return true
		}
	}
	return false
}

// platformOf is the code of the station the track is a platform of, if any.
func platformOf(s *infra.Snapshot, track string) string {
	for _, st := range s.Stations {
		for _, p := range st.Platforms {
			if p.Track == track {
				return st.Code
			}
		}
	}
	return ""
}

func pointCode(s *infra.Snapshot, at string) string {
	if i := pointIndex(s, at); i >= 0 {
		return s.Points[i].Stn
	}
	return ""
}

// commonCode is the code two points share, if they do: a point put between
// them belongs where they both do.
func commonCode(s *infra.Snapshot, a, b string) string {
	if ca := pointCode(s, a); ca == pointCode(s, b) {
		return ca
	}
	return ""
}

func trackName(t infra.SnapshotTrack) string { return cmp.Or(t.Name, t.ID) }

func side(east bool) string {
	if east {
		return "east"
	}
	return "west"
}

// newPoint adds a point {code, typ, n} with the lowest n not taken, and
// returns its id.
func newPoint(s *infra.Snapshot, code, typ string) string {
	p := infra.SnapshotPoint{Stn: code, Type: typ}
	for n := 1; ; n++ {
		p.Seq = strconv.Itoa(n)
		if pointIndex(s, pointID(p).String()) < 0 {
			break
		}
	}
	p.ID = pointID(p).String()
	s.Points = append(s.Points, p)
	return p.ID
}

// newID is an id {code, typ, n} for a switch, with the lowest n not taken.
func newID(s *infra.Snapshot, base ids.PointID) string {
	for n := 1; ; n++ {
		base.Seq = strconv.Itoa(n)
		id := base.String()
		if !slices.ContainsFunc(s.Switches, func(sw infra.SnapshotSwitch) bool { return sw.ID == id }) {
			return id
		}
	}
}

// addTrack appends a track, its id worked out from its points.
func addTrack(s *infra.Snapshot, t infra.SnapshotTrack) {
	from, to := s.Points[pointIndex(s, t.From)], s.Points[pointIndex(s, t.To)]
	t.ID = ids.NewTrackID(pointID(from), pointID(to)).String()
	s.Tracks = append(s.Tracks, t)
}

// removeTracks takes tracks out of the track list only.
func removeTracks(s *infra.Snapshot, tracks ...string) {
	s.Tracks = slices.DeleteFunc(s.Tracks, func(t infra.SnapshotTrack) bool { return slices.Contains(tracks, t.ID) })
}

func removePoint(s *infra.Snapshot, at string) {
	s.Points = slices.DeleteFunc(s.Points, func(p infra.SnapshotPoint) bool { return pointID(p).String() == at })
}

// deleteTrack takes a track out, with the switches it is a leg of, the
// signals on it, its platform and any end left with no track.
func deleteTrack(s *infra.Snapshot, track string) {
	i := trackIndex(s, track)
	if i < 0 {
		return
	}
	t := s.Tracks[i]
	removeTracks(s, track)
	s.Switches = slices.DeleteFunc(s.Switches, func(sw infra.SnapshotSwitch) bool { return slices.Contains(sw.Tracks, track) })
	s.Signals = slices.DeleteFunc(s.Signals, func(sig infra.SnapshotSignal) bool {
		return (sig.At == t.From && sig.Facing == t.To) || (sig.At == t.To && sig.Facing == t.From)
	})
	for k := range s.Stations {
		s.Stations[k].Platforms = slices.DeleteFunc(s.Stations[k].Platforms, func(p infra.SnapshotPlatform) bool { return p.Track == track })
	}
	for _, end := range []string{t.From, t.To} {
		if w, e := tracksAt(s, end); len(w)+len(e) == 0 {
			removePoint(s, end)
			s.Signals = slices.DeleteFunc(s.Signals, func(sig infra.SnapshotSignal) bool { return sig.At == end || sig.Facing == end })
		}
	}
}

// renamePoint gives the point at old the id and parts of p, and moves
// everything that names it, or a track of it, over.
func renamePoint(s *infra.Snapshot, old string, p infra.SnapshotPoint) {
	i := pointIndex(s, old)
	s.Points[i] = p
	newIDs := map[string]string{} // old track id -> new
	for k := range s.Tracks {
		t := &s.Tracks[k]
		if t.From != old && t.To != old {
			continue
		}
		if t.From == old {
			t.From = p.ID
		}
		if t.To == old {
			t.To = p.ID
		}
		from, to := s.Points[pointIndex(s, t.From)], s.Points[pointIndex(s, t.To)]
		was := t.ID
		t.ID = ids.NewTrackID(pointID(from), pointID(to)).String()
		newIDs[was] = t.ID
	}
	for k := range s.Switches {
		sw := &s.Switches[k]
		for j, t := range sw.Tracks {
			if n, ok := newIDs[t]; ok {
				sw.Tracks[j] = n
			}
		}
		if n, ok := newIDs[sw.Default]; ok {
			sw.Default = n
		}
	}
	for k := range s.Signals {
		if s.Signals[k].At == old {
			s.Signals[k].At = p.ID
		}
		if s.Signals[k].Facing == old {
			s.Signals[k].Facing = p.ID
		}
	}
	for k := range s.Stations {
		for j, pf := range s.Stations[k].Platforms {
			if n, ok := newIDs[pf.Track]; ok {
				s.Stations[k].Platforms[j].Track = n
			}
		}
	}
}

// mergePoint folds the point from into into: every track and signal at from
// moves to into, and from goes.
func mergePoint(s *infra.Snapshot, from, into string) {
	p := s.Points[pointIndex(s, into)]
	removePoint(s, into)
	renamePoint(s, from, p)
}
