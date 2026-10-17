package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/visualization/record"
	"trainapp/visualization/schematic"
	"trainapp/visualization/tui"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// recording runs the main scenario (as cmd/trainapp does) and returns its
// recording. Only the test uses the sim; trainviz itself reads the stream.
func recording(t *testing.T) (*vizproto.HelloMsg, []vizproto.Message) {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	for i := range 6 {
		for _, dir := range []struct {
			suffix, pf string
			stops      []string
		}{{"U", "1", []string{"TPJ", "PDKT", "KKDI"}}, {"D", "3", []string{"KKDI", "PDKT", "TPJ"}}} {
			tr := &ops.Train{Name: "T", Number: fmt.Sprintf("045%d%s", i+1, dir.suffix), MaxSpeed: units.KMPH(110)}
			for j, code := range dir.stops {
				sp := ops.NewStopPoint(ids.StnCode(code), 10, dir.pf)
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
	var buf bytes.Buffer
	rec, err := record.New(&buf, s)
	if err != nil {
		t.Fatal(err)
	}
	s.Observer = rec
	if err := rec.Close(s.Run()); err != nil {
		t.Fatal(err)
	}
	hello, msgs, err := vizproto.ReadAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return hello, msgs
}

func newReplayModel(t *testing.T) *model {
	hello, msgs := recording(t)
	d, err := schematic.Build(&hello.World)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(d, newReplay(hello, msgs))
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return m
}

func press(m *model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "shift+right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}
		case "shift+left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "home":
			msg = tea.KeyPressMsg{Code: tea.KeyHome}
		case "end":
			msg = tea.KeyPressMsg{Code: tea.KeyEnd}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		m.Update(msg)
	}
}

func screen(m *model) string { return ansi.Strip(m.render()) }

func TestScreenFitsTheWindow(t *testing.T) {
	m := newReplayModel(t)
	lines := strings.Split(m.render(), "\n")
	if len(lines) != 40 {
		t.Errorf("%d lines, want 40", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 140 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
	for _, want := range []string{"trains", "events", "TPJ", "PDKT", "KKDI", "0451U"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("screen lacks %q:\n%s", want, screen(m))
		}
	}
}

func TestWatchTheMainScenario(t *testing.T) {
	m := newReplayModel(t)
	r := m.rep

	// play it through to the end
	for r.advance(1) {
	}
	for _, n := range r.trains {
		if p := r.state.Trains[n].Phase; p != "exited" {
			t.Errorf("%s ends %s", n, p)
		}
	}
	if len(r.state.Tracks) != 0 {
		t.Errorf("%d tracks still held at the end", len(r.state.Tracks))
	}

	// 0452U waits at TPJ for bsTpjPdkt0, which 0451U holds
	press(m, "home", "tab", "tab", "tab")
	if got := m.selectedTrain(); got != "0452U" {
		t.Fatalf("selected %q, want 0452U", got)
	}
	var waitedAt float64
	for range 50 {
		press(m, "shift+right")
		if w := r.state.Trains["0452U"].Waiting; w != nil && w.BlockedBy == "0451U" {
			waitedAt = r.t
			break
		}
	}
	if waitedAt == 0 {
		t.Fatal("never saw 0452U wait for 0451U")
	}
	s := screen(m)
	for _, want := range []string{"0452U  waiting PDKT 1", "blocked by 0451U", "ROUTE_WAIT", "blocked_by=bsTpjPdkt0"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen at t=%.2f lacks %q:\n%s", r.t, want, s)
		}
	}

	// scrub back and forth: the same moment always looks the same
	press(m, ",", ",", ",")
	if r.t >= waitedAt {
		t.Errorf("stepping back left t at %.2f", r.t)
	}
	press(m, ".", ".", ".")
	if r.t != waitedAt || screen(m) != s {
		t.Errorf("stepping back and forward again gives t=%.2f, want %.2f", r.t, waitedAt)
	}
	r.seek(200)
	r.seek(waitedAt)
	if r.state.Trains["0452U"].Waiting == nil {
		t.Error("seeking back to the wait lost it")
	}
}

func TestZoomAndStationJump(t *testing.T) {
	hello, msgs := recording(t)
	d, _ := schematic.Build(&hello.World)
	m := newModel(d, newReplay(hello, msgs))
	m.Update(tea.WindowSizeMsg{Width: 45, Height: 30})
	if got := zoomSteps[m.step].Level; got != schematic.Overview {
		t.Errorf("a narrow window fits the %s, want the overview", got)
	}
	for range len(zoomSteps) {
		press(m, "+")
	}
	if m.step != len(zoomSteps)-1 {
		t.Errorf("zoomed in to step %d, want %d", m.step, len(zoomSteps)-1)
	}
	press(m, "/", "k", "k", "d", "i", "enter")
	x, _, ok := m.frame.Station("KKDI")
	if pw, _ := m.pane(); !ok || x < m.x || x >= m.x+pw {
		t.Errorf("KKDI at column %d is not in view %d..%d", x, m.x, m.x+pw)
	}
	press(m, "/", "x", "y", "z", "enter")
	if !strings.Contains(screen(m), "no station XYZ") {
		t.Error("no message for an unknown station")
	}
}

// precedenceRecording records a slow passenger held at LPA so an express can
// overtake it.
func precedenceRecording(t *testing.T) (*vizproto.HelloMsg, []vizproto.Message) {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: &sim.PriorityConfig{Precedence: &sim.PrecedenceConfig{
		HeadwayMin: 1, MaxHoldMin: 30, MainLineMaxStayMin: 5, MinPriorityGap: 10, MinSavingMin: 1,
	}}}
	s.SetWorld(worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2}))
	for _, tr := range []struct {
		number string
		prio   int
		kmph   float64
		entry  float64
		pf     string
	}{{"P", 20, 60, 1, "1"}, {"X", 50, 100, 12, "2"}} {
		train := &ops.Train{Name: "T" + tr.number, Number: tr.number, MaxSpeed: units.KMPH(tr.kmph), Priority: tr.prio}
		train.AddSchedule(ops.NewStopPoint("WST", 2, tr.pf).SetEntryTime(tr.entry))
		if tr.number == "P" {
			train.AddSchedule(ops.NewStopPoint("LPA", 2, "1"))
		} else {
			train.AddSchedule(ops.NewPassPoint("LPA", -1, "1"))
		}
		train.AddSchedule(ops.NewPassPoint("HLT", -1, "1"))
		train.AddSchedule(ops.NewStopPoint("EST", 2, tr.pf))
		if err := s.AddTrain(train); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rec, err := record.New(&buf, s)
	if err != nil {
		t.Fatal(err)
	}
	s.Observer = rec
	if err := rec.Close(s.Run()); err != nil {
		t.Fatal(err)
	}
	hello, msgs, err := vizproto.ReadAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return hello, msgs
}

