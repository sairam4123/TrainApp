package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/visualization/record"
	"trainapp/visualization/schematic"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"

	tea "charm.land/bubbletea/v2"
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
	if got := zoomSteps[m.step].level; got != schematic.Overview {
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
