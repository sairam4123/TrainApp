package worlds

import (
	"testing"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
)

func TestWorldsBuild(t *testing.T) {
	for name, build := range map[string]func() any{
		"TpjKkdi":           func() any { return BuildTpjKkdiWorld() },
		"TpjKkdiSingleLine": func() any { return BuildTpjKkdiSingleLineWorld() },
		"TpjKkdiDoubleLine": func() any { return BuildTpjKkdiDoubleLineWorld() },
		"Test":              func() any { return BuildTestWorld() },
		"SingleLine": func() any {
			return BuildSingleLineWorld(SingleLineOptions{TerminusPlatforms: 3, LoopPlatforms: 2, BlocksPerLine: 3})
		},
		"Junction": func() any { return BuildJunctionWorld(2) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("build panicked: %v", r)
				}
			}()
			build()
		})
	}
}

func TestTpjKkdiSingleLineTracks(t *testing.T) {
	w := BuildTpjKkdiSingleLineWorld()
	want := map[ids.StnCode]map[string]infra.TrackClass{
		"TPJ":  {"1": infra.Loop, "9": infra.Through, "5": infra.Main, "8": infra.Loop},
		"KRMG": {"1": infra.Loop, "3": infra.Through, "2": infra.Loop},
		"KRUR": {"1": infra.Loop, "3": infra.Through, "2": infra.Loop},
		"VEL":  {"1": infra.Loop, "3": infra.Through, "2": infra.Loop},
		"PDKT": {"1": infra.Main, "2": infra.Loop, "3": infra.Loop},
		"NMN":  {"1": infra.Main},
		"TYM":  {"1": infra.Loop, "3": infra.Through, "2": infra.Loop},
		"CTND": {"1": infra.Loop, "2": infra.Main, "3": infra.Loop},
		"KTYR": {"1": infra.Main},
		"KKDI": {"1": infra.Loop, "6": infra.Through, "3": infra.Main, "5": infra.Loop},
	}
	if n := len(w.ListStations()); n != len(want) {
		t.Errorf("%d stations, want %d", n, len(want))
	}
	for code, pfs := range want {
		stn, ok := w.GetStation(code)
		if !ok {
			t.Errorf("no station %s", code)
			continue
		}
		for no, class := range pfs {
			trk := stn.FindStnPlatform(no)
			if trk == nil {
				t.Errorf("%s has no platform %s", code, no)
				continue
			}
			if got := stn.TrackClass(trk.Id); got != class {
				t.Errorf("%s pf%s is %s, want %s", code, no, got, class)
			}
		}
	}
	for _, code := range []ids.StnCode{"TPJ", "KKDI"} {
		if stn, _ := w.GetStation(code); stn.RunsThrough() {
			t.Errorf("%s is the world's edge, but can be run through", code)
		}
	}
}

func TestTpjKkdiDoubleLineTracks(t *testing.T) {
	w := BuildTpjKkdiDoubleLineWorld()
	crossing := map[string]infra.TrackClass{"1": infra.Loop, "3": infra.Through, "4": infra.Through, "2": infra.Loop}
	want := map[ids.StnCode]map[string]infra.TrackClass{
		"TPJ":  {"1": infra.Loop, "9": infra.Through, "4": infra.Main, "5": infra.Main, "8": infra.Loop},
		"KRMG": {"1": infra.Main, "3": infra.Through, "2": infra.Loop},
		"KRUR": crossing,
		"VEL":  crossing,
		"PDKT": {"1": infra.Main, "2": infra.Main, "3": infra.Loop},
		"NMN":  {"1": infra.Main, "2": infra.Main},
		"TYM":  crossing,
		"CTND": {"1": infra.Main, "2": infra.Main, "3": infra.Loop},
		"KTYR": {"1": infra.Main, "2": infra.Main},
		"KKDI": {"1": infra.Loop, "6": infra.Through, "2": infra.Main, "3": infra.Main, "5": infra.Loop},
	}
	if n := len(w.ListStations()); n != len(want) {
		t.Errorf("%d stations, want %d", n, len(want))
	}
	for code, pfs := range want {
		stn, ok := w.GetStation(code)
		if !ok {
			t.Errorf("no station %s", code)
			continue
		}
		for no, class := range pfs {
			trk := stn.FindStnPlatform(no)
			if trk == nil {
				t.Errorf("%s has no platform %s", code, no)
				continue
			}
			if got := stn.TrackClass(trk.Id); got != class {
				t.Errorf("%s pf%s is %s, want %s", code, no, got, class)
			}
		}
	}
	for _, code := range []ids.StnCode{"TPJ", "KKDI"} {
		if stn, _ := w.GetStation(code); stn.RunsThrough() {
			t.Errorf("%s is the world's edge, but can be run through", code)
		}
	}
}

