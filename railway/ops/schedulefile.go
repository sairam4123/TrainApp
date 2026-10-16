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

// Schedule files.
//
// A schedule file lists the trains to run as JSON:
//
//	{"trains": [{"number": "0451U", "name": "Train1Up", "class": "Passenger",
//	  "max_speed_kmph": 110, "points": [
//	    {"stn": "TPJ", "kind": "stop", "dwell": 5, "pf": "1", "arr": 1},
//	    {"stn": "KRMG", "kind": "pass"},
//	    {"stn": "PDKT", "kind": "stop", "arr": 40, "dep": 42, "min_dwell": 1}]}]}
//
// A stop gives either a dwell, and the train leaves once it has stood that
// long, or an arr and a dep to keep to, with an optional min_dwell it stands
// even when late. A pass may give an arr, the time it is due through;
// without one it is untimed. pf is the preferred platform; left out, the
// dispatcher picks. The first point's arr is when the train enters the world
// and is required; an untimed stop takes no arr anywhere else. Times are in
// minutes.

type scheduleFile struct {
	Trains []trainSpec `json:"trains"`
}

type trainSpec struct {
	Number       string      `json:"number"`
	Name         string      `json:"name,omitempty"`
	Class        string      `json:"class,omitempty"`
	Priority     int         `json:"priority,omitempty"`
	MaxSpeedKmph float64     `json:"max_speed_kmph"`
	Points       []pointSpec `json:"points"`
}

type pointSpec struct {
	Stn      string            `json:"stn"`
	Kind     SchedulePointKind `json:"kind"` // SpStop or SpPass
	Pf       string            `json:"pf,omitempty"`
	Arr      *float64          `json:"arr,omitempty"`
	Dep      *float64          `json:"dep,omitempty"`
	Dwell    *float64          `json:"dwell,omitempty"`
	MinDwell *float64          `json:"min_dwell,omitempty"`
}

// LoadTrains reads the trains from a schedule file.
func LoadTrains(path string) ([]*Train, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	trains, err := ReadTrains(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return trains, nil
}

// ReadTrains reads the trains from a schedule file's contents. It reports
// everything wrong it can find, rather than stopping at the first.
func ReadTrains(r io.Reader) ([]*Train, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var file scheduleFile
	if err := dec.Decode(&file); err != nil {
		return nil, err
	}
	var trains []*Train
	var errs []error
	for i, spec := range file.Trains {
		t, err := spec.train()
		if err != nil {
			errs = append(errs, fmt.Errorf("train %d (%s): %w", i, spec.Number, err))
			continue
		}
		trains = append(trains, t)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return trains, nil
}

func (spec trainSpec) train() (*Train, error) {
	t := &Train{
		Number: spec.Number, Name: spec.Name, Class: spec.Class, Priority: spec.Priority,
		MaxSpeed: units.KMPH(spec.MaxSpeedKmph),
	}
	var errs []error
	if len(spec.Points) == 0 {
		errs = append(errs, errors.New("no points"))
	} else if spec.Points[0].Arr == nil {
		errs = append(errs, errors.New("the first point needs an arr, when the train enters"))
	}
	for k, p := range spec.Points {
		sp, err := p.point(k == 0)
		if err != nil {
			errs = append(errs, fmt.Errorf("point %d (%s): %w", k, p.Stn, err))
			continue
		}
		t.AddSchedule(sp)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return t, t.Validate()
}

func (p pointSpec) point(first bool) (*SchedulePoint, error) {
	var errs []error
	for _, v := range []struct {
		name string
		v    *float64
	}{{"arr", p.Arr}, {"dep", p.Dep}, {"dwell", p.Dwell}, {"min_dwell", p.MinDwell}} {
		if v.v != nil && !(*v.v >= 0) {
			errs = append(errs, fmt.Errorf("%s must not be negative", v.name))
		}
	}
	if p.Stn == "" {
		errs = append(errs, errors.New("no stn"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	stn := ids.StnCode(p.Stn)

	switch p.Kind {
	case SpPass:
		if p.Dep != nil || p.Dwell != nil || p.MinDwell != nil {
			return nil, errors.New("a pass takes only an arr")
		}
		return NewPassPoint(stn, value(p.Arr, -1), p.Pf), nil
	case SpStop:
		if p.Dep != nil {
			switch {
			case p.Arr == nil:
				return nil, errors.New("a dep needs an arr")
			case *p.Dep < *p.Arr:
				return nil, errors.New("dep is before arr")
			case p.Dwell != nil:
				return nil, errors.New("a stop with arr and dep takes a min_dwell, not a dwell")
			}
			sp := NewArrDepPoint(stn, *p.Arr, *p.Dep, p.Pf)
			if p.MinDwell != nil {
				sp.WithMinDwell(*p.MinDwell)
			}
			return sp, nil
		}
		switch {
		case p.Dwell == nil:
			return nil, errors.New("a stop needs a dwell, or an arr and a dep")
		case p.MinDwell != nil:
			return nil, errors.New("min_dwell needs an arr and a dep")
		case p.Arr != nil && !first:
			return nil, errors.New("an arr without a dep is only for the first point, the entry")
		}
		sp := NewStopPoint(stn, *p.Dwell, p.Pf)
		if p.Arr != nil {
			sp.SetEntryTime(*p.Arr)
		}
		return sp, nil
	}
	return nil, fmt.Errorf("kind %q, want %s or %s", p.Kind, SpStop, SpPass)
}

// WriteTrains writes the trains' schedules, as planned, as a schedule file.
func WriteTrains(w io.Writer, trains []*Train) error {
	file := scheduleFile{Trains: []trainSpec{}}
	for _, t := range trains {
		spec := trainSpec{
			Number: t.Number, Name: t.Name, Class: t.Class, Priority: t.Priority,
			MaxSpeedKmph: t.MaxSpeed.InKMPH(), Points: []pointSpec{},
		}
		for _, sp := range t.schedule {
			p := pointSpec{Stn: string(sp.StnCode), Pf: sp.SpPfNo, Kind: SpStop}
			switch sp.kind {
			case SpPass:
				p.Kind, p.Arr = SpPass, timeOf(sp.ArrTime)
			case SpStop:
				p.Arr, p.Dep, p.MinDwell = timeOf(sp.ArrTime), timeOf(sp.DeptTime), timeOf(sp.DwellTime)
			case SpDwell:
				p.Arr, p.Dwell = timeOf(sp.ArrTime), timeOf(sp.DwellTime)
			default:
				return fmt.Errorf("train %s: %s: unknown kind %q", t.Number, sp.StnCode, sp.kind)
			}
			spec.Points = append(spec.Points, p)
		}
		file.Trains = append(file.Trains, spec)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(file)
}

// timeOf is a time to write: nil when it is unset (negative).
func timeOf(t float64) *float64 {
	if t < 0 {
		return nil
	}
	return &t
}

func value(v *float64, unset float64) float64 {
	if v == nil {
		return unset
	}
	return *v
}
