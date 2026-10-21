package sim

import (
	"log/slog"
	"math"
	"testing"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/motion"
	"trainapp/railway/ops"
	"trainapp/units"
	"trainapp/worlds"
)

// dynObserver calls each of its funcs after every event.
type dynObserver struct {
	after  func(ev des.Event[ops.RailwayEvent])
	logged func(msg string, attrs []slog.Attr)
}

func (o *dynObserver) Logged(_ slog.Level, msg string, attrs []slog.Attr) {
	if o.logged != nil {
		o.logged(msg, attrs)
	}
}

func (o *dynObserver) AfterEvent(ev des.Event[ops.RailwayEvent]) {
	if o.after != nil {
		o.after(ev)
	}
}

// dynSim is a sim of trains on the single line under the IR profile, which
// gives them length, acceleration and braking, with obs watching.
func dynSim(t *testing.T, trains []*ops.Train, obs Observer) *Sim {
	t.Helper()
	cfg, err := LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &Sim{OutputDir: t.TempDir(), Priorities: cfg, Observer: obs}
	s.SetWorld(worlds.BuildTpjKkdiSingleLineWorld())
	for _, tr := range trains {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	for _, tr := range trains {
		if !(tr.Accel > 0 && tr.Brake > 0) {
			t.Fatalf("%s has no dynamics: accel %v, brake %v", tr.Number, tr.Accel, tr.Brake)
		}
	}
	return s
}

func loadTrains(t *testing.T, path string, numbers ...string) []*ops.Train {
	t.Helper()
	all, err := ops.LoadTrains(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(numbers) == 0 {
		return all
	}
	var out []*ops.Train
	for _, tr := range all {
		for _, n := range numbers {
			if tr.Number == n {
				out = append(out, tr)
			}
		}
	}
	return out
}

// curveNow is how far into its curve the train is now, in minutes.
func curveNow(tc *TrainController) float64 {
	t := 0.0
	for i := range tc.wayHead {
		t += tc.curve.Time(i)
	}
	if tc.entered {
		t += min(tc.sim.CurTime()-tc.anchor, tc.curve.Time(tc.wayHead))
	}
	return t
}

// A train alone never stops at a signal: it asks for each section on
// approach, never earlier than the lead before its braking point, and is
// granted it while running.
func TestFreeLineNeverStopsAtSignals(t *testing.T) {
	trains := loadTrains(t, "../../scenarios/schedules/sl-6.json", "0451U")
	var s *Sim
	approaches, onLead, grants, ends := 0, 0, 0, 0
	obs := &dynObserver{after: func(ev des.Event[ops.RailwayEvent]) {
		tc := s.trainCtrllers[ids.TrainID{Number: ev.TrainID}]
		switch ev.Type {
		case ops.TrackApproach:
			approaches++
			lead := float64(s.ApproachLead())
			left := tc.curve.BrakingPoint().T - curveNow(tc)
			if left > lead+0.01 {
				t.Errorf("[%.3f] asked %.3f min before its braking point, more than the %v min lead", ev.Time, left, lead)
			}
			if math.Abs(left-lead) < 0.01 {
				onLead++
			}
		case ops.MovementAuthorized:
			if tc.next != nil {
				grants++
			}
		case ops.MovementAuthorityEnded:
			ends++
		}
	}}
	s = dynSim(t, trains, obs)
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	if ends != 0 {
		t.Errorf("stopped at a signal %d times on a free line", ends)
	}
	if approaches == 0 || grants == 0 || onLead == 0 {
		t.Errorf("%d approaches (%d on the lead), %d grants while running; want some of each", approaches, onLead, grants)
	}
	if !trains[0].Schedule()[len(trains[0].Schedule())-1].Departed() {
		t.Error("did not finish")
	}
}

// stopRun runs one train from TPJ to KKDI, calling at PDKT for dwell
// minutes or, with dwell < 0, passing it. It returns when the train reached
// KKDI and how long it stood at PDKT.
func stopRun(t *testing.T, dwell float64, dynamics bool) (arrKKDI, stood float64) {
	t.Helper()
	tr := &ops.Train{Name: "One", Number: "S1", Class: "Superfast Express", MaxSpeed: units.KMPH(110)}
	tr.AddSchedule(ops.NewStopPoint("TPJ", 5, "1").SetEntryTime(1))
	for _, stn := range []ids.StnCode{"KRMG", "KRUR", "VEL"} {
		tr.AddSchedule(ops.NewPassPoint(stn, -1, ""))
	}
	pdkt := ops.NewPassPoint("PDKT", -1, "1")
	if dwell >= 0 {
		pdkt = ops.NewStopPoint("PDKT", dwell, "1")
	}
	tr.AddSchedule(pdkt)
	for _, stn := range []ids.StnCode{"NMN", "TYM", "CTND", "KTYR"} {
		tr.AddSchedule(ops.NewPassPoint(stn, -1, ""))
	}
	kkdi := ops.NewStopPoint("KKDI", 5, "1")
	tr.AddSchedule(kkdi)

	cfg, err := LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	if !dynamics {
		cfg.RollingStock = nil
	}
	s := &Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(worlds.BuildTpjKkdiSingleLineWorld())
	if err := s.AddTrain(tr); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	if !kkdi.Arrived() {
		t.Fatal("did not reach KKDI")
	}
	return kkdi.ActArrTime, pdkt.ActDeptTime - pdkt.ActArrTime
}

// A stop costs the time it takes to brake to it and get going again: about
// 2 min from 110 km/h at 0.5 and 0.16 m/s², against nothing at constant
// speed.
func TestStopCostsBrakingAndStarting(t *testing.T) {
	for _, dyn := range []bool{false, true} {
		passed, _ := stopRun(t, -1, dyn)
		stopped, stood := stopRun(t, 2, dyn)
		lost := stopped - passed - stood
		switch {
		case dyn && (lost < 1.5 || lost > 2.6):
			t.Errorf("with dynamics a stop at PDKT cost %.3f min beyond its dwell, want about 2", lost)
		case !dyn && math.Abs(lost) > 0.02:
			t.Errorf("at constant speed a stop at PDKT cost %.3f min beyond its dwell, want nothing", lost)
		}
	}
}

// A train refused on approach starts braking; granted partway, it carries on
// from where it is at the speed it has, and gets to the end of its step
// sooner than it would have stopping there. Refused all the way, it stops at
// the end of its authority, at rest.
func TestGrantWhileBrakingAndHeld(t *testing.T) {
	trains := loadTrains(t, "../../scenarios/schedules/sl-12.json")
	var s *Sim
	type before struct {
		curve  motion.Curve
		head   int
		anchor float64
		exit   float64
	}
	pending := map[string]before{}
	waitedRunning := map[string]bool{}
	braking, grants, ends := 0, 0, 0
	obs := &dynObserver{
		logged: func(msg string, attrs []slog.Attr) {
			if msg != "ROUTE_WAIT" {
				return
			}
			for _, a := range attrs {
				if a.Key == "train" {
					if tc := s.trainCtrllers[ids.TrainID{Number: a.Value.String()}]; tc.phase == phaseRunning {
						waitedRunning[tc.train.Number] = true
					}
				}
			}
		},
		after: func(ev des.Event[ops.RailwayEvent]) {
			tc := s.trainCtrllers[ids.TrainID{Number: ev.TrainID}]
			if tc == nil {
				return
			}
			switch ev.Type {
			case ops.RouteGranted:
				if tc.phase == phaseRunning && tc.entered && !tc.atEnd {
					pending[ev.TrainID] = before{tc.curve, tc.wayHead, tc.anchor, tc.edgeExit}
				}
			case ops.MovementAuthorized:
				b, ok := pending[ev.TrainID]
				delete(pending, ev.TrainID)
				if !ok || tc.phase != phaseRunning {
					return
				}
				grants++
				offOld, vOld := b.curve.At(b.head, s.CurTime()-b.anchor)
				offNew, vNew := tc.position()
				if math.Abs(float64(offOld-offNew)) > 1e-6 || math.Abs(float64(vOld-vNew)) > 1e-6 {
					t.Errorf("[%.3f] %s: at %v m and %v km/h on its old curve, %v m and %v km/h on its new one", ev.Time, ev.TrainID, offOld, vOld.InKMPH(), offNew, vNew.InKMPH())
				}
				if tc.edgeExit > b.exit+1e-9 {
					t.Errorf("[%.3f] %s: due off its step at %v, later than %v before the grant", ev.Time, ev.TrainID, tc.edgeExit, b.exit)
				}
				if waitedRunning[ev.TrainID] && s.CurTime()-b.anchor > 0 {
					old := 0.0
					for i := range b.head {
						old += b.curve.Time(i)
					}
					if old+s.CurTime()-b.anchor > b.curve.BrakingPoint().T {
						braking++ // it had started braking for the end of its authority
					}
				}
				waitedRunning[ev.TrainID] = false
			case ops.MovementAuthorityEnded, ops.TrainArrived:
				if !tc.dynamic() {
					return
				}
				if ev.Type == ops.MovementAuthorityEnded {
					ends++
				}
				head, _ := tc.train.Route().Head()
				if off, v := tc.position(); v != 0 || off != head.Edge.Track.Length {
					t.Errorf("[%.3f] %s stopped after %s at %v m of %v at %v km/h, want at its end, at rest", ev.Time, ev.TrainID, ev.Type, off, head.Edge.Track.Length, v.InKMPH())
				}
			}
		},
	}
	s = dynSim(t, trains, obs)
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	if grants == 0 || braking == 0 || ends == 0 {
		t.Errorf("%d grants while running, %d of them while braking, %d stops at a signal; want some of each", grants, braking, ends)
	}
}

// A running train's look-ahead on a free line is what it then does: it
// reaches each stop when its projection said.
func TestProjectionMatchesTheRun(t *testing.T) {
	trains := loadTrains(t, "../../scenarios/schedules/sl-6.json", "0451U")
	tr := trains[0]
	var s *Sim
	var pr *projection
	obs := &dynObserver{after: func(ev des.Event[ops.RailwayEvent]) {
		if pr == nil && ev.Type == ops.TrackApproach {
			pr = s.prec.projectNow(tr) // running, in its first block section
		}
	}}
	s = dynSim(t, trains, obs)
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	if pr == nil || len(pr.visits) < 3 {
		t.Fatalf("projection %+v, want one with visits", pr)
	}
	for _, v := range pr.visits {
		sp := tr.Schedule()[v.stop]
		if math.Abs(v.arr-sp.ActArrTime) > 0.1 {
			t.Errorf("%s: projected to arrive at %.3f, arrived at %.3f", sp.StnCode, v.arr, sp.ActArrTime)
		}
	}
}

// A train comes into the world at line speed, or at its schedule's entry
// speed or a slow_entry disruption's, the lowest of them.
func TestEntrySpeed(t *testing.T) {
	enters := func(entryKmph float64, slow float64) float64 {
		t.Helper()
		tr := loadTrains(t, "../../scenarios/schedules/sl-6.json", "0451U")[0]
		tr.EntrySpeed = units.KMPH(entryKmph)
		if slow > 0 {
			if err := ops.ApplyDisruptions([]*ops.Train{tr}, []ops.Disruption{{Train: tr.Number, Kind: ops.DisruptSlowEntry, Kmph: slow}}); err != nil {
				t.Fatal(err)
			}
		}
		var s *Sim
		at := -1.0
		obs := &dynObserver{after: func(ev des.Event[ops.RailwayEvent]) {
			if ev.Type == ops.TrackEntered && at < 0 {
				_, v := s.trainCtrllers[tr.ID()].position()
				at = v.InKMPH()
			}
		}}
		s = dynSim(t, []*ops.Train{tr}, obs)
		if err := s.Run(); err != nil {
			t.Fatal(err)
		}
		return at
	}
	w := worlds.BuildTpjKkdiSingleLineWorld()
	tpj, _ := w.GetStation("TPJ")
	line := math.Min(110, tpj.FindStnPlatform("1").MaxSpeed.InKMPH())
	for _, c := range []struct{ entry, slow, want float64 }{
		{0, 0, line},
		{30, 0, 30},
		{30, 20, 20},
		{30, 40, 30},
		{200, 0, line},
	} {
		if got := enters(c.entry, c.slow); math.Abs(got-c.want) > 1e-6 {
			t.Errorf("entry_kmph %v, slow_entry %v: entered at %v km/h, want %v", c.entry, c.slow, got, c.want)
		}
	}
}

// With no dynamics, the curve over each track of the worlds is its
// TravelTime exactly, as one step or among others.
func TestNoDynamicsTimesOnTheWorlds(t *testing.T) {
	for _, w := range []*infra.World{worlds.BuildTpjKkdiWorld(), worlds.BuildTpjKkdiSingleLineWorld()} {
		tr := &ops.Train{Number: "T", MaxSpeed: units.KMPH(110)}
		var steps []infra.Step
		for _, trk := range w.TrackGraph.ListTracks() {
			edge, ok := w.TrackGraph.Edge(trk)
			if !ok {
				continue
			}
			steps = append(steps, infra.Step{Edge: edge, From: edge.From, To: edge.To})
		}
		all := motion.Plan(tr, motion.Way{Steps: steps, Stop: true})
		for i, st := range steps {
			want := float64(st.Edge.Track.TravelTime(tr.MaxSpeed))
			one := motion.Plan(tr, motion.Way{Steps: []infra.Step{st}})
			if one.Time(0) != want || all.Time(i) != want {
				t.Errorf("%s: %v alone and %v among others, want %v", st.Edge.Track, one.Time(0), all.Time(i), want)
			}
		}
	}
}
