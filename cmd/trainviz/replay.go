package main

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"trainapp/visualization/tui"
	"trainapp/visualization/vizproto"

	"charm.land/lipgloss/v2"
)

// replay plays a recording: the state is every message up to applied, and t
// is the playback time.
type replay struct {
	world   *vizproto.World
	msgs    []vizproto.Message
	stops   []int // first message of each sim event's batch
	state   *vizproto.State
	applied int
	t       float64
	end     float64

	trains    []string          // in the order the sim added them
	trackName map[string]string // display name of each track
	pfNo      map[string]string // platform number of each platform track, by name
	failure   string            // why the run failed, if it did

	class   map[string]string // each train's class, if it has one
	classes []classKey        // the classes in the run, lowest priority first
}

// classKey is a class of train in the run, with the colour its trains are
// drawn in and its trains' priority when they first appear (the class's base
// priority; 0 in recordings before version 4).
type classKey struct {
	name     string
	priority int
	color    string
}

func newReplay(hello *vizproto.HelloMsg, msgs []vizproto.Message) *replay {
	r := &replay{world: &hello.World, msgs: msgs, trackName: map[string]string{}, pfNo: map[string]string{}}
	r.class = map[string]string{}
	for _, t := range hello.World.Trains {
		r.trains = append(r.trains, t.Number)
		if t.Class != "" {
			r.class[t.Number] = t.Class
		}
	}
	for _, t := range hello.World.Tracks {
		r.trackName[t.ID] = t.Name
	}
	for _, stn := range hello.World.Stations {
		for _, pf := range stn.Platforms {
			r.pfNo[r.trackName[pf.Track]] = pf.No
		}
	}
	for i, m := range msgs {
		_, isEvent := m.(*vizproto.EventMsg)
		if isEvent && (i == 0 || !isEventMsg(msgs[i-1])) {
			r.stops = append(r.stops, i)
		}
		if e, ok := m.(*vizproto.ErrorMsg); ok {
			r.failure = e.Message
		}
	}
	if len(msgs) > 0 {
		r.end = msgs[len(msgs)-1].Head().T
	}
	r.rankClasses()
	r.reset()
	return r
}

func isEventMsg(m vizproto.Message) bool {
	_, ok := m.(*vizproto.EventMsg)
	return ok
}

// reset goes back to the state before the first event.
func (r *replay) reset() {
	r.state = vizproto.NewState()
	r.applied = 0
	start := len(r.msgs)
	if len(r.stops) > 0 {
		start = r.stops[0]
	}
	r.applyTo(start)
	r.t = 0
}

// applyTo makes the state everything before message n, rebuilding it from the
// start if n is behind.
func (r *replay) applyTo(n int) {
	n = min(max(n, 0), len(r.msgs))
	if n < r.applied {
		r.state = vizproto.NewState()
		r.applied = 0
	}
	for ; r.applied < n; r.applied++ {
		r.state.Apply(r.msgs[r.applied])
	}
}

// seek moves playback to time t.
func (r *replay) seek(t float64) {
	t = min(max(t, 0), r.end)
	n := sort.Search(len(r.msgs), func(i int) bool { return r.msgs[i].Head().T > t })
	if len(r.stops) > 0 {
		n = max(n, r.stops[0])
	}
	r.applyTo(n)
	r.t = t
}

// advance plays dt sim minutes and reports whether there is more to play.
func (r *replay) advance(dt float64) bool {
	r.seek(r.t + dt)
	return r.t < r.end
}

// step moves one sim event forward or back.
func (r *replay) step(forward bool) {
	if forward {
		r.moveTo(r.nextStop(r.applied))
		return
	}
	r.moveTo(r.prevStop(r.applied))
}

// jumpTrain moves to just after the train's next (or previous) event.
func (r *replay) jumpTrain(train string, forward bool) {
	if forward {
		for i := r.applied; i < len(r.msgs); i++ {
			if e, ok := r.msgs[i].(*vizproto.EventMsg); ok && e.Train == train {
				r.moveTo(r.nextStop(i))
				return
			}
		}
		return
	}
	for i := r.prevStop(r.applied) - 1; i >= 0; i-- {
		if e, ok := r.msgs[i].(*vizproto.EventMsg); ok && e.Train == train {
			r.moveTo(r.nextStop(i))
			return
		}
	}
}

// moveTo applies messages up to n and sets the time to the last one's.
func (r *replay) moveTo(n int) {
	r.applyTo(n)
	if r.applied > 0 {
		r.t = r.msgs[r.applied-1].Head().T
	}
}

