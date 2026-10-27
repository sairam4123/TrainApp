// Package runs runs scenarios with trainapp for the terminal apps (traindemo,
// trainedit) and reads back what a run leaves: its recording, KPIs and, if
// asked for, timetable. It does not link the sim: it runs the trainapp and
// trainviz binaries, so the apps' runs are the command line's.
//
// A run of a scenario under a rule is written to outDir as
// <scenario>-<rule>.ndjson (the recording), .kpi.json and, for an export,
// .timetable.json, so a run from an earlier session, or the other app, is
// read back from there.
package runs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"trainapp/railway/kpi"
	"trainapp/railway/scenario"
	"trainapp/railway/timetable"
	"trainapp/visualization/vizproto"

	tea "charm.land/bubbletea/v2"
)

// ExitInterrupted is the exit status after ctrl+c, as trainviz uses it.
const ExitInterrupted = 130

// Rules are the precedence rules a scenario can be run under; "" is the
// profile's.
var Rules = []string{"", "thresholds", "weighted", scenario.RuleOff}

// RuleLabel names a rule in file names and on screen.
func RuleLabel(rule string) string {
	if rule == "" {
		return "profile"
	}
	return rule
}

// Files are where a run of the scenario under the rule is written: the
// recording and the KPIs.
func Files(outDir string, sc *scenario.Scenario, rule string) (rec, kpi string) {
	name := filepath.Join(outDir, sc.Name+"-"+RuleLabel(rule))
	return name + ".ndjson", name + ".kpi.json"
}

// TimetableFile is where a run exporting its timetable writes it.
func TimetableFile(outDir string, sc *scenario.Scenario, rule string) string {
	return filepath.Join(outDir, sc.Name+"-"+RuleLabel(rule)+".timetable.json")
}

// Call is when a train reached and left a schedule point in a run, and on
// which platform; -1 and "" until it has.
type Call struct {
	Arr, Dep float64
	Pf       string
}

// Result is how a run went.
type Result struct {
	Report *kpi.Report       // nil if trainapp wrote none
	Calls  map[string][]Call // each train's calls in the run, by number; nil if none
	Failed string            // why trainapp or trainviz failed, if one did
	// Stale reports that the run is older than one of the scenario's files:
	// it ran before the last edit.
	Stale bool
	// Timetable is the timetable the run exported, if it did.
	Timetable *timetable.Timetable
	// Baseline, if set, is a baseline's run of the same scenario under the
	// same rule, which SummaryLines gives each figure's change against.
	Baseline *kpi.Report
}

// LoadResult reads a run written earlier, nil if there is none.
func LoadResult(outDir string, sc *scenario.Scenario, rule string) *Result {
	rec, kpiPath := Files(outDir, sc, rule)
	if !IsFile(rec) {
		return nil
	}
	res := &Result{Report: ReadReport(kpiPath), Calls: ReadCalls(rec), Stale: Stale(rec, sc)}
	if tt, err := timetable.Load(TimetableFile(outDir, sc, rule)); err == nil {
		res.Timetable = &tt
	}
	return res
}

// Stale reports whether the file is older than any of the scenario's files:
// the scenario itself, its world snapshot, schedule, timetable and profile.
func Stale(path string, sc *scenario.Scenario) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	for _, p := range ScenarioFiles(sc) {
		if f, err := os.Stat(p); err == nil && f.ModTime().After(fi.ModTime()) {
			return true
		}
	}
	return false
}

