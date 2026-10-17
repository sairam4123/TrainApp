// Package tui holds what the terminal apps (trainviz, traindemo, trainedit)
// draw alike: styles, boxes, the class palette and clock, and a camera that
// pans and zooms a schematic.
package tui

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	Dim    = lipgloss.NewStyle().Faint(true)
	Bold   = lipgloss.NewStyle().Bold(true)
	Accent = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	Amber  = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // as trainviz draws held trains
	Good   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	Failed = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// Spinner turns while something runs.
var Spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Palette colours train classes from the lowest priority to the highest:
// blue to red, leaving out the amber of held trains.
var Palette = []string{"27", "39", "44", "42", "113", "149", "141", "177", "169", "203"}

// PaletteColor is the colour of the i-th of n classes, lowest priority
// first, spread over the palette; a lone class is red.
func PaletteColor(i, n int) string {
	k := len(Palette) - 1
	if n > 1 {
		k = int(math.Round(float64(i) / float64(n-1) * float64(len(Palette)-1)))
	}
	return Palette[k]
}

// Clock shows minutes from 0 as HH:MM.
func Clock(t float64) string {
	mins := int(math.Round(t))
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// Box draws a single-line border with a title around lines, w by h cells.
// The focused box's title stands out.
func Box(title string, focused bool, lines []string, w, h int) []string {
	if h < 2 {
		return nil
	}
	inner := max(w-2, 0)
	label := ansi.Truncate(" "+title+" ", max(inner-1, 0), "")
	fill := strings.Repeat("─", max(inner-1-ansi.StringWidth(label), 0)) + "┐"
	top := Dim.Render("┌─" + label + fill)
	if focused {
		top = Dim.Render("┌─") + Accent.Bold(true).Render(label) + Dim.Render(fill)
	}
	out := []string{top}
	for i := range h - 2 {
		out = append(out, Dim.Render("│")+Fit(At(lines, i), inner)+Dim.Render("│"))
	}
	return append(out, Dim.Render("└"+strings.Repeat("─", inner)+"┘"))
}

// Fit cuts or pads s to exactly w cells.
func Fit(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// At is line i, or "" past the end.
func At(lines []string, i int) string {
	if i >= 0 && i < len(lines) {
		return lines[i]
	}
	return ""
}

// Wrap wraps text to w cells, at most n lines, the last cut short with "…".
func Wrap(text string, w, n int) []string {
	lines := strings.Split(ansi.Wordwrap(text, max(w, 1), " "), "\n")
	if len(lines) > n {
		lines = lines[:n]
		lines[n-1] = ansi.Truncate(lines[n-1]+" …", w, "…")
	}
	return lines
}

// DimAll dims every line.
func DimAll(lines []string) []string {
	for i, line := range lines {
		lines[i] = Dim.Render(line)
	}
	return lines
}

// ValueOr is s, or or if s is empty.
func ValueOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// Figure is a labelled figure, labels lined up.
func Figure(label, value string) string {
	return Dim.Render(Fit(label, 11)) + value
}

// Bar is a progress bar w cells wide, share of it filled.
func Bar(share float64, w int) string {
	w = max(w, 4)
	filled := min(max(int(share*float64(w)), 0), w)
	return Accent.Render(strings.Repeat("━", filled)) + Dim.Render(strings.Repeat("─", w-filled))
}

// Side puts blocks of lines side by side, each padded to its width, as tall
// as the tallest.
func Side(widths []int, blocks ...[]string) []string {
	h := 0
	for _, b := range blocks {
		h = max(h, len(b))
	}
	out := make([]string, h)
	for i := range out {
		var sb strings.Builder
		for k, b := range blocks {
			sb.WriteString(Fit(At(b, i), widths[k]))
		}
		out[i] = sb.String()
	}
	return out
}
