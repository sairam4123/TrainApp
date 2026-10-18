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
	"trainapp/visualization/schematic"
)

// JunctionSpec is a branch to leave a line at (Junction).
type JunctionSpec struct {
	// Mains are the line's tracks where the branch leaves it, side by side
	// on their own lanes: plain level tracks, any number, any order.
	Mains []string
	// Below puts the branch below the mains, else above; East has it leave
	// going east, else west.
	Below, East bool
	// Lanes is how many lanes beyond the outermost main the nearest of the
	// branch's lines runs, at least 1.
	Lanes int
	// Code is the junction's code, which its points and switches carry
	// (e.g. "JCT"); empty for none.
	Code string
	// Pattern is the branch's lines, top to bottom (ParsePattern).
	Pattern string
	// Pairs is the main each of the branch's lines leaves, top to bottom, as
	// an index into the mains top to bottom; nil for DefaultPairs. Upper
	// branch lines leave upper mains, so no two legs cross.
	Pairs []int
	// The branch's first section: Name (made up if empty), LengthM long in
	// Blocks blocks.
	Name    string
	LengthM float64
	Blocks  int
}

// Junction leaves a line for a branch. A short middle is split out of each
// main (as Crossing does); a switch on a main lets trains off it onto a
// branch line, and where that leg crosses another main on its way to the
// branch, the two cross at a diamond: a flat junction. From the legs the
// branch's lines run on, a first section of them (as Lines lays them), to
// open ends it returns, top to bottom.
func Junction(s *infra.Snapshot, spec JunctionSpec) ([]string, error) {
	ways, err := ParsePattern(spec.Pattern)
	if err != nil {
		return nil, err
	}
	mains, lanes, err := mainsByLane(s, spec.Mains)
	if err != nil {
		return nil, err
	}
	switch {
	case spec.Lanes < 1:
		return nil, errors.New("the branch must run at least a lane beyond the line")
	case !(spec.LengthM > 0):
		return nil, errors.New("the branch needs a length")
	}
	pairs := spec.Pairs
	if pairs == nil {
		if pairs, err = DefaultPairs(s, mains, ways, spec.Below); err != nil {
			return nil, err
		}
	}
	if len(pairs) != len(ways) {
		return nil, fmt.Errorf("%d branch lines, %d pairs", len(ways), len(pairs))
	}
	for b, p := range pairs {
		switch {
		case p < 0 || p >= len(mains):
			return nil, fmt.Errorf("branch line %d leaves main %d, but there are %d", b+1, p+1, len(mains))
		case b > 0 && p <= pairs[b-1]:
			return nil, fmt.Errorf("branch line %d leaves main %d, not one below branch line %d's: the legs would cross", b+1, p+1, b)
		}
	}
	name := spec.Name
	if name == "" {
		name = "br" + title(cmp.Or(spec.Code, "Jn"))
		for n, base := 2, name; lineNameTaken(s, LineName(name, 1, len(ways))); n++ {
			name = base + strconv.Itoa(n)
		}
	}

	// the branch's lanes, top to bottom
	branchLane := make([]int, len(ways))
	for b := range ways {
		if spec.Below {
			branchLane[b] = lanes[len(lanes)-1] + spec.Lanes + b
		} else {
			branchLane[b] = lanes[0] - spec.Lanes - (len(ways) - 1 - b)
		}
	}
	// the junction zone on each main
	type zone struct{ a, b, seg string }
	zones := make([]zone, len(mains))
	for i, m := range mains {
		a, b, err := splitMiddle(s, m)
		if err != nil {
			return nil, err
		}
		zones[i] = zone{a, b, s.Tracks[trackBetween(s, a, b)].ID}
	}
	swLen := cmp.Or(s.Defaults.SwitchLengthM, 100)
	swKmph := cmp.Or(s.Defaults.SwitchKmph, 30)
	block := func(typ string, tracks ...string) {
		s.Switches = append(s.Switches, infra.SnapshotSwitch{
			ID: newID(s, ids.PointID{StnCode: ids.StnCode(spec.Code), Type: typ}), Tracks: tracks, Default: tracks[0],
		})
	}
	var west, east []string // the branch's first section's ends
	for b, p := range pairs {
		q := newPoint(s, spec.Code, "Jn")
		z := zones[p]
		leg := infra.SnapshotTrack{From: z.a, To: q, Lanes: branchLane[b] - lanes[p], LengthM: swLen, MaxKmph: swKmph}
		if !spec.East {
			leg = infra.SnapshotTrack{From: q, To: z.b, Lanes: lanes[p] - branchLane[b], LengthM: swLen, MaxKmph: swKmph}
		}
		addTrack(s, leg)
		legID := s.Tracks[len(s.Tracks)-1].ID
		block("JnSw", z.seg, legID)
		// the mains the leg crosses on its way to the branch
		for i := range mains {
			if (spec.Below && i > p) || (!spec.Below && i < p) {
				block("JnDp", zones[i].seg, legID)
			}
		}
		end := newPoint(s, spec.Code, "Ed")
		if spec.East {
			west, east = append(west, q), append(east, end)
		} else {
			west, east = append(west, end), append(east, q)
		}
	}
	if err := Lines(s, west, east, name, FormatPattern(ways), spec.LengthM, max(spec.Blocks, 1), 0); err != nil {
		return nil, err
	}
	if spec.East {
		return east, nil
	}
	return west, nil
}

