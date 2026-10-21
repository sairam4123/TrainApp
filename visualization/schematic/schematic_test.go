package schematic

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/units"
	"trainapp/visualization/vizproto"
	"trainapp/worlds"
	"unicode/utf8"
)

var testWorlds = map[string]func() *infra.World{
	"TpjKkdi": worlds.BuildTpjKkdiWorld,
	"Test":    worlds.BuildTestWorld,
	"SingleLine": func() *infra.World {
		return worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 3, BlocksPerLine: 3})
	},
	"Junction": func() *infra.World { return worlds.BuildJunctionWorld(3) },
}

func TestWorldsLayOut(t *testing.T) {
	for name, build := range testWorlds {
		t.Run(name, func(t *testing.T) {
			d, err := FromWorld(build())
			if err != nil {
				t.Fatal(err)
			}
			for zoom := 1; zoom <= 3; zoom++ {
				col := d.columns(Layout, zoom)
				for _, trk := range d.tracks {
					if span, rise := col[trk.To]-col[trk.From], abs(trk.Lanes)*rowsPerLane; span < max(rise, 1) {
						t.Errorf("zoom %d: %s spans %d columns, needs %d to change %d lanes", zoom, trk.Name, span, max(rise, 1), trk.Lanes)
					}
				}
				for _, overlap := range levelOverlaps(d, col) {
					t.Errorf("zoom %d: %s", zoom, overlap)
				}
				// every label is drawn and nothing is drawn over it
				drawn := strings.Join(d.Render(Options{Zoom: zoom}), "\n")
				labels := make(map[string]int)
				for _, trk := range d.tracks {
					if l := trk.label(Layout); l != "" {
						labels[l]++
					}
				}
				for label, n := range labels {
					if got := strings.Count(drawn, label); got < n {
						t.Errorf("zoom %d: %q drawn %d times, want %d", zoom, label, got, n)
					}
				}
			}
		})
	}
}

// levelOverlaps lists tracks drawn over each other where they run level,
// except where they leave a common point together before branching apart.
func levelOverlaps(d *Diagram, col map[string]int) []string {
	type level struct {
		trk          *Track
		lane, x1, x2 int
		ends         []string // points the level part starts or ends at
	}
	var parts []level
	for _, t := range d.tracks {
		from, to := t.From, t.To
		bend, resume := t.bend(col[from], col[to], abs(t.Lanes)*rowsPerLane)
		if t.Lanes == 0 {
			parts = append(parts, level{t, d.lane[from], col[from], col[to], []string{from, to}})
			continue
		}
		parts = append(parts,
			level{t, d.lane[from], col[from], bend, []string{from}},
			level{t, d.lane[to], resume, col[to], []string{to}})
	}
	var out []string
	for i, a := range parts {
		for _, b := range parts[i+1:] {
			if a.trk == b.trk || a.lane != b.lane || slices.ContainsFunc(a.ends, func(p string) bool { return slices.Contains(b.ends, p) }) {
				continue
			}
			if max(a.x1, b.x1) < min(a.x2, b.x2) {
				out = append(out, fmt.Sprintf("%s and %s overlap on lane %d", a.trk.Name, b.trk.Name, a.lane))
			}
		}
	}
	return out
}

func TestTpjKkdiPlatformLanes(t *testing.T) {
	world := worlds.BuildTpjKkdiWorld()
	d, err := FromWorld(world)
	if err != nil {
		t.Fatal(err)
	}
	// each station's platforms in order down the page; the up line runs
	// through the second one, the down line through the third
	want := map[ids.StnCode][]string{
		"TPJ":  {"1", "2", "3", "4"},
		"PDKT": {"1", "2", "3", "4"},
		"KKDI": {"1", "2", "3", "4"},
	}
	for code, order := range want {
		stn, _ := world.GetStation(code)
		for lane, pfNo := range order {
			edge, _ := world.TrackGraph.Edge(stn.FindStnPlatform(pfNo))
			if got, _ := d.Lane(edge.From.Id.String()); got != lane {
				t.Errorf("%s pf%s on lane %d, want %d", code, pfNo, got, lane)
			}
		}
	}
}

