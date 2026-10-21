package vizproto

import (
	"strings"
	"testing"
)

func TestReadsOlderVersions(t *testing.T) {
	for _, v := range []string{"1", "2", "3", "4", "5", "6"} {
		rec := `{"type":"hello","t":0,"version":` + v + `,"world":{"points":[],"tracks":[],"switches":[],"signals":[],"stations":[],"trains":[]}}
{"type":"decision","t":3,"id":"hold:A>B@X","action":"hold","train":"A","for":"B","at":"X","status":"active","source":"auto","since":3}
{"type":"end","t":4}
`
		_, msgs, err := ReadAll(strings.NewReader(rec))
		if err != nil {
			t.Fatalf("version %s: %v", v, err)
		}
		st := NewState()
		for _, m := range msgs {
			st.Apply(m)
		}
		if got := st.ActiveDecisions("A"); len(got) != 1 || got[0].For != "B" {
			t.Errorf("version %s: decisions %+v", v, got)
		}
	}
	if _, _, err := ReadAll(strings.NewReader(`{"type":"hello","t":0,"version":7,"world":{}}`)); err == nil {
		t.Error("version 7 accepted")
	}
}

// Keys place the front at constant acceleration between them; without any,
// it runs at constant speed from TStart to TEnd.
func TestKeysPlaceTheFront(t *testing.T) {
	// from rest to 60 km/h over the first half in 2 min, then 60 km/h for
	// the second half, 1 min
	m := TrainMsg{TStart: 10, TEnd: 13, Keys: []Key{{T: 10, Frac: 0, Kmph: 0}, {T: 12, Frac: 0.5, Kmph: 60}, {T: 13, Frac: 1, Kmph: 60}}}
	for _, c := range []struct{ t, frac, kmph float64 }{
		{9, 0, 0},
		{10, 0, 0},
		{11, 0.125, 30}, // a quarter of the distance in half the time
		{12, 0.5, 60},
		{12.5, 0.75, 60},
		{14, 1, 60},
	} {
		if got := m.Fraction(c.t); got < c.frac-1e-9 || got > c.frac+1e-9 {
			t.Errorf("at %v: fraction %v, want %v", c.t, got, c.frac)
		}
		if got, ok := m.Speed(c.t); !ok || got < c.kmph-1e-9 || got > c.kmph+1e-9 {
			t.Errorf("at %v: speed %v (%v), want %v", c.t, got, ok, c.kmph)
		}
	}
	old := TrainMsg{TStart: 10, TEnd: 14}
	if got := old.Fraction(11); got != 0.25 {
		t.Errorf("no keys: fraction %v, want 0.25", got)
	}
	if _, ok := old.Speed(11); ok {
		t.Error("no keys: a speed")
	}
}

func TestScheduleRoundTrips(t *testing.T) {
	want := Train{Number: "1", Name: "One", Class: "Passenger", LengthM: 491, Schedule: []SchedulePoint{
		{Stn: "A", Kind: "dwell", Pf: "1", Arr: 0, Dep: -1},
		{Stn: "B", Kind: "pass", Pf: "2", Arr: 12.5, Dep: 12.5},
	}}
	var sb strings.Builder
	w := NewWriter(&sb)
	if err := w.Write(&HelloMsg{Version: Version, World: World{Trains: []Train{want}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	hello, _, err := ReadAll(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	got := hello.World.Trains[0]
	if got.Number != want.Number || got.Class != want.Class || len(got.Schedule) != 2 || got.Schedule[0] != want.Schedule[0] || got.Schedule[1] != want.Schedule[1] {
		t.Errorf("read back %+v, want %+v", got, want)
	}
}
