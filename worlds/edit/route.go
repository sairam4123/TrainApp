package edit

import (
	"cmp"
	"container/heap"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
)

// Routes over the track: the stations a train passes between two, and the
// points it runs over. trainedit's route picker offers them, and trainimport
// fills in a train's passes from them.

// Route is a way a train can take from one station to another: the stations
// it passes, both ends included, how far it runs between them, and the
// points it runs over, in order.
type Route struct {
	Stations []string
	M        float64
	Points   []string
}

// Label is a way as a choice reads, e.g. "by MID, JCT · 24 km".
func (w Route) Label() string {
	by := "direct"
	if len(w.Stations) > 2 {
		by = "by " + strings.Join(w.Stations[1:len(w.Stations)-1], ", ")
	}
	return fmt.Sprintf("%s · %s", by, strings.TrimSuffix(fmt.Sprintf("%.1f", w.M/1000), ".0")+" km")
}

// waysKept is how many ways onward each point keeps while Routes looks, the
// shortest: enough for any choice a form offers.
const waysKept = 24

// Routes lists the distinct ways a train can take from one station to
// another, as the stations it passes, shortest first, at most max (0 for
// all it finds). A train never reverses, and every track runs west to east,
// so a way runs east all along or west all along. It works on the snapshot,
// so a world that does not build yet still has ways.
func Routes(s *infra.Snapshot, from, to string, max int) []Route {
	g := newTrackGraph(s)
	isStation := map[string]bool{}
	for _, st := range s.Stations {
		isStation[st.Code] = true
	}
	best := map[string]Route{}
	for _, east := range []bool{true, false} {
		memo := map[string][]Route{}
		var onward func(p string) []Route
		// onward is the ways from a point to the station, the stations
		// counted from the point's own
		onward = func(p string) []Route {
			if ws, ok := memo[p]; ok {
				return ws
			}
			memo[p] = nil // a guard only: running one way, track never loops back
			code := g.code[p]
			if code == to {
				memo[p] = []Route{{Stations: []string{to}, Points: []string{p}}}
				return memo[p]
			}
			found := map[string]Route{}
			for _, h := range g.next(p, east) {
				for _, w := range onward(h.to) {
					stations := w.Stations
					if isStation[code] && stations[0] != code {
						stations = append([]string{code}, stations...)
					}
					key := strings.Join(stations, " ")
					if old, ok := found[key]; !ok || w.M+h.m < old.M {
						found[key] = Route{Stations: stations, M: w.M + h.m, Points: append([]string{p}, w.Points...)}
					}
				}
			}
			ws := slices.Collect(maps.Values(found))
			slices.SortFunc(ws, byLength)
			ws = ws[:min(len(ws), waysKept)]
			memo[p] = ws
			return ws
		}
		for _, p := range g.points {
			if g.code[p] != from {
				continue
			}
			for _, w := range onward(p) {
				key := strings.Join(w.Stations, " ")
				if old, ok := best[key]; w.Stations[0] == from && (!ok || w.M < old.M) {
					best[key] = w
				}
			}
		}
	}
	out := slices.Collect(maps.Values(best))
	slices.SortFunc(out, byLength)
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

func byLength(a, b Route) int {
	return cmp.Or(cmp.Compare(a.M, b.M), cmp.Compare(len(a.Stations), len(b.Stations)), slices.Compare(a.Stations, b.Stations))
}

// RouteStations is the stations a train passes between two, in order, both ends
// included: the shortest way (Routes), through via if given.
func RouteStations(s *infra.Snapshot, from, to, via string) ([]string, error) {
	w, err := FindRoute(s, from, to, via)
	if err != nil {
		return nil, err
	}
	return w.Stations, nil
}

// FindRoute is the shortest way between two stations, through via if given,
// or why there is none.
func FindRoute(s *infra.Snapshot, from, to, via string) (Route, error) {
	isStation := map[string]bool{}
	for _, st := range s.Stations {
		isStation[st.Code] = true
	}
	for _, c := range []string{from, to, via} {
		if c != "" && !isStation[c] {
			return Route{}, fmt.Errorf("no station %s", c)
		}
	}
	if from == to {
		return Route{}, errors.New("a train needs to go somewhere")
	}
	for _, w := range Routes(s, from, to, 0) {
		if via == "" || slices.Contains(w.Stations, via) {
			return w, nil
		}
	}
	switch {
	case via != "":
		return Route{}, fmt.Errorf("no way from %s to %s by %s without reversing", from, to, via)
	case newTrackGraph(s).connected(from, to):
		return Route{}, fmt.Errorf("%s to %s needs a train to reverse, which the sim does not do: split it into two trains", from, to)
	}
	return Route{}, fmt.Errorf("no way from %s to %s over the track", from, to)
}

// trackGraph is a snapshot's points and the tracks east and west of each.
type trackGraph struct {
	code       map[string]string
	east, west map[string][]hop
	points     []string
}

// hop is a track from a point to the next, running one way.
type hop struct {
	to string
	m  float64
}

func newTrackGraph(s *infra.Snapshot) *trackGraph { return buildGraph(s, false) }

// newRunGraph is the track graph with no track run against its signals: a
// track signalled one way only (a line of a double line) runs that way.
func newRunGraph(s *infra.Snapshot) *trackGraph { return buildGraph(s, true) }

func buildGraph(s *infra.Snapshot, bySignals bool) *trackGraph {
	g := &trackGraph{code: map[string]string{}, east: map[string][]hop{}, west: map[string][]hop{}}
	for _, p := range s.Points {
		g.code[p.ID] = p.Stn
		g.points = append(g.points, p.ID)
	}
	signal := map[[2]string]bool{} // at, facing
	for _, sig := range s.Signals {
		signal[[2]string{sig.At, sig.Facing}] = true
	}
	for _, t := range s.Tracks {
		east, west := signal[[2]string{t.To, t.From}], signal[[2]string{t.From, t.To}]
		if !bySignals || east || !west {
			g.east[t.From] = append(g.east[t.From], hop{t.To, t.LengthM})
		}
		if !bySignals || west || !east {
			g.west[t.To] = append(g.west[t.To], hop{t.From, t.LengthM})
		}
	}
	return g
}

// next is the hops from a point, running east (or west).
func (g *trackGraph) next(p string, east bool) []hop {
	if east {
		return g.east[p]
	}
	return g.west[p]
}

// connected reports whether two stations are joined by track at all, any
// way round.
func (g *trackGraph) connected(a, b string) bool {
	seen := map[string]bool{}
	var queue []string
	for _, p := range g.points {
		if g.code[p] == a {
			seen[p] = true
			queue = append(queue, p)
		}
	}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		if g.code[at] == b {
			return true
		}
		for _, h := range append(slices.Clone(g.east[at]), g.west[at]...) {
			if !seen[h.to] {
				seen[h.to] = true
				queue = append(queue, h.to)
			}
		}
	}
	return false
}

