package main

import (
	"strings"
	"testing"
)

func TestOccupancySchematic(t *testing.T) {
	m := newKPIModel(t)
	press(m, "4")
	fits(t, m)
	s := screen(m)
	for _, want := range []string{"occupancy · occupied", "capacity bands", "light", "moderate", "blocks", "TPJ", "%"} {
		if !strings.Contains(s, want) {
			t.Errorf("occupancy page lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "km)") {
		t.Errorf("a block shows its length, not its share:\n%s", s)
	}
	if !strings.Contains(m.render(), "38;5;") {
		t.Error("no track drawn in a heat colour")
	}
	press(m, "m")
	if !strings.Contains(screen(m), "occupancy · standing") {
		t.Errorf("m does not change the metric:\n%s", screen(m))
	}
}

func TestOccupancyTables(t *testing.T) {
	m := newKPIModel(t)
	press(m, "4", "v")
	fits(t, m)
	s := screen(m)
	for _, want := range []string{"measured over", "capacity bands", "blocks · by line order", "stations · by line order", "platform tracks · TPJ", "trains/h"} {
		if !strings.Contains(s, want) {
			t.Errorf("occupancy tables lack %q:\n%s", want, s)
		}
	}
	// moving through the stations shows each one's platform tracks
	press(m, "tab")
	if m.st.oFocus != tableStations {
		t.Fatalf("focus %d after tab, want the stations", m.st.oFocus)
	}
	press(m, "j")
	if stn := m.st.selectedStation(); stn == "TPJ" || !strings.Contains(screen(m), "platform tracks · "+stn) {
		t.Errorf("platform tracks of %s not shown:\n%s", stn, screen(m))
	}
	press(m, "t")
	fits(t, m)
	if !strings.Contains(screen(m), "throats · by") {
		t.Errorf("t does not show the throats:\n%s", screen(m))
	}
	press(m, "c")
	if !strings.Contains(screen(m), "blocks · by occupied") {
		t.Errorf("c does not sort:\n%s", screen(m))
	}

	// back to the blocks, and a block on the schematic
	for m.st.oFocus != tableBlocks {
		press(m, "tab")
	}
	block := m.st.occTracks(tableBlocks)[0]
	press(m, "enter")
	if m.st.table || m.st.focus != m.st.trackID[block.Track] {
		t.Errorf("enter on %s: table %v, focus %q", block.Track, m.st.table, m.st.focus)
	}
	press(m, "esc")
	if m.st.focus != "" {
		t.Error("esc keeps the track picked out")
	}
}
