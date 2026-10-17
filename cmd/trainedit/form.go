package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// field is one line of a form: text typed in, or one of some choices picked
// with ← and →.
type field struct {
	key, label string
	value      string
	choices    []string // empty for free text
	help       string
}

// form is a modal dialog of fields. enter accepts it, running ok with the
// values by key; if ok fails, its error shows and the form stays. esc
// cancels.
type form struct {
	title  string
	fields []field
	cur    int
	ok     func(v values) error
	cancel func() // run when the form is backed out of, if set
	err    string
	notes  []string // lines under the fields, e.g. a format
}

// values are a form's answers, by field key.
type values map[string]string

func newForm(title string, ok func(values) error, fields ...field) *form {
	return &form{title: title, fields: fields, ok: ok}
}

func text(key, label, value string) field { return field{key: key, label: label, value: value} }

func number(key, label string, v float64) field {
	return field{key: key, label: label, value: strconv.FormatFloat(v, 'f', -1, 64)}
}

func choice(key, label, value string, choices ...string) field {
	if !slices.Contains(choices, value) && len(choices) > 0 {
		value = choices[0]
	}
	return field{key: key, label: label, value: value, choices: choices}
}

// key handles a key press, reporting whether the form is done (accepted or
// cancelled).
func (f *form) key(msg tea.KeyPressMsg) bool {
	fl := &f.fields[f.cur]
	switch msg.String() {
	case "esc":
		if f.cancel != nil {
			f.cancel()
		}
		return true
	case "enter":
		v := values{}
		for _, fd := range f.fields {
			v[fd.key] = strings.TrimSpace(fd.value)
		}
		if err := f.ok(v); err != nil {
			f.err = err.Error()
			return false
		}
		return true
	case "up", "shift+tab":
		f.cur = (f.cur + len(f.fields) - 1) % len(f.fields)
	case "down", "tab":
		f.cur = (f.cur + 1) % len(f.fields)
	case "left", "right", "space":
		if len(fl.choices) > 0 {
			i := slices.Index(fl.choices, fl.value)
			step := 1
			if msg.String() == "left" {
				step = len(fl.choices) - 1
			}
			fl.value = fl.choices[(i+step)%len(fl.choices)]
		} else if msg.String() == "space" {
			fl.value += " "
		}
	case "backspace":
		if r := []rune(fl.value); len(r) > 0 && len(fl.choices) == 0 {
			fl.value = string(r[:len(r)-1])
		}
	case "ctrl+u":
		if len(fl.choices) == 0 {
			fl.value = ""
		}
	default:
		if msg.Text != "" && len(fl.choices) == 0 {
			fl.value += msg.Text
		}
	}
	f.err = ""
	return false
}

// lines draws the form, w cells wide inside its border.
func (f *form) lines(w int) []string {
	lw := 0
	for _, fd := range f.fields {
		lw = max(lw, ansi.StringWidth(fd.label))
	}
	var out []string
	for i, fd := range f.fields {
		v := fd.value
		if len(fd.choices) > 0 {
			v = "◀ " + v + " ▶"
		}
		line := " " + tui.Fit(fd.label, lw+2) + v
		if i == f.cur {
			if len(fd.choices) == 0 {
				line += "▏"
			}
			line = tui.Accent.Render(tui.Fit(line, w))
		}
		out = append(out, line)
		if i == f.cur && fd.help != "" {
			out = append(out, tui.Dim.Render(" "+strings.Repeat(" ", lw+2)+fd.help))
		}
	}
	for _, n := range f.notes {
		for _, l := range tui.Wrap(n, w-2, 3) {
			out = append(out, tui.Dim.Render(" "+l))
		}
	}
	if f.err != "" {
		out = append(out, "")
		for _, l := range tui.Wrap(f.err, w-2, 4) {
			out = append(out, tui.Failed.Render(" "+l))
		}
	}
	out = append(out, "", tui.Dim.Render(" enter ok  esc cancel  ↑↓ field  ←→ choose"))
	return out
}

// overlay draws box lines over the middle of the screen's lines.
func overlay(screen []string, box []string, width int) []string {
	bw := 0
	for _, l := range box {
		bw = max(bw, ansi.StringWidth(l))
	}
	x := max((width-bw)/2, 0)
	y := max((len(screen)-len(box))/2, 0)
	out := slices.Clone(screen)
	for i, l := range box {
		if y+i >= len(out) {
			break
		}
		bg := out[y+i]
		out[y+i] = tui.Fit(ansi.Cut(bg, 0, x), x) + l + ansi.Cut(bg, x+bw, width)
	}
	return out
}

// drawForm is the form in its box, sized to fit the screen.
func drawForm(f *form, width, height int) []string {
	w := min(max(64, width/2), width-4)
	lines := f.lines(w - 2)
	return tui.Box(f.title, true, lines, w, min(len(lines)+2, height))
}

// parsing helpers for forms

func parseNum(v values, key, what string) (float64, error) {
	s := v[key]
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", what, s)
	}
	return n, nil
}

func parseInt(v values, key, what string) (int, error) {
	s := v[key]
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "+"))
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a whole number", what, s)
	}
	return n, nil
}

// parseTime reads a time as minutes, or as HH:MM; empty is unset (nil).
func parseTime(s, what string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "—" {
		return nil, nil
	}
	if h, m, ok := strings.Cut(s, ":"); ok {
		hh, err1 := strconv.Atoi(h)
		mm, err2 := strconv.ParseFloat(m, 64)
		if err1 != nil || err2 != nil || hh < 0 || mm < 0 || mm >= 60 {
			return nil, fmt.Errorf("%s: %q is not HH:MM", what, s)
		}
		t := float64(hh*60) + mm
		return &t, nil
	}
	t, err := strconv.ParseFloat(s, 64)
	if err != nil || t < 0 {
		return nil, fmt.Errorf("%s: %q is not a time (minutes, or HH:MM)", what, s)
	}
	return &t, nil
}

// showTime writes a time for a form field: HH:MM if whole minutes, else
// minutes; empty for unset.
func showTime(t *float64) string {
	if t == nil {
		return ""
	}
	if *t == float64(int(*t)) {
		return tui.Clock(*t)
	}
	return strconv.FormatFloat(*t, 'f', -1, 64)
}

func showNum(t *float64) string {
	if t == nil {
		return ""
	}
	return strconv.FormatFloat(*t, 'f', -1, 64)
}

func ptr(v float64) *float64 { return &v }
