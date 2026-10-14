package sim

import (
	"io"
	"log/slog"
	"math"
	"strings"
	"trainapp/des"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/railway/interlocking"
	"trainapp/railway/ops"
)

// LogFormat selects how the run log is written. Both formats carry the same
// keys, so a log can be switched between them without changing its readers.
type LogFormat string

const (
	// LogText writes logfmt: one line of key=value pairs per entry, values
	// with spaces quoted. Easy to read and to split with grep/awk or any
	// logfmt parser. The default.
	LogText LogFormat = "text"
	// LogJSON writes one JSON object per line.
	LogJSON LogFormat = "json"
)

// Every log entry has these keys, in this order:
//
//	t      sim time in minutes, rounded to 0.01
//	level  INFO for events, WARN for trains that never finished, ERROR for a failed run
//	event  the event or dispatcher decision, e.g. TRACK_ENTER, ROUTE_WAIT
//
// followed by keys that depend on the entry:
//
//	train, name           the train's number and name (all train entries)
//	track, length_m       the track the train's front is on, or the track released
//	from, to, edges, path a granted route: start and end point, edge count, tracks in order
//	limit, until          a movement authority: last edge index and its track
//	station, pf, entry    a route request: target station, preferred platform, world entry or not
//	reason, blocked_by    why a request waits, and the tracks it waits on
//	released              the track whose release let a waiting request through
//	priority, delay       a waiting train's priority and minutes late (route waits, retries, stuck trains);
//	                      on PRIORITY_CHANGE, priority is the new base priority
//	phase, since          a train that never finished: its phase and when it started waiting
//	err                   why the run failed
const (
	keyTime  = "t"
	keyEvent = "event"
)

// LogPath is where Run writes the log.
func (s *Sim) LogPath() string {
	if s.LogFormat == LogJSON {
		return s.outputPath("logs", "logs.jsonl")
	}
	return s.outputPath("logs", "logs.log")
}

// newLogger writes entries stamped with the sim time instead of the wall
// clock, so the same scenario always produces the same log. An Observer gets
// every entry too.
func (s *Sim) newLogger(w io.Writer) *slog.Logger {
	h := s.newHandler(w)
	if s.Observer != nil {
		h = slog.NewMultiHandler(h, &observerHandler{obs: s.Observer})
	}
	return slog.New(h)
}

func (s *Sim) newHandler(w io.Writer) slog.Handler {
	opts := &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.TimeKey:
				return slog.Float64(keyTime, roundTime(s.CurTime()))
			case slog.MessageKey:
				a.Key = keyEvent
			}
			return a
		},
	}
	if s.LogFormat == LogJSON {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

func roundTime(t float64) float64 {
	return math.Round(t*100) / 100
}

func trainAttrs(t *ops.Train) []any {
	return []any{"train", t.Number, "name", t.Name}
}

func joinTracks(tracks []*infra.TrackSegment) string {
	names := make([]string, len(tracks))
	for i, t := range tracks {
		names[i] = t.String()
	}
	return strings.Join(names, ",")
}

// joinTrackIDs names the tracks the way the rest of the log does.
func (s *Sim) joinTrackIDs(tracks []ids.TrackID) string {
	names := make([]string, len(tracks))
	for i, id := range tracks {
		names[i] = id.String()
		if trk, ok := s.world.TrackGraph.GetTrack(id); ok {
			names[i] = trk.String()
		}
	}
	return strings.Join(names, ",")
}

func routeAttrs(route *interlocking.Route) []any {
	tracks := make([]*infra.TrackSegment, route.End())
	for i := range tracks {
		tracks[i] = route.Step(i).Edge.Track
	}
	return []any{
		"from", route.Path().InitPoint.Id.String(),
		"to", route.Step(route.End() - 1).To.Id.String(),
		"edges", route.End(),
		"path", joinTracks(tracks),
	}
}

func (s *Sim) logEvent(ev des.Event[ops.RailwayEvent]) {
	attrs := []any{"train", ev.TrainID}
	if tc, ok := s.trainCtrllers[ids.TrainID{Number: ev.TrainID}]; ok {
		attrs = trainAttrs(tc.train)
	}
	switch data := ev.Data.(type) {
	case *ops.Train:
		if trk := data.CurrentTrack(); trk != nil {
			attrs = append(attrs, "track", trk.String(), "length_m", int(trk.Length))
		}
	case *infra.TrackSegment:
		attrs = append(attrs, "track", data.String())
	case *ops.MovementAuthority:
		attrs = append(attrs, "limit", data.Limit(), "until", data.Route().Step(data.Limit()).Edge.Track.String())
	case *interlocking.Route:
		attrs = append(attrs, routeAttrs(data)...)
	case priorityChange:
		attrs = append(attrs, "priority", data.to)
	}
	s.log.Info(string(ev.Type), attrs...)
}

func (s *Sim) priorityAttrs(t *ops.Train) []any {
	return []any{"priority", s.Priority(t.ID()), "delay", roundTime(float64(t.Delay(s.CurTime())))}
}

func (s *Sim) logRouteWait(req *RouteRequest, blockers []ids.TrackID) {
	attrs := append(trainAttrs(req.Train),
		"station", req.Station.Code.String(), "pf", req.PrefPfNo, "entry", req.Entry,
		"reason", req.LastErr.Error(), "blocked_by", s.joinTrackIDs(blockers))
	s.log.Info("ROUTE_WAIT", append(attrs, s.priorityAttrs(req.Train)...)...)
}

func (s *Sim) logRouteRetryOK(req *RouteRequest, released *infra.TrackSegment) {
	attrs := append(trainAttrs(req.Train), "station", req.Station.Code.String(), "released", released.String())
	s.log.Info("ROUTE_RETRY_OK", append(attrs, s.priorityAttrs(req.Train)...)...)
}

func (s *Sim) logStuck(st TrainStatus) {
	attrs := append(trainAttrs(st.Train), "phase", st.Phase)
	if w := st.Waiting; w != nil {
		attrs = append(attrs, "station", w.Station.Code.String(), "pf", w.PrefPfNo, "since", roundTime(w.Since), "reason", w.LastErr.Error())
	}
	attrs = append(attrs, "priority", st.Priority, "delay", roundTime(float64(st.Delay)))
	s.log.Warn("STUCK", attrs...)
}