func TestHeldTrainsAndDecisionsAreShown(t *testing.T) {
	hello, msgs := precedenceRecording(t)
	if pfs := hello.World.Stations[slices.IndexFunc(hello.World.Stations, func(s vizproto.Station) bool { return s.Code == "LPA" })].Platforms; pfs[0].Class != "main" || len(pfs[0].MainFor) != 2 || pfs[1].Class != "loop" {
		t.Errorf("LPA platforms described as %+v", pfs)
	}
	d, err := schematic.Build(&hello.World)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(d, newReplay(hello, msgs))
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})

	press(m, "d", "enter") // the first decision: P moved to the loop
	press(m, "enter")      // the second: P held for X
	got := screen(m)
	for _, want := range []string{"held at LPA for X (overtake", "pf 1→2 at LPA (booked for X)", "decisions (enter: next)", "P▶⏸"} {
		if !strings.Contains(got, want) {
			t.Errorf("screen lacks %q:\n%s", want, got)
		}
	}
	if !m.rep.state.Trains["P"].Waiting.Held() {
		t.Error("P not held in the replayed state")
	}

	press(m, "end")
	if got := screen(m); !strings.Contains(got, "released") {
		t.Errorf("released hold not listed at the end:\n%s", got)
	}
}

func TestGraphView(t *testing.T) {
	m := newReplayModel(t)
	press(m, "g")
	if !m.showGraph {
		t.Fatalf("g did not show the graph: %s", m.note)
	}
	s := screen(m)
	for _, want := range []string{"time-distance", "TPJ", "PDKT", "KKDI", "0451U", "00:30", "p planned"} {
		if !strings.Contains(s, want) {
			t.Errorf("graph screen lacks %q:\n%s", want, s)
		}
	}
	lines := strings.Split(m.render(), "\n")
	if len(lines) != 40 {
		t.Errorf("%d lines, want 40", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 140 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}

	// fitted, the whole run is in view; zooming in narrows it around the
	// middle, and 0 fits it again
	start, end := m.graph.Span()
	o := m.graphOptions()
	if o.From != start || o.To < end-m.gPerCol {
		t.Errorf("fitted graph shows %.1f-%.1f, want %.1f-%.1f", o.From, o.To, start, end)
	}
	press(m, "+", "+")
	if z := m.graphOptions(); z.To-z.From >= o.To-o.From {
		t.Errorf("zooming in shows %.1f min, no less than %.1f", z.To-z.From, o.To-o.From)
	}
	press(m, "0")
	if z := m.graphOptions(); z.From != o.From || z.To != o.To {
		t.Errorf("0 shows %.1f-%.1f, want %.1f-%.1f", z.From, z.To, o.From, o.To)
	}

	// a click on the graph seeks there, and the time stays in view as it plays
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 60, Y: 5})
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 60, Y: 5})
	want, _ := m.graph.TimeAt(m.graphOptions(), 59)
	if m.rep.t != want {
		t.Errorf("clicked at column 59: t=%.2f, want %.2f", m.rep.t, want)
	}
	press(m, "+", "+", "+", "end")
	if z := m.graphOptions(); m.rep.t < z.From || m.rep.t > z.To {
		t.Errorf("t=%.2f out of view %.1f-%.1f after end", m.rep.t, z.From, z.To)
	}

	press(m, "g")
	if m.showGraph || !strings.Contains(screen(m), "layout, zoom") {
		t.Error("g did not go back to the schematic")
	}
}

