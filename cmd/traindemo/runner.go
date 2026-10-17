package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"trainapp/railway/scenario"

	tea "charm.land/bubbletea/v2"
)

// run is trainapp running a scenario, in the background while the picker
// draws its progress.
type run struct {
	sc      *scenario.Scenario
	rule    string
	key     string // the picker's resultKey for it
	rec     string // the recording, for trainviz
	kpi     string // the KPIs, for the picker
	cmd     *exec.Cmd
	msgs    chan tea.Msg
	started time.Time

	progress  runProgress
	measuring bool     // the sim is done and trainapp is working out the KPIs
	tail      []string // trainapp's last lines of output, to show if it fails
	cancelled bool
}

// runProgress is a progress line from trainapp: schedule points reached of
// all, trains done and waiting, and the sim time.
type runProgress struct {
	reached, points, done, waiting int
	t                              float64
	ok                             bool // a line has come
}

type (
	progressMsg runProgress
	outputMsg   string // a line trainapp printed
	runDoneMsg  struct{ err error }
	vizDoneMsg  struct{ err error }
	tickMsg     time.Time
)

const tailLines = 6

// progressLine matches trainapp's progress bar (cmd/trainapp/progress.go).
var progressLine = regexp.MustCompile(`(\d+)/(\d+) points · (\d+) done · (\d+) waiting · t=([\d.]+) min`)

func parseProgress(line string) (runProgress, bool) {
	m := progressLine.FindStringSubmatch(line)
	if m == nil {
		return runProgress{}, false
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	t, _ := strconv.ParseFloat(m[5], 64)
	return runProgress{reached: n(m[1]), points: n(m[2]), done: n(m[3]), waiting: n(m[4]), t: t, ok: true}, true
}

// runFiles are where a run of the scenario under the rule is written: the
// recording and the KPIs.
func runFiles(outDir string, sc *scenario.Scenario, rule string) (rec, kpi string) {
	name := filepath.Join(outDir, sc.Name+"-"+ruleLabel(rule))
	return name + ".ndjson", name + ".kpi.json"
}

// loadResult reads a run written earlier, nil if there is none.
func loadResult(outDir string, sc *scenario.Scenario, rule string) *result {
	rec, kpi := runFiles(outDir, sc, rule)
	if !isFile(rec) {
		return nil
	}
	return &result{report: readReport(kpi), calls: readCalls(rec)}
}

// startRun starts trainapp on the scenario under the rule, writing the run
// to outDir.
func startRun(trainapp string, sc *scenario.Scenario, rule, outDir string) (*run, error) {
	r := &run{sc: sc, rule: rule, msgs: make(chan tea.Msg, 64), started: time.Now()}
	r.rec, r.kpi = runFiles(outDir, sc, rule)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	for _, p := range []string{r.rec, r.kpi} {
		os.Remove(p) // not to show an earlier run's
	}
	args := []string{"-scenario", sc.Path, "-record", r.rec, "-kpi", r.kpi, "-progress=true"}
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
		r.msgs <- runDoneMsg{r.cmd.Wait()}
	}()
	return r, nil
}

// read passes on what trainapp writes, a line at a time; the progress bar
// redraws its line after a carriage return.
func (r *run) read(out io.Reader) {
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
		if p, ok := parseProgress(line); ok {
			r.msgs <- progressMsg(p)
		} else if line != "" {
			r.msgs <- outputMsg(line)
		}
	}
}

// next waits for trainapp's next message.
func (r *run) next() tea.Msg { return <-r.msgs }

// handle takes in a message from trainapp.
func (r *run) handle(msg tea.Msg) {
	switch msg := msg.(type) {
	case progressMsg:
		r.progress = runProgress(msg)
	case outputMsg:
		if strings.Contains(string(msg), "trains completed their schedule") {
			r.measuring = true
		}
		r.tail = append(r.tail, string(msg))
		r.tail = r.tail[max(len(r.tail)-tailLines, 0):]
	}
}

// cancel stops trainapp; its runDoneMsg still comes.
func (r *run) cancel() {
	r.cancelled = true
	if r.cmd.Process != nil {
		r.cmd.Process.Signal(syscall.SIGINT)
	}
}

// finish turns how trainapp ended into a result.
func (r *run) finish(err error) *result {
	res := &result{report: readReport(r.kpi), calls: readCalls(r.rec)}
	if err != nil {
		res.failed = "trainapp: " + err.Error()
		if len(r.tail) > 0 {
			res.failed += ": " + r.tail[len(r.tail)-1]
		}
	}
	return res
}

// replay opens the recording in trainviz, handing it the terminal.
func replay(trainviz, rec string) tea.Cmd {
	return tea.ExecProcess(exec.Command(trainviz, "replay", rec), func(err error) tea.Msg { return vizDoneMsg{err} })
}

// interrupted reports whether a command was quit with ctrl+c.
func interrupted(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == exitInterrupted
}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
