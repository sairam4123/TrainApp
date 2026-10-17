// Command trainbench runs benchmark scenarios under several precedence rules
// and prints their KPIs side by side.
//
//	trainbench [-rules thresholds,weighted,off] [scenario.json ...]
//
// With no scenarios it runs every scenarios/*.json; see package scenario for
// the file format. Each rule replaces the profile's precedence rule; "off"
// runs without precedence.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
	"trainapp/railway/kpi"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/railway/sim"
	"trainapp/worlds"
)

// bench is a scenario to run, with its free-running times.
type bench struct {
	*scenario.Scenario
	free map[string]kpi.FreeRun
}

// setup builds a fresh world, trains and profile for one run, with the rule
// in place of the profile's ("" keeps it). The trains are as planned, with
// no disruptions.
func (sc *bench) setup(rule string) (*sim.Sim, []*ops.Train, error) {
	w, err := worlds.Load(sc.WorldName())
	if err != nil {
		return nil, nil, err
	}
	trains, err := ops.LoadTrains(sc.SchedulePath())
	if err != nil {
		return nil, nil, err
	}
	s := &sim.Sim{}
	s.SetWorld(w)
	if p := sc.ProfilePath(); p != "" {
		if s.Priorities, err = sim.LoadPriorityConfig(p); err != nil {
			return nil, nil, err
		}
	}
	if err := scenario.ApplyRule(s.Priorities, rule); err != nil {
		return nil, nil, err
	}
	return s, trains, nil
}

// freeRunning works out the scenario's free-running times.
func (sc *bench) freeRunning() error {
	s, trains, err := sc.setup("")
	if err != nil {
		return err
	}
	sc.free, err = kpi.FreeRunning(s.World(), s.Priorities, trains)
	return err
}

// result is one scenario run under one rule.
type result struct {
	Scenario string      `json:"scenario"`
	Rule     string      `json:"rule"`
	WallSec  float64     `json:"wall_sec"`
	Err      string      `json:"error,omitempty"`
	Report   *kpi.Report `json:"report,omitempty"`
}

func (sc *bench) run(rule, logDir string) result {
	r := result{Scenario: sc.Name, Rule: rule}
	start := time.Now()
	rep, err := sc.runReport(rule, filepath.Join(logDir, sc.Name+"-"+rule))
	if err != nil {
		r.Err = err.Error()
	}
	r.Report, r.WallSec = rep, time.Since(start).Seconds()
	return r
}

func (sc *bench) runReport(rule, outputDir string) (*kpi.Report, error) {
	s, trains, err := sc.setup(rule)
	if err != nil {
		return nil, err
	}
	if err := ops.ApplyDisruptions(trains, sc.Disruptions); err != nil {
		return nil, err
	}
	cfg, err := kpi.ConfigOf(s.Priorities)
	if err != nil {
		return nil, err
	}
	s.OutputDir = outputDir
	for _, t := range trains {
		if err := s.AddTrain(t); err != nil {
			return nil, err
		}
	}
	if err := s.Init(); err != nil {
		return nil, err
	}
	coll := kpi.NewCollector()
	s.Observer = coll
	runErr := s.Run()
	return kpi.Compute(s, coll, kpi.Options{Config: cfg, Free: sc.free, Disruptions: sc.Disruptions}), runErr
}

