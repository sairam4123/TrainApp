package infra

import (
	"cmp"
	"fmt"
	"slices"
	"trainapp/railway/ids"
)

// RunsCorrectWay reports whether running the step obeys the signals on its
// track: unsignalled track can be run either way, signalled track only in a
// direction one of its signals faces.
func (w *World) RunsCorrectWay(step Step) bool {
	sigs := w.signalsOnTrack[step.Edge.Track.Id]
	return len(sigs) == 0 || slices.ContainsFunc(sigs, func(s *Signal) bool { return s.FacesMovement(step.From, step.To) })
}

// FollowsSignals reports whether every step of the path runs the correct way.
// Paths that fail this are wrong-direction running.
func (w *World) FollowsSignals(path *Path) bool {
	steps, err := path.Steps()
	if err != nil {
		return false
	}
	for _, step := range steps {
		if !w.RunsCorrectWay(step) {
			return false
		}
	}
	return true
}

// EndsAtSignal reports whether a train running the step meets a signal at its
// end: a real signal facing the movement, or the invisible signal that stands
// at each end of every platform. Movement authority never runs past one
// without being extended.
func (w *World) EndsAtSignal(step Step) bool {
	if w.IsStationPlatform(step.Edge.Track) {
		return true
	}
	return slices.ContainsFunc(w.signalsOnTrack[step.Edge.Track.Id], func(s *Signal) bool { return s.FacesMovement(step.From, step.To) })
}

// SectionEnd returns the index just past the signal section that starts at
// steps[from]: the section runs up to and including the first step that ends
// at a signal, or to the end of the steps if none does.
func (w *World) SectionEnd(steps []Step, from int) int {
	for i := from; i < len(steps); i++ {
		if w.EndsAtSignal(steps[i]) {
			return i + 1
		}
	}
	return len(steps)
}

// IsLegalPath checks that the path is continuous and never makes a transition
// the layout forbids.
func (w *World) IsLegalPath(path *Path) bool {
	steps, err := path.Steps()
	if err != nil || len(steps) == 0 {
		return false
	}
	for i := 1; i < len(steps); i++ {
		if !w.TrackGraph.TransitionAllowed(steps[i].From, steps[i-1].Edge, steps[i].Edge) {
			return false
		}
	}
	return true
}

// RankedPath is a candidate path with the facts used to rank it.
type RankedPath struct {
	Path           *Path
	FollowsSignals bool
}

// RankedPaths returns the candidate paths from `from` to `to`, signal-correct
// paths first, then shortest first. Ordering is deterministic. via is the edge
// the train is standing on (nil if it is entering the world); paths never
// reverse onto it. Requires Finalize.
func (w *World) RankedPaths(from *TrackPoint, via *GraphEdge, to *TrackSegment) ([]RankedPath, error) {
	paths, err := w.TrackGraph.GenerateCandidatePathsVia(via, from, to)
	if err != nil {
		return nil, err
	}

	type keyed struct {
		RankedPath
		length float64
	}
	ks := make([]keyed, len(paths))
	for i, p := range paths {
		ks[i] = keyed{RankedPath{p, w.FollowsSignals(p)}, float64(p.Length())}
	}
	slices.SortStableFunc(ks, func(a, b keyed) int {
		if a.FollowsSignals != b.FollowsSignals {
			if a.FollowsSignals {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.length, b.length)
	})

	ranked := make([]RankedPath, len(ks))
	for i, k := range ks {
		ranked[i] = k.RankedPath
	}
	return ranked, nil
}

// CanVisit reports whether a train standing at pos can call at each station
// in stops, in order, without using any step for which blocked returns true
// (nil blocks nothing). Calling at a station means running onto one of its
// platforms. For each leg the train must run the correct way if the station
// can be reached the correct way at all, matching how the dispatcher routes;
// otherwise wrong-direction running is allowed. Requires Finalize.
func (w *World) CanVisit(pos Position, stops []ids.StnCode, blocked func(Step) bool) (bool, error) {
	ends, err := w.VisitEnds([]Position{pos}, stops, blocked)
	return len(ends) > 0, err
}

// VisitEnds is CanVisit for a train that may stand at any of starts. It
// returns the positions the train can end at, on a platform track of the last
// stop: none if it cannot call at every stop, the starts themselves if stops
// is empty. Requires Finalize.
func (w *World) VisitEnds(starts []Position, stops []ids.StnCode, blocked func(Step) bool) ([]Position, error) {
	cur := starts
	for _, code := range stops {
		if len(cur) == 0 {
			return nil, nil
		}
		stn, ok := w.stations[code]
		if !ok {
			return nil, fmt.Errorf("unknown station %s", code)
		}
		atStation := func(s Step) bool { return stn.IsPlatform(s.Edge.Track.Id) }

		correctOnly := func(s Step) bool { return w.RunsCorrectWay(s) }
		static, err := w.TrackGraph.Reach(cur, atStation, correctOnly)
		if err != nil {
			return nil, err
		}
		policy := correctOnly
		if len(static) == 0 {
			policy = nil // no correct way exists; wrong-direction running is the only option
		}
		allow := func(s Step) bool {
			if policy != nil && !policy(s) {
				return false
			}
			return blocked == nil || !blocked(s)
		}
		next, err := w.TrackGraph.Reach(cur, atStation, allow)
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}
