package schematic

import (
	"errors"
	"fmt"
	"slices"
)

// Corridor is a single line as a distance axis, for a time-distance graph.
// Each station sits at its distance from the westernmost one; a station's own
// tracks all sit at the station, and a point on the line between two stations
// is placed by how far along the line it is.
type Corridor struct {
	Stations []CorridorStation // west to east
	at       map[string]float64
}

// CorridorStation is a station on the corridor and its distance in meters.
type CorridorStation struct {
	Code, Name string
	At         float64
}

// At is how far along the corridor the point lies, in meters.
func (c *Corridor) At(point string) (float64, bool) {
	m, ok := c.at[point]
	return m, ok
}

// Length is the distance from the first station to the last.
func (c *Corridor) Length() float64 {
	if len(c.Stations) == 0 {
		return 0
	}
	return c.Stations[len(c.Stations)-1].At
}

// ErrNotALine is returned by Corridor for a world whose stations do not lie
// along a single line, e.g. one with a junction.
var ErrNotALine = errors.New("schematic: the stations do not lie along a single line")

// chain is a run of line tracks from one station box to another.
type chain struct {
	from, to *stationBox
	points   []string  // the points between them
	dist     []float64 // how far each is from `from`
	length   float64
}

// Corridor lays the world's stations along a distance axis. Stations one
// line apart are the length of the shortest line between them apart.
func (d *Diagram) Corridor() (*Corridor, error) {
	if d.overview == nil {
		return nil, errors.New("schematic: Corridor needs the full diagram, not its overview")
	}
	pos := make(map[string]int, len(d.order))
	for i, p := range d.order {
		pos[p] = i
	}
	// a box's place west to east is that of its westernmost point
	westOf := make(map[*stationBox]int)
	for p, b := range d.inBox {
		if w, ok := westOf[b]; !ok || pos[p] < w {
			westOf[b] = pos[p]
		}
	}
	west := func(b *stationBox) int { return westOf[b] }

	lines := make(map[string][]*Track) // line tracks at each point
	for _, t := range d.tracks {
		if t.Kind == Line {
			lines[t.From] = append(lines[t.From], t)
			lines[t.To] = append(lines[t.To], t)
		}
	}
	// follow each line out of each box to the next box
	var chains []chain
	for _, p := range d.order {
		b := d.inBox[p]
		if b == nil {
			continue
		}
		for _, t := range lines[p] {
			c, ok := d.follow(b, p, t, lines)
			if ok && west(c.from) < west(c.to) {
				chains = append(chains, c)
			}
		}
	}

	neighbours := make(map[*stationBox]map[*stationBox]bool)
	shortest := make(map[[2]*stationBox]float64)
	for _, c := range chains {
		for _, pair := range [][2]*stationBox{{c.from, c.to}, {c.to, c.from}} {
			if neighbours[pair[0]] == nil {
				neighbours[pair[0]] = map[*stationBox]bool{}
			}
			neighbours[pair[0]][pair[1]] = true
		}
		k := [2]*stationBox{c.from, c.to}
		if l, ok := shortest[k]; !ok || c.length < l {
			shortest[k] = c.length
		}
	}
	for _, n := range neighbours {
		if len(n) > 2 {
			return nil, ErrNotALine
		}
	}

	boxes := slices.Clone(d.overview.boxes)
	slices.SortStableFunc(boxes, func(a, b *stationBox) int { return west(a) - west(b) })
	if len(boxes) == 0 {
		return nil, ErrNotALine
	}
	// walk the line from its west end
	order := []*stationBox{boxes[0]}
	at := map[*stationBox]float64{boxes[0]: 0}
	for cur := boxes[0]; ; {
		var next *stationBox
		for n := range neighbours[cur] {
			if _, seen := at[n]; !seen {
				next = n
			}
		}
		if next == nil {
			break
		}
		at[next] = at[cur] + shortest[[2]*stationBox{cur, next}]
		order = append(order, next)
		cur = next
	}
	if len(order) != len(boxes) {
		return nil, ErrNotALine
	}

	c := &Corridor{at: make(map[string]float64)}
	names := make(map[string]string)
	for _, s := range d.stations {
		names[s.code] = s.name
	}
	for _, b := range order {
		if b.code != "" {
			c.Stations = append(c.Stations, CorridorStation{Code: b.code, Name: names[b.code], At: at[b]})
		}
	}
	for p, b := range d.inBox {
		c.at[p] = at[b]
	}
	for _, ch := range chains {
		a, b := at[ch.from], at[ch.to]
		for i, p := range ch.points {
			c.at[p] = a + (b-a)*ch.dist[i]/ch.length
		}
	}
	if len(c.Stations) == 0 {
		return nil, fmt.Errorf("%w: it has no stations", ErrNotALine)
	}
	return c, nil
}

// follow runs along line track t out of box b at point p, through points
// with no other line, until it reaches a box. It fails at a dead end or where
// the line branches outside a station.
func (d *Diagram) follow(b *stationBox, p string, t *Track, lines map[string][]*Track) (chain, bool) {
	c := chain{from: b}
	at, via := p, t
	for {
		next := via.To
		if next == at {
			next = via.From
		}
		c.length += via.Length
		if box := d.inBox[next]; box != nil {
			if box == b {
				return chain{}, false // inside the station
			}
			c.to = box
			return c, true
		}
		c.points = append(c.points, next)
		c.dist = append(c.dist, c.length)
		if len(lines[next]) != 2 {
			return chain{}, false
		}
		at = next
		if lines[next][0] == via {
			via = lines[next][1]
		} else {
			via = lines[next][0]
		}
	}
}
