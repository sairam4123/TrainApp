package main

import (
	"sort"
	"trainapp/visualization/vizproto"
)

// replay plays a recording: the state is every message up to applied, and t
// is the playback time.
type replay struct {
	msgs    []vizproto.Message
	stops   []int // first message of each sim event's batch
	state   *vizproto.State
	applied int
	t       float64
	end     float64

	trains    []string          // in the order the sim added them
	trackName map[string]string // display name of each track
	failure   string            // why the run failed, if it did
}

func newReplay(hello *vizproto.HelloMsg, msgs []vizproto.Message) *replay {
	r := &replay{msgs: msgs, trackName: map[string]string{}}
	for _, t := range hello.World.Trains {
		r.trains = append(r.trains, t.Number)
	}
	for _, t := range hello.World.Tracks {
		r.trackName[t.ID] = t.Name
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
