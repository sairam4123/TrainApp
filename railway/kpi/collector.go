package kpi

import (
	"log/slog"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
)

// Collector watches a run for when each train's front entered each track,
// reached its far end, and left it: the track occupations utilization is
// worked out from. It is a sim.Observer; attach it before Run.
//
// The times are those of the TRACK_ENTER, travel end and TRACK_RELEASED
// events. A track is left when the train's front enters the next one, since
// train length is not modelled.
type Collector struct {
	occs []occupation
	open map[occKey]int // occupations not left yet, as indexes into occs
	last map[string]int // each train's latest occupation
}

type occKey struct {
	train string
	track ids.TrackID
}

// occupation is one train on one track: running from enter to travelEnd,
// standing from then until leave. Both are -1 until known.
type occupation struct {
	train                   string
	track                   *infra.TrackSegment
	enter, travelEnd, leave float64
}

func NewCollector() *Collector {
	return &Collector{open: map[occKey]int{}, last: map[string]int{}}
}

func (c *Collector) Logged(slog.Level, string, []slog.Attr) {}

func (c *Collector) AfterEvent(ev des.Event[ops.RailwayEvent]) {
	switch ev.Type {
	case ops.TrackEntered:
		t, ok := ev.Data.(*ops.Train)
		if !ok {
			return
		}
		trk := t.CurrentTrack()
		if trk == nil {
			return
		}
		k := occKey{t.Number, trk.Id}
		if _, ok := c.open[k]; ok {
			return
		}
		c.open[k] = len(c.occs)
		c.last[t.Number] = len(c.occs)
		c.occs = append(c.occs, occupation{train: t.Number, track: trk, enter: ev.Time, travelEnd: -1, leave: -1})
	case ops.TrackTravelEnd:
		if i, ok := c.last[ev.TrainID]; ok && c.occs[i].travelEnd < 0 && c.occs[i].leave < 0 {
			c.occs[i].travelEnd = ev.Time
		}
	case ops.TrackReleased:
		trk, ok := ev.Data.(*infra.TrackSegment)
		if !ok {
			return
		}
		// a track only reserved, never entered, is released too: no occupation
		k := occKey{ev.TrainID, trk.Id}
		if i, ok := c.open[k]; ok {
			c.occs[i].leave = ev.Time
			delete(c.open, k)
		}
	}
}

// arrivedAt is when the train's front first reached the far end of a track:
// at its entry platform, when its first dwell began. -1 if never.
func (c *Collector) arrivedAt(train string) float64 {
	for _, o := range c.occs {
		if o.train == train {
			return o.travelEnd
		}
	}
	return -1
}

// span is when the train's front ran and stood on o, with the open ends
// closed at end.
func (o occupation) span(end float64) (enter, travelEnd, leave float64) {
	leave = o.leave
	if leave < 0 {
		leave = end
	}
	travelEnd = o.travelEnd
	if travelEnd < 0 || travelEnd > leave {
		travelEnd = leave
	}
	return o.enter, travelEnd, leave
}
