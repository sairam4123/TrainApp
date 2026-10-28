package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"trainapp/railway/infra"
	"trainapp/railway/ops"
	"trainapp/railway/sim"
	"trainapp/worlds"
)

var opt = options{from: 5 * 60, sourceMin: 5, destMin: 5}

func singleLine(t *testing.T) *infra.Snapshot {
	t.Helper()
	s, err := worlds.BuildTpjKkdiSingleLineWorld().Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func at(code, a, d, day string) call {
	return call{Code: code, A: a, D: d, ADay: day, DDay: day}
}

var express = trainInfo{Class: "Mail/Express", MaxSpeedKmph: 110}

// stations is a train's stations, a pass marked with a leading "-".
func stations(tr ops.TrainSpec) string {
	var out []string
	for _, p := range tr.Points {
		s := p.Stn
		if p.Kind == ops.SpPass {
			s = "-" + s
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// Times run on across midnight, and Source and Destination stand as long as
// asked.
func TestTimes(t *testing.T) {
	stops, err := times([]call{at("TPJ", "Source", "23:25", "1"), at("PDKT", "00:08", "00:10", "2"), at("KKDI", "00:43", "Destination", "2")}, opt)
	if err != nil {
		t.Fatal(err)
	}
	var got []float64
	for _, st := range stops {
		got = append(got, st.arr, st.dep)
	}
	if want := []float64{1400, 1405, 1448, 1450, 1483, 1488}; !slices.Equal(got, want) {
		t.Errorf("times %v, want %v", got, want)
	}

	if _, err := times([]call{at("TPJ", "Source", "23:25", "1"), at("PDKT", "00:08", "00:10", "1")}, opt); err == nil || !strings.Contains(err.Error(), "day 2") {
		t.Errorf("a day that does not agree: %v", err)
	}
	if _, err := times([]call{at("TPJ", "Source", "23:25", "1"), at("PDKT", "Source", "00:10", "2")}, opt); err == nil {
		t.Error("a Source after the first call")
	}
	if _, err := times([]call{at("TPJ", "Source", "7:5x", "1"), at("PDKT", "08:00", "Destination", "1")}, opt); err == nil {
		t.Error("a time that is not one")
	}
}

// A train passes every station on its way that it does not call at, and
// one that enters before the day starts runs at its end.
func TestConvertTrain(t *testing.T) {
	s := singleLine(t)
	tr, warn, err := convertTrain(s, "16618", []call{at("TPJ", "00:05", "00:10", "2"), at("PDKT", "00:58", "01:00", "2"), at("KKDI", "01:43", "01:45", "2")}, express, opt)
	if err != nil || len(warn) > 0 {
		t.Fatal(err, warn)
	}
	if got, want := stations(tr), "TPJ -KRMG -KRUR -VEL PDKT -NMN -TYM -CTND -KTYR KKDI"; got != want {
		t.Errorf("stations %s, want %s", got, want)
	}
	if *tr.Points[0].Arr != 1445 || *tr.Points[len(tr.Points)-1].Dep != 1545 {
		t.Errorf("enters at %v, leaves at %v: want 1445 and 1545", *tr.Points[0].Arr, *tr.Points[len(tr.Points)-1].Dep)
	}

	tr, _, err = convertTrain(s, "16849", []call{at("TPJ", "Source", "07:05", "1"), at("KRMG", "07:17", "07:18", "1"), at("KKDI", "08:30", "Destination", "1")}, express, opt)
	if err != nil {
		t.Fatal(err)
	}
	if *tr.Points[0].Arr != 420 || *tr.Points[0].Dep != 425 || *tr.Points[len(tr.Points)-1].Dep != 515 {
		t.Errorf("Source or Destination: %+v", tr.Points)
	}
	if p := tr.Points[1]; p.Stn != "KRMG" || p.Kind != ops.SpStop || p.Pf != "" {
		t.Errorf("KRMG %+v", p)
	}
}

// A train that comes in without calling where the world ends enters there,
// at the time its own speed gives, or the train list's.
func TestExtend(t *testing.T) {
	s := singleLine(t)
	calls := []call{
		{Code: "PDKT", Dist: "212", Pf: "1", A: "01:43", D: "01:45", ADay: "2", DDay: "2"},
		{Code: "TPJ", Dist: "264", Pf: "3", A: "03:00", D: "03:10", ADay: "2", DDay: "2"},
	}
	tr, warn, err := convertTrain(s, "20497", calls, express, opt)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stations(tr), "-KKDI -KTYR -CTND -TYM -NMN PDKT -VEL -KRUR -KRMG TPJ"; got != want {
		t.Errorf("stations %s, want %s", got, want)
	}
	// 36 km in at 52 km in 75 min is about 51 min
	if e := *tr.Points[0].Arr; e < 1440+43 || e > 1440+60 {
		t.Errorf("enters at %v (%s)", e, clock(e))
	}
	if len(warn) != 1 || !strings.Contains(warn[0], "enters at KKDI") {
		t.Errorf("warnings %v", warn)
	}

	given := express
	given.Enter = &enterAt{Stn: "KKDI", At: "00:55"}
	if tr, _, err = convertTrain(s, "20497", calls, given, opt); err != nil || *tr.Points[0].Arr != 1440+55 {
		t.Errorf("enter given: %v, %v", err, tr.Points)
	}
	given.Enter = &enterAt{Stn: "TPJ", At: "00:55"}
	if _, _, err = convertTrain(s, "20497", calls, given, opt); err == nil {
		t.Error("an enter at a station it does not enter at")
	}

	// a train that starts there runs from there
	calls[0].A = "Source"
	if tr, warn, err = convertTrain(s, "20497", calls, express, opt); err != nil || tr.Points[0].Stn != "PDKT" || len(warn) > 0 {
		t.Errorf("from its Source: %v, %v, %v", err, stations(tr), warn)
	}
}

// Everything wrong is reported, and nothing is converted.
func TestConvertErrors(t *testing.T) {
	s := singleLine(t)
	sched := realSchedule{
		"1": {at("TPJ", "Source", "07:00", "1"), at("KKDI", "08:30", "Destination", "1")},
		"2": {at("TPJ", "Source", "07:00", "1"), {Code: "PDKT", Pf: "9", A: "07:50", D: "07:52", ADay: "1", DDay: "1"}, at("KKDI", "08:30", "Destination", "1")},
		"3": {at("TPJ", "Source", "07:00", "1"), at("KKDI", "08:00", "08:05", "1"), at("PDKT", "08:30", "Destination", "1")},
		"4": {at("TPJ", "Source", "07:00", "1"), at("XYZ", "08:30", "Destination", "1")},
		"5": {at("TPJ", "Source", "07:00", "1"), at("KKDI", "08:30", "Destination", "1")},
	}
	list := trainList{Trains: map[string]trainInfo{"2": express, "3": express, "4": express, "5": {Class: "Hyperloop", MaxSpeedKmph: 110}}}
	trains, _, err := convert(s, sched, list, map[string]int{"Mail/Express": 40}, opt)
	if err == nil || trains != nil {
		t.Fatalf("converted %d trains", len(trains))
	}
	for _, want := range []string{"train 1: not in the train list", "train 2: PDKT has no platform 9", "train 3: no way", "train 4: no station XYZ", `train 5: class "Hyperloop"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("no %q in:\n%v", want, err)
		}
	}
}

// The day's real schedule converts, in entry order, and reads back as
// trains.
func TestRealDay(t *testing.T) {
	var sched realSchedule
	var list trainList
	if err := readFile("../../docs/real_schedule/schedules_3.json", &sched); err != nil {
		t.Skip(err)
	}
	if err := readFile("../../scenarios/sl-irl-wed/trains.json", &list); err != nil {
		t.Fatal(err)
	}
	cfg, err := sim.LoadPriorityConfig("../../profiles/ir.json")
	if err != nil {
		t.Fatal(err)
	}
	trains, warn, err := convert(singleLine(t), sched, list, cfg.Classes, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(trains) != len(sched) || len(warn) != 1 || !strings.HasPrefix(warn[0], "20497") {
		t.Errorf("%d trains, warnings %v", len(trains), warn)
	}
	for i, tr := range trains {
		if e := *tr.Points[0].Arr; e < opt.from || e >= opt.from+1440 || (i > 0 && e < *trains[i-1].Points[0].Arr) {
			t.Errorf("%s enters at %v", tr.Number, e)
		}
	}
	var buf bytes.Buffer
	if err := ops.WriteSchedule(&buf, ops.ScheduleFile{Trains: trains}); err != nil {
		t.Fatal(err)
	}
	if back, err := ops.ReadTrains(&buf); err != nil || len(back) != len(trains) {
		t.Errorf("read back %d trains: %v", len(back), err)
	}
}
