package vizproto

import (
	"fmt"
	"strings"
	"testing"
)

func TestReadsOlderVersions(t *testing.T) {
	for _, v := range []string{"1", "2", "3", "4", "5", "6", "7", "8"} {
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
	newer := fmt.Sprintf(`{"type":"hello","t":0,"version":%d,"world":{}}`, Version+1)
	if _, _, err := ReadAll(strings.NewReader(newer)); err == nil {
		t.Errorf("version %d accepted", Version+1)
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

func TestDeadlockReads(t *testing.T) {
	rec := fmt.Sprintf(`{"type":"hello","t":0,"version":%d,"world":{}}
{"type":"deadlock","t":8,"since":7.5,"cycle":[{"train":"W1","for":"E1","track":"blk"},{"train":"E1","for":"W1","track":"pf2"}],"text":"W1 waits for E1 (blk) → E1 waits for W1 (pf2)"}
{"type":"error","t":8,"message":"deadlock"}
`, Version)
	_, msgs, err := ReadAll(strings.NewReader(rec))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := msgs[0].(*DeadlockMsg)
	if !ok {
		t.Fatalf("first message %T", msgs[0])
	}
	if d.Since != 7.5 || !d.Has("E1") || !d.Has("W1") || d.Has("E2") {
		t.Errorf("deadlock %+v", d)
	}
}
