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