func main() {
	rules := flag.String("rules", "thresholds,weighted,off", "precedence rules to run each scenario under, comma-separated: a hold rule's name, or off")
	outPath := flag.String("out", "logs/bench.json", "also write every run's KPIs to this file (JSON); empty for none")
	logDir := flag.String("logs", "logs/bench", "directory for the runs' logs, one subdirectory per run")
	jobs := flag.Int("j", runtime.NumCPU(), "runs to do at once")
	flag.Parse()

	var scenarios []*bench
	var loaded []*scenario.Scenario
	var err error
	if flag.NArg() == 0 {
		if loaded, err = scenario.LoadDir("scenarios"); err != nil {
			log.Fatal(err)
		}
	}
	for _, p := range flag.Args() {
		sc, err := scenario.Load(p)
		if err != nil {
			log.Fatal(err)
		}
		loaded = append(loaded, sc)
	}
	for _, sc := range loaded {
		scenarios = append(scenarios, &bench{Scenario: sc})
	}
	ruleList := strings.Split(*rules, ",")

	sem := make(chan struct{}, max(*jobs, 1))
	parallel := func(n int, do func(i int)) {
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				do(i)
			})
		}
		wg.Wait()
	}

	freeErrs := make([]error, len(scenarios))
	parallel(len(scenarios), func(i int) {
		if err := scenarios[i].freeRunning(); err != nil {
			freeErrs[i] = fmt.Errorf("%s: free running: %w", scenarios[i].Name, err)
		}
	})
	if err := errors.Join(freeErrs...); err != nil {
		log.Print(err)
	}

	results := make([]result, len(scenarios)*len(ruleList))
	parallel(len(results), func(i int) {
		sc, rule := scenarios[i/len(ruleList)], ruleList[i%len(ruleList)]
		results[i] = sc.run(rule, *logDir)
		fmt.Fprintf(os.Stderr, "%s, %s: done in %.1fs\n", sc.Name, rule, results[i].WallSec)
	})

	if err := printTable(os.Stdout, results); err != nil {
		log.Fatal(err)
	}
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatal(err)
		}
		if err := errors.Join(kpi.WriteJSON(f, results), f.Close()); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\nKPIs written to %s\n", *outPath)
	}
}

// printTable prints one row per run.
func printTable(w *os.File, results []result) error {
	var within []string
	for _, r := range results {
		if r.Report != nil && r.Report.Delay != nil {
			for _, p := range r.Report.Delay.Punctual {
				within = append(within, fmt.Sprintf("%g", p.WithinMin))
			}
			break
		}
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(tw, "scenario\trule\tdone\tdelay\tmean\tweighted\tpunctual <=%s\treactionary\tworst\twait\tlongest wait\tholds\tblock occ\tin section\twall\t\n",
		strings.Join(within, "/"))
	for _, r := range results {
		rep := r.Report
		if rep == nil {
			fmt.Fprintf(tw, "%s\t%s\terror: %s\t\n", r.Scenario, r.Rule, r.Err)
			continue
		}
		row := []string{r.Scenario, r.Rule, fmt.Sprintf("%d/%d", rep.Finished, rep.Trains)}
		if d := rep.Delay; d != nil {
			var shares []string
			for _, p := range d.Punctual {
				shares = append(shares, fmt.Sprintf("%.0f", 100*p.Share))
			}
			worst := "-"
			if d.Worst != "" {
				worst = fmt.Sprintf("%s %.1f", d.Worst, d.WorstMin)
			}
			row = append(row, fmt.Sprintf("%.1f", d.TotalMin), fmt.Sprintf("%.1f", d.MeanMin), fmt.Sprintf("%.1f", d.WeightedMeanMin),
				strings.Join(shares, "/")+"%", reactionary(rep), worst)
		} else {
			row = append(row, "-", "-", "-", "-", "-", "-")
		}
		longest := "-"
		if lw := rep.LongestWait; lw != nil {
			longest = fmt.Sprintf("%s %.1f@%s", lw.Train, lw.Min, lw.At)
		}
		b := rep.Utilization.Blocks
		row = append(row, fmt.Sprintf("%.1f", rep.WaitMin), longest, fmt.Sprint(rep.Holds),
			fmt.Sprintf("%.0f%%", 100*b.MeanOccupancy), fmt.Sprintf("%.1f", b.StandingMin), fmt.Sprintf("%.1fs", r.WallSec))
		if r.Err != "" {
			row = append(row, "error: "+r.Err)
		}
		fmt.Fprintln(tw, strings.Join(row, "\t")+"\t")
	}
	return tw.Flush()
}

func reactionary(rep *kpi.Report) string {
	if rep.Disruption == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f", rep.Disruption.ReactionaryMin)
}
