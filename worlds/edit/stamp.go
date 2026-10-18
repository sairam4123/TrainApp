package edit

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
	"trainapp/visualization/schematic"
	"trainapp/worlds/templates"
)

// Templates Stamp can build (worlds/templates).
const (
	TemplateHalt     = "halt"     // one platform, no points
	TemplateCrossing = "crossing" // a loop platform either side of a through main line
	TemplateStation  = "station"  // any tracks, top to bottom, and their throats
)

// Templates lists the templates, in the order a form offers them.
var Templates = []string{TemplateHalt, TemplateCrossing, TemplateStation}

// How a stamped station is joined to the world (StampSpec.Join).
const (
	JoinSelected = "selected" // the selected point, to the station's line nearest it
	JoinNone     = "none"     // nothing: lines are drawn to it afterwards
	JoinAll      = "all"      // every open end beside the selected point, a line each
)

// Joins lists the ways to join a stamped station, in the order a form
// offers them.
var Joins = []string{JoinSelected, JoinNone, JoinAll}

// StampSpec is a template to stamp and its parameters.
type StampSpec struct {
	Template   string
	Code, Name string
	// PfLengthM and LineKmph size a halt's or crossing station's platforms
	// and set its main line's speed.
	PfLengthM float64
	LineKmph  float64
	// Tracks, Edge and TurnoutKmph are a station's (templates.Station):
	// its tracks top to bottom, the side that is the world's edge ("W",
	// "E" or none) and the speed through its turnouts' diverging legs.
	Tracks      []templates.Track
	Edge        string
	TurnoutKmph float64
	// WestMains and EastMains name a station's mains on each side, by
	// platform number or siding name; nil for the tracks marked straight
	// (an empty list is no mains).
	// WestCrossovers and EastCrossovers are the crossovers between them
	// (templates.Layout).
	WestMains, EastMains           []string
	WestCrossovers, EastCrossovers []string
	// Join is how the station is joined to the world: JoinSelected (the
	// default), JoinNone or JoinAll.
	Join string
	// The lines from the selected point (or points) to the station: a
	// section LinkM long in LinkBlocks blocks, named LinkName (or made up),
	// running as LinkPattern says (one letter a line, see Lines; empty for
	// DefaultPattern); a LinkM of 0 joins the station's throat to the point
	// itself.
	LinkName    string
	LinkM       float64
	LinkBlocks  int
	LinkPattern string
}

