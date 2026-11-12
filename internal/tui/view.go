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
	t, l := ui.NewTheme(m.color), m.layout()
	var lines []string
	if m.width < 24 || m.height < 10 {
		lines = make([]string, l.height)
		if l.height >= 3 {
			lines[0], lines[1] = "LAUNCHPROOF", "Resize terminal"
		}
		lines[l.height-1] = "q quit"
	} else {
		title := "LAUNCHPROOF"
		var body []string
		switch m.page {
		case reportPage:
			title, body = "PROJECT / INSPECT", m.documentView(l, t)
		case guidePage:
			title, body = "A CLEAR START", m.documentView(l, t)
		default:
			if m.page >= workspacePage {
				title, body = m.workspaceTitle(), m.documentView(l, t)
			} else {
				body = m.homeView(l.contentWidth, l.bodyHeight, t)
			}
		}
		lines = append(lines, make([]string, l.edge)...)
		lines = append(lines, m.masthead(l.contentWidth, title, t)...)
		lines = append(lines, "")
		lines = append(lines, body...)
		lines = append(lines, "", t.Border.Render(strings.Repeat("─", l.contentWidth)), t.Dim.Render(m.footer(l.contentWidth)))
		lines = append(lines, make([]string, l.edge)...)
	}
	view := tea.NewView(l.render(lines))
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
	if gap >= 2 {
		left += strings.Repeat(" ", gap) + right
	}
	return []string{ui.Fit(left, width), t.Border.Render(strings.Repeat("─", width))}
}

func (m Model) footer(width int) string {
	if m.page >= workspacePage {
		return m.workspaceFooter(width)
	}
	if m.page == homePage {
		return helpText(width, "↑/↓ navigate    enter open    s status    w workspace    q quit", "s status · enter · q quit", "enter · q quit")
	}
	return helpText(width, "↑/↓ scroll    i inspect    r refresh    esc home    q quit", "↑/↓ scroll · esc back · q quit", "esc back · q quit")
}

func (m Model) homeView(width, height int, t ui.Theme) []string {
	var description []string
	if height >= 12 {
		text := []string{
			"Discover your stack, scripts, docs, and Git state.",
			"Plan, check, read docs, and prepare a handoff.",
			"Back to your terminal.",
		}[m.cursor]
		if m.busy {
			text = "Reading project metadata…"
		}
		// A consistent description area keeps the menu still when selection changes.
		rows := len(wrapped("Discover your stack, scripts, docs, and Git state.", width))
		description = append(description, t.Border.Render(strings.Repeat("─", width)), "")
		description = append(description, block(centeredLines(wrapped(t.Muted.Render(text), width), width), width, rows, false)...)
	}
	mainHeight := height - len(description)
	var main []string
	// Cells are roughly twice as tall as they are wide. The ratio avoids forcing
	// a portrait or square terminal into columns just because it has many cells.
	if width >= 100 && m.width*2 >= m.height*5 {
		leftWidth := (width - 5) / 2
		rightWidth := width - leftWidth - 5
		left := block(m.brand(leftWidth, mainHeight, t), leftWidth, mainHeight, true)
		right := block(m.menu(rightWidth, mainHeight, t), rightWidth, mainHeight, true)
		for i := range left {
			main = append(main, left[i]+"  "+t.Border.Render("│")+"  "+right[i])
		}
	} else {
		menu := m.menu(width, mainHeight, t)
		gap := 0
		brand := m.brand(width, mainHeight-len(menu)-1, t)
		if len(brand) > 0 {
			gap = 1
		}
		main = append(main, brand...)
		main = append(main, make([]string, gap)...)
		main = append(main, menu...)
		main = block(main, width, mainHeight, true)
	}
	return append(main, description...)
}

