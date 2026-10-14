package des

import (
	"math"
	"testing"
)

func TestSameTimeEventsAreFIFO(t *testing.T) {
	d := &DES[int]{}
	d.Init()
	for i := range 20 {
		if err := d.Add(5, i, nil, "T"); err != nil {
			t.Fatal(err)
		}
	}
	for want := range 20 {
		ev, ok := d.NextEvent()
		if !ok || ev.Type != want {
			t.Fatalf("pop %d: got %v (ok=%v), want %d", want, ev.Type, ok, want)
		}
	}
}

func TestOrdersByTime(t *testing.T) {
	d := &DES[string]{}
	d.Init()
	d.Add(3, "c", nil, "")
	d.Add(1, "a", nil, "")
	d.Add(2, "b", nil, "")
	got := ""
	for {
		ev, ok := d.NextEvent()
		if !ok {
			break
		}
		got += ev.Type
	}
	if got != "abc" {
		t.Fatalf("got %q, want abc", got)
	}
	if d.CurTime != 3 {
		t.Fatalf("CurTime = %v, want 3", d.CurTime)
	}
}

func TestRejectsInvalidTimes(t *testing.T) {
	d := &DES[string]{}
	d.Init()
	for _, tm := range []float64{-1, 0, MinDeltaTime / 2, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := d.Add(tm, "x", nil, "T"); err == nil {
			t.Errorf("Add(%v) succeeded, want error", tm)
		}
	}
	if d.Len() != 0 {
		t.Fatalf("queue has %d events, want 0", d.Len())
	}
	if err := d.Add(MinDeltaTime, "ok", nil, "T"); err != nil {
		t.Fatalf("Add(MinDeltaTime) = %v", err)
	}
}
