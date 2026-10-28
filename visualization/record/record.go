// Package record writes a sim run as a vizproto stream for trainviz to
// replay. After every event it compares the interlocking and the trains with
// what it last sent, and sends only what changed.
package record

import (
	"errors"
	"io"
	"log/slog"
	"math"
	"slices"
	"strings"
	"trainapp/des"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/visualization/vizproto"
)

// Recorder is a sim.Observer. Create it after Sim.Init, set it as the sim's
// Observer, and Close it once Run returns.
type Recorder struct {
	sim *sim.Sim
	out *vizproto.Writer
	err error

	tracks    []*infra.TrackSegment
	blocks    []*infra.SwitchBlock
	held      map[string]vizproto.TrackMsg
	switches  map[string]vizproto.SwitchMsg
	trains    map[string]vizproto.TrainMsg
	decisions map[string]vizproto.DecisionMsg
	pending   []*vizproto.EventMsg
}

// New writes the hello message and the starting state.
func New(w io.Writer, s *sim.Sim) (*Recorder, error) {
	if s.Interlocking() == nil {
		return nil, errors.New("record: create the recorder after Sim.Init")
	}
	world := vizproto.FromWorld(s.World())
	for _, st := range s.Status() {
		t := vizproto.Train{Number: st.Train.Number, Name: st.Train.Name, Class: st.Train.Class, LengthM: float64(st.Train.Length)}
		for _, sp := range st.Train.Schedule() {
			t.Schedule = append(t.Schedule, vizproto.SchedulePoint{
				Stn: sp.StnCode.String(), Kind: string(sp.Kind()), Pf: sp.SpPfNo, Arr: sp.ArrTime, Dep: sp.DeptTime,
			})
		}
		world.Trains = append(world.Trains, t)
	}
	r := &Recorder{
		sim:       s,
		out:       vizproto.NewWriter(w),
		tracks:    s.World().TrackGraph.ListTracks(),
		blocks:    s.World().ListSwitchBlocks(),
		held:      map[string]vizproto.TrackMsg{},
		switches:  map[string]vizproto.SwitchMsg{},
		trains:    map[string]vizproto.TrainMsg{},
		decisions: map[string]vizproto.DecisionMsg{},
	}
	r.write(&vizproto.HelloMsg{Version: vizproto.Version, World: *world})
	r.diff(nil)
	r.flush()
	return r, r.err
}

func (r *Recorder) Logged(level slog.Level, event string, attrs []slog.Attr) {
	ev := &vizproto.EventMsg{Event: event, Level: level.String()}
	for _, a := range attrs {
		v := a.Value.Resolve().Any()
		if a.Key == "train" {
			ev.Train, _ = v.(string)
			continue
		}
		ev.Fields = append(ev.Fields, vizproto.Field{Key: a.Key, Value: v})
	}
	r.pending = append(r.pending, ev)
}

func (r *Recorder) AfterEvent(ev des.Event[ops.RailwayEvent]) {
	r.diff(&ev)
	r.flush()
}

// Close sends what is left and ends the stream, with an error message if the
// run failed, after the deadlock if that was why. It returns the first write error.
func (r *Recorder) Close(runErr error) error {
	r.diff(nil)
	var dl *sim.Deadlock
	if errors.As(runErr, &dl) {
		m := &vizproto.DeadlockMsg{Since: round(dl.Since), Text: dl.String()}
		for _, l := range dl.Cycle {
			m.Cycle = append(m.Cycle, vizproto.WaitLink{Train: l.Train.Number, For: l.For.Number, Track: l.Track.Id.String()})
		}
		r.write(m)
	}
	if runErr != nil {
		r.write(&vizproto.ErrorMsg{Message: runErr.Error()})
	} else {
		r.write(&vizproto.EndMsg{})
	}
	r.flush()
	return r.err
}

func (r *Recorder) write(m vizproto.Message) {
	if r.err != nil {
		return
	}
	m.Head().T = round(r.sim.CurTime())
	r.err = r.out.Write(m)
}

// round drops float noise such as 61.019999999999996.
func round(t float64) float64 {
	return math.Round(t*1e6) / 1e6
}

func (r *Recorder) flush() {
	if r.err == nil {
		r.err = r.out.Flush()
	}
}