// nextStop is the first batch start after message i, or the end.
func (r *replay) nextStop(i int) int {
	k := sort.SearchInts(r.stops, i+1)
	if k < len(r.stops) {
		return r.stops[k]
	}
	return len(r.msgs)
}

// prevStop is the last batch start before message i, but never before the
// first one.
func (r *replay) prevStop(i int) int {
	k := sort.SearchInts(r.stops, i) - 1
	if k < 0 {
		if len(r.stops) == 0 {
			return 0
		}
		return r.stops[0]
	}
	return r.stops[k]
}

// nextDecision returns the first decision made after time t, if any.
func (r *replay) nextDecision(t float64) (*vizproto.DecisionMsg, bool) {
	for _, m := range r.msgs {
		if d, ok := m.(*vizproto.DecisionMsg); ok && d.Since > t+1e-9 {
			return d, true
		}
	}
	return nil, false
}

// decisions returns every decision as it stands now: active ones first, then
// the rest, latest first within each.
func (r *replay) decisions() []vizproto.DecisionMsg {
	var out []vizproto.DecisionMsg
	for _, d := range r.state.Decisions {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active() != out[j].Active() {
			return out[i].Active()
		}
		if out[i].Since != out[j].Since {
			return out[i].Since > out[j].Since
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// recent returns up to n of the latest events applied, oldest first, only the
// train's if train is set.
func (r *replay) recent(n int, train string) []*vizproto.EventMsg {
	var out []*vizproto.EventMsg
	for i := r.applied - 1; i >= 0 && len(out) < n; i-- {
		if e, ok := r.msgs[i].(*vizproto.EventMsg); ok && (train == "" || e.Train == train) {
			out = append(out, e)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// call is when a train reached and left a station of its schedule, and on
// which platform; -1 and "" until it has.
type call struct {
	arr, dep float64
	pf       string
}

// calls is how far the train has got through its schedule by now, one entry
// per schedule point: its k-th TRAIN_ARRIVE is at its k-th point, passes
// included, and the TRAIN_DEPART (or, at the last, the SCHEDULE_END) after it
// is when it left.
func (r *replay) calls(train string, points int) []call {
	out := make([]call, points)
	for i := range out {
		out[i] = call{arr: -1, dep: -1}
	}
	k := -1
	for _, m := range r.msgs[:r.applied] {
		e, ok := m.(*vizproto.EventMsg)
		if !ok || e.Train != train {
			continue
		}
		switch e.Event {
		case "TRAIN_ARRIVE":
			k++
			if k < points {
				out[k].arr = e.T
				for _, f := range e.Fields {
					if f.Key == "track" {
						out[k].pf = r.pfNo[fmt.Sprint(f.Value)]
					}
				}
			}
		case "TRAIN_DEPART", "SCHEDULE_END":
			if k >= 0 && k < points && out[k].dep < 0 {
				out[k].dep = e.T
			}
		}
	}
	return out
}

// rankClasses orders the run's classes by their trains' priority when they
// first appear, then by name, and gives each a colour, spread from the lowest
// to the highest.
func (r *replay) rankClasses() {
	base := map[string]int{}
	seen := map[string]bool{}
	for _, m := range r.msgs {
		t, ok := m.(*vizproto.TrainMsg)
		if !ok || seen[t.Train] {
			continue
		}
		seen[t.Train] = true
		if c, ok := r.class[t.Train]; ok {
			base[c] = max(base[c], t.Priority)
		}
	}
	for _, c := range r.class {
		if _, ok := base[c]; !ok {
			base[c] = 0
		}
	}
	r.classes = nil
	for c, p := range base {
		r.classes = append(r.classes, classKey{name: c, priority: p})
	}
	slices.SortFunc(r.classes, func(a, b classKey) int {
		return cmp.Or(cmp.Compare(a.priority, b.priority), cmp.Compare(a.name, b.name))
	})
	for i := range r.classes {
		r.classes[i].color = tui.PaletteColor(i, len(r.classes))
	}
}

// classStyle is how a train's number is drawn: bold, in its class's colour.
func (r *replay) classStyle(train string) lipgloss.Style {
	c, ok := r.class[train]
	if !ok {
		return bold
	}
	for _, k := range r.classes {
		if k.name == c {
			return bold.Foreground(lipgloss.Color(k.color))
		}
	}
	return bold
}