func TestTpjKkdiRender(t *testing.T) {
	d, err := FromWorld(worlds.BuildTpjKkdiWorld())
	if err != nil {
		t.Fatal(err)
	}
	want := `
  TPJ                    PDKT                   KKDI
[═Pf1═══               ══Pf1═══               ══Pf1══]
        ╲             ╱        ╲             ╱
[═Pf2═══─────(55km)─▶──══Pf2═══───(41km)─▶────══Pf2══]
          ╳                                ╳
[═Pf3═══────◀(55km)────══Pf3═══──◀(41km)──────══Pf3══]
        ╱             ╲        ╱             ╲
[═Pf4═══               ══Pf4═══               ══Pf4══]`
	if got := "\n" + strings.Join(d.Render(Options{Zoom: 1}), "\n"); got != want {
		t.Errorf("got:%s\nwant:%s", got, want)
	}
}

func newTestWorld() *infra.World {
	w := &infra.World{}
	w.Init(infra.WorldData{DefaultSwLength: 100, DefaultSwMaxSpeed: 10, DefaultPfLength: 800, DefaultPfMaxSpeed: 10, DefaultRouteSpeed: 10})
	return w
}

func points(w *infra.World, names ...string) []*infra.TrackPoint {
	pts := make([]*infra.TrackPoint, len(names))
	for i, n := range names {
		pts[i] = w.NewTrackPoint(ids.PointID{Type: n})
	}
	return pts
}

func TestBuildRejectsTracksNotRunningWestToEast(t *testing.T) {
	w := newTestWorld()
	p := points(w, "a", "b", "c")
	w.TrackGraph.AddTrack(p[0], p[1], w.NewTrackSegment("", units.M(1)))
	w.TrackGraph.AddTrack(p[1], p[2], w.NewTrackSegment("", units.M(1)))
	w.TrackGraph.AddTrack(p[2], p[0], w.NewTrackSegment("", units.M(1)))
	if _, err := FromWorld(w); err == nil || !strings.Contains(err.Error(), "west to east") {
		t.Errorf("a loop of track should not lay out, got %v", err)
	}
}

func TestBuildRejectsLanesThatDoNotAddUp(t *testing.T) {
	w := newTestWorld()
	p := points(w, "a", "b", "c", "d")
	w.TrackGraph.AddTrack(p[0], p[1], w.NewTrackSegment("", units.M(1))).SetLanes(1)
	w.TrackGraph.AddTrack(p[1], p[3], w.NewTrackSegment("", units.M(1)))
	w.TrackGraph.AddTrack(p[0], p[2], w.NewTrackSegment("", units.M(1)))
	w.TrackGraph.AddTrack(p[2], p[3], w.NewTrackSegment("", units.M(1)))
	if _, err := FromWorld(w); err == nil || !strings.Contains(err.Error(), "lanes do not add up") {
		t.Errorf("d is one lane down via b but level via c, got %v", err)
	}
}

func TestFormatLength(t *testing.T) {
	for m, want := range map[float64]string{55000: "55km", 4500: "4.5km", 800: "800m"} {
		if got := formatLength(m); got != want {
			t.Errorf("formatLength(%v) = %q, want %q", m, got, want)
		}
	}
}

func TestOverviewCollapsesStations(t *testing.T) {
	d, err := FromWorld(worlds.BuildTpjKkdiWorld())
	if err != nil {
		t.Fatal(err)
	}
	want := `
┌─TPJ──┐         ┌─PDKT─┐         ┌─KKDI─┐
│ ○○○○ ├─(55km)─▶┤ ○○○○ ├─(41km)─▶┤ ○○○○ │
│      │         │      │         │      │
│      ├◀(55km)──┤      ├◀(41km)──┤      │
└──────┘         └──────┘         └──────┘`
	if got := "\n" + strings.Join(d.Render(Options{Level: Overview, Zoom: 1}), "\n"); got != want {
		t.Errorf("got:%s\nwant:%s", got, want)
	}
}

func TestEveryLevelDrawsEveryWorld(t *testing.T) {
	for name, build := range testWorlds {
		d, err := FromWorld(build())
		if err != nil {
			t.Fatal(err)
		}
		for _, level := range []Level{Overview, Layout, Detail} {
			f := d.Draw(Options{Level: level, Zoom: 1})
			for _, s := range d.stations {
				if _, _, ok := f.Station(s.code); !ok && len(s.platforms) > 0 {
					t.Errorf("%s %s: station %s not placed", name, level, s.code)
				}
			}
			for id := range d.points {
				if _, ok := f.points[id]; !ok {
					t.Errorf("%s %s: point %s not placed", name, level, id)
				}
			}
		}
	}
}