// diff sends the log entries since the last call, then every track, switch,
// train and precedence decision that changed. ev is the event just handled,
// nil if none.
func (r *Recorder) diff(ev *des.Event[ops.RailwayEvent]) {
	for _, e := range r.pending {
		r.write(e)
	}
	r.pending = nil

	ilk := r.sim.Interlocking()
	for _, trk := range r.tracks {
		res, occ := ilk.TrackHolder(trk)
		msg := vizproto.TrackMsg{Track: trk.Id.String(), ReservedBy: res.Number, OccupiedBy: occ.Number}
		if prev := r.held[msg.Track]; prev.ReservedBy != msg.ReservedBy || prev.OccupiedBy != msg.OccupiedBy {
			r.held[msg.Track] = msg
			r.write(&msg)
		}
	}

	for _, sb := range r.blocks {
		active, lockedBy := ilk.SwitchState(sb)
		msg := vizproto.SwitchMsg{Block: sb.Id, LockedBy: lockedBy.Number}
		if active != nil {
			msg.SetFor = active.Track.Id.String()
		}
		if prev, ok := r.switches[sb.Id]; !ok || prev.SetFor != msg.SetFor || prev.LockedBy != msg.LockedBy {
			r.switches[sb.Id] = msg
			r.write(&msg)
		}
	}

	for _, st := range r.sim.Status() {
		train := st.Train
		prev, seen := r.trains[train.Number]
		msg := prev
		msg.Train = train.Number
		msg.Phase = st.Phase
		msg.Waiting = waiting(st.Waiting)
		msg.Priority = st.Priority
		// the front sets off along an edge at TRACK_ENTER
		switch edge := train.CurrentEdge(); {
		case edge == nil:
			msg.Track, msg.From, msg.To, msg.TStart, msg.TEnd, msg.Keys = "", "", "", 0, 0, nil
		case ev != nil && ev.Type == ops.TrackEntered && ev.TrainID == train.Number:
			step, _ := train.Route().Head()
			msg.Track = edge.Track.Id.String()
			msg.From, msg.To = step.From.Id.String(), step.To.Id.String()
			msg.TStart = round(r.sim.CurTime())
			msg.TEnd = round(st.EdgeExit)
			msg.Keys = keys(st.Keys)
		case msg.Track == edge.Track.Id.String() && st.Keys != nil:
			// planned again partway along, e.g. granted more while running
			msg.TEnd = round(st.EdgeExit)
			msg.Keys = keys(st.Keys)
		}
		if !seen || !sameTrain(prev, msg) {
			r.trains[train.Number] = msg
			r.write(&msg)
		}
	}

	for _, d := range r.sim.Decisions() {
		msg := decision(d)
		if prev, ok := r.decisions[msg.ID]; !ok || prev != msg {
			r.decisions[msg.ID] = msg
			r.write(&msg)
		}
	}
}

// keys are the front's keys on its track, rounded as times are.
func keys(ks []sim.Key) []vizproto.Key {
	if len(ks) == 0 {
		return nil
	}
	out := make([]vizproto.Key, len(ks))
	for i, k := range ks {
		out[i] = vizproto.Key{T: round(k.T), Frac: math.Round(k.Frac*1e4) / 1e4, Kmph: math.Round(k.Kmph*10) / 10}
	}
	return out
}

func decision(d sim.Decision) vizproto.DecisionMsg {
	return vizproto.DecisionMsg{
		ID: d.ID, Action: d.Action, Kind: string(d.Kind), Train: d.Train.Number, For: d.For.Number, At: d.At,
		PfFrom: d.PfFrom, PfTo: d.PfTo, To: d.To, Status: d.Status, Source: d.Source, Rule: d.Rule, Reason: d.Reason,
		Gain: round(float64(d.Gain)), Wait: round(float64(d.Wait)), Since: round(d.Since), Until: round(d.Until), Ended: round(d.Ended),
	}
}

func waiting(req *sim.RouteRequest) *vizproto.Waiting {
	if req == nil {
		return nil
	}
	w := &vizproto.Waiting{Station: string(req.Station.Code), Pf: req.PrefPfNo, Entry: req.Entry, Since: round(req.Since)}
	if req.LastErr != nil {
		w.Reason = req.LastErr.Error()
		var conflict *interlocking.ConflictError
		if errors.As(req.LastErr, &conflict) {
			w.BlockedBy = conflict.HeldBy.Number
		}
		var held *sim.HeldError
		if errors.As(req.LastErr, &held) {
			names := make([]string, len(held.Decisions))
			for i, d := range held.Decisions {
				names[i] = d.For.Number
			}
			w.HeldFor = strings.Join(names, ",")
		}
	}
	return w
}

func sameTrain(a, b vizproto.TrainMsg) bool {
	wa, wb := a.Waiting, b.Waiting
	return a.Train == b.Train && a.Track == b.Track && a.From == b.From && a.To == b.To &&
		a.TStart == b.TStart && a.TEnd == b.TEnd && a.Phase == b.Phase && a.Priority == b.Priority &&
		(wa == nil) == (wb == nil) && (wa == nil || *wa == *wb) && slices.Equal(a.Keys, b.Keys)
}
