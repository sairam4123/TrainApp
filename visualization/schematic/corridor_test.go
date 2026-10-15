package schematic

import (
	"errors"
	"testing"
	"trainapp/worlds"
)

func TestCorridor(t *testing.T) {
	cases := []struct {
		name  string
		world string
		codes []string
		at    []float64 // km, roughly
	}{
		{"TPJ-KKDI", "TpjKkdi", []string{"TPJ", "PDKT", "KKDI"}, []float64{0, 55, 96}},
		{"single line", "SingleLine", []string{"WST", "LPA", "HLT", "EST"}, []float64{0, 12, 20, 30}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := FromWorld(testWorlds[c.world]())
			if err != nil {
				t.Fatal(err)
			}
			cor, err := d.Corridor()
			if err != nil {
				t.Fatal(err)
			}
			if len(cor.Stations) != len(c.codes) {
				t.Fatalf("stations %+v, want %v", cor.Stations, c.codes)
			}
			for i, s := range cor.Stations {
				// the lines join the stations' throats, which are a little
				// way out of the platforms
				if s.Code != c.codes[i] || s.At < c.at[i]*1000 || s.At > c.at[i]*1000+3000 {
					t.Errorf("station %d is %s at %.0f m, want %s at about %.0f km", i, s.Code, s.At, c.codes[i], c.at[i])
				}
			}
			for _, tr := range d.Tracks() {
				for _, p := range []string{tr.From, tr.To} {
					if _, ok := cor.At(p); !ok {
						t.Errorf("point %s of track %s is not on the corridor", p, tr.ID)
					}
				}
			}
		})
	}
}

func TestCorridorPlacesBlockSignalsAlongTheLine(t *testing.T) {
	d, err := FromWorld(worlds.BuildSingleLineWorld(worlds.SingleLineOptions{TerminusPlatforms: 2, LoopPlatforms: 2, BlocksPerLine: 2}))
	if err != nil {
		t.Fatal(err)
	}
	cor, err := d.Corridor()
	if err != nil {
		t.Fatal(err)
	}
	wst, lpa := cor.Stations[0].At, cor.Stations[1].At
	mid := 0
	for _, tr := range d.Tracks() {
		for _, p := range []string{tr.From, tr.To} {
			if at, _ := cor.At(p); at > wst && at < lpa {
				mid++
				if at < wst+5000 || at > lpa-5000 {
					t.Errorf("block point %s at %.0f m, not near the middle of %.0f-%.0f", p, at, wst, lpa)
				}
			}
		}
	}
	if mid == 0 {
		t.Error("no point between WST and LPA")
	}
}

func TestCorridorNeedsASingleLine(t *testing.T) {
	d, err := FromWorld(worlds.BuildJunctionWorld(2))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Corridor(); !errors.Is(err, ErrNotALine) {
		t.Errorf("junction world: err %v, want ErrNotALine", err)
	}
}
