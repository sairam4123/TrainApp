package des

import (
	"math"
	"testing"
)

func TestSameTimeEventsAreFIFO(t *testing.T) {
	d := &DES[int]{}
	d.Init()
	for i := range 20 {
		if _, err := d.Add(5, i, nil, "T"); err != nil {
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
		if _, err := d.Add(tm, "x", nil, "T"); err == nil {
			t.Errorf("Add(%v) succeeded, want error", tm)
		}
	}
	if d.Len() != 0 {
		t.Fatalf("queue has %d events, want 0", d.Len())
	}
	if _, err := d.Add(MinDeltaTime, "ok", nil, "T"); err != nil {
		t.Fatalf("Add(MinDeltaTime) = %v", err)
	}
}

func TestCancelledEventsAreNotDelivered(t *testing.T) {
	d := &DES[string]{}
	d.Init()
	a, _ := d.Add(1, "a", nil, "")
	b, _ := d.Add(2, "b", nil, "")
	c, _ := d.Add(2, "c", nil, "")
	if a == 0 || a == b || b == c {
		t.Fatalf("ids %d, %d, %d: want distinct and non-zero", a, b, c)
	}
	if !d.Cancel(b) {
		t.Fatal("Cancel(b) = false, want true")
	}
	if d.Cancel(b) {
		t.Error("cancelling twice = true, want false")
	}
	if d.Len() != 2 {
		t.Errorf("Len = %d, want 2", d.Len())
	}
	got := ""
	for {
		ev, ok := d.NextEvent()
		if !ok {
			break
		}
		got += ev.Type
	}
	if got != "ac" {
		t.Fatalf("got %q, want ac", got)
	}
	if d.Cancel(a) {
		t.Error("cancelling a delivered event = true, want false")
	}
	if d.Cancel(0) || d.Cancel(99) {
		t.Error("cancelling an id never issued = true, want false")
	}
}

func TestCancelledEventDoesNotMoveTheClock(t *testing.T) {
	d := &DES[string]{}
	d.Init()
	d.Add(1, "a", nil, "")
	late, _ := d.Add(5, "late", nil, "")
	d.NextEvent()
	d.Cancel(late)
	if ev, ok := d.NextEvent(); ok {
		t.Fatalf("got %q, want no event", ev.Type)
	}
	if d.CurTime != 1 {
		t.Fatalf("CurTime = %v, want 1", d.CurTime)
	}
	if d.Len() != 0 {
		t.Fatalf("Len = %d, want 0", d.Len())
	}
}

func TestIDMatchesDeliveredEvent(t *testing.T) {
	d := &DES[string]{}
	d.Init()
	id, _ := d.Add(1, "a", nil, "")
	ev, _ := d.NextEvent()
	if ev.ID() != id {
		t.Fatalf("ID() = %d, want %d", ev.ID(), id)
	}
}
