package main

import (
	"cmp"
	"container/heap"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
)

// way is a way a train can take from one station to another: the stations
// it passes, both ends included, how far it runs between them, and the
// points it runs over, in order.
type way struct {
	stations []string
	m        float64
	points   []string
}

// label is a way as a choice reads, e.g. "by MID, JCT · 24 km".
func (w way) label() string {
	by := "direct"
	if len(w.stations) > 2 {
		by = "by " + strings.Join(w.stations[1:len(w.stations)-1], ", ")
	}
	return fmt.Sprintf("%s · %s", by, strings.TrimSuffix(fmt.Sprintf("%.1f", w.m/1000), ".0")+" km")
}

// waysKept is how many ways onward each point keeps while ways looks, the
// shortest: enough for any choice a form offers.
const waysKept = 24

// ways lists the distinct ways a train can take from one station to
// another, as the stations it passes, shortest first, at most max (0 for
// all it finds). A train never reverses, and every track runs west to east,
// so a way runs east all along or west all along. It works on the snapshot,
// so a world that does not build yet still has ways.
func ways(s *infra.Snapshot, from, to string, max int) []way {
	g := newTrackGraph(s)
	isStation := map[string]bool{}
	for _, st := range s.Stations {
		isStation[st.Code] = true
	}
	best := map[string]way{}
	for _, east := range []bool{true, false} {
		memo := map[string][]way{}
		var onward func(p string) []way
		// onward is the ways from a point to the station, the stations
		// counted from the point's own
		onward = func(p string) []way {
			if ws, ok := memo[p]; ok {
				return ws
			}
			memo[p] = nil // a guard only: running one way, track never loops back
			code := g.code[p]
			if code == to {
				memo[p] = []way{{stations: []string{to}, points: []string{p}}}
				return memo[p]
			}
			found := map[string]way{}
			for _, h := range g.next(p, east) {
				for _, w := range onward(h.to) {
					stations := w.stations
					if isStation[code] && stations[0] != code {
						stations = append([]string{code}, stations...)
					}
					key := strings.Join(stations, " ")
					if old, ok := found[key]; !ok || w.m+h.m < old.m {
						found[key] = way{stations: stations, m: w.m + h.m, points: append([]string{p}, w.points...)}
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
				key := strings.Join(w.stations, " ")
				if old, ok := best[key]; w.stations[0] == from && (!ok || w.m < old.m) {
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

func byLength(a, b way) int {
	return cmp.Or(cmp.Compare(a.m, b.m), cmp.Compare(len(a.stations), len(b.stations)), slices.Compare(a.stations, b.stations))
}

// route is the stations a train passes between two, in order, both ends
// included: the shortest way (ways), through via if given.
func route(s *infra.Snapshot, from, to, via string) ([]string, error) {
	w, err := routeWay(s, from, to, via)
	if err != nil {
		return nil, err
	}
	return w.stations, nil
}

// routeWay is the shortest way between two stations, through via if given,
// or why there is none.
func routeWay(s *infra.Snapshot, from, to, via string) (way, error) {
	isStation := map[string]bool{}
	for _, st := range s.Stations {
		isStation[st.Code] = true
	}
	for _, c := range []string{from, to, via} {
		if c != "" && !isStation[c] {
			return way{}, fmt.Errorf("no station %s", c)
		}
	}
	if from == to {
		return way{}, errors.New("a train needs to go somewhere")
	}
	for _, w := range ways(s, from, to, 0) {
		if via == "" || slices.Contains(w.stations, via) {
			return w, nil
		}
	}
	switch {
	case via != "":
		return way{}, fmt.Errorf("no way from %s to %s by %s without reversing", from, to, via)
	case newTrackGraph(s).connected(from, to):
		return way{}, fmt.Errorf("%s to %s needs a train to reverse, which the sim does not do: split it into two trains", from, to)
	}
	return way{}, fmt.Errorf("no way from %s to %s over the track", from, to)
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

// pattern is a run of trains at a fixed interval along a picked route, both
// ways if asked.
type pattern struct {
	route      pickedRoute // the way out, its stops, its first entry
	interval   float64
	count      int
	both       bool
	offset     float64 // the first return train enters this long after the first one out
	classes    []string
	upNumber   string // e.g. 045{n}U
	downNumber string
	kmph       float64
}

// expand turns a pattern into trains: count each way, the n-th entering
// the route's entry + n·interval, cycling through the classes, numbered by
// the number patterns ({n} is 1, 2, ...). Trains back run the way reversed,
// stopping where the trains out stop, on any platform.
func (pt pattern) expand() ([]ops.TrainSpec, error) {
	if pt.count < 1 {
		return nil, errors.New("a pattern needs at least one train")
	}
	if !strings.Contains(pt.upNumber, "{n}") || (pt.both && !strings.Contains(pt.downNumber, "{n}")) {
		return nil, errors.New("a number pattern needs {n} for the train's place in the run")
	}
	out := pt.route.way.stations
	back := slices.Clone(out)
	slices.Reverse(back)
	var trains []ops.TrainSpec
	add := func(stations []string, platforms bool, number string, i int, entry float64) error {
		r := pt.route
		r.entry = entry
		pts, err := r.points(stations, platforms)
		if err != nil {
			return err
		}
		t := ops.TrainSpec{Number: strings.ReplaceAll(number, "{n}", strconv.Itoa(i+1)), MaxSpeedKmph: pt.kmph, Points: pts}
		if len(pt.classes) > 0 {
			t.Class = pt.classes[i%len(pt.classes)]
		}
		trains = append(trains, t)
		return nil
	}
	first := pt.route.entry
	for i := range pt.count {
		if err := add(out, true, pt.upNumber, i, first+float64(i)*pt.interval); err != nil {
			return nil, err
		}
		if pt.both {
			if err := add(back, false, pt.downNumber, i, first+pt.offset+float64(i)*pt.interval); err != nil {
				return nil, err
			}
		}
	}
	return trains, nil
}

// trainProblems lists what is wrong with a train: what the schedule file's
// rules say (ops), and what the world says (stations and platforms that are
// not there). At is the point it is about, -1 for the train.
type trainProblem struct {
	at   int
	text string
}

func checkTrain(s *infra.Snapshot, t ops.TrainSpec) []trainProblem {
	var out []trainProblem
	if _, err := t.Train(); err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			at := -1
			var k int
			if _, e := fmt.Sscanf(line, "point %d", &k); e == nil {
				at = k
			}
			out = append(out, trainProblem{at, line})
		}
	}
	if t.Number == "" {
		out = append(out, trainProblem{-1, "a train needs a number"})
	}
	pfs := map[string][]string{}
	for _, st := range s.Stations {
		pfs[st.Code] = []string{}
		for _, p := range st.Platforms {
			pfs[st.Code] = append(pfs[st.Code], p.No)
		}
	}
	for k, p := range t.Points {
		no, ok := pfs[p.Stn]
		switch {
		case p.Stn == "":
		case !ok:
			out = append(out, trainProblem{k, fmt.Sprintf("point %d: the world has no station %s", k, p.Stn)})
		case p.Pf != "" && !slices.Contains(no, p.Pf):
			out = append(out, trainProblem{k, fmt.Sprintf("point %d: %s has no platform %s", k, p.Stn, p.Pf)})
		}
	}
	return out
}

// entry is when a train enters, its first point's arr; -1 if it has none.
func entry(t ops.TrainSpec) float64 {
	if len(t.Points) > 0 && t.Points[0].Arr != nil {
		return *t.Points[0].Arr
	}
	return -1
}

// trainPath is the points a train runs over: from its first station to its
// last through each it names, over the platform it names there, the
// shortest way between, never against a track's signals. A train never
// reverses, so it runs east all along or west all along, whichever the
// track allows.
func trainPath(s *infra.Snapshot, points []ops.PointSpec) ([]string, error) {
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

// through is trainPath running one way.
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
