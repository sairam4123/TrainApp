package timetable_test

import (
	"testing"
	"trainapp/railway/timetable"
)

func TestRound(t *testing.T) {
	raw := timetable.Timetable{Trains: []timetable.Train{{
		Number: "1",
		Points: []timetable.Point{
			{Stn: "A", Kind: timetable.KindStop, Arr: 10.25, Dep: 21.2, MinDwell: 10},
			{Stn: "B", Kind: timetable.KindPass, Arr: 30.1, Dep: 30.103},
			{Stn: "C", Kind: timetable.KindHalt, Arr: 40.002, Dep: 43.7},
			{Stn: "D", Kind: timetable.KindStop, Arr: 50.0000000001, Dep: 50.6, MinDwell: 2},
			{Stn: "E", Kind: timetable.KindStop, Arr: -1, Dep: -1, MinDwell: 2},
		},
	}}}
	cases := []struct {
		name     string
		cfg      timetable.Config
		arr, dep []float64
	}{
		{"whole minutes", timetable.Config{RoundingMin: 1}, []float64{10.25, 31, 41, 51, -1}, []float64{22, 31, 44, 53, -1}},
		{"half minutes", timetable.Config{RoundingMin: 0.5}, []float64{10.25, 30.5, 40.5, 50.5, -1}, []float64{21.5, 30.5, 44, 52.5, -1}},
		// running times 8.9, 9.899, 6.3 grow by 10%
		{"recovery", timetable.Config{RoundingMin: 1, RecoveryPct: 10}, []float64{10.25, 32, 43, 51, -1}, []float64{22, 32, 44, 53, -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := timetable.Round(raw, c.cfg)
			if got.RoundingMin != c.cfg.RoundingMin {
				t.Errorf("RoundingMin = %v", got.RoundingMin)
			}
			for i, p := range got.Trains[0].Points {
				if p.Arr != c.arr[i] || p.Dep != c.dep[i] {
					t.Errorf("%s: arr %v dep %v, want %v %v", p.Stn, p.Arr, p.Dep, c.arr[i], c.dep[i])
				}
				r := raw.Trains[0].Points[i]
				if p.Reached() && (p.Arr < r.Arr || p.Dep < r.Dep) {
					t.Errorf("%s moved earlier: %v-%v from %v-%v", p.Stn, p.Arr, p.Dep, r.Arr, r.Dep)
				}
				if p.Reached() && p.Kind == timetable.KindStop && p.Dep-p.Arr < p.MinDwell {
					t.Errorf("%s: dwell %v under %v", p.Stn, p.Dep-p.Arr, p.MinDwell)
				}
			}
			if raw.Trains[0].Points[1].Arr != 30.1 {
				t.Error("Round changed its input")
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	cfg, err := timetable.LoadConfig(nil)
	if err != nil || cfg != timetable.DefaultConfig() {
		t.Errorf("no section: %+v, %v", cfg, err)
	}
	cfg, err = timetable.LoadConfig([]byte(`{"rounding_min": 0.5}`))
	if err != nil || cfg.RoundingMin != 0.5 || cfg.HaltMin != timetable.DefaultConfig().HaltMin {
		t.Errorf("partial section: %+v, %v", cfg, err)
	}
	for _, bad := range []string{`{"rounding_min": 0}`, `{"halt_min": -1}`, `{"nope": 1}`, `{"check_precedence": "maybe"}`} {
		if _, err := timetable.LoadConfig([]byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
