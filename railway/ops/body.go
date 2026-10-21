package ops

import (
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/units"
)

// Pointer is a place on the track: a step and how far along it, from the end
// the train entered by.
type Pointer struct {
	Step   infra.Step
	Offset units.Meters
}

// BodyStep is a step the train occupies, with the route it is on and the
// odometer reading where it starts.
type BodyStep struct {
	Step  infra.Step
	Route *interlocking.Route
	Start float64 // m, on the train's odometer
}

// End is the odometer reading at the step's end.
func (b BodyStep) End() float64 {
	return b.Start + float64(b.Step.Edge.Track.Length)
}

// Body is the steps the train occupies, oldest (under its rear) first; the
// last is its head. The train's odometer is how far its front has run since it
// entered the world. Only the sim's controller changes them, as the train
// moves; see docs/trainapp/length_dynamics/phase2.md.
func (t *Train) Body() []BodyStep {
	return t.body
}

// Odometer is how far the front has run since the train entered the world, as
// of the train's last event.
func (t *Train) Odometer() float64 {
	return t.odo
}

// EnterStep puts the front at the start of step, the next on route.
func (t *Train) EnterStep(route *interlocking.Route, step infra.Step) {
	t.body = append(t.body, BodyStep{Step: step, Route: route, Start: t.odo})
}

// MoveFront sets the odometer: the front has run to odo.
func (t *Train) MoveFront(odo float64) {
	t.odo = odo
}

// DropRear removes the oldest step from the body, once the rear has left it.
func (t *Train) DropRear() BodyStep {
	b := t.body[0]
	t.body[0] = BodyStep{}
	t.body = t.body[1:]
	return b
}

// ClearPoint is the odometer reading at which the rear leaves body step b: its
// end plus the train's length.
func (t *Train) ClearPoint(b BodyStep) float64 {
	return b.End() + float64(t.Length)
}

// Head is where the front of the train is, as of its last event. It reports
// false when the train occupies nothing.
func (t *Train) Head() (Pointer, bool) {
	if len(t.body) == 0 {
		return Pointer{}, false
	}
	h := t.body[len(t.body)-1]
	return Pointer{Step: h.Step, Offset: units.Meters(t.odo - h.Start)}, true
}

// Rear is where the back of the train is, Length metres behind its front, as
// of its last event. It reports false when the train occupies nothing, or
// while its rear is still outside the world, entering.
func (t *Train) Rear() (Pointer, bool) {
	if len(t.body) == 0 {
		return Pointer{}, false
	}
	r := t.odo - float64(t.Length)
	if r < t.body[0].Start {
		return Pointer{}, false
	}
	for _, b := range t.body {
		if r <= b.End() {
			return Pointer{Step: b.Step, Offset: units.Meters(r - b.Start)}, true
		}
	}
	return t.Head()
}

// clearBody forgets where the train is, e.g. when it leaves the world.
func (t *Train) clearBody() {
	t.body = nil
	t.odo = 0
}