// Stamp builds a template into the snapshot and joins it as spec.Join says
// to the point at, if one is given: the station's west throat to a point
// with no track east, else its east throat to one with no track west. A
// station on several lines has a line point for each of its mains: with
// JoinSelected the point joins the one in the same place among them as it
// is among the open ends beside it, top to bottom; with JoinAll each of
// those open ends joins one, and there must be as many. A station of the
// code with no platforms yet is filled in. It returns the station's code.
func Stamp(s *infra.Snapshot, at string, spec StampSpec) (string, error) {
	if spec.Code == "" {
		return "", errors.New("a station needs a code")
	}
	// a station with no platforms yet, added with its code and name alone,
	// is stamped afresh, keeping its name if the spec gives none
	if k := stationIndex(s, spec.Code); k >= 0 {
		if len(s.Stations[k].Platforms) > 0 {
			return "", fmt.Errorf("there is a station %s already", spec.Code)
		}
		spec.Name = cmp.Or(spec.Name, s.Stations[k].Name)
		s.Stations = slices.Delete(s.Stations, k, k+1)
	}
	scratch := &infra.World{}
	scratch.Init(worldData(s.Defaults))
	code := ids.StnCode(spec.Code)
	var west, east []*infra.TrackPoint
	switch spec.Template {
	case TemplateHalt, TemplateCrossing:
		var w, e *infra.TrackPoint
		var err error
		if spec.Template == TemplateHalt {
			w, e, err = templates.Halt(scratch, code, spec.Name, units.M(cmp.Or(spec.PfLengthM, 400)), cmp.Or(spec.LineKmph, s.Defaults.RouteKmph))
		} else {
			w, e, err = templates.CrossingStation(scratch, code, spec.Name, units.M(cmp.Or(spec.PfLengthM, 450)), cmp.Or(spec.LineKmph, s.Defaults.RouteKmph))
		}
		if err != nil {
			return "", err
		}
		west, east = []*infra.TrackPoint{w}, []*infra.TrackPoint{e}
	case TemplateStation:
		layout := templates.Layout{Edge: spec.Edge, TurnoutKmph: spec.TurnoutKmph, WestCrossovers: spec.WestCrossovers, EastCrossovers: spec.EastCrossovers}
		var err error
		if layout.WestMains, err = trackIndexes(spec.Tracks, spec.WestMains); err != nil {
			return "", fmt.Errorf("west mains: %w", err)
		}
		if layout.EastMains, err = trackIndexes(spec.Tracks, spec.EastMains); err != nil {
			return "", fmt.Errorf("east mains: %w", err)
		}
		if west, east, err = buildPiece(func() ([]*infra.TrackPoint, []*infra.TrackPoint, error) {
			return templates.BuildStation(scratch, code, spec.Name, layout, spec.Tracks...)
		}); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("template %q, want one of %s", spec.Template, strings.Join(Templates, ", "))
	}
	piece, err := scratch.Snapshot()
	if err != nil {
		return "", err
	}
	join := cmp.Or(spec.Join, JoinSelected)
	if !slices.Contains(Joins, join) {
		return "", fmt.Errorf("join %q, want one of %s", spec.Join, strings.Join(Joins, ", "))
	}
	if at == "" || join == JoinNone {
		return spec.Code, merge(s, piece)
	}
	// the open ends the station joins, top to bottom, and its line points
	// they join
	var ends []string
	var mine []*infra.TrackPoint
	stationEast := false // the station lies east of the ends
	switch {
	case pointIndex(s, at) < 0:
		return "", fmt.Errorf("no point %s", at)
	case len(west) > 0 && open(s, at, true):
		ends, mine, stationEast = OpenBeside(s, at, true), west, true
	case len(east) > 0 && open(s, at, false):
		ends, mine = OpenBeside(s, at, false), east
	default:
		return "", fmt.Errorf("%s has track both ways, or none the station can join", at)
	}
	var pairs [][2]string // west, east
	pair := func(end string, p *infra.TrackPoint) {
		if stationEast {
			pairs = append(pairs, [2]string{end, p.Id.String()})
		} else {
			pairs = append(pairs, [2]string{p.Id.String(), end})
		}
	}
	if join == JoinAll {
		if len(ends) != len(mine) {
			return "", fmt.Errorf("%d open ends beside %s, but the station has %d lines that way", len(ends), at, len(mine))
		}
		for k := range ends {
			pair(ends[k], mine[k])
		}
	} else {
		k := min(slices.Index(ends, at), len(mine)-1)
		pair(at, mine[max(k, 0)])
	}
	if err := merge(s, piece); err != nil {
		return "", err
	}
	if spec.LinkM == 0 {
		for _, p := range pairs {
			if stationEast {
				mergePoint(s, p[1], p[0])
			} else {
				mergePoint(s, p[0], p[1])
			}
		}
		return spec.Code, nil
	}
	var ws, es []string
	for _, p := range pairs {
		ws, es = append(ws, p[0]), append(es, p[1])
	}
	name := spec.LinkName
	if name == "" {
		name = "sl"
		if len(pairs) > 1 {
			name = "ml"
		}
		name += title(cmp.Or(pointCode(s, ws[0]), "Ln")) + title(cmp.Or(pointCode(s, es[0]), "Ln"))
		for n, base := 2, name; lineNameTaken(s, LineName(name, 1, len(pairs))); n++ {
			name = base + strconv.Itoa(n)
		}
	}
	pattern := cmp.Or(spec.LinkPattern, DefaultPattern(len(pairs)))
	if join != JoinAll {
		pattern = cmp.Or(spec.LinkPattern, string(WayBoth))
	}
	return spec.Code, Lines(s, ws, es, name, pattern, spec.LinkM, max(spec.LinkBlocks, 1), 0)
}

