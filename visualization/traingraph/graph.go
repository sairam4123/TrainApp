// Package traingraph draws a recorded run as a time-distance graph (a train
// graph): time runs left to right, the line's stations top (west) to bottom
// (east) at their distances, and each train is a line through them. A sloping
// line is a train running, a flat one a train standing.
//
// The lines come from the recording's train messages, so they show what the
// trains did. A train's planned times, where its schedule has any, can be
// shown as markers on top: on a timetable's check run they sit on the lines.
package traingraph

import (
	"trainapp/visualization/schematic"
	"trainapp/visualization/vizproto"
)

// Graph is every train's run laid out against time and distance.
type Graph struct {
	cor        *schematic.Corridor
	trains     []*train // in the order the sim added them
	start, end float64
}

// sample is a train's front at a time, at a distance along the corridor.
type sample struct{ t, at float64 }

type train struct {
	number  string
	runs    [][]sample // each a stretch the train was in the world
	planned []sample
	east    bool // runs west to east
}

// movement is one train message's run along a track.
type movement struct {
	track, from, to string
	t0, t1          float64
}

// Build lays out the trains of a recording along the corridor.
func Build(world *vizproto.World, cor *schematic.Corridor, msgs []vizproto.Message) *Graph {
	g := &Graph{cor: cor}
	byNumber := make(map[string]*train)
	stations := make(map[string]float64)
	for _, s := range cor.Stations {
		stations[s.Code] = s.At
	}
	for _, wt := range world.Trains {
		tr := &train{number: wt.Number}
		for _, sp := range wt.Schedule {
			at, ok := stations[sp.Stn]
			if !ok {
				continue
			}
			if sp.Arr >= 0 {
				tr.planned = append(tr.planned, sample{sp.Arr, at})
			}
			if sp.Dep >= 0 && sp.Dep != sp.Arr {
				tr.planned = append(tr.planned, sample{sp.Dep, at})
			}
		}
		g.trains = append(g.trains, tr)
		byNumber[wt.Number] = tr
	}

	last := make(map[string]*movement) // latest movement of each train in the world
	for _, m := range msgs {
		t := m.Head().T
		g.end = max(g.end, t)
		tm, ok := m.(*vizproto.TrainMsg)
		if !ok {
			continue
		}
		tr := byNumber[tm.Train]
		if tr == nil {
			continue
		}
		prev := last[tm.Train]
		if tm.Track == "" {
			if prev != nil {
				// it left the world from where it stood
				g.hold(tr, prev, t)
				delete(last, tm.Train)
			}
			continue
		}
		mv := &movement{track: tm.Track, from: tm.From, to: tm.To, t0: tm.TStart, t1: tm.TEnd}
		if prev != nil && prev.track == mv.track && prev.from == mv.from && prev.to == mv.to && prev.t0 == mv.t0 {
			continue // only its phase or wait changed
		}
		if prev == nil {
			tr.runs = append(tr.runs, nil)
		} else {
			g.hold(tr, prev, mv.t0)
		}
		g.run(tr, mv)
		last[tm.Train] = mv
	}
	for number, mv := range last {
		g.hold(byNumber[number], mv, g.end)
	}

	g.start = g.end
	for _, tr := range g.trains {
		if len(tr.runs) > 0 {
			first, end := tr.runs[0][0], lastSample(tr)
			tr.east = end.at >= first.at
			g.start = min(g.start, first.t)
		}
		for _, p := range tr.planned {
			g.start, g.end = min(g.start, p.t), max(g.end, p.t)
		}
	}
	if g.start > g.end {
		g.start = 0
	}
	return g
}

// run adds the movement's run along its track.
func (g *Graph) run(tr *train, mv *movement) {
	a0, ok0 := g.cor.At(mv.from)
	a1, ok1 := g.cor.At(mv.to)
	if !ok0 || !ok1 {
		return
	}
	r := &tr.runs[len(tr.runs)-1]
	*r = append(*r, sample{mv.t0, a0}, sample{max(mv.t1, mv.t0), a1})
}

// hold has the train stand where its movement ended until t.
func (g *Graph) hold(tr *train, mv *movement, t float64) {
	r := &tr.runs[len(tr.runs)-1]
	if len(*r) == 0 {
		return
	}
	end := (*r)[len(*r)-1]
	switch {
	case t > end.t:
		*r = append(*r, sample{t, end.at})
	case len(*r) >= 2:
		// it moved on before the estimated end of the track
		(*r)[len(*r)-1].t = max(t, (*r)[len(*r)-2].t)
	}
}

func lastSample(tr *train) sample {
	r := tr.runs[len(tr.runs)-1]
	return r[len(r)-1]
}

// Span is the time from the first train entering, or the first planned time,
// to the end of the recording.
func (g *Graph) Span() (start, end float64) { return g.start, g.end }

// Trains lists the trains in the order the sim added them.
func (g *Graph) Trains() []string {
	out := make([]string, len(g.trains))
	for i, tr := range g.trains {
		out[i] = tr.number
	}
	return out
}
