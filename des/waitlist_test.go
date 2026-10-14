package des

import (
	"slices"
	"testing"
)

func TestWaitListWakesOnlyWaitersOfTheResource(t *testing.T) {
	var wl WaitList[string, string]
	wl.Wait("t1", "a")
	wl.Wait("t2", "b")
	wl.Wait("t3", "a", "b")

	if got := wl.Wake("a"); !slices.Equal(got, []string{"t1", "t3"}) {
		t.Fatalf("Wake(a) = %v, want [t1 t3]", got)
	}
	// t3 was detached from b too
	if got := wl.Wake("b"); !slices.Equal(got, []string{"t2"}) {
		t.Fatalf("Wake(b) = %v, want [t2]", got)
	}
	if got := wl.Wake("a"); len(got) != 0 {
		t.Fatalf("second Wake(a) = %v, want none", got)
	}
}

func TestWaitListKeepsPlaceInLine(t *testing.T) {
	var wl WaitList[string, string]
	wl.Wait("old", "x")
	wl.Wait("new", "y")
	// old is woken, fails again and now waits on y as well
	wl.Wake("x")
	wl.Wait("old", "y")
	if got := wl.Wake("y"); !slices.Equal(got, []string{"old", "new"}) {
		t.Fatalf("Wake(y) = %v, want [old new]", got)
	}
	if got := wl.Waiting(); !slices.Equal(got, []string{"old", "new"}) {
		t.Fatalf("Waiting = %v", got)
	}
	wl.Remove("old")
	if wl.Has("old") || !wl.Has("new") {
		t.Fatal("Remove did not take old out")
	}
}

func TestWaitListWakesHighestRankFirst(t *testing.T) {
	rank := map[string]int{"low": 1, "high": 5, "low2": 1}
	wl := WaitList[string, string]{Rank: func(w string) int { return rank[w] }}
	wl.Wait("low", "x")
	wl.Wait("high", "x")
	wl.Wait("low2", "x")
	if got := wl.Waiting(); !slices.Equal(got, []string{"high", "low", "low2"}) {
		t.Fatalf("Waiting = %v, want [high low low2]", got)
	}
	if got := wl.Wake("x"); !slices.Equal(got, []string{"high", "low", "low2"}) {
		t.Fatalf("Wake(x) = %v, want [high low low2]", got)
	}

	// ranks are read when woken, so a change while waiting counts
	wl.Wait("low", "y")
	wl.Wait("high", "y")
	wl.Wait("low2", "y")
	rank["low2"] = 9
	if got := wl.Wake("y"); !slices.Equal(got, []string{"low2", "high", "low"}) {
		t.Fatalf("Wake(y) after rank change = %v, want [low2 high low]", got)
	}
}

func TestWaitListReplacesResources(t *testing.T) {
	var wl WaitList[string, string]
	wl.Wait("t", "a", "a", "b")
	if got := wl.BlockedOn("t"); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("BlockedOn = %v", got)
	}
	wl.Wait("t", "c")
	if got := wl.Wake("a"); len(got) != 0 {
		t.Fatalf("Wake(a) after re-wait = %v, want none", got)
	}
	if got := wl.Wake("c"); !slices.Equal(got, []string{"t"}) {
		t.Fatalf("Wake(c) = %v", got)
	}
	// waiting on nothing keeps it in line but unwakeable
	wl.Wait("t")
	if !wl.Has("t") || len(wl.BlockedOn("t")) != 0 {
		t.Fatal("empty wait lost the waiter")
	}
}