// TrainPath is the points a train runs over: from its first station to its
// last through each it names, over the platform it names there, the
// shortest way between, never against a track's signals. A train never
// reverses, so it runs east all along or west all along, whichever the
// track allows.
func TrainPath(s *infra.Snapshot, points []ops.PointSpec) ([]string, error) {
	g := newRunGraph(s)
	pfs := map[[2]string]infra.SnapshotTrack{} // station, platform no
	tracks := map[string]infra.SnapshotTrack{}
	for _, t := range s.Tracks {
		tracks[t.ID] = t
	}
	for _, st := range s.Stations {
		for _, pf := range st.Platforms {
			pfs[[2]string{st.Code, pf.No}] = tracks[pf.Track]
		}
	}
	errs := map[bool]error{}
	for _, east := range []bool{true, false} {
		path, err := g.through(points, pfs, east)
		if err == nil {
			return path, nil
		}
		errs[east] = err
	}
	// say what stops it running the way its stations go
	bare := slices.Clone(points)
	for k := range bare {
		bare[k].Pf = ""
	}
	if _, err := g.through(bare, pfs, false); err == nil {
		return nil, errs[false]
	}
	return nil, errs[true]
}

// through is TrainPath running one way.
func (g *trackGraph) through(points []ops.PointSpec, pfs map[[2]string]infra.SnapshotTrack, east bool) ([]string, error) {
	var path, starts []string
	for i, p := range points {
		at := func(q string) bool { return g.code[q] == p.Stn }
		pf, onPf := pfs[[2]string{p.Stn, p.Pf}]
		entry, exit := pf.From, pf.To
		if !east {
			entry, exit = exit, entry
		}
		if onPf {
			at = func(q string) bool { return q == entry }
		}
		switch {
		case i == 0 && onPf:
			path, starts = []string{entry, exit}, []string{exit}
			continue
		case i == 0:
			for _, q := range g.points {
				if at(q) {
					starts = append(starts, q)
				}
			}
			continue
		}
		seg := g.shortest(starts, east, at)
		if seg == nil {
			to := p.Stn
			if onPf {
				to += " platform " + p.Pf
			}
			return nil, fmt.Errorf("no way from %s to %s running %s", points[i-1].Stn, to, map[bool]string{true: "east", false: "west"}[east])
		}
		if len(path) > 0 {
			seg = seg[1:]
		}
		path = append(path, seg...)
		if onPf {
			path = append(path, exit)
		}
		starts = path[len(path)-1:]
	}
	return path, nil
}

// shortest is the shortest run east (or west) from one of the starts to a
// point at says is there, the points it runs over; nil if there is none.
func (g *trackGraph) shortest(starts []string, east bool, at func(string) bool) []string {
	for _, p := range starts {
		if at(p) {
			return []string{p}
		}
	}
	dist, prev := map[string]float64{}, map[string]string{}
	q := &hopQueue{}
	for _, p := range starts {
		dist[p] = 0
		heap.Push(q, hop{p, 0})
	}
	for q.Len() > 0 {
		h := heap.Pop(q).(hop)
		if h.m > dist[h.to] {
			continue
		}
		if at(h.to) {
			path := []string{h.to}
			for p := h.to; !slices.Contains(starts, p); {
				p = prev[p]
				path = append(path, p)
			}
			slices.Reverse(path)
			return path
		}
		for _, n := range g.next(h.to, east) {
			if d, seen := dist[n.to]; !seen || h.m+n.m < d {
				dist[n.to], prev[n.to] = h.m+n.m, h.to
				heap.Push(q, hop{n.to, h.m + n.m})
			}
		}
	}
	return nil
}

// hopQueue is points by how far they are, nearest first.
type hopQueue []hop

func (q hopQueue) Len() int           { return len(q) }
func (q hopQueue) Less(i, j int) bool { return q[i].m < q[j].m }
func (q hopQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *hopQueue) Push(x any)        { *q = append(*q, x.(hop)) }
func (q *hopQueue) Pop() any {
	old := *q
	h := old[len(old)-1]
	*q = old[:len(old)-1]
	return h
}
