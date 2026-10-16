package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	"trainapp/des"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
)

const (
	progressEvery = 200 * time.Millisecond
	progressWidth = 20
)

// progress is a sim.Observer that draws a progress bar on one terminal line:
// how many schedule points (stops and passes, over every train) have been
// reached, how many trains are done and how many are queued for a route, the
// sim time and the wall time so far. Sim time can't be the bar, as nobody
// knows when the last train will get out.
type progress struct {
	label string
	s     *sim.Sim
	w     io.Writer
	start time.Time
	last  time.Time
	drawn bool
}

func newProgress(label string, s *sim.Sim) *progress {
	now := time.Now()
	return &progress{label: label, s: s, w: os.Stderr, start: now, last: now}
}

func (p *progress) Logged(slog.Level, string, []slog.Attr) {}

// AfterEvent redraws the bar, at most every progressEvery.
func (p *progress) AfterEvent(des.Event[ops.RailwayEvent]) {
	now := time.Now()
	if now.Sub(p.last) < progressEvery {
		return
	}
	p.last = now
	p.draw(now)
}

func (p *progress) draw(now time.Time) {
	reached, points, done, waiting := 0, 0, 0, 0
	for _, st := range p.s.Status() {
		for _, sp := range st.Train.Schedule() {
			points++
			if sp.Arrived() {
				reached++
			}
		}
		if st.Phase == "exited" {
			done++
		} else if st.Waiting != nil {
			waiting++
		}
	}
	filled := 0
	if points > 0 {
		filled = reached * progressWidth / points
	}
	elapsed := now.Sub(p.start).Truncate(time.Second)
	fmt.Fprintf(p.w, "\r\033[K%s [%s%s] %d/%d points · %d done · %d waiting · t=%.1f min · %d:%02d",
		p.label, strings.Repeat("█", filled), strings.Repeat("░", progressWidth-filled),
		reached, points, done, waiting, p.s.CurTime(),
		int(elapsed.Minutes()), int(elapsed.Seconds())%60)
	p.drawn = true
}

// clear wipes the bar so the run's summary starts on a clean line.
func (p *progress) clear() {
	if p.drawn {
		fmt.Fprint(p.w, "\r\033[K")
	}
}

// isTerminal reports whether f is a terminal rather than a pipe or file.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