func TestDetailShowsIDs(t *testing.T) {
	d, err := FromWorld(worlds.BuildTpjKkdiWorld())
	if err != nil {
		t.Fatal(err)
	}
	drawn := strings.Join(d.Render(Options{Level: Detail, Zoom: 1}), "\n")
	for _, id := range []string{"bsTpjPdkt0", "tpjPf1", "pdktSw0", "tpjPf1S"} {
		if !strings.Contains(drawn, id) {
			t.Errorf("%q not drawn at the detail level", id)
		}
	}
}

func TestStateDrawsTrainsAndColours(t *testing.T) {
	w := vizproto.FromWorld(worlds.BuildTpjKkdiWorld())
	d, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	var line vizproto.Track
	for _, trk := range w.Tracks {
		if trk.Name == "bsTpjPdkt0" {
			line = trk
		}
	}
	st := vizproto.NewState()
	st.Apply(&vizproto.TrackMsg{Track: line.ID, ReservedBy: "0451U", OccupiedBy: "0451U"})
	st.Apply(&vizproto.TrainMsg{Train: "0451U", Track: line.ID, From: line.From, To: line.To, TStart: 10, TEnd: 40, Phase: "running"})

	for _, level := range []Level{Overview, Layout} {
		f := d.Draw(Options{Level: level, Zoom: 1, State: st, Time: 25})
		x, _, ok := f.Train("0451U")
		if !ok {
			t.Fatalf("%s: train not drawn", level)
		}
		path := f.paths[line.ID]
		if mid := path[len(path)/2].X; abs(x-mid) > 1 {
			t.Errorf("%s: halfway along, the train is at column %d, want about %d", level, x, mid)
		}
		label := "0451U"
		if level == Layout {
			label += "▶"
		}
		if drawn := strings.Join(f.Lines(false), "\n"); !strings.Contains(drawn, label) {
			t.Errorf("%s: %q not drawn in\n%s", level, label, drawn)
		}
		if colored := strings.Join(f.Lines(true), "\n"); !strings.Contains(colored, "\x1b[31m─") {
			t.Errorf("%s: occupied track not drawn red", level)
		}
	}
}

// A train's label must start moving as soon as the train does, in either
// direction, not wait at the start of a long track for the front to catch up.
func TestTrainLabelMovesFromTheStartOfATrack(t *testing.T) {
	w := vizproto.FromWorld(worlds.BuildTpjKkdiWorld())
	d, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	var line vizproto.Track
	for _, trk := range w.Tracks {
		if trk.Name == "bsTpjPdkt0" {
			line = trk
		}
	}
	column := func(from, to string, time float64) int {
		st := vizproto.NewState()
		st.Apply(&vizproto.TrainMsg{Train: "0451U", Track: line.ID, From: from, To: to, TStart: 0, TEnd: 100, Phase: "running"})
		for _, row := range d.Render(Options{Zoom: 1, State: st, Time: time}) {
			if i := strings.Index(row, "0451U"); i >= 0 {
				return utf8.RuneCountInString(row[:i])
			}
		}
		t.Fatalf("label not drawn at t=%v", time)
		return 0
	}

	prev := column(line.From, line.To, 0)
	start := prev
	for _, time := range []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100} {
		got := column(line.From, line.To, time)
		if got < prev {
			t.Errorf("eastbound label went back from column %d to %d at t=%v", prev, got, time)
		}
		if time == 20 && got <= start {
			t.Errorf("eastbound label still at column %d a fifth of the way along", got)
		}
		prev = got
	}

	prev = column(line.To, line.From, 0)
	start = prev
	for _, time := range []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100} {
		got := column(line.To, line.From, time)
		if got > prev {
			t.Errorf("westbound label went back from column %d to %d at t=%v", prev, got, time)
		}
		if time == 20 && got >= start {
			t.Errorf("westbound label still at column %d a fifth of the way along", got)
		}
		prev = got
	}
}

