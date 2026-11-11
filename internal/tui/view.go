package tui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kanuwrld/launchproof/internal/ui"
)

func (m Model) View() tea.View {
	t := ui.NewTheme(m.color)
	width := m.contentWidth()
	var lines []string
	if m.width < 24 || m.height < 10 {
		lines = []string{"LAUNCHPROOF", "Resize terminal", "q quit"}
	} else {
		switch m.page {
		case reportPage:
			lines = m.reportView(width, t)
		case guidePage:
			lines = m.guideView(width, t)
		default:
			lines = m.homeView(width, t)
		}
	}
	for i := range lines {
		lines[i] = ui.Fit(lines[i], max(1, m.width-2))
	}
	if len(lines) > m.height {
		lines = lines[:max(0, m.height)]
	}
	body := lipgloss.NewStyle().Width(width).Align(lipgloss.Left).Render(strings.Join(lines, "\n"))
	content := lipgloss.Place(max(1, m.width), max(1, m.height), lipgloss.Center, lipgloss.Center, body)
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "Launchproof"
	if m.color {
		view.BackgroundColor = lipgloss.Color("#101611")
		view.ForegroundColor = lipgloss.Color("#E8EBE3")
	}
	return view
}

func (m Model) masthead(width int, title string, t ui.Theme) []string {
	left := t.Accent.Render("◈") + t.Muted.Render("  "+title)
	right := t.Dim.Render(ui.SafeText(m.version))
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return []string{ui.Fit(left, width), t.Border.Render(strings.Repeat("─", width))}
	}
	return []string{left + strings.Repeat(" ", gap) + right, t.Border.Render(strings.Repeat("─", width))}
}

func (m Model) homeView(width int, t ui.Theme) []string {
	lines := m.masthead(width, "LAUNCHPROOF", t)
	lines = append(lines, "")
	if m.height >= 24 && width >= 74 {
		copy := t.Dim.Render("F R O M  I D E A  T O  E V I D E N C E") + "\n\n" +
			wordmark(t) + "\n\n" + t.Muted.Render("Know what's ready. Know what's next.")
		hero := lipgloss.JoinHorizontal(lipgloss.Center, orbit(m.frame, t), "    ", copy)
		lines = append(lines, strings.Split(hero, "\n")...)
		lines = append(lines, "")
	} else if m.height >= 23 && width >= 44 {
		lines = append(lines, strings.Split(wordmark(t), "\n")...)
		lines = append(lines, "", t.Muted.Render("Know what's ready. Know what's next."), "")
	} else {
		lines = append(lines, t.Title.Render("Know what's ready."))
		if width > 27 {
			lines = append(lines, t.Muted.Render("Know what's next."))
		}
		if m.height >= 19 {
			lines = append(lines, "")
		}
	}
	if m.height >= 19 {
		workspace := ui.SafeText(filepath.Base(m.path))
		lines = append(lines, t.Dim.Render("WORKSPACE  ")+t.Text.Render(ui.Fit(workspace, width-11)), "")
	}
	items := []string{"Inspect this project", "How it works", "Exit"}
	menuWidth := min(width, 40)
	for i, item := range items {
		prefix := "  "
		style := t.Text
		if i == m.cursor {
			prefix, style = "› ", t.Selected
		}
		line := style.Render(prefix + item)
		if i == m.cursor && width >= 44 {
			line += strings.Repeat(" ", max(2, menuWidth-lipgloss.Width(line)-7)) + t.Dim.Render("enter ↵")
		}
		lines = append(lines, lipgloss.NewStyle().Width(menuWidth).Render(line))
	}
	if m.height >= 19 {
		lines = append(lines, "")
		description := []string{
			"Discover your stack, scripts, docs, and Git state.",
			"A clear starting point for your next release.",
			"Back to your terminal.",
		}[m.cursor]
		if m.busy {
			description = "Reading project metadata…"
		}
		lines = append(lines, t.Muted.Render(ui.Fit(description, width)))
	}
	lines = append(lines, t.Border.Render(strings.Repeat("─", width)))
	footer := "↑/↓ navigate    enter open    i inspect    q quit"
	if width < 52 {
		footer = "↑/↓ select · enter · q quit"
	}
	if width < 32 {
		footer = "enter · q quit"
	}
	lines = append(lines, t.Dim.Render(ui.Fit(footer, width)))
	for i := 2; i < len(lines); i++ {
		lines[i] = lipgloss.PlaceHorizontal(width, lipgloss.Center, lines[i])
	}
	return lines
}

