package edit

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/infra"
	"trainapp/visualization/schematic"
)

// Ways a line of a pattern may run (Lines). The sim takes a line's way from
// its signals: one signalled for trains running east only is one-way east.
const (
	WayEast = 'E'
	WayWest = 'W'
	WayBoth = 'B'
)

// ParsePattern reads a line pattern: a letter for each line, top to bottom,
// E (east only), W (west only) or B (both ways), spaces optional, e.g.
// "E W" for a double line, "E E B W W" for five tracks. Any number of lines
// will do.
func ParsePattern(p string) ([]byte, error) {
	var out []byte
	for _, r := range strings.ToUpper(p) {
		switch r {
		case WayEast, WayWest, WayBoth:
			out = append(out, byte(r))
		case ' ', ',':
		default:
			return nil, fmt.Errorf("line pattern %q: %q is not E, W or B", p, r)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("a line pattern needs a line")
	}
	return out, nil
}

// FormatPattern writes a pattern as ParsePattern reads it.
func FormatPattern(ways []byte) string {
	return strings.Join(strings.Split(string(ways), ""), " ")
}

// DefaultPattern is the pattern n lines start as: one line both ways, two as
// a double line (the upper running east, as tpjkkdi), more as many east as
// west, the east ones on top, and a line both ways in the middle if n is odd.
func DefaultPattern(n int) string {
	if n == 1 {
		return "B"
	}
	ways := slices.Repeat([]byte{WayEast}, n/2)
	if n%2 == 1 {
		ways = append(ways, WayBoth)
	}
	ways = append(ways, slices.Repeat([]byte{WayWest}, n/2)...)
	return FormatPattern(ways)
}

// LineName is the name Lines gives line k (1 is the top) of n named name:
// name itself for a single line, else name with L and k after it.
func LineName(name string, k, n int) string {
	if n == 1 {
		return name
	}
	return name + "L" + strconv.Itoa(k)
}

// Lines lays a line from each west[k] to east[k], lengthM long in blocks
// equal blocks at kmph (0 for the world's route speed), each running as
// pattern's letter k says (ParsePattern): there is a signal at every block
// boundary and both ends for each way the line runs, and no other. Each
// west[k] needs no track east and each east[k] none west; ends of the world
// there are opened up. A one-way line is labelled UP or DOWN (its tracks'
// Direction, which only trainedit reads) by the world's Up.
//
// A line is laid as worlds/templates.SingleLine builds one: line k of name
// (LineName) has points {line, Bk, i}, tracks line0, line1, ... and signals
// lineSig0F (for trains running east), lineSig0R, ...
func Lines(s *infra.Snapshot, west, east []string, name, pattern string, lengthM float64, blocks int, kmph float64) error {
	ways, err := ParsePattern(pattern)
	if err != nil {
		return err
	}
	switch {
	case len(west) != len(ways) || len(east) != len(ways):
		return fmt.Errorf("%d lines in the pattern, %d points west and %d east", len(ways), len(west), len(east))
	case name == "":
		return errors.New("a line needs a name")
	case !(lengthM > 0) || blocks < 1:
		return errors.New("a line needs a length and at least one block")
	}
	seen := map[string]bool{}
	for k := range ways {
		for _, p := range []string{west[k], east[k]} {
			if pointIndex(s, p) < 0 {
				return fmt.Errorf("no point %s", p)
			}
			if seen[p] {
				return fmt.Errorf("%s ends two lines", p)
			}
			seen[p] = true
		}
		if !open(s, west[k], true) {
			return fmt.Errorf("%s has a track east already", west[k])
		}
		if !open(s, east[k], false) {
			return fmt.Errorf("%s has a track west already", east[k])
		}
		if ln := LineName(name, k+1, len(ways)); lineNameTaken(s, ln) {
			return fmt.Errorf("the name %s is taken", ln)
		}
	}
	for k, way := range ways {
		layLine(s, west[k], east[k], LineName(name, k+1, len(ways)), way, lengthM, blocks, kmph)
	}
	return nil
}

func lineNameTaken(s *infra.Snapshot, line string) bool {
	return signalIndex(s, line+"Sig0F") >= 0 || signalIndex(s, line+"Sig0R") >= 0 ||
		slices.ContainsFunc(s.Tracks, func(t infra.SnapshotTrack) bool { return t.Name == line+"0" }) ||
		pointIndex(s, pointID(infra.SnapshotPoint{Stn: line, Type: "Bk", Seq: "1"}).String()) >= 0
}

// layLine lays one line of Lines, checked already.
func layLine(s *infra.Snapshot, from, to, name string, way byte, lengthM float64, blocks int, kmph float64) {
	// the line moves as many lanes as its ends are apart, if they are drawn
	// together already (by the lines laid before it, say)
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
		p.ID = pointID(p).String()
		s.Points = append(s.Points, p)
		pts = append(pts, p.ID)
	}
	pts = append(pts, to)
	kmph = cmp.Or(kmph, s.Defaults.RouteKmph)
	label := ""
	if way != WayBoth {
		label = string(infra.Down)
		if (way == WayEast) == (cmp.Or(s.Defaults.Up, "W") == "E") {
			label = string(infra.Up)
		}
	}
	for i := 0; i+1 < len(pts); i++ {
		t := infra.SnapshotTrack{Name: fmt.Sprintf("%s%d", name, i), From: pts[i], To: pts[i+1], LengthM: lengthM / float64(blocks), MaxKmph: kmph, Direction: label}
		if i == 0 {
			t.Lanes = lanes
		}
		addTrack(s, t)
		if way != WayWest {
			s.Signals = append(s.Signals, infra.SnapshotSignal{ID: fmt.Sprintf("%sSig%dF", name, i), At: pts[i+1], Facing: pts[i]})
		}
		if way != WayEast {
			s.Signals = append(s.Signals, infra.SnapshotSignal{ID: fmt.Sprintf("%sSig%dR", name, i), At: pts[i], Facing: pts[i+1]})
		}
	}
}

// Way is the way a track runs by its signals, as the sim sees it: WayEast,
// WayWest, WayBoth, or 0 if no signal stands at either end of it.
func Way(s *infra.Snapshot, track string) byte {
	i := trackIndex(s, track)
	if i < 0 {
		return 0
	}
	t := s.Tracks[i]
	east := slices.ContainsFunc(s.Signals, func(sig infra.SnapshotSignal) bool { return sig.At == t.To && sig.Facing == t.From })
	west := slices.ContainsFunc(s.Signals, func(sig infra.SnapshotSignal) bool { return sig.At == t.From && sig.Facing == t.To })
	switch {
	case east && west:
		return WayBoth
	case east:
		return WayEast
	case west:
		return WayWest
	}
	return 0
}
