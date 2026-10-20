package des

import (
	"container/heap"
	"fmt"
	"math"
)

const MinDeltaTime = 0.001

// EventID identifies a scheduled event, so it can be cancelled. The zero
// value is never issued.
type EventID uint64

type Event[T comparable] struct {
	Time      float64
	CreatedAt float64
	Type      T
	Data      any
	TrainID   string

	seq uint64 // insertion order, breaks ties between events at the same time
}

// ID is the id Add returned for the event.
func (e Event[T]) ID() EventID {
	return EventID(e.seq)
}

type EventQueue[T comparable] []Event[T]

func (pq EventQueue[T]) Len() int {
	return len(pq)
}

// Less orders by time, then by insertion order, so events scheduled for the
// same instant are processed first-in first-out.
func (pq EventQueue[T]) Less(i, j int) bool {
	if pq[i].Time != pq[j].Time {
		return pq[i].Time < pq[j].Time
	}
	return pq[i].seq < pq[j].seq
}

func (pq EventQueue[T]) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *EventQueue[T]) Push(x any) {
	*pq = append(*pq, x.(Event[T]))
}

func (pq *EventQueue[T]) Pop() any {
	old := *pq
	n := len(old)

	item := old[n-1]
	old[n-1] = Event[T]{} // drop the reference to Data
	*pq = old[:n-1]

	return item
}

type DES[T comparable] struct {
	eq      EventQueue[T]
	CurTime float64

	nextSeq uint64
	// pending holds the ids of the queued events that have not been
	// cancelled. A cancelled event stays in eq until it reaches the front,
	// where NextEvent drops it.
	pending map[EventID]struct{}
}

func (d *DES[T]) Init() {
	heap.Init(&d.eq)
	d.pending = map[EventID]struct{}{}
}

// Add schedules an event and returns its id. It returns an error, and
// schedules nothing, when the time is not finite or is less than MinDeltaTime
// after the current time.
func (d *DES[T]) Add(eventTime float64, evtype T, data any, trainId string) (EventID, error) {
	if math.IsNaN(eventTime) || math.IsInf(eventTime, 0) {
		return 0, fmt.Errorf("des: event %v for %q has non-finite time %v", evtype, trainId, eventTime)
	}
	if eventTime < d.CurTime+MinDeltaTime {
		return 0, fmt.Errorf("des: event %v for %q at %.4f is before the earliest allowed time %.4f", evtype, trainId, eventTime, d.CurTime+MinDeltaTime)
	}

	d.nextSeq++
	heap.Push(&d.eq, Event[T]{
		Time:      eventTime,
		CreatedAt: d.CurTime,
		Type:      evtype,
		Data:      data,
		TrainID:   trainId,
		seq:       d.nextSeq,
	})
	id := EventID(d.nextSeq)
	d.pending[id] = struct{}{}
	return id, nil
}

// Cancel stops a scheduled event from being delivered. It reports whether the
// event was still pending: false if it has already been delivered or
// cancelled, or was never scheduled.
func (d *DES[T]) Cancel(id EventID) bool {
	if _, ok := d.pending[id]; !ok {
		return false
	}
	delete(d.pending, id)
	return true
}

// Len is the number of events still to be delivered, not counting cancelled
// ones.
func (d *DES[T]) Len() int {
	return len(d.pending)
}

// NextEvent delivers the earliest pending event and moves the clock to its
// time. Cancelled events are skipped and do not move the clock.
func (d *DES[T]) NextEvent() (Event[T], bool) {
	for d.eq.Len() > 0 {
		event := heap.Pop(&d.eq).(Event[T])
		if _, ok := d.pending[event.ID()]; !ok {
			continue // cancelled
		}
		delete(d.pending, event.ID())
		d.CurTime = event.Time
		return event, true
	}
	return Event[T]{}, false
}
