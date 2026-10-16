package sim

import (
	"context"
	"log/slog"
	"slices"
	"trainapp/des"
	"trainapp/railway/ops"
)

// Observer watches a run. Logged gets every log entry as it is written, and
// AfterEvent is called once each event's controller has run.
type Observer interface {
	Logged(level slog.Level, event string, attrs []slog.Attr)
	AfterEvent(ev des.Event[ops.RailwayEvent])
}

// Observers lets several Observers watch one run, called in order.
type Observers []Observer

func (obs Observers) Logged(level slog.Level, event string, attrs []slog.Attr) {
	for _, o := range obs {
		o.Logged(level, event, attrs)
	}
}

func (obs Observers) AfterEvent(ev des.Event[ops.RailwayEvent]) {
	for _, o := range obs {
		o.AfterEvent(ev)
	}
}

// observerHandler passes log entries to an Observer.
type observerHandler struct {
	obs   Observer
	attrs []slog.Attr
}

func (h *observerHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *observerHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := slices.Clone(h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	h.obs.Logged(r.Level, r.Message, attrs)
	return nil
}

func (h *observerHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &observerHandler{obs: h.obs, attrs: append(slices.Clone(h.attrs), attrs...)}
}

func (h *observerHandler) WithGroup(string) slog.Handler { return h }
