package traingraph_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/visualization/record"
	"trainapp/visualization/schematic"
	"trainapp/visualization/traingraph"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"
)

// recordRun runs the trains over the world and returns the recording. Only
// the test uses the sim; the graph reads the stream.
func recordRun(t *testing.T, world *infra.World, trains ...*ops.Train) (*vizproto.HelloMsg, []vizproto.Message) {
	t.Helper()
	s := &sim.Sim{OutputDir: t.TempDir()}
	s.SetWorld(world)
	for _, tr := range trains {
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
	return hello, msgs
}

func newTrain(number string, entry float64, stops ...string) *ops.Train {
	t := &ops.Train{Name: "T" + number, Number: number, MaxSpeed: units.KMPH(110)}
	for i, code := range stops {
		sp := ops.NewStopPoint(ids.StnCode(code), 5, "1")
		if i == 0 {
			sp.SetEntryTime(entry)
		}
		t.AddSchedule(sp)
	}
	return t
}

func build(t *testing.T, hello *vizproto.HelloMsg, msgs []vizproto.Message) *traingraph.Graph {
	t.Helper()
	d, err := schematic.Build(&hello.World)
	if err != nil {
		t.Fatal(err)
	}
	cor, err := d.Corridor()
	if err != nil {
		t.Fatal(err)
	}
	return traingraph.Build(&hello.World, cor, msgs)
}

func TestDrawTwoTrains(t *testing.T) {
	hello, msgs := recordRun(t, worlds.BuildTpjKkdiWorld(),
		newTrain("UP", 1, "TPJ", "PDKT", "KKDI"),
		newTrain("DN", 1, "KKDI", "PDKT", "TPJ"),
	)
	g := build(t, hello, msgs)
	from, to := g.Span()
	if from != 1 || to < 60 {
		t.Errorf("span %v-%v", from, to)
	}
	lines := g.Draw(traingraph.Options{Width: 70, Height: 14, From: from, To: to, Now: -1})
	t.Log("\n" + strings.Join(lines, "\n"))
	if len(lines) != 14 {
		t.Fatalf("%d lines, want 14", len(lines))
	}
	text := strings.Join(lines, "\n")
	for _, want := range []string{"TPJ", "PDKT", "KKDI", "UP", "DN", "00:"} {
		if !strings.Contains(text, want) {
			t.Errorf("graph has no %q", want)
		}
	}
	// TPJ is on the top row and KKDI on the last above the axis
	if !strings.HasPrefix(lines[0], "TPJ") || !strings.HasPrefix(lines[12], "KKDI") {
		t.Errorf("stations not at the top and bottom:\n%s", text)
	}
	// UP starts top left and runs down; DN starts bottom left and runs up
	if !hasDots(lines[0][:20]) || !hasDots(lines[12][:20]) {
		t.Errorf("lines do not start at both ends:\n%s", text)
	}
}

func TestDrawStopsAtNow(t *testing.T) {
	hello, msgs := recordRun(t, worlds.BuildTpjKkdiWorld(), newTrain("UP", 1, "TPJ", "PDKT", "KKDI"))
	g := build(t, hello, msgs)
	from, to := g.Span()
	o := traingraph.Options{Width: 60, Height: 10, From: from, To: to, Now: (from + to) / 2}
	lines := g.Draw(o)
	t.Log("\n" + strings.Join(lines, "\n"))
	cursor := -1
	for col := range 60 {
		if at, ok := g.TimeAt(o, col); ok && at >= o.Now {
			cursor = col
			break
		}
	}
	if cursor < 0 {
		t.Fatal("no column at now")
	}
	for _, l := range lines[:len(lines)-1] {
		r := []rune(l)
		if hasDots(string(r[cursor+1:])) {
			t.Errorf("line drawn past now:\n%s", strings.Join(lines, "\n"))
			break
		}
	}
}

func TestPlannedTimesAreMarked(t *testing.T) {
	tr := &ops.Train{Name: "T", Number: "P", MaxSpeed: units.KMPH(110)}
	tr.AddSchedule(ops.NewArrDepPoint("TPJ", 1, 12, "1"))
	tr.AddSchedule(ops.NewArrDepPoint("PDKT", 44, 49, "1"))
	tr.AddSchedule(ops.NewArrDepPoint("KKDI", 73, 83, "1"))
	hello, msgs := recordRun(t, worlds.BuildTpjKkdiWorld(), tr)
	g := build(t, hello, msgs)
	from, to := g.Span()
	o := traingraph.Options{Width: 70, Height: 14, From: from, To: to, Now: -1}
	if strings.Contains(strings.Join(g.Draw(o), ""), "◆") {
		t.Error("planned times marked without Planned")
	}
	o.Planned = true
	lines := g.Draw(o)
	t.Log("\n" + strings.Join(lines, "\n"))
	if n := strings.Count(strings.Join(lines, ""), "◆"); n < 4 {
		t.Errorf("%d planned times marked, want 6 (some may share a cell)", n)
	}
}

func TestSelectedTrainIsPickedOut(t *testing.T) {
	hello, msgs := recordRun(t, worlds.BuildTpjKkdiWorld(),
		newTrain("UP", 1, "TPJ", "PDKT", "KKDI"),
		newTrain("DN", 1, "KKDI", "PDKT", "TPJ"),
	)
	g := build(t, hello, msgs)
	from, to := g.Span()
	lines := g.Draw(traingraph.Options{Width: 70, Height: 14, From: from, To: to, Now: -1, Selected: "UP", Color: true})
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "\x1b[1;97m") || !strings.Contains(text, "\x1b[2;33m") {
		t.Errorf("selected train not bold, other not faint: %q", text)
	}
}

func hasDots(s string) bool {
	for _, r := range s {
		if r > 0x2800 && r <= 0x28ff {
			return true
		}
	}
	return false
}

func TestTrainsListed(t *testing.T) {
	hello, msgs := recordRun(t, worlds.BuildTpjKkdiWorld(), newTrain("A", 1, "TPJ", "PDKT"), newTrain("B", 2, "KKDI", "PDKT"))
	if got := fmt.Sprint(build(t, hello, msgs).Trains()); got != "[A B]" {
		t.Errorf("Trains() = %s", got)
	}
}