// Plain reports whether a track is a plain level one, not a switch leg, a
// platform or a diagonal: one a junction or crossing can be laid on.
func Plain(s *infra.Snapshot, track string) bool {
	i := trackIndex(s, track)
	return i >= 0 && s.Tracks[i].Lanes == 0 && !inSwitch(s, track) && platformOf(s, track) == ""
}

// mainsByLane checks a line's tracks are plain, level and on lanes of their
// own, and sorts them top to bottom, with their lanes.
func mainsByLane(s *infra.Snapshot, tracks []string) ([]string, []int, error) {
	if len(tracks) == 0 {
		return nil, nil, errors.New("a junction needs the line's tracks")
	}
	d, err := schematic.FromSnapshot(s)
	if err != nil {
		return nil, nil, err
	}
	lane := map[string]int{}
	for _, id := range tracks {
		i := trackIndex(s, id)
		if i < 0 {
			return nil, nil, fmt.Errorf("no track %s", id)
		}
		t := s.Tracks[i]
		if t.Lanes != 0 || inSwitch(s, t.ID) || platformOf(s, t.ID) != "" {
			return nil, nil, fmt.Errorf("%s must be a plain level track, not a switch leg, a platform or a diagonal", trackName(t))
		}
		lane[id], _ = d.Lane(t.From)
	}
	mains := slices.Clone(tracks)
	slices.SortFunc(mains, func(a, b string) int { return cmp.Compare(lane[a], lane[b]) })
	lanes := make([]int, len(mains))
	for i, m := range mains {
		lanes[i] = lane[m]
		if i > 0 && lanes[i] == lanes[i-1] {
			return nil, nil, fmt.Errorf("%s and %s are on the same lane", mains[i-1], m)
		}
	}
	return mains, lanes, nil
}

// DefaultPairs pairs a branch's lines (top to bottom) with the mains (top
// to bottom) they leave: a single line leaves the main nearest it; more
// leave the mains running their way, in order (a line both ways, or a main
// with no signals, goes with anything).
func DefaultPairs(s *infra.Snapshot, mains []string, ways []byte, below bool) ([]int, error) {
	if len(ways) == 1 {
		if below {
			return []int{len(mains) - 1}, nil
		}
		return []int{0}, nil
	}
	var pairs []int
	next := 0
	for b, way := range ways {
		k := slices.IndexFunc(mains[next:], func(m string) bool {
			mw := Way(s, m)
			return way == WayBoth || mw == WayBoth || mw == 0 || mw == way
		})
		if k < 0 {
			return nil, fmt.Errorf("no main left running the way of branch line %d (%c): pair them yourself", b+1, way)
		}
		pairs = append(pairs, next+k)
		next += k + 1
	}
	return pairs, nil
}

// ParsePairs reads pairs written as main numbers from 1, e.g. "1 2"; empty
// for nil.
func ParsePairs(text string) ([]int, error) {
	var out []int
	for _, f := range strings.Fields(strings.ReplaceAll(text, ",", " ")) {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("main %q, want a number from 1", f)
		}
		out = append(out, n-1)
	}
	return out, nil
}
