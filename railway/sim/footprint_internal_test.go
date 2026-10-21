package sim

import (
	"log/slog"
	"math"
	"slices"
	"testing"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/units"
	"trainapp/worlds"
)

// footprintCheck compares, whenever a train stands at the end of its route,
// the footprint the safety check works out for it with what it occupies.
type footprintCheck struct {
	t      *testing.T
	s      *Sim
	checks int
}

func (c *footprintCheck) Logged(slog.Level, string, []slog.Attr) {}

func (c *footprintCheck) AfterEvent(ev des.Event[ops.RailwayEvent]) {
	if ev.Type != ops.TrainArrived && ev.Type != ops.MovementAuthorityEnded {
		return
	}
	tr := c.s.trainCtrllers[ids.TrainID{Number: ev.TrainID}].train
	if tr.Route() == nil {
		return
	}
	trackIDs := func(trks []*infra.TrackSegment) []ids.TrackID {
		var out []ids.TrackID
		for _, trk := range trks {
			out = append(out, trk.Id)
		}
		slices.SortFunc(out, func(a, b ids.TrackID) int { return a.Compare(b) })
		return out
	}
	var body []*infra.TrackSegment
	for _, b := range tr.Body() {
		body = append(body, b.Step.Edge.Track)
	}
	got, want := trackIDs(c.s.dispatcher.footprint(tr, tr.Route())), trackIDs(body)
	if !slices.Equal(got, want) {
		c.t.Errorf("[%.3f] %s standing after %s: footprint %v, occupies %v", ev.Time, tr.Number, ev.Type, got, want)
	}
	c.checks++
}

func TestFootprintIsWhatTheTrainOccupies(t *testing.T) {
	cfg, err := LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	trains, err := ops.LoadTrains("../../scenarios/schedules/sl-6.json")
	if err != nil {
		t.Fatal(err)
	}
	s := &Sim{OutputDir: t.TempDir(), Priorities: cfg}
	s.SetWorld(worlds.BuildTpjKkdiSingleLineWorld())
	for _, tr := range trains {
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	c := &footprintCheck{t: t, s: s}
	s.Observer = c
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(); err != nil {
		t.Fatal(err)
	}
	if c.checks == 0 {
		t.Fatal("no train ever stood at the end of a route")
	}
}

func TestStandingHoldsCoverALongTail(t *testing.T) {
	w := worlds.BuildTpjKkdiSingleLineWorld()
	if err := w.Finalize(); err != nil {
		t.Fatal(err)
	}
	pdkt, _ := w.GetStation("PDKT")
	pf := pdkt.Platforms()[0]
	edge, _ := w.TrackGraph.Edge(pf.Track)
	end := infra.Position{At: edge.To, Via: edge}
	ck := &safetyCheck{disp: &Dispatcher{world: w}}

	fits := &ops.Train{Number: "F", Length: pf.Track.Length}
	if h := ck.standingHolds(fits, end); len(h) != 1 || !h[pf.Track.Id] {
		t.Errorf("a train as long as the track holds %v, want just the track", h)
	}

	long := &ops.Train{Number: "L", Length: pf.Track.Length + 150}
	h := ck.standingHolds(long, end)
	if !h[pf.Track.Id] || len(h) < 2 {
		t.Fatalf("a train 150 m longer than the track holds %v, want the track and what is behind it", h)
	}
	// the tail is behind the platform track, where it came in (edge.From),
	// never ahead of it
	behindIt := false
	for id := range h {
		if id == pf.Track.Id {
			continue
		}
		trk, _ := w.TrackGraph.GetTrack(id)
		e, _ := w.TrackGraph.Edge(trk)
		if slices.Contains(w.TrackGraph.EdgesAt(edge.To), e) {
			t.Errorf("holds %s, ahead of the platform track", trk)
		}
		behindIt = behindIt || slices.Contains(w.TrackGraph.EdgesAt(edge.From), e)
	}
	if !behindIt {
		t.Errorf("holds %v, none of them right behind the platform track", h)
	}
}

func TestClearTimes(t *testing.T) {
	step := func(id string, m float64) infra.Step {
		return infra.Step{Edge: &infra.GraphEdge{Track: &infra.TrackSegment{Name: id, Length: units.M(m)}}}
	}
	tailStep, a, b, p := step("T", 200), step("A", 1000), step("B", 100), step("P", 800)
	tr := &ops.Train{Number: "X", Length: 300}
	pr := &projection{
		train: tr,
		slots: []slot{
			{step: a, enter: 0, exit: 1},
			{step: b, enter: 1, exit: 1.2},
			{step: p, enter: 1.2, exit: 5}, // arrives at 2, leaves at 5
		},
		visits: []visit{{slot: 2, arr: 2, dep: 5}},
	}
	head := ops.BodyStep{Step: a, Start: 0}
	pr.setClears(&head, []ops.BodyStep{{Step: tailStep, Start: -200}})

	// A's end is at 1000, so its rear is off at 1300: 200 m into P, which
	// the front runs from 1.2 to 2
	for i, want := range []float64{1.2 + 200.0/800*0.8, 1.2 + 300.0/800*0.8, 5} {
		if got := pr.slots[i].clear; math.Abs(got-want) > 1e-9 {
			t.Errorf("%s clears at %v, want %v", pr.slots[i].track(), got, want)
		}
	}
	if len(pr.tail) != 1 || math.Abs(pr.tail[0].clear-0.3) > 1e-9 {
		t.Errorf("tail %+v, want T clearing at 0.3 (300 m into A)", pr.tail)
	}
	if c, ok := pr.clearOf(tailStep.Edge.Track); !ok || math.Abs(c-0.3) > 1e-9 {
		t.Errorf("clearOf(T) = %v, %v; want 0.3", c, ok)
	}

	// no length: clear is exit, no tail
	pr.train = &ops.Train{Number: "Y"}
	pr.tail = nil
	pr.setClears(&head, nil)
	for _, s := range pr.slots {
		if s.clear != s.exit {
			t.Errorf("no length: %s clears at %v, want its exit %v", s.track(), s.clear, s.exit)
		}
	}
}
