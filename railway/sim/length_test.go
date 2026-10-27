package sim_test

import (
	"log/slog"
	"math"
	"slices"
	"testing"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/worlds"
)

// bodyCheck checks, after every event, that each train's body is what the
// interlocking says it occupies, and that its rear is where its length says.
type bodyCheck struct {
	t *testing.T
	s *sim.Sim

	exits      int             // TRACK_EXITED events seen
	maxRoutes  int             // most routes one train's body spanned
	enteredOut int             // trains whose rear was outside the world on entering
	gone       map[string]bool // trains past SCHEDULE_END
	firstEnter map[string]bool // trains seen entering a track
	failed     bool            // stop reporting after the first failure
	trains     map[string]*ops.Train
}

func newBodyCheck(t *testing.T, s *sim.Sim, trains []*ops.Train) *bodyCheck {
	c := &bodyCheck{t: t, s: s, gone: map[string]bool{}, firstEnter: map[string]bool{}, trains: map[string]*ops.Train{}}
	for _, tr := range trains {
		c.trains[tr.Number] = tr
	}
	return c
}

func (c *bodyCheck) Logged(slog.Level, string, []slog.Attr) {}

func (c *bodyCheck) fail(ev des.Event[ops.RailwayEvent], format string, args ...any) {
	if c.failed {
		return
	}
	c.failed = true
	c.t.Errorf("[%.3f] after %s for %s: "+format, append([]any{ev.Time, ev.Type, ev.TrainID}, args...)...)
}

func (c *bodyCheck) AfterEvent(ev des.Event[ops.RailwayEvent]) {
	switch ev.Type {
	case ops.TrackExited:
		c.exits++
		if c.gone[ev.TrainID] {
			c.fail(ev, "rear cleared a track after the train left the world")
		}
		// the rear is now exactly at the start of what it still occupies
		if r, ok := c.trains[ev.TrainID].Rear(); !ok || math.Abs(float64(r.Offset)) > 1e-6 {
			c.fail(ev, "rear at %+v (ok %v), want the start of a step", r.Offset, ok)
		}
	case ops.ScheduleEnd:
		c.gone[ev.TrainID] = true
	case ops.TrackEntered:
		if !c.firstEnter[ev.TrainID] {
			c.firstEnter[ev.TrainID] = true
			if _, ok := c.trains[ev.TrainID].Rear(); !ok {
				c.enteredOut++
			}
		}
	}

	occupied := map[ids.TrainID]map[ids.TrackID]bool{}
	ilk := c.s.Interlocking()
	for _, trk := range c.s.World().TrackGraph.ListTracks() {
		if _, occ := ilk.TrackHolder(trk); !occ.IsZero() {
			if occupied[occ] == nil {
				occupied[occ] = map[ids.TrackID]bool{}
			}
			occupied[occ][trk.Id] = true
		}
	}
	for _, tr := range c.trains {
		body := tr.Body()
		routes := map[any]bool{}
		for _, b := range body {
			routes[b.Route] = true
			if !occupied[tr.ID()][b.Step.Edge.Track.Id] {
				c.fail(ev, "%s's body has %s, which it does not occupy", tr.Number, b.Step.Edge.Track)
			}
		}
		c.maxRoutes = max(c.maxRoutes, len(routes))
		if n := len(occupied[tr.ID()]); n != len(body) {
			c.fail(ev, "%s occupies %d tracks, its body has %d", tr.Number, n, len(body))
		}
		if len(body) == 0 {
			continue
		}
		// nothing held behind the tail, nothing freed under it
		rear := tr.Odometer() - float64(tr.Length)
		if first := body[0]; rear > first.End()+1e-6 {
			c.fail(ev, "%s still holds %s, %v m behind its rear", tr.Number, first.Step.Edge.Track, rear-first.End())
		} else if rear < first.Start-1e-6 && first.Start != 0 {
			c.fail(ev, "%s's rear is %v m behind %s, which it no longer holds", tr.Number, first.Start-rear, first.Step.Edge.Track)
		}
	}
}

