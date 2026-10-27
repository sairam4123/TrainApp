package runs

import (
	"fmt"
	"strings"
	"trainapp/railway/kpi"
	"trainapp/railway/scenario"
	"trainapp/railway/sim"
	"trainapp/railway/timetable"
	"trainapp/visualization/compare"
	"trainapp/visualization/tui"
)

// SummaryLines are a run's figures, a line each: trains done, delay,
// punctuality, reactionary delay, holds; and, for an export, how its
// timetable's check run went. With a baseline, each figure is followed by
// its change against it, green where better and red where worse.
func SummaryLines(r *Result) []string {
	rep := r.Report
	if rep == nil {
		return []string{tui.Failed.Render("no KPIs: the run did not finish")}
	}
	var figs []compare.Figure
	if r.Baseline != nil {
		figs = compare.Figures([]*kpi.Report{r.Baseline, rep}, []compare.Kind{compare.SameSchedule, compare.Schedules(r.Baseline, rep)})
	}
	// against is a figure's change against the baseline, if there is one.
	against := func(key string) string {
		f, ok := compare.Find(figs, key)
		if !ok {
			return ""
		}
		d := f.DeltaText(1)
		switch f.Verdict(1) {
		case 1:
			d = tui.Good.Render(d)
		case -1:
			d = tui.Failed.Render(d)
		default:
			d = tui.Dim.Render(d)
		}
		return " " + d
	}
	done := fmt.Sprintf("%d/%d trains", rep.Finished, rep.Trains)
	if rep.Finished == rep.Trains {
		done = tui.Good.Render(done)
	} else {
		done = tui.Failed.Render(done)
	}
	out := []string{tui.Figure("done", done)}
	if d := rep.Delay; d != nil {
		out = append(out, tui.Figure("avg delay", fmt.Sprintf("%.1f min", d.MeanMin)+against("delay_mean")))
		if n := len(d.Punctual); n > 0 { // the widest band
			pu := d.Punctual[n-1]
			out = append(out, tui.Figure("on time", fmt.Sprintf("%.0f%%", 100*pu.Share)+tui.Dim.Render(fmt.Sprintf(" within %g min", pu.WithinMin))+against(fmt.Sprintf("on_time_%g", pu.WithinMin))))
		}
	}
	if dis := rep.Disruption; dis != nil {
		out = append(out, tui.Figure("knock-on", fmt.Sprintf("%.1f min", dis.ReactionaryMin)+against("reactionary")))
	}
	out = append(out, tui.Figure("holds", fmt.Sprint(rep.Holds)+against("holds")))
	if r.Timetable != nil {
		out = append(out, tui.Figure("timetable", CheckText(r.Timetable.Check)))
	}
	return out
}

// CheckText says in a few words how a timetable's check run went.
func CheckText(c *timetable.CheckReport) string {
	switch {
	case c == nil:
		return tui.Dim.Render("not checked")
	case c.OK():
		return tui.Good.Render(fmt.Sprintf("holds (±%g min)", c.ToleranceMin))
	case c.Err != "":
		return tui.Failed.Render("check run failed")
	}
	var parts []string
	if n := len(c.Late); n > 0 {
		parts = append(parts, fmt.Sprintf("%d late", n))
	}
	if n := len(c.Unfinished); n > 0 {
		parts = append(parts, fmt.Sprintf("%d unfinished", n))
	}
	return tui.Amber.Render("does not hold: " + strings.Join(parts, ", "))
}

// RuleExplanation says what a rule does, with the profile's settings. cfg is
// the profile, nil for none; timetabled says the scenario runs a timetable,
// which the profile's rule keeps to its plan.
func RuleExplanation(cfg *sim.PriorityConfig, rule string, timetabled bool) string {
	if rule == "" {
		if cfg == nil || cfg.Precedence == nil {
			return "the profile's: " + RuleExplanation(cfg, scenario.RuleOff, timetabled)
		}
		if timetabled {
			mode := timetable.CheckPlan
			if c, err := timetable.LoadConfig(cfg.Timetable); err == nil {
				mode = c.CheckPrecedence
			}
			if mode == timetable.CheckPlan {
				return "the timetable's plan: each train on its platforms, and held for the trains it waits for in the timetable"
			}
		}
		own := tui.ValueOr(cfg.Precedence.Rule, "thresholds") // the sim's default
		return "the profile's " + own + ": " + RuleExplanation(cfg, own, timetabled)
	}
	if rule == scenario.RuleOff {
		return "no precedence: no train is held back or moved to another platform for a higher-priority one"
	}
	if cfg == nil || cfg.Precedence == nil {
		return "the profile has no precedence settings, so this rule cannot run"
	}
	c := cfg.Precedence
	var text string
	switch rule {
	case "thresholds":
		text = fmt.Sprintf("hold a train for one ≥%d priority higher that would lose ≥%g min", c.MinPriorityGap, float64(c.MinSavingMin))
		if c.MaxWaitRatio > 0 {
			text += fmt.Sprintf(", if it waits ≤%g min per minute saved", c.MaxWaitRatio)
		}
	case "weighted":
		text = fmt.Sprintf("hold a train when the other's priority × minutes saved, less its own × minutes waited, is over %g", c.MinNetGain)
	default:
		text = "rule " + rule
	}
	if c.MaxHoldMin > 0 {
		text += fmt.Sprintf("; holds last ≤%g min", float64(c.MaxHoldMin))
	}
	return text
}
