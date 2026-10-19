package edit_test

import (
	"fmt"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/units"
	"trainapp/visualization/schematic"
	"trainapp/worlds/edit"
)

// Worlds drawn with nothing but the editor's edits, run: every train gets
// through.

type drawing struct {
	t *testing.T
	d *edit.Doc
}

func newDrawing(t *testing.T) *drawing {
	return &drawing{t, edit.NewDoc(edit.Empty(edit.DefaultDefaults()))}
}

func (w *drawing) do(edit func(*infra.Snapshot) error) {
	w.t.Helper()
	if err := w.d.Do(edit); err != nil {
		w.t.Fatal(err)
	}
}

func (w *drawing) stamp(at string, spec edit.StampSpec) {
	w.t.Helper()
	w.do(func(s *infra.Snapshot) error { _, err := edit.Stamp(s, at, spec); return err })
}

// ends is a station's open ends east (or west), top to bottom.
func (w *drawing) ends(code string, east bool) []string {
	w.t.Helper()
	s := w.d.S
	for _, p := range s.Points {
		if p.Stn != code || p.DeadEnd {
			continue
		}
		wt, et := 0, 0
		for _, t := range s.Tracks {
			if t.To == p.ID {
				wt++
			}
			if t.From == p.ID {
				et++
			}
		}
		if (east && et == 0 && wt > 0) || (!east && wt == 0 && et > 0) {
			return edit.OpenBeside(s, p.ID, east)
		}
	}
	w.t.Fatalf("%s has no open end %v", code, east)
	return nil
}

// run checks the world has no problems, draws it, and runs the trains, each
// stopping at the stations given, entering a few minutes apart.
func (w *drawing) run(routes ...[]string) {
	w.t.Helper()
	if ps := edit.Problems(w.d.S); len(ps) > 0 {
		w.t.Fatalf("problems: %v", ps)
	}
	d, err := schematic.FromSnapshot(w.d.S)
	if err != nil {
		w.t.Fatal(err)
	}
	for _, l := range d.Draw(schematic.Options{Level: schematic.Layout, Zoom: 1}).Lines(false) {
		w.t.Log(l)
	}
	world, err := w.d.S.Build()
	if err != nil {
		w.t.Fatal(err)
	}
	s := &sim.Sim{OutputDir: w.t.TempDir()}
	s.SetWorld(world)
	for i, r := range routes {
		tr := &ops.Train{Name: fmt.Sprint("T", i), Number: fmt.Sprint(100 + i), MaxSpeed: units.KMPH(110)}
		for k, code := range r {
			sp := ops.NewStopPoint(ids.StnCode(code), 2, "")
			if k == 0 {
				sp.SetEntryTime(float64(1 + 7*i))
			}
			tr.AddSchedule(sp)
		}
		if err := s.AddTrain(tr); err != nil {
			w.t.Fatal(err)
		}
	}
	if err := s.Init(); err != nil {
		w.t.Fatal(err)
	}
	if err := s.Run(); err != nil {
		w.t.Fatal(err)
	}
	if n := len(s.Status()); n != len(routes) {
		w.t.Errorf("%d trains ran, want %d", n, len(routes))
	}
	for _, st := range s.Status() {
		if st.Phase != "exited" {
			w.t.Errorf("%s ended %s (waiting: %+v)", st.Train, st.Phase, st.Waiting)
		}
	}
}

func repeat(n int, routes ...[]string) [][]string {
	var out [][]string
	for range n {
		out = append(out, routes...)
	}
	return out
}

// tpjkkdi's shape: three stations of four platforms on a double line, the
// stations joined a line at a time, by hand.
func TestAcceptanceDoubleLine(t *testing.T) {
	w := newDrawing(t)
	tracks, err := edit.ParseTracks("1:600 2:600* 3:600* 4:600")
	if err != nil {
		t.Fatal(err)
	}
	w.stamp("", edit.StampSpec{Template: edit.TemplateStation, Code: "TPJ", Edge: "W", Tracks: tracks, EastCrossovers: []string{"scissors"}})
	w.stamp("", edit.StampSpec{Template: edit.TemplateStation, Code: "PDKT", Tracks: tracks, WestCrossovers: []string{"scissors"}, EastCrossovers: []string{"scissors"}})
	w.stamp("", edit.StampSpec{Template: edit.TemplateStation, Code: "KKDI", Edge: "E", Tracks: tracks, WestCrossovers: []string{"scissors"}})
	for _, leg := range []struct {
		from, to string
		km       float64
	}{{"TPJ", "PDKT", 55}, {"PDKT", "KKDI", 41}} {
		west, east := w.ends(leg.from, true), w.ends(leg.to, false)
		name := "dl" + leg.from + leg.to
		w.do(func(s *infra.Snapshot) error {
			return edit.Line(s, west[0], east[0], name+"Dn", edit.WayEast, leg.km*1000, 2, 0)
		})
		w.do(func(s *infra.Snapshot) error {
			return edit.Line(s, west[1], east[1], name+"Up", edit.WayWest, leg.km*1000, 2, 0)
		})
	}
	w.run(repeat(3, []string{"TPJ", "PDKT", "KKDI"}, []string{"KKDI", "PDKT", "TPJ"})...)
}

