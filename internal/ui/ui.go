// Package ui is every byte this tool prints.
//
// Centralised for one reason: it is the only place that could accidentally
// print a secret, so it is the only place that has to be read carefully to know
// that none ever is.
package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

var (
	// Colours are adaptive so the output stays legible on a light terminal.
	cSubtle = lipgloss.AdaptiveColor{Light: "#6c6f85", Dark: "#8a8fa3"}
	cAccent = lipgloss.AdaptiveColor{Light: "#8839ef", Dark: "#c39cf5"}
	cOK     = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#5ed88a"}
	cWarn   = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#e0b341"}
	cErr    = lipgloss.AdaptiveColor{Light: "#c01c28", Dark: "#f77b7b"}

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	styleStep  = lipgloss.NewStyle().Bold(true)
	styleDim   = lipgloss.NewStyle().Foreground(cSubtle)
	styleOK    = lipgloss.NewStyle().Foreground(cOK)
	styleWarn  = lipgloss.NewStyle().Foreground(cWarn)
	styleErr   = lipgloss.NewStyle().Bold(true).Foreground(cErr)
	styleFP    = lipgloss.NewStyle().Foreground(cSubtle).Italic(true)
)

// Title prints the banner for a command.
func Title(format string, a ...any) {
	fmt.Println()
	fmt.Println(styleTitle.Render(fmt.Sprintf(format, a...)))
}

// Step prints a section heading.
func Step(format string, a ...any) {
	fmt.Println()
	fmt.Println(styleStep.Render(fmt.Sprintf(format, a...)))
}

// Note prints an indented, dimmed line.
func Note(format string, a ...any) {
	fmt.Println(styleDim.Render("  " + fmt.Sprintf(format, a...)))
}

// OK prints an indented success line.
func OK(format string, a ...any) {
	fmt.Printf("  %s %s\n", styleOK.Render("✓"), fmt.Sprintf(format, a...))
}

// Warn prints an indented warning to stderr.
func Warn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", styleWarn.Render("!"), fmt.Sprintf(format, a...))
}

// Err prints an error block to stderr.
func Err(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleErr.Render("error:"), fmt.Sprintf(format, a...))
}

// Fingerprint renders a digest in the one style used everywhere, so a digest is
// always recognisable as a digest and never mistaken for a value.
func Fingerprint(fp string) string {
	return styleFP.Render("sha256:" + fp)
}

// Blank prints an empty line.
func Blank() { fmt.Println() }

// Log adapts Note/OK to the callback signature the stores expect.
func Log(msg string, good bool) {
	if good {
		OK("%s", msg)
	} else {
		Warn("%s", msg)
	}
}

// Table renders a bordered table with the given headers and rows.
func Table(headers []string, rows [][]string) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(cSubtle)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return s.Bold(true).Foreground(cAccent)
			}
			if col == 0 {
				return s.Bold(true)
			}
			return s
		})
	return t.String()
}

// Present and Absent are the cell markers used by the status table.
func Present() string { return styleOK.Render("●") }
func Absent() string  { return styleDim.Render("·") }

// Indent shifts a multi-line block right, for nesting under a heading.
func Indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
