package traingraph

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Options say what part of the graph to draw and how.
type Options struct {
	Width, Height int     // cells, the time axis included
	From, To      float64 // the time window
	// Now is the replay time: the lines stop there and a cursor marks it.
	// Negative draws every line in full and no cursor.
	Now      float64
	Selected string // a train to pick out, "" for none
	Planned  bool   // mark the trains' planned times
	Color    bool
	// Under, if set, is another run on the same corridor, drawn faint
	// first, e.g. the baseline when comparing runs; its Selected train is
	// drawn dotted, so the gap to this run's line is the time it lost or
	// gained.
	Under *Graph
}

// colours: trains running east and west, picked out or not
const (
	sgrEast     = "36"
	sgrWest     = "33"
	sgrFaint    = "2"
	sgrSelected = "1;97"
	sgrPlanned  = "35"
	sgrGrid     = "90"
	sgrUnder    = "2;37"
)

// minute steps for the time axis labels
var tickSteps = []float64{1, 2, 5, 10, 15, 30, 60, 120, 240, 480, 720, 1440}

// Gutter is how many columns the station names take on the left.
func (g *Graph) Gutter() int {
	w := 0
	for _, s := range g.cor.Stations {
		w = max(w, utf8.RuneCountInString(s.Code))
	}
	return w + 1
}

// TimeAt is the time at a column of a drawing made with o.
func (g *Graph) TimeAt(o Options, col int) (float64, bool) {
	pw := (o.Width - g.Gutter()) * 2
	x := (col - g.Gutter()) * 2
	if x < 0 || x >= pw || pw < 2 {
		return 0, false
	}
	return o.From + float64(x)/float64(pw-1)*(o.To-o.From), true
}

// Draw draws the graph, o.Height lines of o.Width cells.
func (g *Graph) Draw(o Options) []string {
	gut := g.Gutter()
	cw, ch := o.Width-gut, o.Height-1
	if cw < 4 || ch < 2 || !(o.To > o.From) {
		return []string{"too small to draw the graph"}
	}
	cv := newCanvas(o.Width, o.Height)
	pw, ph := cw*2, ch*4
	length := max(g.cor.Length(), 1)
	px := func(t float64) float64 { return (t - o.From) / (o.To - o.From) * float64(pw-1) }
	py := func(at float64) float64 { return at / length * float64(ph-1) }

	// stations: names in the gutter, a faint line across
	for _, s := range g.cor.Stations {
		row := int(math.Round(py(s.At))) / 4
		cv.text(0, row, s.Code, sgrGrid)
		for x := gut; x < o.Width; x++ {
			cv.set(x, row, '┄', sgrGrid)
		}
	}
	// the cursor
	if o.Now >= o.From && o.Now <= o.To {
		col := gut + int(math.Round(px(o.Now)))/2
		for y := range ch {
			cv.set(col, y, '│', sgrGrid)
		}
	}

	if u := o.Under; u != nil {
		var sel *train
		for _, tr := range u.trains {
			if tr.number == o.Selected {
				sel = tr
				continue
			}
			u.drawRuns(cv, tr, gut, px, py, pw, ph, 1, sgrUnder)
		}
		if sel != nil {
			u.drawRuns(cv, sel, gut, px, py, pw, ph, 3, sgrSelected)
		}
	}

	// the selected train last, so it stays on top
	order := make([]*train, 0, len(g.trains))
	var sel *train
	for _, tr := range g.trains {
		if tr.number == o.Selected {
			sel = tr
			continue
		}
		order = append(order, tr)
	}
	if sel != nil {
		order = append(order, sel)
	}
	style := func(tr *train) string {
		s := sgrWest
		if tr.east {
			s = sgrEast
		}
		switch {
		case tr == sel:
			s = sgrSelected
		case sel != nil:
			s = sgrFaint + ";" + s
		}
		return s
	}
	for _, tr := range order {
		style := style(tr)
		for _, r := range tr.runs {
			for i := 1; i < len(r); i++ {
				a, b := r[i-1], r[i]
				if o.Now >= 0 && a.t > o.Now {
					break
				}
				if o.Now >= 0 && b.t > o.Now {
					// cut the segment at now
					f := (o.Now - a.t) / (b.t - a.t)
					b = sample{o.Now, a.at + f*(b.at-a.at)}
				}
				cv.line(gut, px(a.t), py(a.at), px(b.t), py(b.at), pw, ph, style, 1)
			}
		}
		if o.Planned {
			for _, p := range tr.planned {
				x, y := px(p.t), py(p.at)
				if x >= 0 && x < float64(pw) {
					cv.set(gut+int(x)/2, int(y)/4, '◆', sgrPlanned)
				}
			}
		}
	}

	// labels once every line is in, so none is drawn over
	for _, tr := range order {
		for _, r := range tr.runs {
			if len(r) > 0 && (o.Now < 0 || r[0].t <= o.Now) && cv.label(gut, px(r[0].t), py(r[0].at), pw, ch, tr.number, style(tr)) {
				break
			}
		}
	}

	g.axis(cv, o, gut, cw, ch)
	out := make([]string, o.Height)
	for y := range out {
		out[y] = cv.row(y, o.Color)
	}
	return out
}

