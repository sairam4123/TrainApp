package sim

import (
	"cmp"
	"slices"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
)

// fit is how well a train fits a platform track for a call at it; lower is
// better. See docs/trainapp/length_dynamics/phase2.md, §4.
type fit int

const (
	// fitsPlatform: for a stop, the platform is long enough; for a pass or a
	// halt (no passengers), the track is.
	fitsPlatform fit = iota
	// fitsTrack: a stop where the track is long enough but the platform is
	// not, so some coaches stand beyond it.
	fitsTrack
	// fitsNeither: the train is longer than the track, so its tail stands on
	// the track behind.
	fitsNeither
)

// passengers reports whether passengers get on or off at sp: a stop, not a
// pass or a halt timed to stand.
func passengers(sp *ops.SchedulePoint) bool {
	return !sp.IsPass() && !sp.IsHalt()
}

// fitOf is how well train t fits platform pf for its call sp. A train with no
// length fits everywhere.
func fitOf(t *ops.Train, sp *ops.SchedulePoint, pf *infra.Platform) fit {
	l := t.Length
	switch {
	case l <= 0:
		return fitsPlatform
	case passengers(sp) && l <= pf.Length:
		return fitsPlatform
	case l <= pf.Track.Length:
		if passengers(sp) {
			return fitsTrack
		}
		return fitsPlatform
	}
	return fitsNeither
}

// callable reports whether pf can take a train's call sp. A call with
// passengers (a stop, not a pass or a halt) is only ever made at a track with
// a platform: a through track has none, so it is for trains that run through
// or stand without calling. A nil sp is unknown, and anything goes.
func callable(sp *ops.SchedulePoint, pf *infra.Platform) bool {
	return sp == nil || !passengers(sp) || !pf.IsThrough()
}

// visitOf is the schedule point as a stop VisitEnds checks: a call if
// passengers get on or off there.
func visitOf(sp *ops.SchedulePoint) infra.Visit {
	return infra.Visit{Stn: sp.StnCode, Call: passengers(sp)}
}

// byFit orders the platforms best fit first for train t's call sp, keeping
// their order within each fit, and leaves out those it cannot call at
// (callable).
func byFit(t *ops.Train, sp *ops.SchedulePoint, pfs []*infra.Platform) []*infra.Platform {
	pfs = slices.DeleteFunc(slices.Clone(pfs), func(pf *infra.Platform) bool { return !callable(sp, pf) })
	if t.Length <= 0 || sp == nil {
		return pfs
	}
	out := slices.Clone(pfs)
	slices.SortStableFunc(out, func(a, b *infra.Platform) int {
		return cmp.Compare(fitOf(t, sp, a), fitOf(t, sp, b))
	})
	return out
}

// stopOf is the schedule point a request is for: the first on entry, else the
// one the train is heading to.
func (disp *Dispatcher) stopOf(req *RouteRequest) *ops.SchedulePoint {
	sched := req.Train.Schedule()
	k := 0
	if !req.Entry {
		k = headingTo(disp.sim.trainCtrllers[req.Train.ID()])
	}
	if k >= len(sched) {
		return nil
	}
	return sched[k]
}
