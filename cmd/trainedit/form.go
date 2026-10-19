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
	// pos is where the cursor is in free text, in runes, once moved; until
	// then it is at the end
	pos   int
	moved bool
	// off is a cell that does not apply, drawn as a dash and passed over;
	// fixed is one drawn as it is and passed over, e.g. a row's name
	off, fixed bool
	// filter is what has been typed into a choice to pick one by
	filter string
}

// cursor is the cursor's place in the field's text.
func (fd *field) cursor() int {
	n := len([]rune(fd.value))
	if !fd.moved || fd.pos > n {
		return n
	}
	return max(fd.pos, 0)
}

// moveTo puts the cursor at a place in the text; past the end is the end.
func (fd *field) moveTo(at int) {
	fd.pos, fd.moved = max(at, 0), at < len([]rune(fd.value))
}

// insert types text at the cursor.
func (fd *field) insert(text string) {
	r, at := []rune(fd.value), fd.cursor()
	fd.value = string(r[:at]) + text + string(r[at:])
	fd.moveTo(at + len([]rune(text)))
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
	top    bool     // drawn at the top, not the middle, to keep what is under it in sight
}

// value is a field's value, trimmed.
func (f *form) value(key string) string {
	for _, fd := range f.fields {
		if fd.key == key {
			return strings.TrimSpace(fd.value)
		}
	}
	return ""
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
	default:
		fl.edit(msg)
	}
	f.err = ""
	return false
}

// edit takes a key that edits the field: in a choice, ← → and space choose,
// and typing picks the choice that starts with what is typed; in text, ← →
// home end move the cursor, and typing, backspace and delete work at it.
func (fd *field) edit(msg tea.KeyPressMsg) {
	if len(fd.choices) > 0 && msg.Text != "" && msg.String() != "space" {
		fd.pick(msg.Text)
		return
	}
	fd.filter = ""
	switch msg.String() {
	case "left", "right", "space":
		switch {
		case len(fd.choices) > 0:
			i := slices.Index(fd.choices, fd.value)
			step := 1
			if msg.String() == "left" {
				step = len(fd.choices) - 1
			}
			fd.value = fd.choices[(i+step)%len(fd.choices)]
		case msg.String() == "space":
			fd.insert(" ")
		case msg.String() == "left":
			fd.moveTo(fd.cursor() - 1)
		default:
			fd.moveTo(fd.cursor() + 1)
		}
	case "home", "ctrl+a":
		fd.pos, fd.moved = 0, true
	case "end", "ctrl+e":
		fd.moved = false
	case "backspace":
		if r, at := []rune(fd.value), fd.cursor(); at > 0 && len(fd.choices) == 0 {
			fd.value = string(r[:at-1]) + string(r[at:])
			fd.moveTo(at - 1)
		}
	case "delete":
		if r, at := []rune(fd.value), fd.cursor(); at < len(r) && len(fd.choices) == 0 {
			fd.value = string(r[:at]) + string(r[at+1:])
			fd.moveTo(at)
		}
	case "ctrl+u":
		if len(fd.choices) == 0 {
			fd.value, fd.moved = "", false
		}
	default:
		if msg.Text != "" && len(fd.choices) == 0 {
			fd.insert(msg.Text)
		}
	}
}

// pick narrows a choice to the first that starts with what has been typed
// (any case), starting again from what was just typed if none does.
func (fd *field) pick(text string) {
	match := func(prefix string) bool {
		i := slices.IndexFunc(fd.choices, func(c string) bool { return strings.HasPrefix(strings.ToLower(c), strings.ToLower(prefix)) })
		if i >= 0 {
			fd.value = fd.choices[i]
		}
		return i >= 0
	}
	if match(fd.filter + text) {
		fd.filter += text
		return
	}
	fd.filter = ""
	if match(text) {
		fd.filter = text
	}
}

// lines draws the form, w cells wide inside its border, and says which line
// the current field is on.
func (f *form) lines(w int) (out []string, cur int) {
	lw := 0
	for _, fd := range f.fields {
		lw = max(lw, ansi.StringWidth(fd.label))
	}
	for i, fd := range f.fields {
		v := fd.value
		if len(fd.choices) > 0 {
			v = "◀ " + v + " ▶"
		} else if i == f.cur {
			r, at := []rune(v), fd.cursor()
			v = string(r[:at]) + "▏" + string(r[at:])
		}
		line := " " + tui.Fit(fd.label, lw+2) + v
		if i == f.cur {
			cur = len(out)
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
	out = append(out, "", tui.Dim.Render(" enter ok  esc cancel  ↑↓ field  ←→ choose, or move in text"))
	return out, cur
}

// overlay draws box lines over the middle of the screen's lines.
func overlay(screen []string, box []string, width int, top bool) []string {
	bw := 0
	for _, l := range box {
		bw = max(bw, ansi.StringWidth(l))
	}
	x := max((width-bw)/2, 0)
	y := max((len(screen)-len(box))/2, 0)
	if top {
		y = 0
	}
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

// drawForm is the form in its box, sized to fit the screen. A form taller
// than the screen scrolls to keep the current field, its help and the keys
// at the bottom in sight.
func drawForm(f *form, width, height int) []string {
	w := min(max(64, width/2), width-4)
	lines, cur := f.lines(w - 2)
	if inner := height - 2; len(lines) > inner && inner > 4 {
		keys := lines[len(lines)-1]
		body := lines[:len(lines)-2]
		room := inner - 1
		top := min(max(cur-room/2, 0), max(len(body)-room, 0))
		lines = append(slices.Clone(body[top:min(top+room, len(body))]), keys)
	}
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
