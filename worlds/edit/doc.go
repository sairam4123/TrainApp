// Package edit changes a world snapshot one edit at a time, for trainedit.
//
// The model is an *infra.Snapshot, not an *infra.World: a World only grows,
// and Snapshot.Build ends in Finalize, which a half-drawn world fails (a
// track end that leads nowhere, say). A snapshot is plain data, so an edit is
// a function on it, and undo keeps copies.
//
// Checks come in two tiers. An edit is refused, leaving the snapshot as it
// was, if the world could no longer be drawn (Check): two points with one id,
// a track that does not run west to east, lanes that contradict each other.
// Anything else wrong, everything Snapshot.Build reports and a few things it
// does not (open track ends, say), is a Problem: a world with problems can be
// saved, but not run.
package edit

import (
	"encoding/json"
	"errors"
	"fmt"
	"trainapp/railway/ids"
	"trainapp/railway/infra"
	"trainapp/visualization/schematic"
)

// Doc is a snapshot being edited, with its undo and redo history.
type Doc struct {
	S          *infra.Snapshot
	undo, redo []*infra.Snapshot
	// Dirty reports whether the snapshot changed since it was opened or
	// MarkSaved last called.
	Dirty bool
}

// NewDoc starts editing a copy of s, its point and track ids filled in.
func NewDoc(s *infra.Snapshot) *Doc {
	c := Clone(s)
	normalize(c)
	return &Doc{S: c}
}

// Empty is a new world with nothing in it but the given defaults.
func Empty(defaults infra.SnapshotDefaults) *infra.Snapshot {
	return &infra.Snapshot{
		Version: infra.SnapshotVersion, Defaults: defaults,
		Points: []infra.SnapshotPoint{}, Tracks: []infra.SnapshotTrack{}, Switches: []infra.SnapshotSwitch{},
		Signals: []infra.SnapshotSignal{}, Stations: []infra.SnapshotStation{},
	}
}

// DefaultDefaults are the defaults of a new world: the single-line worlds'.
func DefaultDefaults() infra.SnapshotDefaults {
	return infra.SnapshotDefaults{SwitchKmph: 30, SwitchLengthM: 100, PlatformKmph: 50, PlatformLengthM: 800, RouteKmph: 110}
}

// Do applies an edit to a copy of the snapshot and keeps it if the edit
// succeeds and the world can still be drawn; otherwise the snapshot is left
// as it was and the error says why.
func (d *Doc) Do(edit func(*infra.Snapshot) error) error {
	next := Clone(d.S)
	if err := edit(next); err != nil {
		return err
	}
	normalize(next)
	if err := Check(next); err != nil {
		return err
	}
	d.undo = append(d.undo, d.S)
	d.redo = nil
	d.S = next
	d.Dirty = true
	return nil
}

// Undo goes back an edit, reporting whether there was one.
func (d *Doc) Undo() bool {
	if len(d.undo) == 0 {
		return false
	}
	d.redo = append(d.redo, d.S)
	d.S = d.undo[len(d.undo)-1]
	d.undo = d.undo[:len(d.undo)-1]
	d.Dirty = true
	return true
}

// Redo does again an edit Undo took back.
func (d *Doc) Redo() bool {
	if len(d.redo) == 0 {
		return false
	}
	d.undo = append(d.undo, d.S)
	d.S = d.redo[len(d.redo)-1]
	d.redo = d.redo[:len(d.redo)-1]
	d.Dirty = true
	return true
}

// MarkSaved notes the snapshot as saved.
func (d *Doc) MarkSaved() { d.Dirty = false }

// Clone is a deep copy of a snapshot.
func Clone(s *infra.Snapshot) *infra.Snapshot {
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("edit: a snapshot does not marshal: %v", err))
	}
	var c infra.Snapshot
	if err := json.Unmarshal(b, &c); err != nil {
		panic(fmt.Sprintf("edit: a snapshot does not unmarshal: %v", err))
	}
	return &c
}

// Check reports what keeps the world from being drawn, nil if nothing does:
// a point with no id or two with one, a track whose ends are missing or the
// same, two tracks between the same points, and whatever the schematic
// layout refuses (tracks that do not all run west to east, lanes that do not
// add up).
func Check(s *infra.Snapshot) error {
	var errs []error
	points := map[string]bool{}
	for i, p := range s.Points {
		id := pointID(p)
		switch {
		case id.IsZero():
			errs = append(errs, fmt.Errorf("point %d has no id", i))
		case points[id.String()]:
			errs = append(errs, fmt.Errorf("two points are %s", id))
		}
		points[id.String()] = true
	}
	tracks := map[string]bool{}
	for _, t := range s.Tracks {
		switch {
		case !points[t.From] || !points[t.To]:
			errs = append(errs, fmt.Errorf("track %s-%s: an end is missing", t.From, t.To))
		case t.From == t.To:
			errs = append(errs, fmt.Errorf("track at %s runs from the point to itself", t.From))
		case tracks[t.ID]:
			errs = append(errs, fmt.Errorf("two tracks join %s and %s", t.From, t.To))
		}
		tracks[t.ID] = true
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if _, err := schematic.FromSnapshot(s); err != nil {
		return err
	}
	return nil
}

// normalize fills in every point's id from its parts and every track's from
// its points, as Snapshot.Build works them out.
func normalize(s *infra.Snapshot) {
	byID := map[string]ids.PointID{}
	for i := range s.Points {
		id := pointID(s.Points[i])
		s.Points[i].ID = id.String()
		byID[id.String()] = id
	}
	for i := range s.Tracks {
		t := &s.Tracks[i]
		from, okF := byID[t.From]
		to, okT := byID[t.To]
		if okF && okT {
			t.ID = ids.NewTrackID(from, to).String()
		}
	}
}

func pointID(p infra.SnapshotPoint) ids.PointID {
	return ids.PointID{StnCode: ids.StnCode(p.Stn), Type: p.Type, Seq: p.Seq, Other: p.Other}
}