func TestNoGraphForAJunction(t *testing.T) {
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(worlds.BuildJunctionWorld(2))
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rec, err := record.New(&buf, s)
	if err != nil {
		t.Fatal(err)
	}
	s.Observer = rec
	if err := rec.Close(s.Run()); err != nil {
		t.Fatal(err)
	}
	hello, msgs, err := vizproto.ReadAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	d, err := schematic.Build(&hello.World)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(d, newReplay(hello, msgs))
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	press(m, "g")
	if m.showGraph || !strings.Contains(screen(m), "single line") {
		t.Errorf("junction world: graph shown %v, note %q", m.showGraph, m.note)
	}
}

func TestScheduleIsShown(t *testing.T) {
	m := newReplayModel(t)
	press(m, "s")
	if s := screen(m); !strings.Contains(s, "select a train with tab") {
		t.Errorf("schedule without a train selected:\n%s", s)
	}

	// 0451U has left TPJ and PDKT by t=60, and not yet reached KKDI
	press(m, "tab")
	m.rep.seek(60)
	m.redraw()
	s := screen(m)
	for _, want := range []string{"schedule 0451U", "kind", "stop", "00:02", "00:12"} {
		if !strings.Contains(s, want) {
			t.Errorf("schedule lacks %q:\n%s", want, s)
		}
	}
	calls := m.rep.calls("0451U", 3)
	if calls[0].pf != "1" || calls[1].arr < 0 || calls[1].dep < 0 || calls[2].arr >= 0 {
		t.Errorf("calls at t=60: %+v", calls)
	}

	press(m, "s")
	if s := screen(m); !strings.Contains(s, "events 0451U") {
		t.Errorf("s again does not go back to the events:\n%s", s)
	}
}

