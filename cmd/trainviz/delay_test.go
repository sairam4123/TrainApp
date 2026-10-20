package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDelayPage(t *testing.T) {
	m := newKPIModel(t)
	press(m, "3")
	fits(t, m)
	s := screen(m)
	for _, want := range []string{"delay", "punctuality", "delay bands", "waits and holds", "trains · by delay", "waits by place", "finished", "within 5 min", "0451U", "0456D"} {
		if !strings.Contains(s, want) {
			t.Errorf("delay page lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "paused") || strings.Contains(s, " late ") && strings.Contains(s, "  late  ") {
		t.Errorf("the timeline, or a late column for untimed trains, shows:\n%s", s)
	}
	press(m, "c")
	if !strings.Contains(screen(m), "trains · by late") {
		t.Errorf("c does not change the sort:\n%s", screen(m))
	}
	press(m, "c", "c", "c")

	// the most delayed train first, its waits beside the table; enter shows
	// its longest wait
	first, _ := m.st.selectedTrain()
	if first.DelayMin == nil || *first.DelayMin < *m.st.rep.PerTrain[0].DelayMin {
		t.Fatalf("first row %+v is not the most delayed", first)
	}
	if !strings.Contains(screen(m), "─ "+first.Train+" ") {
		t.Errorf("no box for %s:\n%s", first.Train, screen(m))
	}
	press(m, "enter")
	if m.page != pageSchematic || m.selectedTrain() != first.Train {
		t.Fatalf("page %d, train %q after enter, want the schematic and %s", m.page, m.selectedTrain(), first.Train)
	}
	want := first.EntryMin
	longest := -1.0
	for _, w := range first.Waits {
		if w.Min > longest {
			want, longest = w.FromMin, w.Min
		}
	}
	if m.rep.t != want {
		t.Errorf("at t=%.2f, want %.2f", m.rep.t, want)
	}
}

func TestDelayPageWaitsByPlace(t *testing.T) {
	m := newKPIModel(t)
	press(m, "3", "shift+tab")
	if m.st.dFocus != dPlaces {
		t.Fatal("shift+tab does not move back to the waits by place")
	}
	places := waitPlaces(m.st.rep)
	if len(places) == 0 {
		t.Skip("no waits in the run")
	}
	press(m, "esc")
	if m.st.dFocus != dTrains {
		t.Error("esc does not go back to the trains")
	}
	press(m, "shift+tab", "enter")
	if m.page != pageSchematic || !strings.Contains(m.note, places[0].at) {
		t.Errorf("enter on %s: page %d, note %q", places[0].at, m.page, m.note)
	}
}

func TestDelayPageScrollsWithTheSelection(t *testing.T) {
	m := newKPIModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	press(m, "3")
	for range len(m.st.rep.PerTrain) {
		press(m, "j")
	}
	fits(t, m)
	last := m.st.delayTrains()[len(m.st.rep.PerTrain)-1].Train
	if s := screen(m); !strings.Contains(s, last) || !strings.Contains(s, "class") {
		t.Errorf("the last train %s or the table header is out of view:\n%s", last, s)
	}
}

func TestDelayFilters(t *testing.T) {
	m := newKPIModel(t)
	press(m, "3")
	all := len(m.st.rep.PerTrain)
	for range m.st.filters()[1:] {
		press(m, "b")
		f := m.st.filter()
		want := 0
		for _, tr := range m.st.rep.PerTrain {
			if f.admits(tr.DelayMin) {
				want++
			}
		}
		if got := len(m.st.delayTrains()); got != want {
			t.Errorf("%s keeps %d trains, want %d", f.name, got, want)
		}
		if s := screen(m); !strings.Contains(s, fmt.Sprintf("trains · %s · %d of %d", f.name, want, all)) {
			t.Errorf("%s: the trains box does not say so:\n%s", f.name, s)
		}
	}
	press(m, "esc")
	if m.st.dFilter != 0 || len(m.st.delayTrains()) != all {
		t.Errorf("esc keeps the filter %q", m.st.filter().name)
	}
}

func TestDelayStations(t *testing.T) {
	m := newKPIModel(t)
	press(m, "3", "tab")
	if m.st.dFocus != dStations {
		t.Fatalf("focus %d after tab, want the stations", m.st.dFocus)
	}
	fits(t, m)
	s := screen(m)
	for _, want := range []string{"stations · by line order", "calls at TPJ", "TPJ", "PDKT", "KKDI", "on time"} {
		if !strings.Contains(s, want) {
			t.Errorf("stations lack %q:\n%s", want, s)
		}
	}
	press(m, "c")
	if !strings.Contains(screen(m), "stations · by fewest on time") {
		t.Errorf("c does not sort the stations:\n%s", screen(m))
	}
	press(m, "c", "c", "c")

	// the calls on time at the picked station
	stn, _ := m.st.selectedDelayStation()
	for m.st.filter().name != "over 5 min" {
		press(m, "b")
	}
	for _, c := range m.st.stationCalls() {
		if c.call.Stn != stn.Stn || c.call.DelayMin == nil || *c.call.DelayMin <= 5 {
			t.Errorf("call %+v kept by over 5 min at %s", c.call, stn.Stn)
		}
	}
	if !strings.Contains(screen(m), "calls at "+stn.Stn+" · over 5 min") {
		t.Errorf("the calls box does not say the filter:\n%s", screen(m))
	}
	press(m, "esc")

	// into the calls, and one on the schematic
	press(m, "enter")
	if m.st.dFocus != dCalls {
		t.Fatalf("enter on a station: focus %d, want its calls", m.st.dFocus)
	}
	call := m.st.stationCalls()[0]
	press(m, "enter")
	if m.page != pageSchematic || m.selectedTrain() != call.train.Train || m.rep.t != call.at() {
		t.Errorf("enter on %s's call: page %d, train %q, t=%.2f; want the schematic at %.2f", call.train.Train, m.page, m.selectedTrain(), m.rep.t, call.at())
	}
}

func TestDelaySchematic(t *testing.T) {
	m := newKPIModel(t)
	press(m, "3", "v")
	fits(t, m)
	s := screen(m)
	for _, want := range []string{"Delay · schematic", "delay · stations by on time", "stations · on time", "blocks", "TPJ ", "m"} {
		if !strings.Contains(s, want) {
			t.Errorf("delay schematic lacks %q:\n%s", want, s)
		}
	}
	if !strings.Contains(m.render(), "38;5;") {
		t.Error("nothing drawn in a delay colour")
	}
	press(m, "m")
	if !strings.Contains(screen(m), "stations by mean delay") {
		t.Errorf("m does not change the metric:\n%s", screen(m))
	}
	press(m, "v")
	if m.st.dMap || !strings.Contains(screen(m), "trains · by delay") {
		t.Errorf("v does not go back to the tables:\n%s", screen(m))
	}
}
