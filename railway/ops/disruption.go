package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"trainapp/railway/ids"
	"trainapp/units"
)

// Disruptions.
//
// A disruption makes one train run worse than planned, so a run shows how the
// dispatching copes with it:
//
//	{"disruptions": [
//	  {"train": "0452U", "kind": "stand", "stn": "PDKT", "min": 30},
//	  {"train": "0453D", "kind": "late_entry", "min": 20},
//	  {"train": "0454D", "kind": "slow_entry", "kmph": 30}]}
//
// A stand keeps the train at a station for min minutes on top of its dwell,
// as for a failure; at a pass point it stops there. It is not foreseen: the
// dispatcher learns of it only once the train has arrived. A late entry has
// the train enter the world min minutes after its schedule says. A slow entry
// has it come into the world at kmph, if that is slower than it would (see
// Train.EntersAt). Either way the schedule itself is unchanged, so a timed
// train is late against it.

// Disruption kinds.
const (
	DisruptStand     = "stand"
	DisruptLateEntry = "late_entry"
	DisruptSlowEntry = "slow_entry"
)

// Disruption is one train running worse than planned; see above.
type Disruption struct {
	Train string      `json:"train"`
	Kind  string      `json:"kind"`
	Stn   ids.StnCode `json:"stn,omitempty"` // a stand: where
	Min   float64     `json:"min,omitempty"`
	Kmph  float64     `json:"kmph,omitempty"` // a slow entry: how fast
}

type disruptionFile struct {
	Disruptions []Disruption `json:"disruptions"`
}

// LoadDisruptions reads disruptions from a file.
func LoadDisruptions(path string) ([]Disruption, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ds, err := ReadDisruptions(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ds, nil
}

// ReadDisruptions reads disruptions from a file's contents.
func ReadDisruptions(r io.Reader) ([]Disruption, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var file disruptionFile
	if err := dec.Decode(&file); err != nil {
		return nil, err
	}
	return file.Disruptions, nil
}

// ApplyDisruptions applies the disruptions to the trains, before they are
// run. It reports everything wrong it can find, rather than stopping at the
// first.
func ApplyDisruptions(trains []*Train, ds []Disruption) error {
	byNumber := map[string]*Train{}
	for _, t := range trains {
		byNumber[t.Number] = t
	}
	var errs []error
	for i, d := range ds {
		if err := d.apply(byNumber[d.Train]); err != nil {
			errs = append(errs, fmt.Errorf("disruption %d (%s %s): %w", i, d.Kind, d.Train, err))
		}
	}
	return errors.Join(errs...)
}

func (d Disruption) apply(t *Train) error {
	switch {
	case t == nil:
		return errors.New("no such train")
	case d.Kind == DisruptSlowEntry:
		switch {
		case !(d.Kmph > 0):
			return errors.New("kmph must be positive")
		case d.Stn != "" || d.Min != 0:
			return errors.New("a slow entry takes only kmph")
		}
		if t.slowEntry == 0 || units.KMPH(d.Kmph) < t.slowEntry {
			t.slowEntry = units.KMPH(d.Kmph)
		}
		return nil
	case !(d.Min > 0):
		return errors.New("min must be positive")
	case d.Kmph != 0:
		return errors.New("only a slow entry takes kmph")
	}
	switch d.Kind {
	case DisruptStand:
		for _, sp := range t.schedule {
			if sp.StnCode == d.Stn {
				sp.extraStand += d.Min
				return nil
			}
		}
		return fmt.Errorf("the train does not run through %q", d.Stn)
	case DisruptLateEntry:
		if d.Stn != "" {
			return errors.New("a late entry takes no stn")
		}
		t.schedule[0].ArrTime += d.Min
		return nil
	}
	return fmt.Errorf("kind %q, want %s, %s or %s", d.Kind, DisruptStand, DisruptLateEntry, DisruptSlowEntry)
}