func TestTrainsAreColouredByClass(t *testing.T) {
	s := &sim.Sim{OutputDir: t.TempDir(), Priorities: &sim.PriorityConfig{
		Classes: map[string]int{"Superfast Express": 50, "Mail/Express": 40, "Passenger": 20},
	}}
	s.SetWorld(worlds.BuildTpjKkdiWorld())
	for _, c := range []struct{ number, class string }{{"SF", "Superfast Express"}, {"PS", "Passenger"}, {"ME", "Mail/Express"}, {"XX", ""}} {
		tr := &ops.Train{Name: c.number, Number: c.number, MaxSpeed: units.KMPH(110), Class: c.class}
		for j, code := range []ids.StnCode{"TPJ", "PDKT", "KKDI"} {
			sp := ops.NewStopPoint(code, 5, "1")
			if j == 0 {
				sp.SetEntryTime(float64(1 + len(s.Status())*10))
			}
			tr.AddSchedule(sp)
		}
		if err := s.AddTrain(tr); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rec, err := record.New(&buf, s)
	if err != nil {
		t.Fatal(err)
	}
	s.Observer = rec
	if err := rec.Close(s.Run()); err != nil {
		t.Fatal(err)
	}
	hello, msgs, err := vizproto.ReadAll(&buf)
	if err != nil {
		t.Fatal(err)
	}

	r := newReplay(hello, msgs)
	var names []string
	for _, k := range r.classes {
		names = append(names, fmt.Sprintf("%s %d %s", k.name, k.priority, k.color))
	}
	want := []string{"Passenger 20 " + tui.Palette[0], "Mail/Express 40 " + tui.Palette[5], "Superfast Express 50 " + tui.Palette[9]}
	if !slices.Equal(names, want) {
		t.Errorf("classes %v, want %v", names, want)
	}
	if got := r.classStyle("SF").GetForeground(); got != lipgloss.Color(tui.Palette[9]) {
		t.Errorf("SF drawn in %v", got)
	}
	if got := r.classStyle("XX").GetForeground(); got != bold.GetForeground() {
		t.Errorf("a train without a class drawn in %v", got)
	}

	d, err := schematic.Build(&hello.World)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(d, r)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	sc := screen(m)
	for _, want := range []string{"■ Superfast Express (50)", "■ Mail/Express (40)", "■ Passenger (20)"} {
		if !strings.Contains(sc, want) {
			t.Errorf("trains pane lacks %q:\n%s", want, sc)
		}
	}
	if i, j := strings.Index(sc, "Superfast Express (50)"), strings.Index(sc, "Passenger (20)"); i > j {
		t.Error("the key does not list the highest priority first")
	}
}

// Following a train, the view glides after it, never moving much further
// in a tick than the train itself does, rather than jumping to re-centre it;
// and the train stays on the pane.
func TestFollowGlides(t *testing.T) {
	hello, msgs := recording(t)
	d, err := schematic.Build(&hello.World)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(d, newReplay(hello, msgs))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	press(m, "+", "+", "+", "tab", "f", "space") // zoomed in, so the line is wider than the pane
	if m.frame.Width() <= 80 {
		t.Fatalf("frame %d wide: the test needs it wider than the pane", m.frame.Width())
	}
	pw, _ := m.pane()
	now, followed := m.lastTick, 0
	maxView, maxTrain, lastX := 0, 0, -1
	for range 3000 {
		now = now.Add(tickEvery)
		was := m.x
		m.Update(tickMsg(now))
		x, _, ok := m.frame.Train("0451U")
		if !ok {
			lastX = -1
			continue
		}
		followed++
		if x < m.x || x >= m.x+pw {
			t.Fatalf("t=%.2f: 0451U at column %d, off the pane %d-%d", m.rep.t, x, m.x, m.x+pw-1)
		}
		if lastX >= 0 {
			maxView, maxTrain = max(maxView, abs(m.x-was)), max(maxTrain, abs(x-lastX))
		}
		lastX = x
	}
	if followed < 100 {
		t.Fatalf("followed 0451U for only %d ticks", followed)
	}
	t.Logf("largest step: view %d cells, train %d, pane %d", maxView, maxTrain, pw)
	if maxView > maxTrain+2 {
		t.Errorf("the view jumped %d cells in a tick, the train at most %d", maxView, maxTrain)
	}
}

func abs(n int) int { return max(n, -n) }