// slRun runs the single line's sl-6 schedule on the IR profile, its trains
// 491 m long, checking bodies after every event.
func slRun(t *testing.T, precedence bool) *bodyCheck {
	t.Helper()
	cfg, err := sim.LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	if !precedence {
		cfg.Precedence = nil
	}
	trains, err := ops.LoadTrains("../../scenarios/schedules/sl-6.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(worlds.BuildTpjKkdiSingleLineWorld())
	for _, tr := range trains {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	c := newBodyCheck(t, s, trains)
	s.Observer = c
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	for _, tr := range trains {
		if tr.Length < 400 {
			t.Fatalf("%s is %v m long, want the profile's rakes", tr.Number, tr.Length)
		}
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	assertAllExitedAndClean(t, s)
	return c
}

func TestLongTrainsBodyMatchesInterlocking(t *testing.T) {
	for _, prec := range []bool{true, false} {
		c := slRun(t, prec)
		if c.exits == 0 {
			t.Errorf("precedence %v: no TRACK_EXITED, so no rear ever cleared a track while running", prec)
		}
		if c.maxRoutes < 2 {
			t.Errorf("precedence %v: no tail ever spanned two routes", prec)
		}
		if c.enteredOut != len(c.trains) {
			t.Errorf("precedence %v: %d of %d trains entered with the rear outside the world", prec, c.enteredOut, len(c.trains))
		}
	}
}

func TestNoLengthFreesAsTheFrontMovesOn(t *testing.T) {
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	var trains []*ops.Train
	for i, n := range []string{"A", "B"} {
		tr := newTrain(n, float64(1+20*i), [2]string{"TPJ", "1"}, [2]string{"PDKT", "1"}, [2]string{"KKDI", "1"})
		trains = append(trains, tr)
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	c := newBodyCheck(t, s, trains)
	s.Observer = c
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	assertAllExitedAndClean(t, s)
	if c.exits != 0 || c.maxRoutes > 1 {
		t.Errorf("no length: %d TRACK_EXITED and bodies across %d routes, want none and 1", c.exits, c.maxRoutes)
	}
}

// pdktRun runs one 491 m train on the single line from TPJ to a 5 min stop at
// PDKT, timetabled for PDKT's platform 1, after shorten has changed PDKT's
// platforms. It returns the PDKT stop; log records the run's entries.
func pdktRun(t *testing.T, precedence bool, shorten func(pfs []*infra.Platform), log *eventLog) *ops.SchedulePoint {
	t.Helper()
	cfg, err := sim.LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	if !precedence {
		cfg.Precedence = nil
	}
	world := worlds.BuildTpjKkdiSingleLineWorld()
	pdkt, _ := world.GetStation("PDKT")
	shorten(pdkt.Platforms())

	length := 491.0
	tr := &ops.Train{Name: "Long", Number: "L1", MaxSpeed: units.KMPH(110), Stock: &ops.Stock{LengthM: &length}}
	tr.AddSchedule(ops.NewStopPoint("TPJ", 5, "1").SetEntryTime(1))
	for _, stn := range []ids.StnCode{"KRMG", "KRUR", "VEL"} {
		tr.AddSchedule(ops.NewPassPoint(stn, -1, ""))
	}
	stop := ops.NewStopPoint("PDKT", 5, "1")
	tr.AddSchedule(stop)

	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(world)
	if err := s.AddTrain(tr); err != nil {
		t.Fatal(err)
	}
	c := newBodyCheck(t, s, []*ops.Train{tr})
	s.Observer = sim.Observers{c, log}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	assertAllExitedAndClean(t, s)
	return stop
}

func TestPlatformChangeForLength(t *testing.T) {
	for _, prec := range []bool{false, true} {
		log := &eventLog{}
		stop := pdktRun(t, prec, func(pfs []*infra.Platform) {
			pfs[0].Length = 300 // PDKT 1 is too short for 491 m
		}, log)
		if stop.ActPfNo == "1" || stop.ActPfNo == "" {
			t.Errorf("precedence %v: stopped at PDKT platform %q, want another that fits", prec, stop.ActPfNo)
		}
		if prec && len(log.find("PLATFORM_CHANGE", map[string]string{"reason": "too_short"})) == 0 {
			t.Errorf("precedence on: no PLATFORM_CHANGE with reason too_short in %v", log.entries)
		}
	}
}

func TestShortPlatformLengthensTheStop(t *testing.T) {
	for _, prec := range []bool{false, true} {
		log := &eventLog{}
		stop := pdktRun(t, prec, func(pfs []*infra.Platform) {
			for _, pf := range pfs {
				pf.Length = 400 // none fits: 91 m stand beyond the platform
			}
		}, log)
		// 5 + 1 for the started 100 m beyond, plus the event hops to depart
		if dwell := stop.ActDeptTime - stop.ActArrTime; dwell < 6-1e-9 || dwell > 6.01 {
			t.Errorf("precedence %v: stood %v min at PDKT, want 6 (5 + 1 for the started 100 m beyond)", prec, dwell)
		}
		if len(log.find("TRAIN_ARRIVE", map[string]string{"overhang_m": "91", "extra_dwell": "1"})) != 1 {
			t.Errorf("precedence %v: no arrival logged with overhang_m 91 and extra_dwell 1 in %v", prec, log.entries)
		}
	}
}

// releases records when each track was released, by train.
type releases struct {
	at map[string]map[string][]float64
}

func (r *releases) Logged(slog.Level, string, []slog.Attr) {}

func (r *releases) AfterEvent(ev des.Event[ops.RailwayEvent]) {
	if ev.Type != ops.TrackReleased {
		return
	}
	if r.at[ev.TrainID] == nil {
		r.at[ev.TrainID] = map[string][]float64{}
	}
	trk := ev.Data.(*infra.TrackSegment).Id.String()
	r.at[ev.TrainID][trk] = append(r.at[ev.TrainID][trk], ev.Time)
}

func TestViaOutIsWhenTheRearClears(t *testing.T) {
	for _, long := range []bool{true, false} {
		tr := &ops.Train{Name: "Via", Number: "V1", MaxSpeed: units.KMPH(110)}
		if long {
			length := 491.0
			tr.Stock = &ops.Stock{LengthM: &length}
		}
		tr.AddSchedule(ops.NewStopPoint("TPJ", 5, "1").SetEntryTime(1))
		for _, stn := range []ids.StnCode{"KRMG", "KRUR", "VEL"} {
			tr.AddSchedule(ops.NewPassPoint(stn, -1, ""))
		}
		tr.AddSchedule(ops.NewStopPoint("PDKT", 5, "1"))

		s := &sim.Sim{OutputDir: t.TempDir()}
		s.SetWorld(worlds.BuildTpjKkdiSingleLineWorld())
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
		rel := &releases{at: map[string]map[string][]float64{}}
		s.Observer = rel
		if err := s.Init(); err != nil {
			t.Fatal(err)
		}
		if err := s.Run(); err != nil {
			t.Fatal(err)
		}
		// a track is released the event hop after the rear leaves it
		hop := 2 * des.MinDeltaTime
		later := 0 // vias the rear left well after the front entered the next
		for k, sp := range tr.Schedule() {
			for n, v := range sp.Via {
				if v.Out < v.In {
					t.Errorf("long %v: point %d via %s out %v, in %v", long, k, v.Track, v.Out, v.In)
					continue
				}
				if !slices.ContainsFunc(rel.at["V1"][v.Track], func(at float64) bool { return at >= v.Out && at-v.Out <= hop }) {
					t.Errorf("long %v: via %s out %v, but released at %v", long, v.Track, v.Out, rel.at["V1"][v.Track])
				}
				if n+1 == len(sp.Via) {
					continue
				}
				next := sp.Via[n+1].In
				if v.Out-next > 0.1 {
					later++
				}
				if !long && math.Abs(v.Out-next) > hop {
					t.Errorf("no length: via %s out %v, want when the front entered the next track, %v", v.Track, v.Out, next)
				}
			}
		}
		if long && later == 0 {
			t.Error("491 m: no rear left a via track well after the front entered the next")
		}
	}
}
