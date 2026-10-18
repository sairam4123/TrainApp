package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func sheetKeys(s *sheet, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		s.key(msg)
	}
}

// A sheet's cursor runs over its head, table and foot, passing over cells
// that are off; the picked cell edits as a form's field does.
func TestSheet(t *testing.T) {
	s := &sheet{
		head:  [][]field{{text("a", "a", "x"), choice("b", "b", "one", "one", "two", "twelve")}},
		table: [][]field{{text("n", "", "1"), {key: "o", off: true}, choice("k", "", "p", "p", "q")}, {text("n2", "", "2"), {key: "o2", off: true}, choice("k2", "", "p", "p", "q")}},
		foot:  [][]field{{choice("z", "z", "m", "m", "n")}},
		cols:  []column{{"n", 4}, {"o", 3}, {"k", 5}},
	}
	s.fix()
	at := func() string { return s.cur().key }
	sheetKeys(s, "tab")
	if at() != "b" {
		t.Fatalf("tab: at %s, want b", at())
	}
	sheetKeys(s, "t", "w", "e")
	if v := s.cur().value; v != "twelve" {
		t.Errorf("typing twe picks %q, want twelve", v)
	}
	sheetKeys(s, "tab", "tab")
	if at() != "k" {
		t.Errorf("tab over an off cell: at %s, want k", at())
	}
	sheetKeys(s, "down")
	if at() != "k2" {
		t.Errorf("down keeps the column: at %s, want k2", at())
	}
	sheetKeys(s, "shift+tab", "backspace", "7")
	if at() != "n2" || s.cur().value != "7" {
		t.Errorf("edit n2: at %s = %q", at(), s.cur().value)
	}
	sheetKeys(s, "down", "right")
	if at() != "z" || s.cur().value != "n" {
		t.Errorf("down to the foot: at %s = %q", at(), s.cur().value)
	}
	out := ansi.Strip(strings.Join(s.render(60, 20), "\n"))
	for _, want := range []string{"b twelve", "n    o   k", "—", "◀ n ▶"} {
		if !strings.Contains(out, want) {
			t.Errorf("drawn without %q:\n%s", want, out)
		}
	}
	if v, ok := cellValue(s.table, "n2"); !ok || v != "7" {
		t.Errorf("cellValue n2 = %q, %v", v, ok)
	}
}