// OpenBeside lists the open ends that way beside a point, top to bottom: the
// points of its station (or of the open line, if it has none) with no track
// that way, not ends of the world. The point is among them.
func OpenBeside(s *infra.Snapshot, at string, east bool) []string {
	code := pointCode(s, at)
	var out []string
	for _, p := range s.Points {
		if p.ID == at || (p.Stn == code && !p.DeadEnd && open(s, p.ID, east) && !open(s, p.ID, !east)) {
			out = append(out, p.ID)
		}
	}
	lane := map[string]int{}
	if d, err := schematic.FromSnapshot(s); err == nil {
		for _, p := range out {
			lane[p], _ = d.Lane(p)
		}
	}
	slices.SortStableFunc(out, func(a, b string) int { return cmp.Or(cmp.Compare(lane[a], lane[b]), cmp.Compare(a, b)) })
	return out
}

// buildPiece builds a template into a world of its own, the station's line
// points each side. infra panics on a world built wrong; here that is an
// error, so a station drawn wrong in an editor cannot take the editor down.
func buildPiece(build func() (west, east []*infra.TrackPoint, err error)) (west, east []*infra.TrackPoint, err error) {
	defer func() {
		if r := recover(); r != nil {
			west, east, err = nil, nil, fmt.Errorf("the station cannot be built: %v", r)
		}
	}()
	return build()
}

// trackIndexes finds tracks by platform number or siding name: nil for nil
// (the tracks marked straight), none for none.
func trackIndexes(tracks []templates.Track, names []string) ([]int, error) {
	if names == nil {
		return nil, nil
	}
	out := []int{}
	for _, n := range names {
		i := slices.IndexFunc(tracks, func(t templates.Track) bool {
			return t.PfNo == n || (t.Siding != "" && t.Siding == strings.TrimPrefix(n, "~"))
		})
		if i < 0 {
			return nil, fmt.Errorf("no track %s", n)
		}
		out = append(out, i)
	}
	return out, nil
}

