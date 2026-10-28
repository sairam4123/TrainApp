package sim

import (
	"fmt"
	"slices"
	"strings"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

// Deadlock is a cycle of trains, each waiting for track the next one holds,
// that a run ended in. With the safe-state check on (safety.go) a run never
// should; Sim.NoDeadlockAvoidance turns it off to show one.
type Deadlock struct {
	Since float64    // when the last train in the cycle began to wait
	Cycle []WaitLink // each waits for the next, the last for the first
}

// WaitLink is one train in a deadlock waiting for another.
type WaitLink struct {
	Train ids.TrainID
	For   ids.TrainID         // the train holding what Train waits for
	Track *infra.TrackSegment // the track Train waits for that For holds
}

func (d *Deadlock) Error() string {
	return fmt.Sprintf("deadlock at t=%.2f: %s", roundTime(d.Since), d.String())
}

// String is the cycle, e.g. "E1 waits for W1 (B pf2) → W1 waits for E1
// (A-B block)".
func (d *Deadlock) String() string {
	parts := make([]string, len(d.Cycle))
	for i, l := range d.Cycle {
		parts[i] = fmt.Sprintf("%s waits for %s (%s)", l.Train, l.For, l.Track)
	}
	return strings.Join(parts, " → ")
}

// Trains are the trains in the cycle, in its order.
func (d *Deadlock) Trains() []ids.TrainID {
	out := make([]ids.TrainID, len(d.Cycle))
	for i, l := range d.Cycle {
		out[i] = l.Train
	}
	return out
}

// deadlock finds a cycle in the wait-for graph as it is now: each queued
// request waits for the trains holding the tracks it waits on. nil if there
// is none; trains stuck outside a cycle are not a deadlock.
func (disp *Dispatcher) deadlock() *Deadlock {
	waiting := disp.waits.Waiting()
	edges := map[ids.TrainID][]WaitLink{}
	for _, id := range waiting {
		for _, tid := range disp.waits.BlockedOn(id) {
			trk, ok := disp.world.TrackGraph.GetTrack(tid)
			if !ok {
				continue
			}
			res, occ := disp.ilk.TrackHolder(trk)
			for _, h := range []ids.TrainID{occ, res} {
				if !h.IsZero() && h != id && !slices.ContainsFunc(edges[id], func(l WaitLink) bool { return l.For == h }) {
					edges[id] = append(edges[id], WaitLink{Train: id, For: h, Track: trk})
				}
			}
		}
	}
	cycle := findCycle(waiting, edges)
	if cycle == nil {
		return nil
	}
	d := &Deadlock{Cycle: cycle}
	for _, l := range cycle {
		d.Since = max(d.Since, disp.requests[l.Train].Since)
	}
	return d
}

// findCycle returns a cycle in the graph, searched from the nodes in order,
// or nil if there is none.
func findCycle(nodes []ids.TrainID, edges map[ids.TrainID][]WaitLink) []WaitLink {
	const (
		unseen = iota
		onPath
		done
	)
	mark := map[ids.TrainID]int{}
	var path []WaitLink
	var visit func(n ids.TrainID) []WaitLink
	visit = func(n ids.TrainID) []WaitLink {
		mark[n] = onPath
		for _, l := range edges[n] {
			switch mark[l.For] {
			case onPath:
				i := slices.IndexFunc(path, func(p WaitLink) bool { return p.Train == l.For })
				return append(slices.Clone(path[i:]), l)
			case unseen:
				path = append(path, l)
				if c := visit(l.For); c != nil {
					return c
				}
				path = path[:len(path)-1]
			}
		}
		mark[n] = done
		return nil
	}
	for _, n := range nodes {
		if mark[n] == unseen {
			if c := visit(n); c != nil {
				return c
			}
		}
	}
	return nil
}
