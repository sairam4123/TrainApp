package sim

import (
	"errors"
	"fmt"
	"slices"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/railway/ops"
)

// Dispatcher decides which route a train should get next and asks the
// interlocking to reserve it. Routes are one signal section long: from where
// the train stands up to the next signal or platform end. Every grant must
// leave the railway in a safe state (see safety.go). A request that cannot be
// met waits on the tracks that stood in its way and is retried only when one
// of them is released.
type Dispatcher struct {
	sim   *Sim
	world *infra.World
	ilk   *interlocking.Interlocking

	requests map[ids.TrainID]*RouteRequest
	waits    des.WaitList[ids.TrackID, ids.TrainID]

	frontier map[ids.TrainID]*interlocking.Route // latest route granted to each train
}

func newDispatcher(s *Sim) *Dispatcher {
	return &Dispatcher{
		sim:      s,
		world:    s.world,
		ilk:      s.ilk,
		requests: map[ids.TrainID]*RouteRequest{},
		frontier: map[ids.TrainID]*interlocking.Route{},
	}
}

// RouteRequest is a queued request for a route towards a station.
type RouteRequest struct {
	Train    *ops.Train
	Station  *infra.Station
	PrefPfNo string

	// Entry is true for a train waiting to enter the world, which appears on a
	// boundary platform rather than running from its current position.
	Entry bool

	Since   float64
	LastErr error
}

var (
	ErrNoEntryPlatform = errors.New("no free platform at the world boundary")
	ErrUnsafe          = errors.New("granting it could deadlock")
)

// RequestEntry finds a route for a train entering the world at a station:
// the train appears on a free platform that touches the sim boundary,
// preferring prefPfNo. On failure the request is queued.
func (disp *Dispatcher) RequestEntry(train *ops.Train, toStn *infra.Station, prefPfNo string) (*interlocking.Route, bool) {
	return disp.request(&RouteRequest{Train: train, Station: toStn, PrefPfNo: prefPfNo, Entry: true})
}

// RequestRoute finds the next signal section from where the train stands
// towards a platform of the station. On failure the request is queued.
func (disp *Dispatcher) RequestRoute(train *ops.Train, toStn *infra.Station, prefPfNo string) (*interlocking.Route, bool) {
	return disp.request(&RouteRequest{Train: train, Station: toStn, PrefPfNo: prefPfNo})
}

// Authorize grants movement authority over a reserved route, up to its next
// signal.
func (disp *Dispatcher) Authorize(train *ops.Train, route *interlocking.Route) (*ops.MovementAuthority, error) {
	if route.Train() != train.ID() {
		return nil, fmt.Errorf("route belongs to %s, not %s", route.Train(), train.Number)
	}
	return ops.NewMovementAuthority(disp.world, route)
}

// Forget drops everything the dispatcher knows about a train that left.
func (disp *Dispatcher) Forget(train ids.TrainID) {
	delete(disp.requests, train)
	delete(disp.frontier, train)
	disp.waits.Remove(train)
}

func (disp *Dispatcher) request(req *RouteRequest) (*interlocking.Route, bool) {
	id := req.Train.ID()
	route, blockers, err := disp.try(req)
	if err == nil {
		disp.served(id, route)
		return route, true
	}
	if existing := disp.requests[id]; existing != nil {
		req.Since = existing.Since
	} else {
		req.Since = disp.sim.CurTime()
	}
	req.LastErr = err
	disp.requests[id] = req
	disp.waits.Wait(id, blockers...)
	disp.sim.logf("ROUTE_WAIT - %s to %s: %v", req.Train.Name, req.Station.Code, err)
	return nil, false
}

func (disp *Dispatcher) served(id ids.TrainID, route *interlocking.Route) {
	delete(disp.requests, id)
	disp.waits.Remove(id)
	disp.frontier[id] = route
}

// OnTrackReleased retries the requests waiting on the track, longest-waiting
// first. Each either gets its route or waits again on whatever now blocks it.
func (disp *Dispatcher) OnTrackReleased(track *infra.TrackSegment) {
	for _, id := range disp.waits.Wake(track.Id) {
		req := disp.requests[id]
		route, blockers, err := disp.try(req)
		if err != nil {
			req.LastErr = err
			disp.waits.Wait(id, blockers...)
			continue
		}
		disp.served(id, route)
		disp.sim.logf("ROUTE_RETRY_OK - %s to %s after %s was released", req.Train.Name, req.Station.Code, track)
		disp.sim.ScheduleEventNext(ops.RouteGranted, route, id)
	}
}

// Waiting returns the queued requests, longest-waiting first.
func (disp *Dispatcher) Waiting() []*RouteRequest {
	ids := disp.waits.Waiting()
	out := make([]*RouteRequest, len(ids))
	for i, id := range ids {
		out[i] = disp.requests[id]
	}
	return out
}