// merge adds another snapshot's pieces, refusing any whose id is taken.
func merge(s, piece *infra.Snapshot) error {
	var errs []error
	for _, p := range piece.Points {
		if pointIndex(s, p.ID) >= 0 {
			errs = append(errs, fmt.Errorf("there is a point %s already", p.ID))
		}
	}
	for _, sw := range piece.Switches {
		if slices.ContainsFunc(s.Switches, func(o infra.SnapshotSwitch) bool { return o.ID == sw.ID }) {
			errs = append(errs, fmt.Errorf("there is a switch %s already", sw.ID))
		}
	}
	for _, sig := range piece.Signals {
		if signalIndex(s, sig.ID) >= 0 {
			errs = append(errs, fmt.Errorf("there is a signal %s already", sig.ID))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	s.Points = append(s.Points, piece.Points...)
	s.Tracks = append(s.Tracks, piece.Tracks...)
	s.Switches = append(s.Switches, piece.Switches...)
	s.Signals = append(s.Signals, piece.Signals...)
	s.Stations = append(s.Stations, piece.Stations...)
	slices.SortFunc(s.Stations, func(a, b infra.SnapshotStation) int { return cmp.Compare(a.Code, b.Code) })
	return nil
}

func worldData(d infra.SnapshotDefaults) infra.WorldData {
	return infra.WorldData{
		DefaultSwMaxSpeed: units.KMPH(d.SwitchKmph),
		DefaultSwLength:   units.M(d.SwitchLengthM),
		DefaultPfMaxSpeed: units.KMPH(d.PlatformKmph),
		DefaultPfLength:   units.M(d.PlatformLengthM),
		DefaultRouteSpeed: units.KMPH(d.RouteKmph),
	}
}

// title is a code as it reads inside a name, e.g. "PDKT" -> "Pdkt".
func title(code string) string {
	if code == "" {
		return ""
	}
	s := strings.ToLower(code)
	return strings.ToUpper(s[:1]) + s[1:]
}

// ParseTracks reads a station's tracks, top to bottom, from a line such as
//
//	1:600 9 2:600 5:600* ~Gd1:700 ~Sd1<
//
// Each item is a platform, NO[:LENGTH], or a siding, ~NAME[:LENGTH]. A
// platform with no length is a through track. After it, * makes it a main,
// the straight road a line joins (one track must be; a station on several
// lines has one for each), @KMPH sets its speed, on a siding < or > puts a
// buffer stop at its west or east end, and on a track between two mains ^
// hangs it off the main above, v off the one below and ^v off both (a v
// straight after a siding's name is part of it: give the length first).
func ParseTracks(line string) ([]templates.Track, error) {
	var out []templates.Track
	var errs []error
	for i, item := range strings.Fields(strings.ReplaceAll(line, ",", " ")) {
		t, err := parseTrack(item)
		if err != nil {
			errs = append(errs, fmt.Errorf("track %d (%s): %w", i+1, item, err))
			continue
		}
		out = append(out, t)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("a station needs a track")
	}
	return out, nil
}

func parseTrack(item string) (templates.Track, error) {
	var t templates.Track
	// the flags come last, in any order
	for {
		switch {
		case strings.HasSuffix(item, "*"):
			t.Straight, item = true, item[:len(item)-1]
			continue
		case strings.HasSuffix(item, "<"):
			t.Buffer, item = "W", item[:len(item)-1]
			continue
		case strings.HasSuffix(item, ">"):
			t.Buffer, item = "E", item[:len(item)-1]
			continue
		case strings.HasSuffix(item, "^"):
			t.Join, item = joinWith(t.Join, templates.JoinAbove), item[:len(item)-1]
			continue
		// a v ends a siding's name, unless a length or another flag is before it
		case len(item) > 1 && strings.HasSuffix(item, "v") && strings.ContainsAny(item[len(item)-2:len(item)-1], "0123456789*^<>"):
			t.Join, item = joinWith(t.Join, templates.JoinBelow), item[:len(item)-1]
			continue
		}
		if at := strings.LastIndex(item, "@"); at >= 0 {
			v, err := strconv.ParseFloat(item[at+1:], 64)
			if err != nil || !(v > 0) {
				return t, fmt.Errorf("speed %q", item[at+1:])
			}
			t.Kmph, item = v, item[:at]
			continue
		}
		break
	}
	name, length, hasLength := strings.Cut(item, ":")
	var m float64
	if hasLength {
		v, err := strconv.ParseFloat(length, 64)
		if err != nil || !(v >= 0) {
			return t, fmt.Errorf("length %q", length)
		}
		m = v
	}
	if siding, ok := strings.CutPrefix(name, "~"); ok {
		if siding == "" {
			return t, errors.New("a siding needs a name")
		}
		t.Siding, t.SidingLength = siding, units.M(m)
		return t, nil
	}
	if name == "" {
		return t, errors.New("a platform needs a number")
	}
	if t.Buffer != "" {
		return t, errors.New("only a siding ends at a buffer stop")
	}
	t.PfNo, t.Length = name, units.M(m)
	return t, nil
}

// joinWith adds a main to hang off to a track's Join.
func joinWith(join, more string) string {
	if join != "" && join != more {
		return templates.JoinBoth
	}
	return more
}

// FormatTracks writes tracks as ParseTracks reads them.
func FormatTracks(tracks []templates.Track) string {
	var items []string
	for _, t := range tracks {
		var b strings.Builder
		if t.PfNo != "" {
			b.WriteString(t.PfNo)
			if t.Length > 0 {
				fmt.Fprintf(&b, ":%g", float64(t.Length))
			}
		} else {
			b.WriteString("~" + t.Siding)
			if t.SidingLength > 0 {
				fmt.Fprintf(&b, ":%g", float64(t.SidingLength))
			}
		}
		if t.Straight {
			b.WriteString("*")
		}
		if t.Kmph > 0 {
			fmt.Fprintf(&b, "@%g", t.Kmph)
		}
		switch t.Buffer {
		case "W":
			b.WriteString("<")
		case "E":
			b.WriteString(">")
		}
		b.WriteString(t.Join)
		items = append(items, b.String())
	}
	return strings.Join(items, " ")
}