func (m Model) brand(width, height int, t ui.Theme) []string {
	if height < 3 {
		return nil
	}
	title := []string{t.Title.Render("LAUNCHPROOF")}
	if width >= lipgloss.Width(wordmark(t)) {
		title = strings.Split(wordmark(t), "\n")
	}
	eyebrow := helpText(width, "F R O M  I D E A  T O  E V I D E N C E", "FROM IDEA TO EVIDENCE")
	tagline := wrapped(t.Muted.Render("Know what's ready. Know what's next."), width)
	compact := append([]string{t.Dim.Render(eyebrow), ""}, title...)
	compact = append(compact, "")
	compact = append(compact, tagline...)
	if width >= 24 && height >= len(compact)+12 {
		full := []string{t.Dim.Render(eyebrow), ""}
		full = append(full, centeredLines(strings.Split(orbit(m.frame, t), "\n"), width)...)
		full = append(full, "")
		full = append(full, title...)
		full = append(full, "")
		full = append(full, tagline...)
		return centeredLines(full, width)
	}
	if len(compact) <= height {
		return centeredLines(compact, width)
	}
	minimal := append([]string{t.Title.Render("LAUNCHPROOF")}, tagline...)
	if len(minimal) <= height {
		return centeredLines(minimal, width)
	}
	return nil
}

func (m Model) menu(width, height int, t ui.Theme) []string {
	menuWidth := min(width, 48)
	var lines []string
	if height >= 6 {
		workspace := ui.SafeText(filepath.Base(m.path))
		if menuWidth >= 32 {
			lines = append(lines, t.Dim.Render("WORKSPACE  ")+t.Text.Render(ui.Fit(workspace, menuWidth-11)))
		} else {
			lines = append(lines, t.Dim.Render("WORKSPACE"), t.Text.Render(ui.Fit(workspace, menuWidth)))
		}
		lines = append(lines, "")
	}
	for i, item := range []string{"Inspect this project", "Project workspace", "Exit"} {
		prefix, style := "  ", t.Text
		if i == m.cursor {
			prefix, style = "› ", t.Selected
		}
		line := style.Render(prefix + item)
		if i == m.cursor && menuWidth >= 34 {
			line += strings.Repeat(" ", menuWidth-lipgloss.Width(line)-7) + t.Dim.Render("enter ↵")
		}
		lines = append(lines, line)
		if i < 2 && height >= 10 {
			lines = append(lines, "")
		}
	}
	return centeredLines(block(lines, menuWidth, len(lines), false), width)
}

func (m Model) pageLines(width int, t ui.Theme) []string {
	if m.page >= workspacePage {
		return m.workspaceLines(width, t)
	}
	if m.page == reportPage {
		if m.err != nil {
			return append([]string{t.Title.Render("Could not inspect this project."), ""}, wrapped(t.Text.Render(ui.SafeText(m.err.Error())), width)...)
		}
		return ui.ReportLines(m.report, width, t)
	}
	var lines []string
	for _, section := range [][2]string{
		{"01  Start with facts", "Inspect package.json, go.mod, project documents, and Git. Your files stay on your machine."},
		{"02  Make the next step clear", "See available scripts and missing setup documents. Inspection never runs your project scripts."},
		{"03  Run explicit checks", "Open Project workspace to initialize a plan and review launchproof.json. Only Run checks executes its configured commands; use it in projects you trust."},
		{"04  Keep evidence honest", "Reports contain real check results and a source fingerprint. Source changes mark earlier evidence stale. Skipped checks and unverified assumptions stay visible."},
		{"05  See what remains", "Press s from home to see Git changes, current evidence, requirements linked to checks, and next actions. Status never executes commands; c opens the check plan for review."},
	} {
		lines = append(lines, wrapped(t.Accent.Render(section[0]), width)...)
		lines = append(lines, wrapped(t.Muted.Render(section[1]), width)...)
		lines = append(lines, "")
	}
	return lines[:len(lines)-1]
}

func (m Model) viewport(lines int) int {
	height := m.layout().bodyHeight
	if lines > height {
		height-- // Reserve a scroll-position row without moving the footer.
	}
	return max(1, height)
}

func (m Model) documentView(l frameLayout, t ui.Theme) []string {
	content := m.pageLines(l.contentWidth, t)
	visible := m.viewport(len(content))
	start := min(m.offset, max(0, len(content)-visible))
	end := min(len(content), start+visible)
	lines := block(content[start:end], l.contentWidth, visible, false)
	if len(content) > visible {
		lines = append(lines, t.Dim.Render(ui.Fit(fmt.Sprintf("%d–%d of %d lines", start+1, end, len(content)), l.contentWidth)))
	}
	return block(lines, l.contentWidth, l.bodyHeight, false)
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
