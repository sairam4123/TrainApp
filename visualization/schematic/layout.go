// Package schematic lays a world out as a railway line schematic: tracks run
// west to east along horizontal lanes, switches and crossovers move between
// lanes, and nothing is drawn to scale.
//
// The layout comes from the world description (vizproto.World): every track
// runs west to east (From to To), and the templates that built the world
// record how many lanes each track moves.
package schematic

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/infra"
	"trainapp/visualization/vizproto"
)

// Kind is what a track is, which decides how it is drawn.
type Kind int

const (
	Line     Kind = iota // plain track, e.g. between stations
	Platform             // a station platform
	Switch               // a leg of a switch, crossing or slip
	box                  // a collapsed station at the overview level
)

// Track is one track of the world as the schematic draws it.
type Track struct {
	ID, Name string
	From, To string // west and east end
	Lanes    int
	Kind     Kind
	Length   float64
	PfNo     string // platform number, for a platform
	Class    string // main, through or loop, for a platform (empty if unknown)
	// Direction is a line's UP or DOWN label, drawn with its length.
	Direction string

	span int // fixed width, for a box

	// branchWest and branchEast mark an end where the track branches off
	// other tracks leaving the point the same way, e.g. a switch toe. A track
	// that changes lanes leaves the lane right where it branches.
	branchWest, branchEast bool
}

// label is drawn on the track: its platform number or length. At the detail
// level a named track shows its name instead of the platform number, and a
// station track its class if it is a main line or through track. A through
// track has no platform, so it is T1 rather than Pf1.
func (t *Track) label(level Level) string {
	named := level == Detail && t.Name != t.ID
	switch {
	case t.Kind == Line && named:
		return t.Name + " (" + t.lengthLabel() + ")"
	case t.Kind == Line:
		return "(" + t.lengthLabel() + ")"
	case named && (t.Class == "main" || t.Class == "through"):
		return t.Name + " (" + t.Class + ")"
	case named:
		return t.Name
	case t.Kind == Platform && t.Class == "through":
		return "T" + t.PfNo
	case t.Kind == Platform:
		return "Pf" + t.PfNo
	}
	return ""
}

// lengthLabel is a line's length, and its UP or DOWN label if it has one.
func (t *Track) lengthLabel() string {
	switch t.Direction {
	case string(infra.Up), string(infra.Down):
		return formatLength(t.Length) + " " + t.Direction
	}
	return formatLength(t.Length)
}

// station is a station and the tracks of its platforms.
type station struct {
	code, name string
	platforms  []*Track
}

// Diagram is a world laid out on lanes and in west-to-east order. Columns
// depend on the zoom, so they are worked out when drawing.
type Diagram struct {
	tracks   []*Track // in the world's order
	track    map[string]*Track
	points   map[string]vizproto.Point
	rank     map[string]int    // position in the world's point list; breaks ties
	order    []string          // west to east: every track's From comes before its To
	lane     map[string]int    // lane of each point, 0 at the top
	group    map[string]string // points that share a column (e.g. one end of all platforms of a station)
	stations []station
	switches []vizproto.SwitchBlock
	signals  []vizproto.Signal
	lanes    int // lanes 0..lanes-1 are in use

	trainLen map[string]float64 // metres, by train number; trains without one are left out

	overview *Diagram // the world with stations collapsed; nil for an overview itself
	boxes    []*stationBox
	inBox    map[string]*stationBox // original point -> its box
}

// FromWorld lays out a finalized world directly, without a recording.
func FromWorld(world *infra.World) (*Diagram, error) {
	return Build(vizproto.FromWorld(world))
}

// FromSnapshot lays out a world snapshot as it stands, built or not (see
// vizproto.FromSnapshot), e.g. one being edited. A point with no track is
// drawn on a lane of its own.
func FromSnapshot(s *infra.Snapshot) (*Diagram, error) {
	return Build(vizproto.FromSnapshot(s))
}

