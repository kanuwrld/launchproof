package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kanuwrld/launchproof/internal/ui"
)

// All pages share the same frame so resizing cannot move the footer into the body.
type frameLayout struct {
	width, height, gutter, edge, contentWidth, bodyHeight int
}

func (m Model) layout() frameLayout {
	l := frameLayout{width: max(1, m.width), height: max(1, m.height)}
	switch {
	case l.width >= 120:
		l.gutter = 4
	case l.width >= 60:
		l.gutter = 2
	case l.width >= 24:
		l.gutter = 1
	}
	if l.height >= 16 {
		l.edge = 1
	}
	l.contentWidth = l.width - 2*l.gutter
	// Header and footer each occupy two rows, with one gap beside the body.
	l.bodyHeight = max(0, l.height-2*l.edge-6)
	return l
}

func (l frameLayout) render(lines []string) string {
	rows := make([]string, l.height)
	for i := range rows {
		line := ""
		if i < len(lines) {
			line = ui.Fit(lines[i], l.contentWidth)
		}
		rows[i] = strings.Repeat(" ", l.gutter) +
			lipgloss.PlaceHorizontal(l.contentWidth, lipgloss.Left, line) + strings.Repeat(" ", l.gutter)
	}
	return strings.Join(rows, "\n")
}

func block(lines []string, width, height int, centered bool) []string {
	rows := make([]string, max(0, height))
	start := 0
	if centered {
		start = max(0, (height-len(lines))/2)
	}
	for i := range rows {
		line := ""
		if i >= start && i-start < len(lines) {
			line = lines[i-start]
		}
		rows[i] = lipgloss.PlaceHorizontal(width, lipgloss.Left, ui.Fit(line, width))
	}
	return rows
}

func centeredLines(lines []string, width int) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = lipgloss.PlaceHorizontal(width, lipgloss.Center, ui.Fit(line, width))
	}
	return result
}

func wrapped(text string, width int) []string {
	lines := strings.Split(lipgloss.NewStyle().Width(max(1, width)).Render(text), "\n")
	for i, line := range lines {
		// Width rendering adds padding; remove it before centering the actual text.
		textWidth := lipgloss.Width(strings.TrimRight(ansi.Strip(line), " "))
		lines[i] = ansi.Truncate(line, textWidth, "")
	}
	return lines
}

func helpText(width int, options ...string) string {
	for _, text := range options {
		if lipgloss.Width(text) <= width {
			return text
		}
	}
	return ui.Fit("q quit", width)
}
