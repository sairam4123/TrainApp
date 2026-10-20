package sim

import (
	"fmt"
	"trainapp/des"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/railway/motion"
	"trainapp/railway/ops"
	"trainapp/units"
)

type trainPhase int

const (
	phaseOutside  trainPhase = iota // not in the world yet
	phaseRunning                    // moving under a movement authority
	phaseHeld                       // stopped at the end of its authority, short of its stop
	phaseDwelling                   // stopped at a scheduled platform
	phaseReady                      // dwell over, waiting to depart
	phaseExited                     // left the world
)

func (p trainPhase) String() string {
	switch p {
	case phaseOutside:
		return "outside"
	case phaseRunning:
		return "running"
	case phaseHeld:
		return "held"
	case phaseDwelling:
		return "dwelling"
	case phaseReady:
		return "ready"
	case phaseExited:
		return "exited"
	}
	return fmt.Sprintf("phase(%d)", int(p))
}

// TrainController is the state machine for one train.
type TrainController struct {
	sim   *Sim
	train *ops.Train
	phase trainPhase

	// for the look-ahead (projection.go)
	since     float64 // when the front entered the track it is on
	edgeExit  float64 // when the front is due to leave it, while running
	arrivedAt float64 // when it last arrived at a stop
	dwellEnd  float64 // when its current dwell ends
}

func (tc *TrainController) fail(format string, args ...any) {
	tc.sim.fail(fmt.Errorf("train %s (%s): %s", tc.train.Number, tc.phase, fmt.Sprintf(format, args...)))
}

func (tc *TrainController) OnEvent(event ops.RailwayEvent, data any) {
	train := tc.train
	sim := tc.sim
	disp := sim.dispatcher

	switch event {
	case ops.WorldEntered:
		if tc.phase != phaseOutside {
			tc.fail("entered the world twice")
			return
		}
		stop, _ := train.CurrentStop()
		stn, ok := sim.world.GetStation(stop.StnCode)
		if !ok {
			tc.fail("unknown station %s in schedule", stop.StnCode)
			return
		}
		if route, ok := disp.RequestEntry(train, stn, stop.SpPfNo); ok {
			sim.ScheduleEventNext(ops.RouteGranted, route, train.ID())
		}

	case ops.RouteGranted:
		route := data.(*interlocking.Route)
		ma, err := disp.Authorize(train, route)
		if err != nil {
			tc.fail("cannot authorize granted route: %v", err)
			return
		}
		sim.ScheduleEventNext(ops.MovementAuthorized, ma, train.ID())

	case ops.MovementAuthorized:
		ma := data.(*ops.MovementAuthority)
		switch tc.phase {
		case phaseOutside:
			// entering the world is arriving at the first stop, whose
			// timetabled arrival is the entry time
			stop, _ := train.CurrentStop()
			stop.RecordArrival(sim.CurTime())
			tc.begin(ma)
			tc.recordPlatform(stop)
		case phaseHeld:
			stop, _ := train.CurrentStop()
			stop.EndSignalStop(sim.CurTime(), disp.WokenBy(train.ID()))
			tc.begin(ma)
		case phaseReady:
			stop, _ := train.CurrentStop()
			stop.RecordDepartureAfter(sim.CurTime(), disp.WokenBy(train.ID()))
			train.AdvanceStop()
			tc.phase = phaseRunning
			sim.ScheduleEventNext(ops.TrainDeparted, ma, train.ID())
		default:
			tc.fail("unexpected movement authority")
		}

	case ops.TrainDeparted:
		tc.begin(data.(*ops.MovementAuthority))

	case ops.TrackEntered:
		tc.since = sim.CurTime()
		curTrack := train.CurrentTrack()
		if curTrack == nil {
			tc.fail("entered a track but is on none")
			return
		}
		tc.recordVia(curTrack)
		step, _ := train.Route().Head()
		run := motion.Plan(train, []infra.Step{step}, false).Times[0]
		tc.edgeExit = tc.since + run
		sim.ScheduleEventAfter(units.Min(run), ops.TrackTravelEnd, train, train.ID())

	case ops.TrackTravelEnd:
		ma := train.Authority()
		switch {
		case ma.PermitsNext():
			tc.advance()
		case ma.CanExtend():
			// at a signal with more of the route already reserved beyond it
			if err := ma.Extend(sim.world); err != nil {
				tc.fail("%v", err)
				return
			}
			sim.ScheduleEventNext(ops.MovementAuthorityExtended, ma, train.ID())
		default:
			sim.ScheduleEventNext(ops.PathCompleted, train, train.ID())
		}

	case ops.MovementAuthorityExtended:
		tc.advance()

	case ops.PathCompleted:
		stop, _ := train.CurrentStop()
		stn, _ := sim.world.GetStation(stop.StnCode)
		route := train.Route()
		if route.FullyEntered() && stn.IsPlatform(train.CurrentTrack().Id) {
			sim.ScheduleEventNext(ops.TrainArrived, train, train.ID())
		} else {
			sim.ScheduleEventNext(ops.MovementAuthorityEnded, train, train.ID())
		}

	case ops.MovementAuthorityEnded:
		// stopped at a signal short of the stop; ask for the next section
		tc.phase = phaseHeld
		tc.release(train.Route())
		stop, _ := train.CurrentStop()
		stop.BeginSignalStop(sim.CurTime(), train.CurrentTrack().String())
		stn, _ := sim.world.GetStation(stop.StnCode)
		if route, ok := disp.RequestRoute(train, stn, stop.SpPfNo); ok {
			sim.ScheduleEventNext(ops.RouteGranted, route, train.ID())
		}

	case ops.TrainArrived:
		tc.phase = phaseDwelling
		stop, _ := train.CurrentStop()
		if !stop.Arrived() { // the first stop's arrival is the entry
			stop.RecordArrival(sim.CurTime())
		}
		tc.recordPlatform(stop)
		dwell := stop.ExpDwellTime(sim.CurTime(), sim.MinStop()) + stop.ExtraStand()
		tc.arrivedAt = sim.CurTime()
		tc.dwellEnd = sim.CurTime() + max(float64(dwell), des.MinDeltaTime)
		sim.ScheduleEventAfter(dwell, ops.TrainDwellEnd, train, train.ID())

	case ops.TrainDwellEnd:
		tc.phase = phaseReady
		next, ok := train.NextStop()
		if !ok {
			sim.ScheduleEventNext(ops.ScheduleEnd, train, train.ID())
			return
		}
		nextStn, ok := sim.world.GetStation(next.StnCode)
		if !ok {
			tc.fail("unknown station %s in schedule", next.StnCode)
			return
		}
		if route, ok := disp.RequestRoute(train, nextStn, next.SpPfNo); ok {
			sim.ScheduleEventNext(ops.RouteGranted, route, train.ID())
		}

	case ops.ScheduleEnd:
		stop, _ := train.CurrentStop()
		stop.RecordDeparture(sim.CurTime()) // it leaves the world from its last stop
		route := train.Route()
		tc.release(route)
		for route.Left() < route.Entered() {
			tc.leave(route)
		}
		train.ClearMovement()
		disp.Forget(train.ID())
		tc.phase = phaseExited
		sim.ScheduleEventNext(ops.WorldExited, train, train.ID())

	case ops.PrecedenceReview:
		disp.Review(train.ID())

	case ops.PriorityChanged:
		// waiting requests are ordered when they are next woken, so nothing
		// else needs to change
		train.Priority = data.(priorityChange).to

	case ops.TrackReleased, ops.WorldExited:
		// logged only

	default:
		tc.fail("unhandled event %s", event)
	}
}

