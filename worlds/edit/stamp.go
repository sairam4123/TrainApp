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
	// The line from the selected point to the station: a block section
	// LinkM long in LinkBlocks blocks, named LinkName (or made up); a
	// LinkM of 0 joins the station's throat to the point itself.
	LinkName   string
	LinkM      float64
	LinkBlocks int
}

// Stamp builds a template into the snapshot and joins it to the point at,
// if one is given: the station's west throat to a point with no track east,
// else its east throat to one with no track west. It returns the station's
// code.
func Stamp(s *infra.Snapshot, at string, spec StampSpec) (string, error) {
	if spec.Code == "" {
		return "", errors.New("a station needs a code")
	}
	if stationIndex(s, spec.Code) >= 0 {
		return "", fmt.Errorf("there is a station %s already", spec.Code)
	}
	scratch := &infra.World{}
	scratch.Init(worldData(s.Defaults))
	code := ids.StnCode(spec.Code)
	var west, east *infra.TrackPoint
	var err error
	switch spec.Template {
	case TemplateHalt:
		west, east, err = templates.Halt(scratch, code, spec.Name, units.M(cmp.Or(spec.PfLengthM, 400)), cmp.Or(spec.LineKmph, s.Defaults.RouteKmph))
	case TemplateCrossing:
		west, east, err = templates.CrossingStation(scratch, code, spec.Name, units.M(cmp.Or(spec.PfLengthM, 450)), cmp.Or(spec.LineKmph, s.Defaults.RouteKmph))
	case TemplateStation:
		west, east, err = templates.Station(scratch, code, spec.Name, spec.Edge, spec.TurnoutKmph, spec.Tracks...)
	default:
		return "", fmt.Errorf("template %q, want one of %s", spec.Template, strings.Join(Templates, ", "))
	}
	if err != nil {
		return "", err
	}
	piece, err := scratch.Snapshot()
	if err != nil {
		return "", err
	}
	var linkW, linkE string
	if at != "" {
		switch {
		case pointIndex(s, at) < 0:
			return "", fmt.Errorf("no point %s", at)
		case west != nil && open(s, at, true):
			linkW, linkE = at, west.Id.String()
		case east != nil && open(s, at, false):
			linkW, linkE = east.Id.String(), at
		default:
			return "", fmt.Errorf("%s has track both ways, or none the station can join", at)
		}
	}
	if err := merge(s, piece); err != nil {
		return "", err
	}
	if at == "" {
		return spec.Code, nil
	}
	if spec.LinkM == 0 {
		station := linkE
		if linkE == at {
			station = linkW
		}
		mergePoint(s, station, at)
		return spec.Code, nil
	}
	name := spec.LinkName
	if name == "" {
		name = "sl" + title(cmp.Or(pointCode(s, linkW), "Ln")) + title(cmp.Or(pointCode(s, linkE), "Ln"))
		for n := 2; slices.ContainsFunc(s.Signals, func(sig infra.SnapshotSignal) bool { return sig.ID == name+"Sig0F" }); n++ {
			name = strings.TrimRight(name, "0123456789") + strconv.Itoa(n)
		}
	}
	return spec.Code, BlockSection(s, linkW, linkE, name, spec.LinkM, max(spec.LinkBlocks, 1), 0)
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
// platform with no length is a through track. After it, * makes it the
// straight road (one track must be), @KMPH sets its speed, and on a siding <
// or > puts a buffer stop at its west or east end.
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
		items = append(items, b.String())
	}
	return strings.Join(items, " ")
}