// A train running past a signal from behind is not governed by it, so the
// signal is drawn ≡ in the track's colours, both while the train's route is
// set over it and once the train is on its track.
func TestSignalPassedFromBehindHasNoAspect(t *testing.T) {
	w := vizproto.FromWorld(worlds.BuildTpjKkdiSingleLineWorld())
	d, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	const block, throat = "krmgThE1-krurThW1", "krurThW0-krurThW1"
	var sig vizproto.Signal
	for _, s := range w.Signals {
		if s.ID == "slKrmgKrurSig0F" { // eastbound, at KRUR's west throat
			sig = s
		}
	}
	signal := func(st *vizproto.State) cell {
		f := d.Draw(Options{Level: Layout, Zoom: 1, State: st, Time: 0.5}) // trains halfway, clear of the signal
		trk := d.between(sig.Facing, sig.At)
		path := f.paths[trk.ID]
		p := path[1]
		if trk.To == sig.At {
			p = path[len(path)-2]
		}
		return f.cv.cells[p.Y][p.X]
	}
	hold := func(st *vizproto.State, train string, tracks ...string) {
		for _, trk := range tracks {
			st.Apply(&vizproto.TrackMsg{Track: trk, ReservedBy: train})
		}
	}

	st := vizproto.NewState()
	hold(st, "0451D", throat, block)
	st.Apply(&vizproto.TrainMsg{Train: "0451D", Track: throat, From: "krurThW0", To: "krurThW1", TStart: 0, TEnd: 1, Phase: "running"})
	if c := signal(st); c.r != '≡' || !c.track || c.style != styleTrack {
		t.Errorf("westbound train with its route set over the block: signal drawn %q (style %d, track %v), want ≡ as track", c.r, c.style, c.track)
	}

	st = vizproto.NewState()
	hold(st, "0451D", block)
	st.Apply(&vizproto.TrainMsg{Train: "0451D", Track: block, From: "krurThW1", To: "krmgThE1", TStart: 0, TEnd: 1, Phase: "running"})
	if c := signal(st); c.r != '≡' {
		t.Errorf("westbound train on the block: signal drawn %q, want ≡", c.r)
	}

	st = vizproto.NewState()
	hold(st, "0451U", block, throat)
	st.Apply(&vizproto.TrainMsg{Train: "0451U", Track: block, From: "krmgThE1", To: "krurThW1", TStart: 0, TEnd: 1, Phase: "running"})
	if c := signal(st); c.r == '≡' {
		t.Errorf("eastbound train on the block: signal drawn ≡, want an aspect")
	}
}

// heavyAt lists the indices of the path's cells drawn heavy.
func heavyAt(f *Frame, path []pos) []int {
	var out []int
	for i, p := range path {
		if f.cv.cells[p.Y][p.X].r == '━' {
			out = append(out, i)
		}
	}
	return out
}

func TestTrainBodyIsDrawn(t *testing.T) {
	w := vizproto.FromWorld(worlds.BuildTpjKkdiWorld())
	var line vizproto.Track
	for _, trk := range w.Tracks {
		if trk.Name == "bsTpjPdkt0" {
			line = trk
		}
	}
	draw := func(length, time float64, alsoOccupied ...string) (*Frame, []pos) {
		cw := *w
		cw.Trains = []vizproto.Train{{Number: "0451U", LengthM: length}}
		d, err := Build(&cw)
		if err != nil {
			t.Fatal(err)
		}
		st := vizproto.NewState()
		for _, id := range append([]string{line.ID}, alsoOccupied...) {
			st.Apply(&vizproto.TrackMsg{Track: id, ReservedBy: "0451U", OccupiedBy: "0451U"})
		}
		st.Apply(&vizproto.TrainMsg{Train: "0451U", Track: line.ID, From: line.From, To: line.To, TStart: 0, TEnd: 100, Phase: "running"})
		// zoomed in, so the line is long enough for the body to show beside
		// the train's label
		f := d.Draw(Options{Level: Layout, Zoom: 6, State: st, Time: time})
		return f, f.paths[line.ID]
	}

	// halfway along, a quarter of the line long: the body covers 25% to 50%
	f, path := draw(line.Length/4, 50)
	last := float64(len(path) - 1)
	got := heavyAt(f, path)
	if len(got) == 0 {
		t.Fatal("no body drawn")
	}
	for _, i := range got {
		if fr := float64(i) / last; fr < 0.25-1/last || fr > 0.5+1/last {
			t.Errorf("cell %d (%.2f along) drawn heavy, want only 0.25 to 0.50", i, fr)
		}
	}

	// no length (an older recording): no body
	if f, path := draw(0, 50); len(heavyAt(f, path)) != 0 {
		t.Error("a train with no length drew a body")
	}

	// entering: 10% along with nothing occupied behind, the body stops at the
	// start of the track
	f, path = draw(line.Length/4, 10)
	for _, i := range heavyAt(f, path) {
		if fr := float64(i) / last; fr > 0.1+1/last {
			t.Errorf("entering: cell %d (%.2f along) drawn heavy, want none past 0.10", i, fr)
		}
	}

	// longer than what it has run of the line: the rest is on the track
	// behind, ending at the joint
	for _, prev := range w.Tracks {
		if prev.ID == line.ID || (prev.To != line.From && prev.From != line.From) {
			continue
		}
		f, _ := draw(line.Length/10+prev.Length/2, 10, prev.ID)
		pp := f.paths[prev.ID]
		plain := slices.ContainsFunc(pp, func(p pos) bool { r := f.cv.cells[p.Y][p.X].r; return r == '─' || r == '━' })
		if plain && len(heavyAt(f, pp)) == 0 {
			t.Errorf("nothing drawn on %s, the occupied track behind the front's", prev.ID)
		}
	}
}

