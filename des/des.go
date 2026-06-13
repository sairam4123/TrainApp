package des

import (
	"container/heap"
	"fmt"
)

const MinDeltaTime = 0.01

type Event[T comparable] struct {
	Time      float64
	CreatedAt float64
	Type      T
	Data      any
}

type EventQueue[T comparable] []Event[T]

func (pq EventQueue[T]) Len() int {
	return len(pq)
}

func (pq EventQueue[T]) Less(i, j int) bool {
	return pq[i].Time < pq[j].Time
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
	*pq = old[:n-1]

	return item
}

type DES[T comparable] struct {
	eq      EventQueue[T]
	CurTime float64
}

func (d *DES[T]) Init() {
	heap.Init(&d.eq)
}

func (d *DES[T]) Add(eventTime float64, evtype T, data any) {
	if eventTime < d.CurTime+MinDeltaTime {
		fmt.Println("Event time is below minimum time")
		return
	}

	heap.Push(&d.eq, Event[T]{
		Time:      eventTime,
		CreatedAt: d.CurTime,
		Type:      evtype,
		Data:      data,
	})
}

func (d *DES[T]) NextEvent() (Event[T], bool) {
	if d.eq.Len() <= 0 {
		return Event[T]{}, false
	}
	event := heap.Pop(&d.eq).(Event[T])
	d.CurTime = event.Time
	return event, true
}
