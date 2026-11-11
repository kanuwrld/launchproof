package ui

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Theme struct {
	Text, Title, Muted, Dim, Accent, Selected, Border lipgloss.Style
}

func NewTheme(color bool) Theme {
	if !color {
		return Theme{}
	}
	return Theme{
		Text:     lipgloss.NewStyle().Foreground(lipgloss.Color("#E8EBE3")),
		Title:    lipgloss.NewStyle().Foreground(lipgloss.Color("#F4F6ED")).Bold(true),
		Muted:    lipgloss.NewStyle().Foreground(lipgloss.Color("#A1AAA1")),
		Dim:      lipgloss.NewStyle().Foreground(lipgloss.Color("#737E77")),
		Accent:   lipgloss.NewStyle().Foreground(lipgloss.Color("#C5EF91")),
		Selected: lipgloss.NewStyle().Foreground(lipgloss.Color("#C5EF91")).Bold(true),
		Border:   lipgloss.NewStyle().Foreground(lipgloss.Color("#35433B")),
	}
}

// SafeText keeps repository-controlled names from emitting terminal control sequences.
func SafeText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, ansi.Strip(text))
}

func Fit(text string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(text, width, "…")
}

func DisplayPath(path string) string {
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return SafeText(rel)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "~/" + SafeText(filepath.ToSlash(rel))
		}
	}
	return SafeText(path)
}