func (m Model) reportView(width int, t ui.Theme) []string {
	lines := m.masthead(width, "PROJECT / INSPECT", t)
	lines = append(lines, "")
	if m.err != nil {
		body := "Could not inspect this project.\n\n" + ui.SafeText(m.err.Error())
		errorLines := strings.Split(lipgloss.NewStyle().Width(width).Render(t.Text.Render(body)), "\n")
		lines = append(lines, errorLines[:min(len(errorLines), max(1, m.height-7))]...)
	} else {
		content := ui.ReportLines(m.report, width, t)
		visible := max(1, m.height-9)
		start := min(m.offset, max(0, len(content)-visible))
		end := min(len(content), start+visible)
		lines = append(lines, content[start:end]...)
		for len(lines) < visible+3 {
			lines = append(lines, "")
		}
		if len(content) > visible {
			lines = append(lines, t.Dim.Render(fmt.Sprintf("%d–%d of %d lines", start+1, end, len(content))))
		} else {
			lines = append(lines, "")
		}
	}
	footer := "↑/↓ scroll    r refresh    esc home    q quit"
	if width < 50 {
		footer = "↑/↓ scroll · esc back · q quit"
	}
	if width < 32 {
		footer = "esc back · q quit"
	}
	lines = append(lines, "", t.Border.Render(strings.Repeat("─", width)), t.Dim.Render(ui.Fit(footer, width)))
	return lines
}

func (m Model) guideView(width int, t ui.Theme) []string {
	lines := m.masthead(width, "A CLEAR START", t)
	paragraphs := []string{
		"01  Start with facts",
		"Inspect package.json, go.mod, project documents, and Git. Your files stay on your machine.",
		"02  Make the next step clear",
		"See available scripts and missing setup documents. Inspection never runs your project scripts.",
		"03  Keep evidence honest",
		"This first release inspects metadata. It does not execute tests or certify release readiness.",
	}
	lines = append(lines, "")
	for i, paragraph := range paragraphs {
		style := t.Muted
		if i%2 == 0 {
			style = t.Accent
		}
		lines = append(lines, strings.Split(lipgloss.NewStyle().Width(width).Render(style.Render(paragraph)), "\n")...)
		if i%2 == 1 {
			lines = append(lines, "")
		}
	}
	if len(lines) > m.height-3 {
		lines = lines[:max(3, m.height-3)]
	}
	footer := "esc home    i inspect    q quit"
	if width < 32 {
		footer = "esc back · q quit"
	}
	lines = append(lines, t.Border.Render(strings.Repeat("─", width)), t.Dim.Render(footer))
	return lines
}

func wordmark(t ui.Theme) string {
	glyphs := map[rune][]string{
		'L': {"█  ", "█  ", "█  ", "█  ", "███"},
		'A': {"▄█▄", "█ █", "███", "█ █", "█ █"},
		'U': {"█ █", "█ █", "█ █", "█ █", "▀█▀"},
		'N': {"█  █", "██ █", "█ ██", "█  █", "█  █"},
		'C': {"▄██", "█  ", "█  ", "█  ", "▀██"},
		'H': {"█ █", "█ █", "███", "█ █", "█ █"},
		'P': {"██▄", "█ █", "██▀", "█  ", "█  "},
		'R': {"██▄", "█ █", "██▀", "█ █", "█ █"},
		'O': {"▄█▄", "█ █", "█ █", "█ █", "▀█▀"},
		'F': {"███", "█  ", "██ ", "█  ", "█  "},
	}
	rows := make([]string, 5)
	for row := range rows {
		for i, letter := range "LAUNCHPROOF" {
			if i > 0 {
				rows[row] += " "
			}
			rows[row] += glyphs[letter][row]
		}
		rows[row] = t.Title.Render(rows[row])
	}
	return strings.Join(rows, "\n")
}

func orbit(frame int, t ui.Theme) string {
	const width, height = 24, 11
	cells := make([][]rune, height)
	for y := range cells {
		cells[y] = []rune(strings.Repeat(" ", width))
	}
	for i := range 100 {
		a := float64(i) / 100 * 2 * math.Pi
		x := int(math.Round(11.5 + 10.5*math.Cos(a)))
		y := int(math.Round(5 + 2.8*math.Sin(a) - 1.3*math.Cos(a)))
		cells[y][x] = '·'
	}
	for y, row := range []string{"  ╱╲  ", " ╱  ╲ ", "╱    ╲", "╲    ╱", " ╲  ╱ ", "  ╲╱  "} {
		for x, cell := range []rune(row) {
			if cell != ' ' {
				cells[y+2][x+9] = cell
			}
		}
	}
	a := -math.Pi/3 + float64(min(frame, 60))/60*2*math.Pi
	x := int(math.Round(11.5 + 10.5*math.Cos(a)))
	y := int(math.Round(5 + 2.8*math.Sin(a) - 1.3*math.Cos(a)))
	cells[y][x] = '◆'
	cells[5][12] = '✦'
	rows := make([]string, height)
	for y := range cells {
		for _, cell := range cells[y] {
			style := t.Border
			switch cell {
			case '◆', '✦':
				style = t.Accent
			case '╱', '╲':
				style = t.Muted
			}
			rows[y] += style.Render(string(cell))
		}
	}
	return strings.Join(rows, "\n")
}
