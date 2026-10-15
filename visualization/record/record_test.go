package record_test

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/visualization/record"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"
)

// mainScenario mirrors cmd/trainapp: 6 up and 6 down trains, 20 minutes apart.
func mainScenario(t *testing.T) *sim.Sim {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	for i := range 6 {
		for _, dir := range []struct {
			suffix string
			stops  []string
			pf     string
		}{{"U", []string{"TPJ", "PDKT", "KKDI"}, "1"}, {"D", []string{"KKDI", "PDKT", "TPJ"}, "3"}} {
			tr := &ops.Train{Name: "T", Number: fmt.Sprintf("045%d%s", i+1, dir.suffix), MaxSpeed: units.KMPH(110)}
			for j, code := range dir.stops {
				sp := ops.NewStopPoint(ids.StnCode(code), 5, dir.pf)
				if j == 0 {
					sp.SetEntryTime(float64(i*20 + 1))
				}
				tr.AddSchedule(sp)
			}
			if err := s.AddTrain(tr); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	return s
}

// checker replays the stream as it is written and compares it with the sim
// after every event.
type checker struct {
	t     *testing.T
	s     *sim.Sim
	rec   *record.Recorder
	buf   *bytes.Buffer
	read  int
	state *vizproto.State
}

func (c *checker) Logged(level slog.Level, event string, attrs []slog.Attr) {
	c.rec.Logged(level, event, attrs)
}

func (c *checker) AfterEvent(ev des.Event[ops.RailwayEvent]) {
	c.rec.AfterEvent(ev)
	c.catchUp()
	c.compare(fmt.Sprintf("after %s %s at %.2f", ev.Type, ev.TrainID, ev.Time))
}

func (c *checker) catchUp() {
	r := vizproto.NewReader(bytes.NewReader(c.buf.Bytes()[c.read:]))
	c.read = c.buf.Len()
	for {
		m, err := r.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			c.t.Fatal(err)
		}
		c.state.Apply(m)
	}
}

func (c *checker) compare(when string) {
	c.t.Helper()
	ilk := c.s.Interlocking()
	for _, trk := range c.s.World().TrackGraph.ListTracks() {
		r, o := ilk.TrackHolder(trk)
		gr, gotO := c.state.TrackHolder(trk.Id.String())
		if gr != r.Number || gotO != o.Number {
			c.t.Fatalf("%s: track %s replays as reserved %q occupied %q, sim has %q %q", when, trk, gr, gotO, r, o)
		}
	}
	for _, sb := range c.s.World().ListSwitchBlocks() {
		active, locked := ilk.SwitchState(sb)
		got := c.state.Switches[sb.Id]
		if got.SetFor != active.Track.Id.String() || got.LockedBy != locked.Number {
			c.t.Fatalf("%s: switch %s replays as %+v, sim has %s locked by %q", when, sb.Id, got, active.Track, locked)
		}
	}
	for _, st := range c.s.Status() {
		if got := c.state.Trains[st.Train.Number]; got.Phase != st.Phase {
			c.t.Fatalf("%s: train %s replays as %s, sim has %s", when, st.Train.Number, got.Phase, st.Phase)
		}
	}
}

func TestReplayMatchesTheSimAfterEveryEvent(t *testing.T) {
	s := mainScenario(t)
	var buf bytes.Buffer
	rec, err := record.New(&buf, s)
	if err != nil {
		t.Fatal(err)
	}
	c := &checker{t: t, s: s, rec: rec, buf: &buf, state: vizproto.NewState()}
	c.catchUp()
	s.Observer = c
	runErr := s.Run()
	if err := rec.Close(runErr); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	c.catchUp()
	c.compare("at the end")

	hello, msgs, err := vizproto.ReadAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(hello.World.Trains) != 12 {
		t.Errorf("hello lists %d trains, want 12", len(hello.World.Trains))
	}
	if sched := hello.World.Trains[0].Schedule; len(sched) != 3 || sched[0].Stn != "TPJ" || sched[0].Arr != 1 || sched[1].Kind != "dwell" {
		t.Errorf("first train's schedule in hello: %+v", sched)
	}
	if last := msgs[len(msgs)-1]; last.Head().Type != vizproto.TypeEnd {
		t.Errorf("stream ends with %s, want end", last.Head().Type)
	}
	waits := 0
	for _, m := range msgs {
		if e, ok := m.(*vizproto.EventMsg); ok && e.Event == "ROUTE_WAIT" && e.Train == "0452U" {
			waits++
		}
	}
	if waits == 0 {
		t.Error("no ROUTE_WAIT for 0452U in the stream")
	}
}

func TestRecordingDoesNotChangeTheLog(t *testing.T) {
	logOf := func(s *sim.Sim) string {
		b, err := os.ReadFile(filepath.Join(s.OutputDir, "logs", "logs.log"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	plain := mainScenario(t)
	if err := plain.Run(); err != nil {
		t.Fatal(err)
	}

	recorded := mainScenario(t)
	rec, err := record.New(io.Discard, recorded)
	if err != nil {
		t.Fatal(err)
	}
	recorded.Observer = rec
	if err := recorded.Run(); err != nil {
		t.Fatal(err)
	}
	if logOf(plain) != logOf(recorded) {
		t.Error("the log differs when the run is recorded")
	}
}
