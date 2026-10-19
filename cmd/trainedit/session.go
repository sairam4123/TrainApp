package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/scenario"
	"trainapp/railway/sim"
	"trainapp/railway/timetable"
	"trainapp/worlds"
	"trainapp/worlds/edit"
)

// session is the scenario being edited: the scenario file, its world as a
// snapshot, its schedule as specs, and whether each has changed since it
// was opened or saved.
type session struct {
	sc    *scenario.Scenario
	isNew bool // not saved yet: its path follows its name
	dir   string
	// moveTo is the file a saved scenario moves to on the next save, after
	// a rename; empty if it stays
	moveTo string

	world   *edit.Doc
	builtin bool // the world is a built-in one; saving it makes a snapshot

	sched      ops.ScheduleFile
	schedDirty bool
	scDirty    bool

	profile    *sim.PriorityConfig // nil without one, or if it does not load
	profileErr error
	tt         *timetable.Timetable // the scenario's timetable, if it has one
	ttErr      error
	openErr    error // why the scenario file did not read as it is, if it did not
}

// openSession opens a scenario file to edit. One that does not read as a
// scenario is read as far as it goes, so it can be put right; openErr says
// what was wrong.
func openSession(path string) (*session, error) {
	sc, err := scenario.Load(path)
	var openErr error
	if err != nil {
		openErr = err
		sc, err = loadLenient(path)
		if err != nil {
			return nil, err
		}
	}
	s := &session{sc: sc, dir: filepath.Dir(path), openErr: openErr}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// loadLenient reads a scenario file ignoring what it does not know and what
// it lacks.
func loadLenient(path string) (*scenario.Scenario, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc := &scenario.Scenario{Path: path}
	if err := json.Unmarshal(b, sc); err != nil {
		sc = &scenario.Scenario{Path: path} // not even JSON: start it again
	}
	if sc.Name == "" {
		sc.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return sc, nil
}

// newSession starts a scenario in dir, from another one's world, schedule
// and profile if from is given, else an empty world.
func newSession(dir, from string) (*session, error) {
	s := &session{dir: dir, isNew: true}
	sc := &scenario.Scenario{}
	if from != "" {
		base, err := scenario.Load(from)
		if err != nil {
			return nil, err
		}
		sc.Name = base.Name + "-copy"
		sc.Description = base.Description
		sc.Path = filepath.Join(dir, "x.json") // for Ref, till it is named
		sc.World = base.World
		if filepath.Ext(base.World) == ".json" {
			sc.World = sc.Ref(base.WorldName())
		}
		sc.Schedule = sc.Ref(base.SchedulePath())
		sc.Timetable = sc.Ref(base.TimetablePath())
		sc.Profile = sc.Ref(base.ProfilePath())
		sc.Disruptions = slices.Clone(base.Disruptions)
	} else {
		sc.Name = "new"
		sc.Path = filepath.Join(dir, "x.json")
		if p := filepath.Join(dir, "..", "profiles", "ir.json"); fileExists(p) {
			sc.Profile = sc.Ref(p)
		}
	}
	sc.Name = uniqueName(dir, sc.Name)
	sc.Path = filepath.Join(dir, sc.Name+".json")
	s.sc = sc
	if from == "" {
		s.world = edit.NewDoc(edit.Empty(edit.DefaultDefaults()))
		s.loadProfile()
		s.scDirty = true
		return s, nil
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	s.scDirty = true
	return s, nil
}

// uniqueName is name, or name-2, name-3, ... if a scenario file is taken.
func uniqueName(dir, name string) string {
	out := name
	for n := 2; fileExists(filepath.Join(dir, out+".json")); n++ {
		out = name + "-" + strconv.Itoa(n)
	}
	return out
}

// load reads the scenario's world, schedule, profile and timetable.
func (s *session) load() error {
	sc := s.sc
	snap := edit.Empty(edit.DefaultDefaults())
	switch {
	case sc.World == "":
	case filepath.Ext(sc.World) == ".json":
		var err error
		if snap, err = readSnapshot(sc.WorldName()); err != nil {
			return err
		}
	default:
		w, err := worlds.Load(sc.World)
		if err != nil {
			return err
		}
		if snap, err = w.Snapshot(); err != nil {
			return err
		}
		s.builtin = true
	}
	s.world = edit.NewDoc(snap)
	s.sched = ops.ScheduleFile{}
	if sc.Schedule != "" {
		f, err := ops.LoadSchedule(sc.SchedulePath())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		s.sched = f
	}
	s.loadProfile()
	s.loadTimetable()
	return nil
}

func (s *session) loadProfile() {
	s.profile, s.profileErr = nil, nil
	if p := s.sc.ProfilePath(); p != "" {
		s.profile, s.profileErr = sim.LoadPriorityConfig(p)
	}
}

func (s *session) loadTimetable() {
	s.tt, s.ttErr = nil, nil
	if s.sc.Timetable != "" {
		tt, err := timetable.Load(s.sc.TimetablePath())
		s.tt, s.ttErr = &tt, err
		if err != nil {
			s.tt = nil
		}
	}
}

// readSnapshot reads a world snapshot as it is, built or not.
func readSnapshot(path string) (*infra.Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var snap infra.Snapshot
	if err := dec.Decode(&snap); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &snap, nil
}

// dirty reports whether anything is unsaved.
func (s *session) dirty() bool {
	return s.world.Dirty || s.schedDirty || s.scDirty
}

// rename names the scenario, and its file follows: a new one's at once, a
// saved one's when it is next saved (save).
func (s *session) rename(name string) error {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name %q: a scenario needs a name that can be a file name", name)
	}
	path := filepath.Join(s.dir, name+".json")
	if !samePath(path, s.sc.Path) && fileExists(path) {
		return fmt.Errorf("there is a scenario %s already", name)
	}
	switch {
	case s.isNew:
		s.sc.Path = path
	case samePath(path, s.sc.Path):
		s.moveTo = ""
	default:
		s.moveTo = path
	}
	s.sc.Name = name
	s.scDirty = true
	return nil
}

// move moves a saved scenario's file to moveTo, with the world, schedule
// and timetable files made for it (named after its file, in worlds/,
// schedules/ and timetables/ beside it) that no other scenario uses. The
// scenario is saved at its new path, and its old file goes.
func (s *session) move() error {
	sc, old := s.sc, s.sc.Path
	if fileExists(s.moveTo) {
		return fmt.Errorf("there is a scenario %s already", filepath.Base(s.moveTo))
	}
	base := strings.TrimSuffix(filepath.Base(old), ".json")
	own := func(path, sub string) bool {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		n, numbered := strings.CutPrefix(name, base+"-")
		_, err := strconv.Atoi(n)
		return path != "" && fileExists(path) && samePath(filepath.Dir(path), filepath.Join(s.dir, sub)) &&
			(name == base || numbered && err == nil) && len(s.sharers(path)) == 0
	}
	var moves [][2]string // from, to
	var refs []*string
	for _, f := range []struct {
		ref  *string
		path string
		sub  string
	}{
		{&sc.World, sc.WorldName(), "worlds"},
		{&sc.Schedule, sc.SchedulePath(), "schedules"},
		{&sc.Timetable, sc.TimetablePath(), "timetables"},
	} {
		if *f.ref != "" && filepath.Ext(*f.ref) == ".json" && own(f.path, f.sub) {
			moves = append(moves, [2]string{f.path, freePath(filepath.Join(s.dir, f.sub), sc.Name)})
			refs = append(refs, f.ref)
		}
	}
	for k, m := range moves {
		if err := os.Rename(m[0], m[1]); err != nil {
			return err
		}
		*refs[k] = sc.Ref(m[1])
	}
	sc.Path = s.moveTo
	if err := sc.Save(); err != nil {
		return err
	}
	s.moveTo = ""
	return os.Remove(old)
}

// sharers are the other scenarios in the scenario's directory that use the
// file at path (a world snapshot or schedule), by name.
func (s *session) sharers(path string) []string {
	scs, err := scenario.ListDir(s.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, o := range scs {
		if o.Err != nil || samePath(o.Path, s.sc.Path) {
			continue
		}
		for _, p := range []string{o.WorldName(), o.SchedulePath()} {
			if p != "" && samePath(p, path) {
				out = append(out, o.Name)
				break
			}
		}
	}
	return out
}

// shared is a file to save that other scenarios use, and who they are.
type shared struct {
	what  string // "world" or "schedule"
	path  string
	users []string
}

// sharedSaves lists the files a save would write that other scenarios use
// too, so the user can say whether the change is for them all.
func (s *session) sharedSaves() []shared {
	var out []shared
	if s.world.Dirty && !s.builtin && s.sc.World != "" {
		if users := s.sharers(s.sc.WorldName()); len(users) > 0 {
			out = append(out, shared{"world", s.sc.WorldName(), users})
		}
	}
	if s.schedDirty && s.sc.Schedule != "" {
		if users := s.sharers(s.sc.SchedulePath()); len(users) > 0 {
			out = append(out, shared{"schedule", s.sc.SchedulePath(), users})
		}
	}
	return out
}

// save writes what changed: the world (as a new snapshot beside the
// scenario if it was built in, or if copy says so), the schedule (likewise)
// and the scenario file. copies names the files ("world", "schedule") to
// save as this scenario's own copy rather than over the shared one.
func (s *session) save(copies map[string]bool) error {
	sc := s.sc
	if s.world.Dirty || sc.World == "" {
		path := sc.WorldName()
		if s.builtin || sc.World == "" || copies["world"] {
			path = freePath(filepath.Join(s.dir, "worlds"), sc.Name)
		}
		if err := writeJSON(path, s.world.S); err != nil {
			return err
		}
		sc.World, s.builtin = sc.Ref(path), false
		s.world.MarkSaved()
	}
	if s.schedDirty || (sc.Schedule == "" && sc.Timetable == "") {
		path := sc.SchedulePath()
		if sc.Schedule == "" || copies["schedule"] {
			path = freePath(filepath.Join(s.dir, "schedules"), sc.Name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := ops.WriteSchedule(&buf, s.sched); err != nil {
			return err
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			return err
		}
		sc.Schedule = sc.Ref(path)
		s.schedDirty = false
	}
	if err := os.MkdirAll(filepath.Dir(sc.Path), 0o755); err != nil {
		return err
	}
	if s.moveTo != "" {
		if err := s.move(); err != nil {
			return err
		}
	} else if err := sc.Save(); err != nil {
		return err
	}
	s.scDirty, s.isNew, s.openErr = false, false, nil
	return nil
}

// freePath is dir/name.json, or dir/name-2.json, ... if it is taken.
func freePath(dir, name string) string {
	p := filepath.Join(dir, name+".json")
	for n := 2; fileExists(p); n++ {
		p = filepath.Join(dir, name+"-"+strconv.Itoa(n)+".json")
	}
	return p
}

// scratch writes the scenario as it stands, unsaved changes and all, under
// dir for a run, and returns the scratch scenario. Its files name the
// scenario's own where they have not changed.
func (s *session) scratch(dir string) (*scenario.Scenario, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	sc := *s.sc
	sc.Path = filepath.Join(dir, s.sc.Name+".json")
	abs := func(p string) string {
		if p == "" {
			return ""
		}
		a, err := filepath.Abs(p)
		if err != nil {
			return p
		}
		return a
	}
	if s.world.Dirty || s.sc.World == "" {
		p := filepath.Join(dir, s.sc.Name+".world.json")
		if err := writeJSON(p, s.world.S); err != nil {
			return nil, err
		}
		sc.World = abs(p)
	} else if filepath.Ext(s.sc.World) == ".json" {
		sc.World = abs(s.sc.WorldName())
	}
	if s.schedDirty || (s.sc.Schedule == "" && s.sc.Timetable == "") {
		p := filepath.Join(dir, s.sc.Name+".schedule.json")
		var buf bytes.Buffer
		if err := ops.WriteSchedule(&buf, s.sched); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			return nil, err
		}
		sc.Schedule = abs(p)
	} else {
		sc.Schedule = abs(s.sc.SchedulePath())
	}
	sc.Timetable, sc.Profile = abs(s.sc.TimetablePath()), abs(s.sc.ProfilePath())
	if err := sc.Save(); err != nil {
		return nil, err
	}
	return &sc, nil
}

// useTimetable copies a timetable file into the scenario's timetables/ and
// makes the scenario run it.
func (s *session) useTimetable(from string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, "timetables", s.sc.Name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	s.sc.Timetable = s.sc.Ref(path)
	s.scDirty = true
	s.loadTimetable()
	return s.ttErr
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func samePath(a, b string) bool {
	x, errX := filepath.Abs(a)
	y, errY := filepath.Abs(b)
	return errX == nil && errY == nil && x == y
}

// stationCodes are the world's stations, by code.
func (s *session) stationCodes() []string {
	var out []string
	for _, st := range s.world.S.Stations {
		out = append(out, st.Code)
	}
	return out
}

// platforms are a station's platform numbers.
func (s *session) platforms(code string) []string {
	for _, st := range s.world.S.Stations {
		if st.Code == code {
			var out []string
			for _, p := range st.Platforms {
				out = append(out, p.No)
			}
			return out
		}
	}
	return nil
}

// classes are the profile's train classes, highest priority first.
func (s *session) classes() []string {
	if s.profile == nil {
		return nil
	}
	var out []string
	for c := range s.profile.Classes {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b string) int {
		if d := s.profile.Classes[b] - s.profile.Classes[a]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return out
}

func newDoc(s *infra.Snapshot) *edit.Doc { return edit.NewDoc(s) }