// Four tracks, paired by direction, between two stations of four mains.
func TestAcceptanceFourTracks(t *testing.T) {
	w := newDrawing(t)
	tracks, err := edit.ParseTracks("1:600 2:600* 3* 4* 5:600* 6:600")
	if err != nil {
		t.Fatal(err)
	}
	cross := []string{"scissors", "none", "scissors"}
	w.stamp("", edit.StampSpec{Template: edit.TemplateStation, Code: "AAA", Edge: "W", Tracks: tracks, EastCrossovers: cross})
	w.stamp(w.ends("AAA", true)[0], edit.StampSpec{Template: edit.TemplateStation, Code: "BBB", Edge: "E", Tracks: tracks, WestCrossovers: cross,
		Join: edit.JoinAll, LinkM: 20000, LinkBlocks: 3, LinkPattern: "E E W W"})
	w.run(repeat(3, []string{"AAA", "BBB"}, []string{"BBB", "AAA"})...)
}

// BuildJunctionWorld's Y, drawn: a line WJN - MID - EJN left for NJN.
func TestAcceptanceJunction(t *testing.T) {
	w := newDrawing(t)
	terminus, err := edit.ParseTracks("1:600* 2:600")
	if err != nil {
		t.Fatal(err)
	}
	w.stamp("", edit.StampSpec{Template: edit.TemplateStation, Code: "WJN", Edge: "W", Tracks: terminus})
	w.stamp(w.ends("WJN", true)[0], edit.StampSpec{Template: edit.TemplateCrossing, Code: "MID", LinkM: 10000, LinkBlocks: 2})
	w.stamp(w.ends("MID", true)[0], edit.StampSpec{Template: edit.TemplateStation, Code: "EJN", Edge: "E", Tracks: terminus, LinkM: 14000, LinkBlocks: 1})
	var line string
	for _, tr := range w.d.S.Tracks {
		if strings.HasPrefix(tr.Name, "slMidEjn") {
			line = tr.ID
		}
	}
	var branch []string
	w.do(func(s *infra.Snapshot) (err error) {
		branch, err = edit.Junction(s, edit.JunctionSpec{Mains: []string{line}, East: true, Lanes: 2, Code: "JCT", Pattern: "B", LengthM: 7000, Blocks: 2})
		return
	})
	w.stamp(branch[0], edit.StampSpec{Template: edit.TemplateStation, Code: "NJN", Edge: "E", Tracks: terminus})
	w.run(repeat(2, []string{"WJN", "MID", "NJN"}, []string{"NJN", "MID", "WJN"}, []string{"WJN", "MID", "EJN"}, []string{"EJN", "MID", "WJN"})...)
}

// A station's two lines extended, the next station stamped onto the first:
// j at the second's end joins it to the station's line facing it.
func TestAcceptanceJoinFacingEnd(t *testing.T) {
	w := newDrawing(t)
	tracks, err := edit.ParseTracks("1:600 2:600* 3:600* 4:600")
	if err != nil {
		t.Fatal(err)
	}
	w.stamp("", edit.StampSpec{Template: edit.TemplateStation, Code: "AAA", Edge: "W", Tracks: tracks, EastCrossovers: []string{"scissors"}})
	var lineEnds []string
	for _, end := range w.ends("AAA", true) {
		w.do(func(s *infra.Snapshot) error {
			p, err := edit.Extend(s, end, true, 11000, 0, 0)
			lineEnds = append(lineEnds, p)
			return err
		})
	}
	w.stamp(lineEnds[0], edit.StampSpec{Template: edit.TemplateStation, Code: "BBB", Edge: "E", Tracks: tracks, WestCrossovers: []string{"scissors"}})
	w.do(func(s *infra.Snapshot) error { return edit.Join(s, lineEnds[1]) })
	w.run(repeat(2, []string{"AAA", "BBB"}, []string{"BBB", "AAA"})...)
}
