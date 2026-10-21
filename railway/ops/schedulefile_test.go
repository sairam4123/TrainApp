package ops_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"trainapp/railway/ops"
	"trainapp/units"
)

const exampleSchedule = `{"trains": [
  {"number": "1U", "name": "Up", "class": "Passenger", "max_speed_kmph": 110, "points": [
    {"stn": "A", "kind": "stop", "dwell": 5, "pf": "1", "arr": 1},
    {"stn": "B", "kind": "pass"},
    {"stn": "C", "kind": "pass", "arr": 30, "pf": "2"},
    {"stn": "D", "kind": "stop", "arr": 40, "dep": 42, "min_dwell": 1},
    {"stn": "E", "kind": "stop", "dwell": 0}]},
  {"number": "2D", "priority": 7, "max_speed_kmph": 80, "points": [
    {"stn": "E", "kind": "stop", "arr": 0, "dep": 3}]}
]}`

func TestReadTrains(t *testing.T) {
	trains, err := ops.ReadTrains(strings.NewReader(exampleSchedule))
	if err != nil {
		t.Fatal(err)
	}
	if len(trains) != 2 {
		t.Fatalf("%d trains, want 2", len(trains))
	}
	up := trains[0]
	if up.Number != "1U" || up.Name != "Up" || up.Class != "Passenger" || up.MaxSpeed != units.KMPH(110) {
		t.Errorf("train 1U read as %+v", up)
	}
	type point struct {
		kind            ops.SchedulePointKind
		stn, pf         string
		arr, dep, dwell float64
	}
	var got []point
	for _, sp := range up.Schedule() {
		got = append(got, point{sp.Kind(), string(sp.StnCode), sp.SpPfNo, sp.ArrTime, sp.DeptTime, sp.DwellTime})
	}
	want := []point{
		{ops.SpDwell, "A", "1", 1, -1, 5},
		{ops.SpPass, "B", "", -1, -1, 0},
		{ops.SpPass, "C", "2", 30, 30, 0},
		{ops.SpStop, "D", "", 40, 42, 1},
		{ops.SpDwell, "E", "", -1, -1, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("points\n got %+v\nwant %+v", got, want)
	}
	if down := trains[1]; down.Priority != 7 || down.Schedule()[0].DwellTime != -1 {
		t.Errorf("train 2D: priority %d, min dwell %v; want 7 and none", down.Priority, down.Schedule()[0].DwellTime)
	}
}

func TestWriteTrainsReadsBack(t *testing.T) {
	trains, err := ops.ReadTrains(strings.NewReader(exampleSchedule))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := ops.WriteTrains(&buf, trains); err != nil {
		t.Fatal(err)
	}
	back, err := ops.ReadTrains(&buf)
	if err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if !reflect.DeepEqual(trains, back) {
		t.Errorf("trains differ after writing and reading back:\n%s", buf.String())
	}
}

func TestReadTrainsErrors(t *testing.T) {
	for _, c := range []struct {
		name, points string
		wants        []string
	}{
		{"no entry", `{"stn": "A", "kind": "stop", "dwell": 1}`, []string{"first point needs an arr"}},
		{"no points", ``, []string{"no points"}},
		{"unknown kind", `{"stn": "A", "kind": "halt", "arr": 0, "dwell": 1}`, []string{`kind "halt"`}},
		{"stop needs a dwell", `{"stn": "A", "kind": "stop", "arr": 0}`, []string{"needs a dwell"}},
		{"dep before arr", `{"stn": "A", "kind": "stop", "arr": 5, "dep": 4}`, []string{"dep is before arr"}},
		{"later arr without dep, negative dwell", `{"stn": "A", "kind": "stop", "arr": 0, "dwell": 1},
			{"stn": "B", "kind": "stop", "arr": 9, "dwell": 1},
			{"stn": "C", "kind": "stop", "dwell": -1}`, []string{"point 1 (B): an arr without a dep", "point 2 (C): dwell must not be negative"}},
		{"pass with dwell", `{"stn": "A", "kind": "stop", "arr": 0, "dwell": 1}, {"stn": "B", "kind": "pass", "dwell": 1}`, []string{"a pass takes only an arr"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := `{"trains": [{"number": "X", "max_speed_kmph": 100, "points": [` + c.points + `]}]}`
			_, err := ops.ReadTrains(strings.NewReader(in))
			if err == nil {
				t.Fatal("read without an error")
			}
			for _, want := range c.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
		})
	}
	if _, err := ops.ReadTrains(strings.NewReader(`{"trains": [{"number": "X", "speed": 1}]}`)); err == nil || !strings.Contains(err.Error(), `unknown field "speed"`) {
		t.Errorf("unknown field: error %v", err)
	}
}

// A train's entry speed reads from entry_kmph and is written back.
func TestEntryKmphRoundTrips(t *testing.T) {
	src := `{"trains": [{"number": "1", "max_speed_kmph": 110, "entry_kmph": 45, "points": [{"stn": "A", "kind": "stop", "dwell": 5, "arr": 1}]}]}`
	trains, err := ops.ReadTrains(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := trains[0].EntrySpeed; got != units.KMPH(45) {
		t.Fatalf("entry speed %v km/h, want 45", got.InKMPH())
	}
	var buf bytes.Buffer
	if err := ops.WriteTrains(&buf, trains); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"entry_kmph": 45`) {
		t.Errorf("written without entry_kmph 45:\n%s", buf.String())
	}
	if _, err := ops.ReadTrains(strings.NewReader(strings.Replace(src, `"entry_kmph": 45`, `"entry_kmph": -1`, 1))); err == nil {
		t.Error("a negative entry_kmph accepted")
	}
}