// The selected train's label shows its speed, from its keys, while it runs;
// another train's, or one with no keys, does not.
func TestSelectedTrainShowsItsSpeed(t *testing.T) {
	w := vizproto.FromWorld(worlds.BuildTpjKkdiWorld())
	var line vizproto.Track
	for _, trk := range w.Tracks {
		if trk.Name == "bsTpjPdkt0" {
			line = trk
		}
	}
	d, err := Build(w)
	if err != nil {
		t.Fatal(err)
	}
	draw := func(selected string, keys []vizproto.Key) string {
		st := vizproto.NewState()
		st.Apply(&vizproto.TrainMsg{Train: "0451U", Track: line.ID, From: line.From, To: line.To, TStart: 0, TEnd: 100, Phase: "running", Keys: keys})
		f := d.Draw(Options{Level: Layout, Zoom: 6, State: st, Time: 50, Selected: selected})
		return strings.Join(f.Lines(false), "\n")
	}
	keys := []vizproto.Key{{T: 0, Frac: 0, Kmph: 0}, {T: 40, Frac: 0.4, Kmph: 87}, {T: 100, Frac: 1, Kmph: 87}}
	if got := draw("0451U", keys); !strings.Contains(got, "0451U▶ 87") {
		t.Errorf("selected, running at 87 km/h: no \"0451U▶ 87\" in\n%s", got)
	}
	for _, c := range []struct {
		selected string
		keys     []vizproto.Key
	}{{"", keys}, {"0451U", nil}} {
		if got := draw(c.selected, c.keys); strings.Contains(got, "0451U▶ ") {
			t.Errorf("selected %q, %d keys: a speed shown in\n%s", c.selected, len(c.keys), got)
		}
	}
}

// A train with a length has a couple of coaches drawn behind its label,
// however short it is at the zoom; one with none (an older recording) not.
func TestCoachesBehindTheLabel(t *testing.T) {
	w := vizproto.FromWorld(worlds.BuildTpjKkdiWorld())
	var line vizproto.Track
	for _, trk := range w.Tracks {
		if trk.Name == "bsTpjPdkt0" {
			line = trk
		}
	}
	draw := func(length float64, from, to string) string {
		cw := *w
		cw.Trains = []vizproto.Train{{Number: "0451U", LengthM: length}}
		d, err := Build(&cw)
		if err != nil {
			t.Fatal(err)
		}
		st := vizproto.NewState()
		st.Apply(&vizproto.TrainMsg{Train: "0451U", Track: line.ID, From: from, To: to, TStart: 0, TEnd: 100, Phase: "running"})
		return strings.Join(d.Draw(Options{Level: Layout, State: st, Time: 50}).Lines(false), "\n")
	}
	// two coaches within reach behind the label: they may step over a
	// signal on the line
	behind := func(got, label string, east bool) string {
		for _, row := range strings.Split(got, "\n") {
			r := []rune(row)
			i := strings.Index(row, label)
			if i < 0 {
				continue
			}
			at := len([]rune(row[:i]))
			if east {
				return string(r[max(at-coachMarks-coachSkip, 0):at])
			}
			end := at + len([]rune(label))
			return string(r[end:min(end+coachMarks+coachSkip, len(r))])
		}
		return ""
	}
	if got := draw(491, line.From, line.To); strings.Count(behind(got, "0451U▶", true), "▬") != 2 {
		t.Errorf("running east: not two coaches behind the label in\n%s", got)
	}
	if got := draw(491, line.To, line.From); strings.Count(behind(got, "◀0451U", false), "▬") != 2 {
		t.Errorf("running west: not two coaches behind the label in\n%s", got)
	}
	if got := draw(0, line.From, line.To); strings.Contains(got, "▬") {
		t.Errorf("no length: coaches drawn in\n%s", got)
	}
}
