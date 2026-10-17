package timetable

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Load reads a timetable file, as WriteJSON writes it.
func Load(path string) (Timetable, error) {
	f, err := os.Open(path)
	if err != nil {
		return Timetable{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var tt Timetable
	if err := dec.Decode(&tt); err != nil {
		return tt, fmt.Errorf("%s: %w", path, err)
	}
	if len(tt.Trains) == 0 {
		return tt, fmt.Errorf("%s: a timetable with no trains", path)
	}
	return tt, nil
}

// WriteJSON writes the timetable as indented JSON.
func WriteJSON(w io.Writer, tt Timetable) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(tt)
}

// Print writes the timetable as one table per train, then the check run's
// report if there is one.
func Print(w io.Writer, tt Timetable) error {
	seconds := tt.RoundingMin == 0 || tt.RoundingMin != math.Trunc(tt.RoundingMin)
	for i, tr := range tt.Trains {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s  %s", tr.Number, tr.Name)
		if tr.Class != "" {
			fmt.Fprintf(w, "  (%s)", tr.Class)
		}
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  STN\tKIND\tPF\tARR\tDEP\tDWELL\tRUN\tNOTES")
		for j, p := range tr.Points {
			dwell, run := "—", "—"
			if p.Reached() && p.Dep >= 0 && p.Kind != KindPass {
				dwell = FormatMin(p.Dwell)
			}
			if j > 0 && p.Reached() && tr.Points[j-1].Dep >= 0 {
				run = FormatMin(p.Run)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.Stn, p.Kind, p.Pf,
				FormatTime(p.Arr, seconds), FormatTime(p.Dep, seconds), dwell, run, strings.Join(notes(p), "; "))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		for _, warn := range tr.Warnings {
			fmt.Fprintf(w, "  ! %s\n", warn)
		}
	}
	if tt.Check != nil {
		fmt.Fprintln(w)
		printCheck(w, tt.Check, len(tt.Trains))
	}
	return nil
}

func notes(p Point) []string {
	var out []string
	if h := p.Halt; h != nil {
		why := h.Reason
		switch {
		case h.For == "":
		case h.Reason == ReasonTechnical:
			why += ", waited for " + h.For
		case h.Reason == ReasonOvertake:
			why = "overtaken by " + h.For
		default:
			why += " " + h.For
		}
		if p.Kind == KindHalt {
			out = append(out, fmt.Sprintf("halt %s (%s)", FormatMin(h.Min), why))
		} else {
			out = append(out, fmt.Sprintf("dwell +%s (%s)", FormatMin(h.Min), why))
		}
	}
	for _, s := range p.SignalStops {
		note := fmt.Sprintf("stopped at signal %s min on %s", FormatMin(s.Min), s.Track)
		if s.Min < 0 {
			note = fmt.Sprintf("stuck at signal on %s since %s", s.Track, FormatTime(s.At, true))
		}
		if s.For != "" {
			note += ", waited for " + s.For
		}
		out = append(out, note)
	}
	return out
}

func printCheck(w io.Writer, r *CheckReport, trains int) {
	how := ""
	if r.Precedence != "" {
		how = ", precedence " + r.Precedence
	}
	if r.OK() {
		fmt.Fprintf(w, "check run: all %d trains kept their times (tolerance %s min%s)\n", trains, FormatMin(r.ToleranceMin), how)
		return
	}
	fmt.Fprintf(w, "check run: the timetable does not hold (tolerance %s min%s)\n", FormatMin(r.ToleranceMin), how)
	for _, l := range r.Late {
		fmt.Fprintf(w, "  %s up to %s min late (%s %s)\n", l.Train, FormatMin(l.LateMin), l.Event, l.Stn)
	}
	for _, n := range r.Unfinished {
		fmt.Fprintf(w, "  %s did not finish\n", n)
	}
	if r.Err != "" {
		fmt.Fprintf(w, "  run failed: %s\n", r.Err)
	}
}

// FormatTime shows minutes from 0 as HH:MM, or HH:MM:SS with seconds; "—"
// for a time that never came.
func FormatTime(m float64, seconds bool) string {
	if m < 0 {
		return "—"
	}
	if seconds {
		s := int(math.Round(m * 60))
		return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	mins := int(math.Round(m))
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// FormatMin shows a number of minutes to two decimals at most.
func FormatMin(m float64) string {
	return strconv.FormatFloat(math.Round(m*100)/100, 'f', -1, 64)
}
