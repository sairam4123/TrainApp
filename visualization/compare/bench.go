package compare

import (
	"trainapp/railway/kpi/report"
)

// BenchPair is one scenario under one rule in two bench files.
type BenchPair struct {
	Scenario, Rule string
	Old, New       report.BenchRun
	Kind           Kind // how New's trains compare with Old's
	// Figures are Old's and New's figures, then their wall times; nil if
	// either has no report.
	Figures []Figure
}

// BenchDiff is two bench files lined up by scenario and rule.
type BenchDiff struct {
	Pairs            []BenchPair
	OnlyOld, OnlyNew []report.BenchRun
}

// RuleLabel names a rule as traindemo does: "" is the profile's.
func RuleLabel(rule string) string {
	if rule == "" {
		return "profile"
	}
	return rule
}

// Bench lines up two bench files, old the baseline, in new's order.
func Bench(old, new []report.BenchRun) BenchDiff {
	key := func(r report.BenchRun) [2]string { return [2]string{r.Scenario, RuleLabel(r.Rule)} }
	olds := map[[2]string]report.BenchRun{}
	for _, r := range old {
		olds[key(r)] = r
	}
	var d BenchDiff
	paired := map[[2]string]bool{}
	for _, n := range new {
		o, ok := olds[key(n)]
		if !ok {
			d.OnlyNew = append(d.OnlyNew, n)
			continue
		}
		paired[key(n)] = true
		d.Pairs = append(d.Pairs, Pair(o, n))
	}
	for _, o := range old {
		if !paired[key(o)] {
			d.OnlyOld = append(d.OnlyOld, o)
		}
	}
	return d
}

// Pair compares two runs of one scenario under one rule.
func Pair(old, new report.BenchRun) BenchPair {
	p := BenchPair{Scenario: new.Scenario, Rule: RuleLabel(new.Rule), Old: old, New: new}
	if old.Report == nil || new.Report == nil {
		return p
	}
	p.Kind = Schedules(old.Report, new.Report)
	p.Figures = Figures([]*report.Report{old.Report, new.Report}, []Kind{SameSchedule, p.Kind})
	if old.WallSec > 0 && new.WallSec > 0 {
		p.Figures = append(p.Figures, Figure{Key: "wall", Name: "wall", Group: "run", Unit: Seconds, Better: Lower,
			Values: []Value{{V: old.WallSec, OK: true, Fair: true}, {V: new.WallSec, OK: true, Fair: true}}})
	}
	return p
}
