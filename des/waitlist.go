package des

import (
	"cmp"
	"slices"
)

// WaitList tracks waiters blocked on resources. A waiter can wait on several
// resources at once (any of them freeing might unblock it). Releasing a
// resource wakes its waiters highest Rank first, then first-in first-out; a
// waiter keeps its place in line across repeated waits until it is removed.
type WaitList[R comparable, W comparable] struct {
	// Rank, if set, orders waiters: higher ranks are woken first. It is read
	// afresh on every Wake and Waiting, so ranks may change while waiting.
	Rank func(W) int

	byRes map[R][]W
	on    map[W][]R
	seq   map[W]uint64 // place in line, assigned on first Wait

	nextSeq uint64
}

func (wl *WaitList[R, W]) init() {
	if wl.byRes == nil {
		wl.byRes = make(map[R][]W)
		wl.on = make(map[W][]R)
		wl.seq = make(map[W]uint64)
	}
}

// Wait registers w as blocked on the given resources, replacing whatever it
// was waiting on before. With no resources w stays in line but nothing will
// wake it.
func (wl *WaitList[R, W]) Wait(w W, resources ...R) {
	wl.init()
	wl.detach(w)
	if _, ok := wl.seq[w]; !ok {
		wl.seq[w] = wl.nextSeq
		wl.nextSeq++
	}
	seen := make(map[R]struct{}, len(resources))
	for _, r := range resources {
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		wl.byRes[r] = append(wl.byRes[r], w)
		wl.on[w] = append(wl.on[w], r)
	}
	if _, ok := wl.on[w]; !ok {
		wl.on[w] = nil
	}
}

// Wake returns the waiters blocked on r, highest rank then longest-waiting
// first, and detaches them from all their resources. They keep their place in
// line; call Wait to block them again or Remove once they are served.
func (wl *WaitList[R, W]) Wake(r R) []W {
	wl.init()
	woken := slices.Clone(wl.byRes[r])
	wl.sort(woken)
	for _, w := range woken {
		wl.detach(w)
	}
	return woken
}

// Remove takes w out of the list entirely.
func (wl *WaitList[R, W]) Remove(w W) {
	wl.init()
	wl.detach(w)
	delete(wl.on, w)
	delete(wl.seq, w)
}

// Waiting returns every waiter still in line, highest rank then
// longest-waiting first.
func (wl *WaitList[R, W]) Waiting() []W {
	wl.init()
	out := make([]W, 0, len(wl.seq))
	for w := range wl.seq {
		out = append(out, w)
	}
	wl.sort(out)
	return out
}

// sort orders waiters highest rank first, then by place in line.
func (wl *WaitList[R, W]) sort(ws []W) {
	rank := make(map[W]int, len(ws))
	if wl.Rank != nil {
		for _, w := range ws {
			rank[w] = wl.Rank(w)
		}
	}
	slices.SortFunc(ws, func(a, b W) int {
		if c := cmp.Compare(rank[b], rank[a]); c != 0 {
			return c
		}
		return cmp.Compare(wl.seq[a], wl.seq[b])
	})
}

// BlockedOn returns the resources w is waiting on.
func (wl *WaitList[R, W]) BlockedOn(w W) []R {
	wl.init()
	return slices.Clone(wl.on[w])
}

// Has reports whether w is in line.
func (wl *WaitList[R, W]) Has(w W) bool {
	wl.init()
	_, ok := wl.seq[w]
	return ok
}

// detach removes w from every resource it waits on, keeping its place in line.
func (wl *WaitList[R, W]) detach(w W) {
	for _, r := range wl.on[w] {
		wl.byRes[r] = slices.DeleteFunc(wl.byRes[r], func(x W) bool { return x == w })
		if len(wl.byRes[r]) == 0 {
			delete(wl.byRes, r)
		}
	}
	if _, ok := wl.on[w]; ok {
		wl.on[w] = nil
	}
}
