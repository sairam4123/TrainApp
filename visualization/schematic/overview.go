package schematic

import (
	"fmt"
	"slices"
	"trainapp/visualization/vizproto"
	"unicode/utf8"
)

// stationBox is a station's platforms, switches and throats, drawn as one box
// at the overview level. The lines that leave the station meet its sides on
// their own lanes.
type stationBox struct {
	id        string
	code      string   // empty for switches outside any station
	platforms []*Track // in platform order, for the occupancy dots
	west      string   // overview points at the box's west and east side
	east      string
	span      int             // columns from west to east side
	lo, hi    int             // overview lanes the box covers
	sides     map[string]bool // overview points where a line meets a side
}

// buildOverview collapses every connected group of platform and switch
// tracks into a box, keeping only the lines between them.
func (d *Diagram) buildOverview() (*Diagram, error) {
	parent := make(map[string]string)
	var find func(string) string
	find = func(p string) string {
		if q, ok := parent[p]; ok && q != p {
			r := find(q)
			parent[p] = r
			return r
		}
		return p
	}
	union := func(p, q string) {
		a, b := find(p), find(q)
		parent[a], parent[b] = a, a
	}
	for _, t := range d.tracks {
		if t.Kind != Line {
			union(t.From, t.To)
		}
	}
	for _, s := range d.stations {
		for _, pf := range s.platforms {
			union(s.platforms[0].From, pf.From)
		}
	}

	boxes := make(map[string]*stationBox) // by root point
	var roots []string
	for _, t := range d.tracks {
		if t.Kind == Line {
			continue
		}
		for _, p := range []string{t.From, t.To} {
			r := find(p)
			if _, ok := boxes[r]; !ok {
				boxes[r] = &stationBox{sides: map[string]bool{}}
				roots = append(roots, r)
			}
			d.inBox[p] = boxes[r]
		}
	}
	slices.SortFunc(roots, d.byRank)
	for i, r := range roots {
		boxes[r].id = fmt.Sprintf("box%d", i)
	}
	for _, s := range d.stations {
		if len(s.platforms) == 0 {
			continue
		}
		b := d.inBox[s.platforms[0].From]
		if b.code == "" {
			b.code = s.code
		}
		b.platforms = append(b.platforms, s.platforms...)
	}

	ov := newDiagram()
	ov.signals = d.signals
	addPoint := func(id string, rank, lane int) {
		if _, ok := ov.points[id]; !ok {
			ov.points[id] = vizproto.Point{ID: id, DeadEnd: d.points[id].DeadEnd, Boundary: d.points[id].Boundary}
		}
		ov.rank[id] = rank
		ov.lane[id] = lane
	}
	side := func(b *stationBox, edge string, lane int) string {
		id := fmt.Sprintf("%s/%s%d", b.id, edge, lane)
		ov.group[id] = b.id + "/" + edge
		b.sides[id] = true
		return id
	}
	for _, t := range d.tracks {
		if t.Kind != Line {
			continue
		}
		fb, tb := d.inBox[t.From], d.inBox[t.To]
		if fb != nil && fb == tb {
			continue // inside a station
		}
		c := *t
		if fb != nil {
			c.From = side(fb, "e", d.lane[t.From])
		}
		if tb != nil {
			c.To = side(tb, "w", d.lane[t.To])
		}
		addPoint(c.From, d.rank[t.From], d.lane[t.From])
		addPoint(c.To, d.rank[t.To], d.lane[t.To])
		ov.addTrack(&c)
	}

	for _, r := range roots {
		b := boxes[r]
		first := true
		for id := range b.sides {
			if l := ov.lane[id]; first || l < b.lo {
				b.lo = l
			}
			if l := ov.lane[id]; first || l > b.hi {
				b.hi = l
			}
			first = false
		}
		if first {
			b.lo, b.hi = d.lane[r], d.lane[r]
		}
		b.span = max(utf8.RuneCountInString(b.code), len(b.platforms), 1) + 3
		b.west, b.east = b.id+"/w", b.id+"/e"
		ov.group[b.west], ov.group[b.east] = b.id+"/w", b.id+"/e"
		addPoint(b.west, d.rank[r], b.lo)
		addPoint(b.east, d.rank[r], b.lo)
		ov.addTrack(&Track{ID: b.id, From: b.west, To: b.east, Kind: box, span: b.span})
		ov.boxes = append(ov.boxes, b)
	}

	// close up the lanes left empty by the platforms that were collapsed
	used := make(map[int]bool)
	for _, l := range ov.lane {
		used[l] = true
	}
	for _, b := range ov.boxes {
		for l := b.lo; l <= b.hi; l++ {
			used[l] = true
		}
	}
	var lanes []int
	for l := range used {
		lanes = append(lanes, l)
	}
	slices.Sort(lanes)
	dense := make(map[int]int)
	for i, l := range lanes {
		dense[l] = i
	}
	for id, l := range ov.lane {
		ov.lane[id] = dense[l]
	}
	for _, b := range ov.boxes {
		b.lo, b.hi = dense[b.lo], dense[b.hi]
	}
	ov.lanes = max(len(lanes), 1)

	if err := ov.finish(); err != nil {
		return nil, err
	}
	return ov, nil
}
