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
	out := pt.route.way.Stations
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
