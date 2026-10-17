// Package scenario reads and writes scenario files. A scenario names a
// world, a schedule file, a profile and any disruptions:
//
//	{"name": "sl-12-disrupt", "description": "...", "world": "tpjkkdi-sl",
//	 "schedule": "schedules/sl-12.json", "profile": "../profiles/ir.json",
//	 "disruptions": [{"train": "0452U", "kind": "stand", "stn": "PDKT", "min": 30}]}
//
// world is tpjkkdi, tpjkkdi-sl or a world snapshot; file paths are relative
// to the scenario file.
//
// A scenario may also name a timetable, as trainapp -timetable writes one
// from a run of it. It then runs the timetable's timed trains in place of the
// schedule (see Trains), and the schedule, the draft the timetable was made
// from, may be left out.
package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/railway/timetable"
)

// RuleOff runs without precedence.
const RuleOff = "off"

// Scenario is a scenario file.
type Scenario struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	World       string           `json:"world"`
	Schedule    string           `json:"schedule,omitempty"`
	Timetable   string           `json:"timetable,omitempty"`
	Profile     string           `json:"profile"`
	Disruptions []ops.Disruption `json:"disruptions,omitempty"`

	// Path is the scenario file.
	Path string `json:"-"`
	// Err is why the file did not read, for a scenario ListDir could not
	// load; such a scenario has only its Path and Name.
	Err error `json:"-"`
}

// Load reads a scenario file. A scenario with no name is named after its
// file.
func Load(path string) (*Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	sc := &Scenario{Path: path}
	if err := dec.Decode(sc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if sc.Name == "" {
		sc.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if sc.World == "" || (sc.Schedule == "" && sc.Timetable == "") {
		return nil, fmt.Errorf("%s: a scenario needs a world and a schedule or a timetable", path)
	}
	return sc, nil
}

// Save writes the scenario to its Path.
func (sc *Scenario) Save() error {
	b, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sc.Path, append(b, '\n'), 0o644)
}

// ListDir reads every *.json scenario in dir, in file name order, as
// LoadDir does, but keeps going past one that does not read: it comes back
// with its Path, a Name from its file name and the Err.
func ListDir(dir string) ([]*Scenario, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []*Scenario
	for _, p := range paths {
		sc, err := Load(p)
		if err != nil {
			sc = &Scenario{Path: p, Name: strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)), Err: err}
		}
		out = append(out, sc)
	}
	return out, nil
}

// LoadDir reads every *.json scenario in dir, in file name order.
func LoadDir(dir string) ([]*Scenario, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no scenarios in %s", dir)
	}
	var out []*Scenario
	for _, p := range paths {
		sc, err := Load(p)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, nil
}

// Rel resolves a path in the scenario file against its directory.
func (sc *Scenario) Rel(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(filepath.Dir(sc.Path), p)
}

// WorldName is the world to give worlds.Load: a built-in world's name, or
// the snapshot's path resolved against the scenario file.
func (sc *Scenario) WorldName() string {
	if filepath.Ext(sc.World) == ".json" {
		return sc.Rel(sc.World)
	}
	return sc.World
}

// Ref is a path as the scenario file names it: relative to its directory
// if it can be.
func (sc *Scenario) Ref(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.Rel(filepath.Dir(sc.Path), p); err == nil {
		return r
	}
	return p
}

// SchedulePath is the schedule file, resolved against the scenario file.
func (sc *Scenario) SchedulePath() string { return sc.Rel(sc.Schedule) }

// TimetablePath is the timetable file, resolved against the scenario file;
// empty for none.
func (sc *Scenario) TimetablePath() string { return sc.Rel(sc.Timetable) }

// Trains are the trains the scenario runs, as planned (before its
// disruptions): its timetable's timed trains if it has one, else its
// schedule's.
func (sc *Scenario) Trains() ([]*ops.Train, error) {
	if sc.Timetable != "" {
		tt, err := timetable.Load(sc.TimetablePath())
		if err != nil {
			return nil, err
		}
		return tt.TimedTrains(), nil
	}
	return ops.LoadTrains(sc.SchedulePath())
}

// ProfilePath is the profile, resolved against the scenario file; empty for
// none.
func (sc *Scenario) ProfilePath() string { return sc.Rel(sc.Profile) }

// ApplyRule replaces the profile's precedence rule with rule: a hold rule's
// name, or RuleOff to run without precedence. An empty rule keeps the
// profile's.
func ApplyRule(cfg *sim.PriorityConfig, rule string) error {
	switch rule {
	case "":
	case RuleOff:
		if cfg != nil {
			cfg.Precedence = nil
		}
	default:
		if cfg == nil || cfg.Precedence == nil {
			return fmt.Errorf("rule %s: the profile has no precedence section", rule)
		}
		cfg.Precedence.Rule = rule
	}
	return nil
}
