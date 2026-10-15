package timetable

import (
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
)

// Why a train stood.
//
// A precedence hold at the station says so directly: it names the kind of
// conflict and the other train. Otherwise the sim records which train's
// leaving a track finally let the train go (SchedulePoint.WaitedFor), and the
// two trains' schedules tell how they met there: one from the other way is a
// crossing, one the same way that came in after it and left first an
// overtake. Anything else, a train ahead on the same line included, is a
// technical halt; it still names the train waited for, if any.

type classifier struct {
	trains    map[ids.TrainID]*ops.Train
	decisions []sim.Decision
}

func newClassifier(trains []*ops.Train, decisions []sim.Decision) *classifier {
	c := &classifier{trains: map[ids.TrainID]*ops.Train{}, decisions: decisions}
	for _, t := range trains {
		c.trains[t.ID()] = t
	}
	return c
}

// classify says why train t stood at its k-th point from `from` to `to`.
func (c *classifier) classify(t *ops.Train, k int, from, to float64) Halt {
	sp := t.Schedule()[k]
	waited := sp.WaitedFor
	for _, d := range c.decisions {
		if d.Action != sim.ActionHold || d.Train != t.ID() || d.At != sp.StnCode.String() {
			continue
		}
		end := d.Ended
		if d.Status == sim.StatusActive {
			end = to
		}
		if d.Since > to || end < from {
			continue
		}
		switch d.Kind {
		case sim.KindCross:
			return Halt{Reason: ReasonCross, For: d.For.Number}
		case sim.KindOvertake:
			return Halt{Reason: ReasonOvertake, For: d.For.Number}
		}
		if waited.IsZero() {
			waited = d.For
		}
	}
	h := Halt{Reason: ReasonTechnical, For: waited.Number}
	other, ok := c.trains[waited]
	if !ok {
		return h
	}
	for j, osp := range other.Schedule() {
		if osp.StnCode != sp.StnCode {
			continue
		}
		switch direction(t.Schedule(), k, other.Schedule(), j) {
		case opposite:
			h.Reason = ReasonCross
		case same:
			if osp.Arrived() && osp.ActArrTime > sp.ActArrTime {
				h.Reason = ReasonOvertake
			}
		}
		break
	}
	return h
}

type relation int

const (
	unrelated relation = iota
	same
	opposite
)

// direction compares how two trains run through the same station, a's k-th
// point and b's j-th, by the stations either side of it.
func direction(a []*ops.SchedulePoint, k int, b []*ops.SchedulePoint, j int) relation {
	aPrev, aNext := neighbours(a, k)
	bPrev, bNext := neighbours(b, j)
	switch {
	case aPrev != "" && aPrev == bPrev, aNext != "" && aNext == bNext:
		return same
	case aPrev != "" && aPrev == bNext, aNext != "" && aNext == bPrev:
		return opposite
	}
	return unrelated
}

func neighbours(s []*ops.SchedulePoint, k int) (prev, next ids.StnCode) {
	if k > 0 {
		prev = s[k-1].StnCode
	}
	if k+1 < len(s) {
		next = s[k+1].StnCode
	}
	return prev, next
}
