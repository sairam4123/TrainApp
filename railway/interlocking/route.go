package interlocking

import (
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

// Route is a path the interlocking has reserved for one train: every track on
// it is reserved for the train and every switch on it is set and locked.
// Only the Interlocking that issued it can change it.
//
// A train runs a route edge by edge. Edges [0, Left()) have been passed and
// freed, [Left(), Entered()) are occupied by the train, and
// [Entered(), End()) are reserved ahead of it.
type Route struct {
	train ids.TrainID
	path  *infra.Path
	steps []infra.Step

	entered int
	left    int
	end     int // edges at or past end are no longer reserved (see Interlocking.Release)
}

func (r *Route) Train() ids.TrainID { return r.train }
func (r *Route) Path() *infra.Path  { return r.path }

// Len is the number of edges in the route's path.
func (r *Route) Len() int { return len(r.steps) }

// Step returns edge i with the direction the train runs it.
func (r *Route) Step(i int) infra.Step { return r.steps[i] }

// Entered is how many edges the train has entered.
func (r *Route) Entered() int { return r.entered }

// Left is how many edges the train has left.
func (r *Route) Left() int { return r.left }

// End is how many edges of the path are (or were) held by the route.
func (r *Route) End() int { return r.end }

// Head is the step the front of the train is on, if it has entered the route.
func (r *Route) Head() (infra.Step, bool) {
	if r.entered == 0 {
		return infra.Step{}, false
	}
	return r.steps[r.entered-1], true
}

// FullyEntered reports whether the train has entered every held edge.
func (r *Route) FullyEntered() bool { return r.entered == r.end }

// Done reports whether the train has left every edge it entered and holds
// nothing ahead.
func (r *Route) Done() bool { return r.left == r.end }
