package main

import (
	"strings"
	"trainapp/visualization/tui"

	"github.com/charmbracelet/x/ansi"
)

// Boxes and tables the KPI pages are laid out with, as the other apps
// lay out theirs.

// cols lines cells up in columns of the widths, right-aligning those
// marked in right.
func cols(widths []int, right []bool, cells ...string) string {
	var sb strings.Builder
	for i, c := range cells {
		w := widths[i]
		if i < len(right) && right[i] {
			c = ansi.Truncate(c, w-1, "")
			sb.WriteString(strings.Repeat(" ", max(w-1-ansi.StringWidth(c), 0)) + c + " ")
		} else {
			sb.WriteString(fit(c, w))
		}
	}
	return sb.String()
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

// panel is a box before it is drawn.
type panel struct {
	title   string
	lines   []string
	focused bool
}

// width is how wide the panel wants to be: its widest line or its title,
// with its border and a cell of padding either side.
func (p panel) width() int {
	w := ansi.StringWidth(p.title) + 4
	for _, l := range p.lines {
		w = max(w, ansi.StringWidth(l)+4)
	}
	return w
}

// draw draws the panel w by h, its lines padded a cell from the border.
func (p panel) draw(w, h int) []string {
	lines := make([]string, len(p.lines))
	for i, l := range p.lines {
		lines[i] = " " + l
	}
	return tui.Box(p.title, p.focused, lines, w, h)
}

// boxRows draws the panels across w: as many to a row as fit at their own
// width, sharing out what is left between them (each at most twice as wide
// as it wants), each row as tall as its tallest panel. Rows past maxH lines are left out, but never the first;
// maxH <= 0 keeps them all.
func boxRows(w, maxH int, panels []panel) []string {
	var out []string
	for len(panels) > 0 {
		n, used := 0, 0
		for n < len(panels) && (n == 0 || used+panels[n].width() <= w) {
			used += min(panels[n].width(), w)
			n++
		}
		row := panels[:n]
		panels = panels[n:]
		h, widths := 0, make([]int, n)
		for i, p := range row {
			h = max(h, len(p.lines)+2)
			want := min(p.width(), w)
			widths[i] = want + min((w-used)/n, want)
		}
		if spare := w - sum(widths); spare < n {
			widths[n-1] += spare
		}
		if maxH > 0 && len(out) > 0 && len(out)+h > maxH {
			break
		}
		drawn := make([][]string, n)
		for i, p := range row {
			drawn[i] = p.draw(widths[i], h)
		}
		for y := range h {
			var sb strings.Builder
			for i := range row {
				sb.WriteString(drawn[i][y])
			}
			out = append(out, sb.String()+strings.Repeat(" ", max(w-sum(widths), 0)))
		}
	}
	return out
}

// beside puts two columns of lines side by side.
func beside(left, right []string, lw, rw int) []string {
	out := make([]string, max(len(left), len(right)))
	for i := range out {
		out[i] = fit(tui.At(left, i), lw) + fit(tui.At(right, i), rw)
	}
	return out
}

// table is a box's rows under a header line.
type table struct {
	header string
	rows   []string
	faint  []bool // rows drawn faint, e.g. tracks no train used
	empty  string // what to say when there are no rows
}

// lines draws the table in h lines, or all of it if h <= 0: the header,
// then the rows from *off, scrolled so row sel shows; sel is picked out,
// reversed if the table has the focus.
func (t table) lines(sel int, focused bool, off *int, h int) []string {
	out := []string{dim.Render(t.header)}
	if len(t.rows) == 0 {
		return append(out, dim.Render(t.empty))
	}
	rows := len(t.rows)
	if h > 0 {
		rows = max(h-1, 1)
	}
	if sel < *off {
		*off = sel
	}
	if sel >= *off+rows {
		*off = sel - rows + 1
	}
	*off = min(max(*off, 0), max(len(t.rows)-rows, 0))
	for i := *off; i < min(*off+rows, len(t.rows)); i++ {
		row := t.rows[i]
		switch {
		case i == sel && focused:
			row = bold.Reverse(true).Render(ansi.Strip(row))
		case i == sel:
			row = bold.Render("▸") + row[min(1, len(row)):]
		case i < len(t.faint) && t.faint[i]:
			row = dim.Render(ansi.Strip(row))
		}
		out = append(out, row)
	}
	return out
}

// tableBox draws a table in a box w by h, or as tall as it needs if h <= 0.
func tableBox(title string, t table, sel int, focused bool, off *int, w, h int) []string {
	inner := h - 2
	if h <= 0 {
		inner = len(t.rows) + 1
	}
	return panel{title: title, lines: t.lines(sel, focused, off, inner), focused: focused}.draw(w, inner+2)
}