// drawRuns draws a train's runs in full, every every-th pixel of them: 1
// for a solid line, more for a dotted one.
func (g *Graph) drawRuns(cv *canvas, tr *train, gut int, px, py func(float64) float64, pw, ph, every int, sgr string) {
	for _, r := range tr.runs {
		for i := 1; i < len(r); i++ {
			a, b := r[i-1], r[i]
			cv.line(gut, px(a.t), py(a.at), px(b.t), py(b.at), pw, ph, sgr, every)
		}
	}
}

// axis labels the time axis along the bottom row with clock times.
func (g *Graph) axis(cv *canvas, o Options, gut, cw, row int) {
	span := o.To - o.From
	step := tickSteps[len(tickSteps)-1]
	for _, s := range tickSteps {
		if s/span*float64(cw) >= 7 { // "HH:MM" and a gap
			step = s
			break
		}
	}
	for t := math.Ceil(o.From/step) * step; t <= o.To; t += step {
		col := gut + int((t-o.From)/span*float64(cw-1)+0.5)
		if col+5 > o.Width {
			break
		}
		cv.text(col, row, clock(t), sgrGrid)
	}
}

// clock shows minutes from 0 as HH:MM.
func clock(m float64) string {
	mins := int(math.Round(m))
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// canvas is a grid of cells; a cell holds a character, or braille dots once
// a line has been drawn through it.
type canvas struct {
	w, h  int
	cells []cell
}

type cell struct {
	r    rune
	dots uint8
	sgr  string
}

func newCanvas(w, h int) *canvas {
	cv := &canvas{w: w, h: h, cells: make([]cell, w*h)}
	for i := range cv.cells {
		cv.cells[i].r = ' '
	}
	return cv
}

func (cv *canvas) at(x, y int) *cell {
	if x < 0 || y < 0 || x >= cv.w || y >= cv.h {
		return nil
	}
	return &cv.cells[y*cv.w+x]
}

func (cv *canvas) set(x, y int, r rune, sgr string) {
	if c := cv.at(x, y); c != nil {
		c.r, c.dots, c.sgr = r, 0, sgr
	}
}

func (cv *canvas) text(x, y int, s, sgr string) {
	for _, r := range s {
		cv.set(x, y, r, sgr)
		x++
	}
}

// braille dot bits by column (0, 1) and row (0-3) within a cell
var dotBits = [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

// dot sets a braille dot at pixel (x, y) of the plot, which starts at column
// gut.
func (cv *canvas) dot(gut, x, y int, sgr string) {
	c := cv.at(gut+x/2, y/4)
	if c == nil || x < 0 {
		return
	}
	if c.dots == 0 {
		c.r = 0
	}
	c.dots |= dotBits[x%2][y%4]
	c.sgr = sgr
}

// line draws a straight line between two plot pixels, clipped to the plot:
// every pixel of it, or for every > 1 every every-th, dotted.
func (cv *canvas) line(gut int, x0, y0, x1, y1 float64, pw, ph int, sgr string, every int) {
	n := int(math.Ceil(max(math.Abs(x1-x0), math.Abs(y1-y0)))) + 1
	for i := 0; i < n; i += max(every, 1) {
		f := 0.0
		if n > 1 {
			f = float64(i) / float64(n-1)
		}
		x := int(math.Round(x0 + f*(x1-x0)))
		y := int(math.Round(y0 + f*(y1-y0)))
		if x >= 0 && x < pw && y >= 0 && y < ph {
			cv.dot(gut, x, y, sgr)
		}
	}
}

// label writes a train's number by where its line starts: before it, or
// failing that just after it on the row below or above. It reports whether it
// found room among the rows of the plot.
func (cv *canvas) label(gut int, x, y float64, pw, rows int, text, sgr string) bool {
	if x < 0 || x >= float64(pw) {
		return false
	}
	col, row := gut+int(x)/2, int(y)/4
	n := utf8.RuneCountInString(text)
	for _, at := range [][2]int{{col - n - 1, row}, {col + 1, row + 1}, {col + 1, row - 1}, {col + 1, row}} {
		if at[0] < gut || at[0]+n > cv.w || at[1] < 0 || at[1] >= rows || !cv.free(at[0], at[1], n) {
			continue
		}
		cv.text(at[0], at[1], text, sgr)
		return true
	}
	return false
}

// free reports whether n cells from (x, y) hold no line or label.
func (cv *canvas) free(x, y, n int) bool {
	for i := range n {
		c := cv.at(x+i, y)
		if c == nil || c.dots != 0 || (c.r != ' ' && c.r != '┄' && c.r != '│') {
			return false
		}
	}
	return true
}

// row renders a line of the canvas, with colour if asked.
func (cv *canvas) row(y int, color bool) string {
	var sb strings.Builder
	cur := ""
	for x := range cv.w {
		c := cv.cells[y*cv.w+x]
		r := c.r
		if c.dots != 0 {
			r = 0x2800 + rune(c.dots)
		}
		if color && c.sgr != cur {
			sb.WriteString("\x1b[0m")
			if c.sgr != "" {
				sb.WriteString("\x1b[")
				sb.WriteString(c.sgr)
				sb.WriteString("m")
			}
			cur = c.sgr
		}
		sb.WriteRune(r)
	}
	if color && cur != "" {
		sb.WriteString("\x1b[0m")
	}
	return sb.String()
}
