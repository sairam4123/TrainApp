package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
)

// route is the stations a train passes between two, in order, both ends
// included: along the shortest way over the world's track (fewest tracks),
// through via if given. It works on the snapshot, so a world that does not
// build yet still has routes.
func route(s *infra.Snapshot, from, to, via string) ([]string, error) {
	if via != "" {
		a, err := route(s, from, via, "")
		if err != nil {
			return nil, err
		}
		b, err := route(s, via, to, "")
		if err != nil {
			return nil, err
		}
		return append(a, b[1:]...), nil
	}
	isStation := map[string]bool{}
	for _, st := range s.Stations {
		isStation[st.Code] = true
	}
	for _, c := range []string{from, to} {
		if !isStation[c] {
			return nil, fmt.Errorf("no station %s", c)
		}
	}
	if from == to {
		return nil, errors.New("a train needs to go somewhere")
	}
	code := map[string]string{}
	adj := map[string][]string{}
	for _, p := range s.Points {
		code[p.ID] = p.Stn
	}
	for _, t := range s.Tracks {
		adj[t.From] = append(adj[t.From], t.To)
		adj[t.To] = append(adj[t.To], t.From)
	}
	prev := map[string]string{}
	var queue []string
	for _, p := range s.Points {
		if p.Stn == from {
			prev[p.ID] = ""
			queue = append(queue, p.ID)
		}
	}
	end := ""
	for len(queue) > 0 && end == "" {
		at := queue[0]
		queue = queue[1:]
		for _, n := range adj[at] {
			if _, seen := prev[n]; seen {
				continue
			}
			prev[n] = at
			if code[n] == to {
				end = n
				break
			}
			queue = append(queue, n)
		}
	}
	if end == "" {
		return nil, fmt.Errorf("no way from %s to %s over the track", from, to)
	}
	var path []string
	for p := end; p != ""; p = prev[p] {
		path = append(path, p)
	}
	slices.Reverse(path)
	out := []string{from}
	for _, p := range path {
		if c := code[p]; isStation[c] && c != out[len(out)-1] {
			out = append(out, c)
		}
	}
	return out, nil
}

// fillPoints makes a train's points for the stations of a route: it stops
// at the first and last and at stops (with dwell minutes), and passes the
// rest. The first point keeps the entry time.
func fillPoints(stations []string, stops map[string]bool, entry, dwell float64) []ops.PointSpec {
	var out []ops.PointSpec
	for i, stn := range stations {
		p := ops.PointSpec{Stn: stn, Kind: ops.SpPass}
		if i == 0 || i == len(stations)-1 || stops[stn] {
			p.Kind, p.Dwell = ops.SpStop, ptr(dwell)
		}
		if i == 0 {
			p.Arr = ptr(entry)
		}
		out = append(out, p)
	}
	return out
}

// pattern is a run of trains at a fixed interval, both ways if asked.
type pattern struct {
	from, to, via   string
	stops           []string
	first, interval float64
	count           int
	dwell           float64
	both            bool
	offset          float64 // the first return train enters this long after the first one out
	classes         []string
	upNumber        string // e.g. 045{n}U
	downNumber      string
	kmph            float64
}

// expand turns a pattern into trains: count each way, the n-th entering
// first + n·interval, cycling through the classes, numbered by the number
// patterns ({n} is 1, 2, ...).
func (pt pattern) expand(s *infra.Snapshot) ([]ops.TrainSpec, error) {
	if pt.count < 1 {
		return nil, errors.New("a pattern needs at least one train")
	}
	if !strings.Contains(pt.upNumber, "{n}") || (pt.both && !strings.Contains(pt.downNumber, "{n}")) {
		return nil, errors.New("a number pattern needs {n} for the train's place in the run")
	}
	way, err := route(s, pt.from, pt.to, pt.via)
	if err != nil {
		return nil, err
	}
	stops := map[string]bool{}
	for _, c := range pt.stops {
		if !slices.Contains(way, c) {
			return nil, fmt.Errorf("%s is not on the way from %s to %s", c, pt.from, pt.to)
		}
		stops[c] = true
	}
	back := slices.Clone(way)
	slices.Reverse(back)
	var out []ops.TrainSpec
	add := func(stations []string, number string, i int, entry float64) {
		t := ops.TrainSpec{Number: strings.ReplaceAll(number, "{n}", strconv.Itoa(i+1)), MaxSpeedKmph: pt.kmph,
			Points: fillPoints(stations, stops, entry, pt.dwell)}
		if len(pt.classes) > 0 {
			t.Class = pt.classes[i%len(pt.classes)]
		}
		out = append(out, t)
	}
	for i := range pt.count {
		add(way, pt.upNumber, i, pt.first+float64(i)*pt.interval)
		if pt.both {
			add(back, pt.downNumber, i, pt.first+pt.offset+float64(i)*pt.interval)
		}
	}
	return out, nil
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