// begin starts the train on the route of a new authority: its front enters the
// first edge, and its rear leaves whatever it occupied on the previous route.
func (tc *TrainController) begin(ma *ops.MovementAuthority) {
	train := tc.train
	old := train.Route()

	step, err := tc.sim.ilk.Enter(ma.Route())
	if err != nil {
		tc.fail("%v", err)
		return
	}
	if err := train.Authorize(ma); err != nil {
		tc.fail("%v", err)
		return
	}
	train.FacingToward = step.To

	if old != nil {
		tc.release(old)
		for old.Left() < old.Entered() {
			tc.leave(old)
		}
	}
	tc.phase = phaseRunning
	tc.sim.ScheduleEventNext(ops.TrackEntered, train, train.ID())
}

// recordPlatform notes the platform of the stop the train is on, if it is on
// one.
func (tc *TrainController) recordPlatform(stop *ops.SchedulePoint) {
	stn, ok := tc.sim.world.GetStation(stop.StnCode)
	if !ok {
		return
	}
	if pf := platformOf(stn, tc.train.CurrentTrack()); pf != nil {
		stop.RecordPlatform(pf.PfNo)
	}
}

// recordVia notes the track the front has entered on the way to the current
// stop. It is not of the way if it is a platform track of that stop's station
// or of the one before: those are the stops' own tracks.
func (tc *TrainController) recordVia(trk *infra.TrackSegment) {
	train := tc.train
	stop, ok := train.CurrentStop()
	if !ok || train.StopIndex() == 0 {
		return // the first point has no way to it
	}
	via := true
	if stn, ok := tc.sim.world.StationOfPlatform(trk); ok {
		via = stn.Code != stop.StnCode && stn.Code != train.Schedule()[train.StopIndex()-1].StnCode
	}
	stop.EnterTrack(tc.sim.CurTime(), trk.Id.String(), via)
}

// advance moves the train's front onto the next edge of its route and its rear
// off the previous one.
func (tc *TrainController) advance() {
	route := tc.train.Route()
	step, err := tc.sim.ilk.Enter(route)
	if err != nil {
		tc.fail("%v", err)
		return
	}
	tc.train.FacingToward = step.To
	tc.leave(route)
	tc.sim.ScheduleEventNext(ops.TrackEntered, tc.train, tc.train.ID())
}

// leave frees the rearmost edge the train occupies on the route.
func (tc *TrainController) leave(route *interlocking.Route) {
	step, err := tc.sim.ilk.Leave(route)
	if err != nil {
		tc.fail("%v", err)
		return
	}
	tc.sim.ScheduleEventNext(ops.TrackReleased, step.Edge.Track, tc.train.ID())
	tc.sim.dispatcher.OnTrackReleased(step.Edge.Track, tc.train.ID())
}

// release gives back the part of the route the train has not entered.
func (tc *TrainController) release(route *interlocking.Route) {
	for _, trk := range tc.sim.ilk.Release(route) {
		tc.sim.ScheduleEventNext(ops.TrackReleased, trk, tc.train.ID())
		tc.sim.dispatcher.OnTrackReleased(trk, tc.train.ID())
	}
}