// A train runs from the station before onto a platform of the next on its
// own line, the way the signals let it; it reaches the other line's tracks
// only over a crossover (TPJ, KRMG, PDKT, CTND, KKDI).
func TestTpjKkdiDoubleLineRoutes(t *testing.T) {
	w := BuildTpjKkdiDoubleLineWorld()
	point := func(id ids.PointID) *infra.TrackPoint {
		for _, e := range w.TrackGraph.Edges {
			if e.From.Id == id {
				return e.From
			}
			if e.To.Id == id {
				return e.To
			}
		}
		t.Fatalf("no point %s", id)
		return nil
	}
	// runs reports whether a train leaving platform fromNo of from (its
	// east end going east, its west end going west) can run onto platform
	// no of to
	runs := func(from ids.StnCode, fromNo string, east bool, to ids.StnCode, no string) bool {
		end := "W"
		if east {
			end = "E"
		}
		start := point(ids.PointID{StnCode: from, Type: "Pf", Seq: fromNo, Other: map[string]string{"E": "E", "W": "S"}[end]})
		stn, _ := w.GetStation(to)
		trk := stn.FindStnPlatform(no)
		if trk == nil {
			t.Fatalf("%s has no platform %s", to, no)
		}
		paths, err := w.TrackGraph.GenerateCandidatePaths(start, trk)
		if err != nil {
			return false
		}
		for _, p := range paths {
			if w.IsLegalPath(p) && w.FollowsSignals(p) {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		from, fromNo string
		east         bool
		to, no       string
		want         bool
	}{
		// the east line, TPJ to KKDI
		{"TPJ", "8", true, "KRMG", "1", true}, // over TPJ's scissors
		{"KRMG", "1", true, "KRUR", "1", true},
		{"KRMG", "1", true, "KRUR", "3", true},
		{"KRMG", "1", true, "KRUR", "2", false}, // no crossover at KRUR
		{"KRUR", "1", true, "VEL", "4", false},
		{"VEL", "3", true, "PDKT", "3", true}, // over PDKT's scissors
		{"PDKT", "1", true, "NMN", "1", true},
		{"PDKT", "1", true, "NMN", "2", false},
		{"TYM", "1", true, "CTND", "1", true},
		{"KTYR", "1", true, "KKDI", "5", true}, // over KKDI's scissors
		// the west line, KKDI to TPJ
		{"KKDI", "1", false, "KTYR", "2", true},
		{"KTYR", "2", false, "CTND", "2", true},
		{"KTYR", "2", false, "CTND", "1", true},
		{"CTND", "3", false, "TYM", "2", true},
		{"CTND", "3", false, "TYM", "1", false},
		{"KRUR", "2", false, "KRMG", "2", true},
		{"KRUR", "2", false, "KRMG", "1", true},
		{"KRMG", "3", false, "TPJ", "1", true},
		// neither line runs the wrong way
		{"KRMG", "2", true, "KRUR", "2", false},
		{"KRUR", "1", false, "KRMG", "1", false},
	} {
		if got := runs(ids.StnCode(c.from), c.fromNo, c.east, ids.StnCode(c.to), c.no); got != c.want {
			way := "west"
			if c.east {
				way = "east"
			}
			t.Errorf("%s pf%s running %s to %s pf%s: %v, want %v", c.from, c.fromNo, way, c.to, c.no, got, c.want)
		}
	}
}