// Build lays out a world. It fails if the tracks do not all run west to east
// (they form a loop) or if the recorded lanes do not add up around some loop
// of track.
func Build(w *vizproto.World) (*Diagram, error) {
	d := newDiagram()
	for i, p := range w.Points {
		d.points[p.ID] = p
		d.rank[p.ID] = i
	}
	for _, wt := range w.Tracks {
		for _, end := range []string{wt.From, wt.To} {
			if _, ok := d.points[end]; !ok {
				return nil, fmt.Errorf("schematic: track %s ends at unknown point %s", wt.ID, end)
			}
		}
		t := &Track{ID: wt.ID, Name: wt.Name, From: wt.From, To: wt.To, Lanes: wt.Lanes, Length: wt.Length, Direction: wt.Direction}
		switch wt.Kind {
		case vizproto.KindPlatform:
			t.Kind = Platform
		case vizproto.KindSwitch:
			t.Kind = Switch
		}
		d.addTrack(t)
	}

	for _, wt := range w.Trains {
		if wt.LengthM > 0 {
			if d.trainLen == nil {
				d.trainLen = map[string]float64{}
			}
			d.trainLen[wt.Number] = wt.LengthM
		}
	}

	for _, ws := range w.Stations {
		s := station{code: ws.Code, name: ws.Name}
		for _, pf := range ws.Platforms {
			t, ok := d.track[pf.Track]
			if !ok {
				return nil, fmt.Errorf("schematic: station %s platform %s is unknown track %s", ws.Code, pf.No, pf.Track)
			}
			t.PfNo, t.Class = pf.No, pf.Class
			s.platforms = append(s.platforms, t)
			d.group[t.From] = ws.Code + "/west"
			d.group[t.To] = ws.Code + "/east"
		}
		d.stations = append(d.stations, s)
	}
	d.switches = w.Switches
	d.signals = w.Signals
	d.groupCrossings()

	if err := d.finish(); err != nil {
		return nil, err
	}
	if err := d.assignLanes(); err != nil {
		return nil, err
	}
	ov, err := d.buildOverview()
	if err != nil {
		return nil, err
	}
	d.overview = ov
	return d, nil
}

func newDiagram() *Diagram {
	return &Diagram{
		track:  make(map[string]*Track),
		points: make(map[string]vizproto.Point),
		rank:   make(map[string]int),
		lane:   make(map[string]int),
		group:  make(map[string]string),
		inBox:  make(map[string]*stationBox),
	}
}

func (d *Diagram) addTrack(t *Track) {
	d.tracks = append(d.tracks, t)
	d.track[t.ID] = t
}

// groupCrossings keeps each crossing's two west ends in one column and its
// two east ends in another, so its legs meet in an X: two tracks of a switch
// block (a diamond or slip) that change lanes opposite ways. Otherwise a long
// line into one of its ends pushes that end east alone, and the legs are
// drawn as two diagonals side by side.
func (d *Diagram) groupCrossings() {
	for _, sb := range d.switches {
		for i, a := range sb.Tracks {
			for _, b := range sb.Tracks[i+1:] {
				ta, tb := d.track[a], d.track[b]
				if ta == nil || tb == nil || ta.Lanes == 0 || ta.Lanes != -tb.Lanes {
					continue
				}
				d.sameColumn(ta.From, tb.From, sb.ID+"/west")
				d.sameColumn(ta.To, tb.To, sb.ID+"/east")
			}
		}
	}
}

// sameColumn puts points a and b in one group, joining any groups they are
// already in, and naming it name if neither is.
func (d *Diagram) sameColumn(a, b, name string) {
	ga, gb := d.group[a], d.group[b]
	switch {
	case ga == "" && gb == "":
		d.group[a], d.group[b] = name, name
	case ga == "":
		d.group[a] = gb
	case gb == "" || ga == gb:
		d.group[b] = ga
	default:
		for id, g := range d.group {
			if g == gb {
				d.group[id] = ga
			}
		}
	}
}

// finish marks branching ends and orders the points west to east.
func (d *Diagram) finish() error {
	leaving := make(map[string]int) // tracks leaving each point eastwards
	arriving := make(map[string]int)
	for _, t := range d.tracks {
		leaving[t.From]++
		arriving[t.To]++
	}
	for _, t := range d.tracks {
		t.branchWest = leaving[t.From] > 1
		t.branchEast = arriving[t.To] > 1
	}
	return d.orderWestToEast()
}

