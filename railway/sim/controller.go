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
//
// A running train follows a speed curve (motion.Curve) from where it was last
// planned to the end of its authority, where it comes to rest. The curve is
// planned when the train sets off, and again when it is granted more while
// running. The front entering a track, reaching its end (TRACK_TRAVEL_END)
// and the rear clearing a track behind it (TRACK_EXITED) still happen one
// step at a time: a step's events are scheduled from the curve when the front
// enters it (TRACK_ENTER), and again whenever the curve changes.
//
// A train that brakes (Train.Brake) asks for the section after its authority
// on approach: at its braking point less the profile's approach lead
// (TRACK_APPROACH). Granted, it runs on without stopping; refused, it brakes
// and stops at the end of its authority, its request waiting. A train that
// stops instantly asks once it has stopped, at the end of its authority, as it
// always has. See docs/trainapp/length_dynamics/phase3.md.
type TrainController struct {
	sim   *Sim
	train *ops.Train
	phase trainPhase

	// for the look-ahead (projection.go)
	since     float64 // when the front entered the track it is on
	edgeExit  float64 // when the front is due to leave it, while running
	arrivedAt float64 // when it last arrived at a stop
	dwellEnd  float64 // when its current dwell ends

	// the speed curve, over way: the step the front was on when it was
	// planned, then the rest of the train's authority. wayHead is the step
	// the front is on now.
	curve   motion.Curve
	way     []infra.Step
	wayHead int
	// anchor is when the front was where the curve's part of its step
	// starts: when it entered the step, or when the curve was planned again
	// partway along it
	anchor float64
	// entered is set once the front's TRACK_ENTER for its step has run, so
	// the step's events are scheduled; atEnd once it has reached the step's
	// end (TRACK_TRAVEL_END) and not yet moved on
	entered, atEnd bool

	// the pending events of the front's step: its TRACK_TRAVEL_END, its
	// TRACK_APPROACH, and the TRACK_EXITED of the rear clearing body steps,
	// in the order they fire
	travelEnd, approach des.EventID
	exits               []des.EventID

	// asked is set once the train has asked for the section after its
	// authority, until it starts on a new one; next is that section's
	// authority once granted, while it is still on the one before
	asked bool
	next  *ops.MovementAuthority
	// passing is set once the train has asked for a section beyond its
	// current stop, a pass, to run through it; it heads to the stop after
	passing bool
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
			tc.depart(ma)
		case phaseRunning, phaseDwelling:
			// granted on approach: it runs on to it from the end of its
			// route, or departs with it once its dwell at a pass is over
			if tc.next != nil {
				tc.fail("granted a second section ahead")
				return
			}
			tc.next = ma
			if tc.phase == phaseRunning {
				tc.replan()
			}
		default:
			tc.fail("unexpected movement authority")
		}

	case ops.TrainDeparted:
		tc.begin(data.(*ops.MovementAuthority))

	case ops.TrackEntered:
		tc.since = sim.CurTime()
		tc.anchor = tc.since
		tc.entered = true
		curTrack := train.CurrentTrack()
		if curTrack == nil {
			tc.fail("entered a track but is on none")
			return
		}
		tc.recordVia(curTrack)
		if step, _ := train.Route().Head(); tc.wayHead >= len(tc.way) || tc.way[tc.wayHead] != step {
			tc.fail("entered %s, off its curve", curTrack)
			return
		}
		tc.scheduleStep()

	case ops.TrackExited:
		// the rear has left the oldest step the train occupies
		body := train.Body()
		if len(tc.exits) == 0 || len(body) < 2 || body[0].Step.Edge.Track != data.(*infra.TrackSegment) {
			tc.fail("rear cleared %v out of turn", data)
			return
		}
		tc.exits = tc.exits[1:]
		train.MoveFront(train.ClearPoint(body[0]))
		tc.leaveRear()

	case ops.TrackApproach:
		tc.approach = 0
		if tc.phase == phaseRunning && !tc.asked && tc.next == nil {
			tc.ask()
		}

	case ops.TrackTravelEnd:
		tc.travelEnd = 0
		tc.atEnd = true
		if body := train.Body(); len(body) > 0 {
			train.MoveFront(body[len(body)-1].End())
		}
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
		case tc.next != nil:
			tc.runOn()
		default:
			sim.ScheduleEventNext(ops.PathCompleted, train, train.ID())
		}

	case ops.MovementAuthorityExtended:
		tc.advance()

	case ops.PathCompleted:
		if tc.next != nil {
			// granted just as it reached the end of its route
			tc.runOn()
			return
		}
		stop, _ := train.CurrentStop()
		stn, _ := sim.world.GetStation(stop.StnCode)
		route := train.Route()
		if route.FullyEntered() && stn.IsPlatform(train.CurrentTrack().Id) {
			sim.ScheduleEventNext(ops.TrainArrived, train, train.ID())
		} else {
			sim.ScheduleEventNext(ops.MovementAuthorityEnded, train, train.ID())
		}

	case ops.MovementAuthorityEnded:
		if tc.next != nil {
			tc.runOn()
			return
		}
		// stopped at a signal short of the stop; ask for the next section,
		// unless it already has on approach
		tc.phase = phaseHeld
		tc.release(train.Route())
		stop, _ := train.CurrentStop()
		stop.BeginSignalStop(sim.CurTime(), train.CurrentTrack().String())
		if tc.asked {
			return
		}
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
		if stn, ok := sim.world.GetStation(stop.StnCode); ok {
			_, extra := sim.overhang(train, stop, platformOf(stn, train.CurrentTrack()))
			dwell += units.Min(extra)
		}
		tc.arrivedAt = sim.CurTime()
		tc.dwellEnd = sim.CurTime() + max(float64(dwell), des.MinDeltaTime)
		sim.ScheduleEventAfter(dwell, ops.TrainDwellEnd, train, train.ID())

	case ops.TrainDwellEnd:
		tc.phase = phaseReady
		if tc.next != nil {
			// granted on approach to a pass it then had to stop at
			ma := tc.next
			tc.next = nil
			tc.depart(ma)
			return
		}
		next, ok := train.NextStop()
		if !ok {
			sim.ScheduleEventNext(ops.ScheduleEnd, train, train.ID())
			return
		}
		if tc.asked {
			return // its request from the approach is waiting
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
		tc.cancelStep()
		tc.release(train.Route())
		for len(train.Body()) > 0 {
			tc.leaveRear()
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

// depart has the train, ready at its stop, leave it under ma.
func (tc *TrainController) depart(ma *ops.MovementAuthority) {
	train := tc.train
	stop, _ := train.CurrentStop()
	stop.RecordDepartureAfter(tc.sim.CurTime(), tc.sim.dispatcher.WokenBy(train.ID()))
	train.AdvanceStop()
	tc.passing = false
	tc.phase = phaseRunning
	tc.sim.ScheduleEventNext(ops.TrainDeparted, ma, train.ID())
}

// runOn has the train, at the end of its route, carry straight on under the
// authority it was granted on approach. At a pass it runs through: it
// arrives and departs at once.
func (tc *TrainController) runOn() {
	train := tc.train
	sim := tc.sim
	ma := tc.next
	tc.next = nil
	if tc.passing {
		now := sim.CurTime()
		stop, _ := train.CurrentStop()
		if !stop.Arrived() { // the first stop's arrival is the entry
			stop.RecordArrival(now)
		}
		tc.recordPlatform(stop)
		stop.RecordDepartureAfter(now, sim.dispatcher.WokenBy(train.ID()))
		if sim.prec != nil {
			sim.prec.finishPlatform(train.ID(), stop.StnCode)
		}
		sim.logPass(train, stop)
		train.AdvanceStop()
		tc.passing = false
	}
	tc.begin(ma)
}

// begin starts the train on the route of a new authority: its front enters the
// first edge, the unused rest of the previous route is given back, and the
// rear leaves whatever it has cleared. Its curve carries on if it already
// runs onto the route (granted on approach); otherwise it sets off from rest,
// or comes into the world at its entry speed.
func (tc *TrainController) begin(ma *ops.MovementAuthority) {
	train := tc.train
	old := train.Route()
	entering := tc.phase == phaseOutside

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
	train.EnterStep(ma.Route(), step)
	tc.asked = false
	if tc.wayHead+1 < len(tc.way) && tc.way[tc.wayHead+1] == step {
		tc.wayHead++
	} else {
		v := units.MetersPerMin(0)
		if entering {
			v = train.EntersAt()
		}
		tc.plan(0, v)
	}
	tc.headChanged()

	if old != nil {
		tc.release(old)
	}
	tc.freeCleared()
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

// recordCleared notes the rear has left trk, on the way to the stop the train
// is on or one before: a long train's rear can still be on the way to the
// last stop when its front has reached it, or left it.
func (tc *TrainController) recordCleared(trk *infra.TrackSegment) {
	sched := tc.train.Schedule()
	for k := min(tc.train.StopIndex(), len(sched)-1); k >= 0; k-- {
		if sched[k].ClearTrack(tc.sim.CurTime(), trk.Id.String()) {
			return
		}
	}
}

// advance moves the train's front onto the next edge of its route, and its
// rear off whatever it has cleared.
func (tc *TrainController) advance() {
	route := tc.train.Route()
	step, err := tc.sim.ilk.Enter(route)
	if err != nil {
		tc.fail("%v", err)
		return
	}
	tc.train.FacingToward = step.To
	tc.train.EnterStep(route, step)
	tc.wayHead++
	tc.headChanged()
	tc.freeCleared()
	tc.sim.ScheduleEventNext(ops.TrackEntered, tc.train, tc.train.ID())
}

// headChanged notes the front has moved onto a new step: when it enters it
// and when it is due to leave it. The front sets off along it at its
// TRACK_ENTER, one hop from now, which schedules the step's events, but the
// look-ahead may need these before then.
func (tc *TrainController) headChanged() {
	tc.entered, tc.atEnd = false, false
	tc.since = tc.sim.CurTime() + des.MinDeltaTime
	tc.edgeExit = tc.since + tc.curve.Time(tc.wayHead)
}

// ahead is the steps from the front's to the end of the train's authority:
// the rest of its route, then the route it was granted on approach.
func (tc *TrainController) ahead() []infra.Step {
	t := tc.train
	route := t.Route()
	head, _ := route.Head()
	steps := []infra.Step{head}
	for i := route.Entered(); i < route.End(); i++ {
		steps = append(steps, route.Step(i))
	}
	if tc.next != nil {
		r := tc.next.Route()
		for i := range r.End() {
			steps = append(steps, r.Step(i))
		}
	}
	return steps
}

// plan gives the train a new curve from off metres along its front's step at
// speed v, to rest at the end of its authority.
func (tc *TrainController) plan(off units.Meters, v units.MetersPerMin) {
	t := tc.train
	body := t.Body()
	behind := make([]infra.Step, 0, len(body))
	for _, b := range body[:max(len(body)-1, 0)] {
		behind = append(behind, b.Step)
	}
	tc.way = tc.ahead()
	tc.wayHead = 0
	tc.curve = motion.Plan(t, motion.Way{Behind: behind, Steps: tc.way, From: off, Speed: v, Stop: true})
}

// position is where the front is now, metres along its step, and its speed.
func (tc *TrainController) position() (units.Meters, units.MetersPerMin) {
	i := tc.wayHead
	switch {
	case tc.atEnd:
		return tc.curve.Length(i), tc.curve.EndSpeed(i)
	case !tc.entered:
		return tc.curve.At(i, 0)
	}
	return tc.curve.At(i, tc.sim.CurTime()-tc.anchor)
}

// dynamic reports whether the train accelerates or brakes at a finite rate.
func (tc *TrainController) dynamic() bool {
	return tc.train.Accel > 0 || tc.train.Brake > 0
}

// keys are the front's keys on its step (see Key), for a train that
// accelerates or brakes, once it has set off along the step.
func (tc *TrainController) keys() []Key {
	if !tc.dynamic() || !tc.entered || tc.wayHead >= len(tc.way) {
		return nil
	}
	i := tc.wayHead
	length := float64(tc.curve.Length(i))
	var out []Key
	for _, k := range tc.curve.Keys(i) {
		frac := 1.0
		if length > 0 {
			frac = float64(k.Off) / length
		}
		out = append(out, Key{T: tc.anchor + k.T, Frac: frac, Kmph: k.Kmph})
	}
	return out
}

// standTrack is the track the train stands on if what it asks for next is
// refused: the one its front is on, or for a running train the last of what
// it has been granted.
func (tc *TrainController) standTrack() *infra.TrackSegment {
	if tc.phase == phaseRunning {
		if r := tc.sim.dispatcher.frontier[tc.train.ID()]; r != nil && r.End() > 0 {
			return r.Step(r.End() - 1).Edge.Track
		}
	}
	return tc.train.CurrentTrack()
}

// projectFrom is where the look-ahead starts the running train from (see
// projection.go): where the front was when the curve's part of its step
// began, at its speed then, and when. Once the curve brakes to the end of the
// authority on this step, the train no longer runs as it would free, so it
// starts from where it is now instead.
func (tc *TrainController) projectFrom() (units.Meters, units.MetersPerMin, float64) {
	now := tc.sim.CurTime()
	i := tc.wayHead
	switch {
	case tc.atEnd:
		return tc.curve.Length(i), tc.curve.EndSpeed(i), now
	case !tc.entered:
		off, v := tc.curve.At(i, 0)
		return off, v, tc.since
	}
	if bp := tc.curve.BrakingPoint(); bp.Step < i || (bp.Step == i && bp.Off < tc.curve.Length(i)) {
		off, v := tc.position()
		return off, v, now
	}
	off, v := tc.curve.At(i, 0)
	return off, v, tc.anchor
}

// replan plans the train's curve again from where it is now, once it has
// been granted more while running, and schedules its step's events from it.
func (tc *TrainController) replan() {
	off, v := tc.position()
	tc.plan(off, v)
	switch {
	case !tc.entered:
		// its TRACK_ENTER schedules the step's events
		tc.edgeExit = tc.since + tc.curve.Time(tc.wayHead)
	case !tc.atEnd:
		tc.cancelStep()
		tc.anchor = tc.sim.CurTime()
		tc.scheduleStep()
	}
}

// cancelStep cancels the pending events of the front's step.
func (tc *TrainController) cancelStep() {
	for _, id := range append(tc.exits, tc.travelEnd, tc.approach) {
		if id != 0 {
			tc.sim.CancelEvent(id)
		}
	}
	tc.exits, tc.travelEnd, tc.approach = nil, 0, 0
}

// scheduleStep schedules the events of the front's step from the curve, from
// now, where the curve's part of the step starts: its TRACK_TRAVEL_END, a
// TRACK_EXITED for each step behind it whose clear point the front reaches
// strictly before the step's end, and its TRACK_APPROACH if the approach
// point is on it or already behind. A clear point at the end itself is left
// for freeCleared when the front moves on.
func (tc *TrainController) scheduleStep() {
	t := tc.train
	sim := tc.sim
	i := tc.wayHead
	tc.edgeExit = tc.anchor + tc.curve.Time(i)
	tc.travelEnd = sim.ScheduleEventAfter(units.Min(tc.curve.Time(i)), ops.TrackTravelEnd, t, t.ID())

	body := t.Body()
	head := body[len(body)-1]
	for _, b := range body[:len(body)-1] {
		cp := t.ClearPoint(b)
		if cp <= head.Start || cp >= head.End() {
			continue
		}
		at := tc.curve.TimeIn(i, units.Meters(cp-head.Start))
		tc.exits = append(tc.exits, sim.ScheduleEventAfter(units.Min(at), ops.TrackExited, b.Step.Edge.Track, t.ID()))
	}

	if !tc.asksOnApproach() {
		return
	}
	ap := tc.curve.PointAt(tc.curve.BrakingPoint().T - float64(sim.ApproachLead()))
	switch {
	case ap.Step < i:
		tc.approach = sim.ScheduleEventNext(ops.TrackApproach, t, t.ID())
	case ap.Step == i:
		tc.approach = sim.ScheduleEventAfter(units.Min(tc.curve.TimeIn(i, ap.Off)), ops.TrackApproach, t, t.ID())
	}
}

// asksOnApproach reports whether the train is to ask for the section after
// its authority before it gets there: it brakes, has not asked yet, and does
// not stop at the end of its authority anyway (see nextStop).
func (tc *TrainController) asksOnApproach() bool {
	if !(tc.train.Brake > 0) || tc.asked || tc.next != nil {
		return false
	}
	_, _, ok := tc.nextStop()
	return ok
}

// nextStop is the stop the train heads for once past the end of its route,
// and whether that means running through its current stop, a pass. It
// reports false when the route ends at a platform of the current stop and
// the train stands there: a call, a halt, a stand for a disruption, or its
// last point.
func (tc *TrainController) nextStop() (k int, passing, ok bool) {
	t := tc.train
	route := t.Route()
	sched := t.Schedule()
	k = t.StopIndex()
	if route == nil || route.End() == 0 || k >= len(sched) {
		return 0, false, false
	}
	stn, found := tc.sim.world.GetStation(sched[k].StnCode)
	if !found {
		return 0, false, false
	}
	if !stn.IsPlatform(route.Step(route.End() - 1).Edge.Track.Id) {
		return k, false, true // a signal short of the stop
	}
	if sp := sched[k]; !sp.IsPass() || sp.ExtraStand() > 0 || k+1 >= len(sched) {
		return 0, false, false
	}
	return k + 1, true, true
}

// ask has the running train ask for the section after its route, at its
// approach point.
func (tc *TrainController) ask() {
	t := tc.train
	sim := tc.sim
	k, passing, ok := tc.nextStop()
	if !ok {
		return
	}
	stop := t.Schedule()[k]
	stn, found := sim.world.GetStation(stop.StnCode)
	if !found {
		tc.fail("unknown station %s in schedule", stop.StnCode)
		return
	}
	tc.asked = true
	tc.passing = passing
	if route, ok := sim.dispatcher.RequestAhead(t, stn, stop.SpPfNo, routeEnd(t.Route())); ok {
		sim.ScheduleEventNext(ops.RouteGranted, route, t.ID())
	}
}

// freeCleared frees every step behind the head whose clear point the front
// has reached: the rear has left it. With no length, that is the step the
// front has just left.
func (tc *TrainController) freeCleared() {
	t := tc.train
	for body := t.Body(); len(body) > 1 && t.ClearPoint(body[0]) <= t.Odometer(); body = t.Body() {
		tc.leaveRear()
	}
}

// leaveRear frees the oldest step the train occupies: its rear has left it.
func (tc *TrainController) leaveRear() {
	b := tc.train.DropRear()
	step, err := tc.sim.ilk.Leave(b.Route)
	if err != nil {
		tc.fail("%v", err)
		return
	}
	if step.Edge != b.Step.Edge {
		tc.fail("rear left %s, but the interlocking freed %s", b.Step.Edge.Track, step.Edge.Track)
		return
	}
	tc.recordCleared(step.Edge.Track)
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