func (disp *Dispatcher) waitingFor(train ids.TrainID) *RouteRequest {
	return disp.requests[train]
}

// blockerSet collects the tracks a failed request should wait on.
type blockerSet struct {
	seen map[ids.TrackID]struct{}
	ids  []ids.TrackID
}

func (b *blockerSet) add(tracks ...*infra.TrackSegment) {
	if b.seen == nil {
		b.seen = map[ids.TrackID]struct{}{}
	}
	for _, t := range tracks {
		if _, ok := b.seen[t.Id]; !ok {
			b.seen[t.Id] = struct{}{}
			b.ids = append(b.ids, t.Id)
		}
	}
}

// attempt reserves the section and keeps it only if the result is safe.
func (disp *Dispatcher) attempt(section *infra.Path, req *RouteRequest, blockers *blockerSet) (*interlocking.Route, error) {
	train := req.Train
	route, err := disp.ilk.Reserve(section, train.ID())
	if err != nil {
		var conflict *interlocking.ConflictError
		if errors.As(err, &conflict) {
			blockers.add(conflict.Blockers()...)
		}
		return nil, err
	}
	ok, holders := disp.safe(disp.sim.trainCtrllers[train.ID()], route)
	if !ok {
		disp.ilk.Release(route) // nothing changed overall, so nobody needs waking
		blockers.add(holders...)
		return nil, ErrUnsafe
	}
	return route, nil
}

func (disp *Dispatcher) try(req *RouteRequest) (*interlocking.Route, []ids.TrackID, error) {
	var blockers blockerSet
	var route *interlocking.Route
	var err error
	if req.Entry {
		route, err = disp.tryEntry(req, &blockers)
	} else {
		route, err = disp.tryRoute(req, &blockers)
	}
	return route, blockers.ids, err
}

// platformsByPreference lists the station's platforms, preferred one first.
func platformsByPreference(stn *infra.Station, prefPfNo string) []*infra.Platform {
	pfs := stn.Platforms()
	if i := slices.IndexFunc(pfs, func(p *infra.Platform) bool { return p.PfNo == prefPfNo }); i > 0 {
		pref := pfs[i]
		pfs = append([]*infra.Platform{pref}, slices.Delete(pfs, i, i+1)...)
	}
	return pfs
}

func (disp *Dispatcher) tryEntry(req *RouteRequest, blockers *blockerSet) (*interlocking.Route, error) {
	var firstErr error
	for _, pf := range platformsByPreference(req.Station, req.PrefPfNo) {
		boundary := disp.world.TrackGraph.FindWorldBoundaryPoint(pf.Track)
		if boundary == nil {
			continue
		}
		edge, _ := disp.world.TrackGraph.Edge(pf.Track)
		route, err := disp.attempt(&infra.Path{InitPoint: boundary, Edges: []*infra.GraphEdge{edge}}, req, blockers)
		if err == nil {
			return route, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = ErrNoEntryPlatform
	}
	return nil, firstErr
}

// tryRoute reserves the first signal section of the best way from the train
// to a platform of the station. Ways to free platforms are tried before ways
// to occupied ones (preferred platform first within each), then by rank.
// Wrong-direction running is only considered when the station cannot be
// reached the right way at all (e.g. single-line working).
func (disp *Dispatcher) tryRoute(req *RouteRequest, blockers *blockerSet) (*interlocking.Route, error) {
	train := req.Train
	from := train.FacingToward
	if from == nil {
		return nil, errors.New("train has no position")
	}
	via := train.CurrentEdge()

	type candidates struct {
		pf     *infra.Platform
		ranked []infra.RankedPath
	}
	var free, busy []candidates
	anyCorrect := false
	for _, pf := range platformsByPreference(req.Station, req.PrefPfNo) {
		ranked, err := disp.world.RankedPaths(from, via, pf.Track)
		if err != nil {
			return nil, err
		}
		c := candidates{pf, ranked}
		if disp.ilk.IsTrackFree(pf.Track) {
			free = append(free, c)
		} else {
			busy = append(busy, c)
		}
		anyCorrect = anyCorrect || slices.ContainsFunc(ranked, func(r infra.RankedPath) bool { return r.FollowsSignals })
	}

	tried := map[string]struct{}{}
	var firstErr error
	for _, c := range append(free, busy...) {
		for _, r := range c.ranked {
			if anyCorrect && !r.FollowsSignals {
				continue
			}
			steps, err := r.Path.Steps()
			if err != nil {
				continue
			}
			section := r.Path.Prefix(disp.world.SectionEnd(steps, 0))
			key := section.String()
			if _, done := tried[key]; done {
				continue
			}
			tried[key] = struct{}{}

			route, err := disp.attempt(section, req, blockers)
			if err == nil {
				return route, nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr == nil {
		firstErr = fmt.Errorf("no route to %s", req.Station.Code)
	}
	return nil, firstErr
}
