package vizproto

import (
	"strings"
	"testing"
)

func TestReadsOlderVersions(t *testing.T) {
	for _, v := range []string{"1", "2", "3", "4"} {
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
	if _, _, err := ReadAll(strings.NewReader(`{"type":"hello","t":0,"version":5,"world":{}}`)); err == nil {
		t.Error("version 5 accepted")
	}
}

func TestScheduleRoundTrips(t *testing.T) {
	want := Train{Number: "1", Name: "One", Class: "Passenger", Schedule: []SchedulePoint{
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