// Lane is the lane a point is drawn on, 0 at the top.
func (d *Diagram) Lane(pt string) (int, bool) {
	l, ok := d.lane[pt]
	return l, ok
}

// Order returns the points west to east: every track's From before its To.
func (d *Diagram) Order() []string {
	return slices.Clone(d.order)
}

// Tracks returns the tracks in the world's order.
func (d *Diagram) Tracks() []*Track {
	return slices.Clone(d.tracks)
}

func (d *Diagram) byRank(a, b string) int { return cmp.Or(d.rank[a]-d.rank[b], strings.Compare(a, b)) }

// orderWestToEast sorts the points so every track's From comes before its To,
// picking the first in the world's order whenever there is a choice so the
// order is stable.
func (d *Diagram) orderWestToEast() error {
	inDegree := make(map[string]int)
	out := make(map[string][]*Track)
	used := make(map[string]bool)
	for id := range d.points {
		used[id] = true // a point with no track is laid out on its own
	}
	for _, t := range d.tracks {
		used[t.From], used[t.To] = true, true
		inDegree[t.To]++
		out[t.From] = append(out[t.From], t)
	}

	var ready []string
	for id := range used {
		if inDegree[id] == 0 {
			ready = append(ready, id)
		}
	}
	d.order = nil
	for len(ready) > 0 {
		slices.SortFunc(ready, d.byRank)
		id := ready[0]
		ready = ready[1:]
		d.order = append(d.order, id)
		for _, t := range out[id] {
			inDegree[t.To]--
			if inDegree[t.To] == 0 {
				ready = append(ready, t.To)
			}
		}
	}

	if len(d.order) < len(used) {
		var stuck []string
		for id, n := range inDegree {
			if n > 0 {
				stuck = append(stuck, id)
			}
		}
		slices.Sort(stuck)
		return fmt.Errorf("schematic: tracks do not all run west to east, there is a loop through %s", strings.Join(stuck, ", "))
	}
	return nil
}

// assignLanes walks each connected piece of track from its first point,
// moving Lanes lanes along each track, then shifts each piece so its top lane
// is 0 and stacks the pieces one below the other.
func (d *Diagram) assignLanes() error {
	adjacent := make(map[string][]*Track)
	for _, t := range d.tracks {
		adjacent[t.From] = append(adjacent[t.From], t)
		adjacent[t.To] = append(adjacent[t.To], t)
	}
	starts := slices.Clone(d.order)
	slices.SortFunc(starts, d.byRank)

	top := 0 // first free lane below the pieces laid out so far
	for _, start := range starts {
		if _, done := d.lane[start]; done {
			continue
		}
		piece := []string{start}
		d.lane[start] = 0
		for i := 0; i < len(piece); i++ {
			at := piece[i]
			for _, t := range adjacent[at] {
				next, lane := t.To, d.lane[at]+t.Lanes
				if at == t.To {
					next, lane = t.From, d.lane[at]-t.Lanes
				}
				if got, ok := d.lane[next]; ok {
					if got != lane {
						return fmt.Errorf("schematic: lanes do not add up at track %s: %s is on lane %d one way round and %d the other", t.Name, next, got, lane)
					}
					continue
				}
				d.lane[next] = lane
				piece = append(piece, next)
			}
		}

		lo, hi := d.lane[start], d.lane[start]
		for _, id := range piece {
			lo, hi = min(lo, d.lane[id]), max(hi, d.lane[id])
		}
		for _, id := range piece {
			d.lane[id] += top - lo
		}
		top += hi - lo + 2 // one empty lane between pieces
	}
	d.lanes = max(top-1, 1)
	return nil
}

// formatLength is "55km", "4.5km" or "800m".
func formatLength(m float64) string {
	if m < 1000 {
		return fmt.Sprintf("%.0fm", m)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", m/1000), ".0") + "km"
}
