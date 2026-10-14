package des

import (
	"container/heap"
	"fmt"
	"math"
)

const MinDeltaTime = 0.01

type Event[T comparable] struct {
	Time      float64
	CreatedAt float64
	Type      T
	Data      any
	TrainID   string

	seq uint64 // insertion order, breaks ties between events at the same time
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
}

func (d *DES[T]) Init() {
	heap.Init(&d.eq)
}

// Add schedules an event. It returns an error, and schedules nothing, when the
// time is not finite or is less than MinDeltaTime after the current time.
func (d *DES[T]) Add(eventTime float64, evtype T, data any, trainId string) error {
	if math.IsNaN(eventTime) || math.IsInf(eventTime, 0) {
		return fmt.Errorf("des: event %v for %q has non-finite time %v", evtype, trainId, eventTime)
	}
	if eventTime < d.CurTime+MinDeltaTime {
		return fmt.Errorf("des: event %v for %q at %.4f is before the earliest allowed time %.4f", evtype, trainId, eventTime, d.CurTime+MinDeltaTime)
	}

	heap.Push(&d.eq, Event[T]{
		Time:      eventTime,
		CreatedAt: d.CurTime,
		Type:      evtype,
		Data:      data,
		TrainID:   trainId,
		seq:       d.nextSeq,
	})
	d.nextSeq++
	return nil
}

func (d *DES[T]) Len() int {
	return d.eq.Len()
}

func (d *DES[T]) NextEvent() (Event[T], bool) {
	if d.eq.Len() <= 0 {
		return Event[T]{}, false
	}
	event := heap.Pop(&d.eq).(Event[T])
	d.CurTime = event.Time
	return event, true
}
