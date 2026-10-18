package main

import (
	"strings"
	"trainapp/visualization/tui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// panel is a full-screen editor drawn over a tab: the station builder, the
// route picker. While one is open it takes every key; a form can still open
// over it. key reports whether it is done (accepted or backed out of).
type panel interface {
	key(msg tea.KeyPressMsg) (done bool)
	render(w, h int) []string
	keyLine() string
}

// column is one of a sheet's table columns.
type column struct {
	title string
	width int
}

// sheet is what a panel edits: rows of fields in three sections, a head, a
// table and a foot, with one cell picked. Head and foot rows draw each cell
// as its label and value; table rows draw their cells under the columns.
type sheet struct {
	head, table, foot [][]field
	cols              []column

	sec, row, col int // the picked cell: section (0 head, 1 table, 2 foot), row, cell
}

// section is a section's rows.
func (s *sheet) section(sec int) [][]field {
	return [][][]field{s.head, s.table, s.foot}[sec]
}

// cur is the picked cell, nil if there is none.
func (s *sheet) cur() *field {
	rows := s.section(s.sec)
	if s.row >= len(rows) || s.col >= len(rows[s.row]) {
		return nil
	}
	return &rows[s.row][s.col]
}

// pick picks a cell, as near the one asked for as there is.
func (s *sheet) pick(sec, row, col int) {
	s.sec, s.row, s.col = sec, row, col
	s.fix()
}

// fix moves the picked cell onto a cell that is there and not off.
func (s *sheet) fix() {
	for range 3 {
		if len(s.section(s.sec)) > 0 {
			break
		}
		s.sec, s.row = (s.sec+1)%3, 0
	}
	rows := s.section(s.sec)
	if len(rows) == 0 {
		return
	}
	s.row = min(max(s.row, 0), len(rows)-1)
	cells := rows[s.row]
	s.col = min(max(s.col, 0), len(cells)-1)
	for d := 0; d < len(cells); d++ {
		for _, c := range []int{s.col - d, s.col + d} {
			if c >= 0 && c < len(cells) && !cells[c].off && !cells[c].fixed {
				s.col = c
				return
			}
		}
	}
}

// lines are every row in order, as (section, row) pairs.
func (s *sheet) lines() [][2]int {
	var out [][2]int
	for sec := range 3 {
		for r := range s.section(sec) {
			out = append(out, [2]int{sec, r})
		}
	}
	return out
}

// moveRow picks the cell in the row d rows on, in the same column if it has
// one.
func (s *sheet) moveRow(d int) {
	ls := s.lines()
	at := 0
	for i, l := range ls {
		if l == [2]int{s.sec, s.row} {
			at = i
		}
	}
	at = min(max(at+d, 0), len(ls)-1)
	if len(ls) > 0 {
		s.pick(ls[at][0], ls[at][1], s.col)
	}
}

// moveCell picks the next cell that is not off (or the one before),
// running on into the next row.
func (s *sheet) moveCell(d int) {
	type at struct{ sec, row, col int }
	var cells []at
	cur := 0
	for _, l := range s.lines() {
		for c, f := range s.section(l[0])[l[1]] {
			if l == [2]int{s.sec, s.row} && c == s.col {
				cur = len(cells)
			}
			if (!f.off && !f.fixed) || (l == [2]int{s.sec, s.row} && c == s.col) {
				cells = append(cells, at{l[0], l[1], c})
			}
		}
	}
	if len(cells) == 0 {
		return
	}
	n := cells[(cur+d+len(cells))%len(cells)]
	s.sec, s.row, s.col = n.sec, n.row, n.col
}

// key moves about the sheet or edits the picked cell, reporting whether a
// cell's value may have changed.
func (s *sheet) key(msg tea.KeyPressMsg) (edited bool) {
	switch msg.String() {
	case "up":
		s.moveRow(-1)
	case "down":
		s.moveRow(1)
	case "tab":
		s.moveCell(1)
	case "shift+tab":
		s.moveCell(-1)
	default:
		if f := s.cur(); f != nil && !f.off && !f.fixed {
			before := f.value
			f.edit(msg)
			return f.value != before
		}
	}
	return false
}

// render draws the sheet w wide, scrolled to show the picked row in h lines.
func (s *sheet) render(w, h int) []string {
	var out []string
	picked := 0
	for sec := range 3 {
		rows := s.section(sec)
		if sec == 1 && len(rows) > 0 {
			var b strings.Builder
			b.WriteString("  ")
			for _, c := range s.cols {
				b.WriteString(tui.Fit(c.title, c.width) + " ")
			}
			out = append(out, tui.Dim.Render(b.String()))
		}
		for r, cells := range rows {
			here := sec == s.sec && r == s.row
			if here {
				picked = len(out)
			}
			var b strings.Builder
			if sec == 1 {
				b.WriteString(map[bool]string{true: tui.Accent.Render("▸ "), false: "  "}[here])
				for c, f := range cells {
					width := 8
					if c < len(s.cols) {
						width = s.cols[c].width
					}
					b.WriteString(drawCell(f, here && c == s.col, width) + " ")
				}
			} else {
				b.WriteString(" ")
				for c, f := range cells {
					if f.label != "" {
						b.WriteString(tui.Dim.Render(f.label) + " ")
					}
					b.WriteString(drawCell(f, here && c == s.col, 0) + "   ")
				}
			}
			out = append(out, ansi.Truncate(b.String(), w, "…"))
		}
		if sec != 1 && len(rows) > 0 {
			out = append(out, "")
		}
	}
	// scroll to keep the picked row in sight
	if len(out) > h {
		top := min(max(picked-h/2, 0), len(out)-h)
		out = out[top : top+h]
	}
	return out
}

// drawCell is a cell's value, width wide (0 for as wide as it is): the
// picked cell highlighted, with its cursor or its arrows.
func drawCell(f field, picked bool, width int) string {
	v := f.value
	picked = picked && !f.fixed
	switch {
	case f.off:
		v = "—"
	case picked && len(f.choices) > 0 && (width == 0 || ansi.StringWidth(v)+4 <= width):
		v = "◀ " + v + " ▶" // in a narrow column the highlight alone says it is picked
	case picked:
		r, at := []rune(v), f.cursor()
		v = string(r[:at]) + "▏" + string(r[at:])
	}
	if width > 0 {
		v = tui.Fit(v, width)
	}
	switch {
	case f.off:
		return tui.Dim.Render(v)
	case picked:
		return tui.Accent.Reverse(true).Render(v)
	}
	return v
}

// cellValue is the value of the cell with a key in rows, and whether there
// is one.
func cellValue(rows [][]field, key string) (string, bool) {
	for _, cells := range rows {
		for _, f := range cells {
			if f.key == key {
				return strings.TrimSpace(f.value), true
			}
		}
	}
	return "", false
}

// sidePanes lays out a panel: the sheet on the left, a preview on the right
// if there is room, each in its box.
func sidePanes(title string, left []string, previewTitle string, preview func(w, h int) []string, w, h int) []string {
	lw := w
	if w >= 100 {
		lw = max(64, w*11/20)
	}
	leftBox := tui.Box(title, true, left, lw, h)
	if lw == w {
		return leftBox
	}
	pw := w - lw
	return tui.Side([]int{lw, pw}, leftBox, tui.Box(previewTitle, false, preview(pw-2, h-2), pw, h))
}