// ScenarioFiles are the files a scenario is made of: itself, its world
// snapshot (if it is not a built-in world), schedule, timetable and profile.
func ScenarioFiles(sc *scenario.Scenario) []string {
	out := []string{sc.Path}
	if filepath.Ext(sc.World) == ".json" {
		out = append(out, sc.WorldName())
	}
	for _, p := range []string{sc.SchedulePath(), sc.TimetablePath(), sc.ProfilePath()} {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ReadReport reads a run's KPIs from trainapp's -kpi file, nil if there are
// none.
func ReadReport(path string) *kpi.Report {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var reports map[string]*kpi.Report
	if json.NewDecoder(f).Decode(&reports) != nil {
		return nil
	}
	return reports["run"]
}

// ReadCalls reads each train's calls from a recording, one per schedule
// point, as trainviz's schedule pane does: each point is a TRAIN_ARRIVE, with
// the TRAIN_DEPART (or, at the last, the SCHEDULE_END) after it when it left,
// or a TRAIN_PASS where it ran through, arriving and leaving at once. Nil if
// the recording does not read.
func ReadCalls(path string) map[string][]Call {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	hello, msgs, err := vizproto.ReadAll(f)
	if err != nil {
		return nil
	}
	trackName := map[string]string{}
	for _, t := range hello.World.Tracks {
		trackName[t.ID] = t.Name
	}
	pfNo := map[string]string{} // by track name, as TRAIN_ARRIVE gives it
	for _, stn := range hello.World.Stations {
		for _, pf := range stn.Platforms {
			pfNo[trackName[pf.Track]] = pf.No
		}
	}
	calls := map[string][]Call{}
	for _, m := range msgs {
		e, ok := m.(*vizproto.EventMsg)
		if !ok || e.Train == "" {
			continue
		}
		cs := calls[e.Train]
		switch e.Event {
		case "TRAIN_ARRIVE":
			c := Call{Arr: e.T, Dep: -1}
			for _, f := range e.Fields {
				if f.Key == "track" {
					c.Pf = pfNo[fmt.Sprint(f.Value)]
				}
			}
			calls[e.Train] = append(cs, c)
		case "TRAIN_PASS":
			c := Call{Arr: e.T, Dep: e.T}
			for _, f := range e.Fields {
				if f.Key == "pf" {
					c.Pf = fmt.Sprint(f.Value)
				}
			}
			calls[e.Train] = append(cs, c)
		case "TRAIN_DEPART", "SCHEDULE_END":
			if n := len(cs); n > 0 && cs[n-1].Dep < 0 {
				cs[n-1].Dep = e.T
			}
		}
	}
	return calls
}

// Run is trainapp running a scenario, in the background while an app draws
// its progress.
type Run struct {
	Scenario  *scenario.Scenario
	Rule      string
	Key       string // the app's name for it
	Rec       string // the recording, for trainviz
	KPI       string // the KPIs
	Timetable string // the timetable, if the run exports one
	Started   time.Time

	Progress  Progress
	Measuring bool     // the sim is done and trainapp is working out the KPIs
	Tail      []string // trainapp's last lines of output, to show if it fails
	Cancelled bool

	cmd  *exec.Cmd
	msgs chan tea.Msg
}

// Progress is a progress line from trainapp: schedule points reached of
// all, trains done and waiting, and the sim time.
type Progress struct {
	Reached, Points, Done, Waiting int
	T                              float64
	OK                             bool // a line has come
}

// The messages a Run sends while it runs: its progress, a line trainapp
// printed, and its end.
type (
	ProgressMsg Progress
	OutputMsg   string
	DoneMsg     struct{ Err error }
)

const tailLines = 6

// progressLine matches trainapp's progress bar (cmd/trainapp/progress.go).
var progressLine = regexp.MustCompile(`(\d+)/(\d+) points · (\d+) done · (\d+) waiting · t=([\d.]+) min`)

// ParseProgress reads trainapp's progress bar.
func ParseProgress(line string) (Progress, bool) {
	m := progressLine.FindStringSubmatch(line)
	if m == nil {
		return Progress{}, false
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	t, _ := strconv.ParseFloat(m[5], 64)
	return Progress{Reached: n(m[1]), Points: n(m[2]), Done: n(m[3]), Waiting: n(m[4]), T: t, OK: true}, true
}

// Options are what a run does beyond running.
type Options struct {
	// Timetable exports the run's timetable (trainapp -timetable) to
	// TimetableFile, with its check run recorded beside it.
	Timetable bool
}

// Start starts trainapp on the scenario under the rule, writing the run to
// outDir. Read its messages with Next and hand them to Handle.
func Start(trainapp string, sc *scenario.Scenario, rule, outDir string, opt Options) (*Run, error) {
	r := &Run{Scenario: sc, Rule: rule, msgs: make(chan tea.Msg, 64), Started: time.Now()}
	r.Rec, r.KPI = Files(outDir, sc, rule)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	files := []string{r.Rec, r.KPI}
	args := []string{"-scenario", sc.Path, "-record", r.Rec, "-kpi", r.KPI, "-progress=true"}
	if opt.Timetable {
		r.Timetable = TimetableFile(outDir, sc, rule)
		check := strings.TrimSuffix(r.Timetable, ".json") + ".check.ndjson"
		args = append(args, "-timetable", r.Timetable, "-check-record", check)
		files = append(files, r.Timetable, check)
	}
	for _, p := range files {
		os.Remove(p) // not to show an earlier run's
	}
	if rule != "" {
		args = append(args, "-rule", rule)
	}
	r.cmd = exec.Command(trainapp, args...)
	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := r.cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := r.cmd.Start(); err != nil {
		return nil, err
	}
	var wg sync.WaitGroup
	for _, out := range []io.Reader{stdout, stderr} {
		wg.Go(func() { r.read(out) })
	}
	go func() {
		wg.Wait()
		r.msgs <- DoneMsg{r.cmd.Wait()}
	}()
	return r, nil
}

// read passes on what trainapp writes, a line at a time; the progress bar
// redraws its line after a carriage return.
func (r *Run) read(out io.Reader) {
	sc := bufio.NewScanner(out)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\x1b[K"))
		if p, ok := ParseProgress(line); ok {
			r.msgs <- ProgressMsg(p)
		} else if line != "" {
			r.msgs <- OutputMsg(line)
		}
	}
}

// Next waits for trainapp's next message.
func (r *Run) Next() tea.Msg { return <-r.msgs }

// Handle takes in a message from trainapp.
func (r *Run) Handle(msg tea.Msg) {
	switch msg := msg.(type) {
	case ProgressMsg:
		r.Progress = Progress(msg)
	case OutputMsg:
		if strings.Contains(string(msg), "trains completed their schedule") {
			r.Measuring = true
		}
		r.Tail = append(r.Tail, string(msg))
		r.Tail = r.Tail[max(len(r.Tail)-tailLines, 0):]
	}
}

// Cancel stops trainapp; its DoneMsg still comes.
func (r *Run) Cancel() {
	r.Cancelled = true
	if r.cmd.Process != nil {
		r.cmd.Process.Signal(syscall.SIGINT)
	}
}

// Discard removes what a cancelled run wrote, not to replay half a run.
func (r *Run) Discard() {
	for _, f := range []string{r.Rec, r.KPI, r.Timetable} {
		if f != "" {
			os.Remove(f)
		}
	}
}

// Finish turns how trainapp ended into a result.
func (r *Run) Finish(err error) *Result {
	res := &Result{Report: ReadReport(r.KPI), Calls: ReadCalls(r.Rec)}
	if r.Timetable != "" {
		if tt, err := timetable.Load(r.Timetable); err == nil {
			res.Timetable = &tt
		}
	}
	if err != nil {
		res.Failed = "trainapp: " + err.Error()
		if len(r.Tail) > 0 {
			res.Failed += ": " + r.Tail[len(r.Tail)-1]
		}
	}
	return res
}

// Replay opens the recording in trainviz, handing it the terminal; done
// makes the message for when it quits. args go to trainviz replay before
// the recording, e.g. "-page", "delay".
func Replay(trainviz, rec string, done func(error) tea.Msg, args ...string) tea.Cmd {
	return tea.ExecProcess(exec.Command(trainviz, append(append([]string{"replay"}, args...), rec)...), done)
}

// Interrupted reports whether a command was quit with ctrl+c.
func Interrupted(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == ExitInterrupted
}

// FindBin finds one of the commands: in dir if set, else next to this one,
// in dist/ or on $PATH. hint says how to build it. On Windows it looks for
// name.exe.
func FindBin(name, dir, hint string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if dir != "" {
		p := filepath.Join(dir, name)
		if !IsFile(p) {
			return "", fmt.Errorf("no %s in %s", name, dir)
		}
		return p, nil
	}
	var tried []string
	if exe, err := os.Executable(); err == nil {
		tried = append(tried, filepath.Dir(exe))
	}
	tried = append(tried, "dist")
	for _, d := range tried {
		if p := filepath.Join(d, name); IsFile(p) {
			return filepath.Abs(p)
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("no %s next to this command, in dist/ or on $PATH: %s", name, hint)
}

// IsFile reports whether p is a regular file.
func IsFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
