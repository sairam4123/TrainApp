package kpi

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Print writes the report for a person to read, headed by title.
func Print(w io.Writer, title string, r *Report) error {
	u := r.Utilization
	fmt.Fprintf(w, "KPIs, %s: %d/%d trains finished, t=%.1f-%.1f\n", title, r.Finished, r.Trains, u.FromMin, u.ToMin)
	if len(r.Unfinished) > 0 {
		fmt.Fprintf(w, "  not finished, left out of delay: %s\n", strings.Join(r.Unfinished, ", "))
	}
	printStats(w, "delay against free running", r.Delay)
	printStats(w, "lateness against the timetable", r.Lateness)
	if d := r.Disruption; d != nil {
		fmt.Fprintf(w, "  disruptions: %.1f min primary delay, %.1f min reactionary\n", d.PrimaryMin, d.ReactionaryMin)
	}
	fmt.Fprintf(w, "  waits: %.1f min in all", r.WaitMin)
	if lw := r.LongestWait; lw != nil {
		fmt.Fprintf(w, "; longest %s %.1f min at %s (%s, from t=%.1f)", lw.Train, lw.Min, lw.At, lw.Kind, lw.FromMin)
	}
	fmt.Fprintln(w)
	if r.Holds > 0 || r.PlatformChanges > 0 {
		fmt.Fprintf(w, "  precedence: %s lasting %.1f min in all, %s\n", count(r.Holds, "hold"), r.HeldMin, count(r.PlatformChanges, "platform change"))
	}
	b := u.Blocks
	fmt.Fprintf(w, "  blocks: %.0f%% occupied on average (%.0f-%.0f%%), %.1f%% standing (%.1f min in section)\n",
		100*b.MeanOccupancy, 100*b.MinOccupancy, 100*b.MaxOccupancy, 100*b.MeanStanding, b.StandingMin)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "    block\toccupied\trunning\tstanding\ttrains/h\t")
	for _, tu := range u.Tracks {
		if tu.Kind == KindBlock && tu.Entries > 0 {
			fmt.Fprintf(tw, "    %s\t%.0f%%\t%.0f%%\t%.1f%%\t%.1f\t\n", tu.Track, 100*tu.Occupancy, 100*tu.Running, 100*tu.Standing, tu.TrainsPerHour)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	parts := make([]string, len(u.Stations))
	for i, su := range u.Stations {
		parts[i] = fmt.Sprintf("%s %.0f%%", su.Stn, 100*su.Occupancy)
	}
	_, err := fmt.Fprintf(w, "  platform tracks occupied: %s\n", strings.Join(parts, ", "))
	return err
}

func printStats(w io.Writer, name string, st *Stats) {
	if st == nil {
		return
	}
	fmt.Fprintf(w, "  %s: %.1f min in all, %.1f a train, %.1f priority-weighted", name, st.TotalMin, st.MeanMin, st.WeightedMeanMin)
	if st.Worst != "" {
		fmt.Fprintf(w, "; worst %s %.1f min", st.Worst, st.WorstMin)
	}
	fmt.Fprintln(w)
	if len(st.Punctual) == 0 {
		return
	}
	parts := make([]string, len(st.Punctual))
	for i, p := range st.Punctual {
		parts[i] = fmt.Sprintf("%.0f%% within %g min", 100*p.Share, p.WithinMin)
	}
	fmt.Fprintf(w, "    punctual: %s\n", strings.Join(parts, ", "))
}

// WriteJSON writes v (a report, or reports keyed by name) as indented JSON.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}
