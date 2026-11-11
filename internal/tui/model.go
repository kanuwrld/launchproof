package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kanuwrld/launchproof/internal/project"
	"github.com/kanuwrld/launchproof/internal/ui"
)

type page int

const (
	homePage page = iota
	reportPage
	guidePage
)

type tickMsg struct{}
type inspectedMsg struct {
	report project.Report
	err    error
}

type Model struct {
	ctx                   context.Context
	path, version         string
	width, height         int
	cursor, frame, offset int
	page                  page
	color, motion, busy   bool
	report                project.Report
	err                   error
	workspace             workspaceState
}

func New(ctx context.Context, path, version string, color, motion bool) Model {
	return Model{ctx: ctx, path: path, version: version, width: 80, height: 28, color: color, motion: motion}
}

func (m Model) Init() tea.Cmd {
	if m.motion {
		return nextFrame()
	}
	return nil
}

func nextFrame() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) inspect() tea.Cmd {
	return func() tea.Msg {
		report, err := project.Inspect(m.ctx, m.path)
		return inspectedMsg{report, err}
	}
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.workspaceUpdate(message); handled {
		return next, cmd
	}
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.offset = min(m.offset, m.scrollLimit())
		if m.listCount() > 0 {
			m = m.revealSelection()
		}
	case tickMsg:
		m.frame++
		if m.motion && m.frame < 60 && m.page == homePage {
			return m, nextFrame()
		}
	case inspectedMsg:
		if !m.busy {
			return m, nil
		}
		m.busy, m.report, m.err, m.page, m.offset = false, msg.report, msg.err, reportPage, 0
	case tea.KeyPressMsg:
		key := msg.String()
		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc", "backspace":
			m.page, m.offset, m.busy = homePage, 0, false
			return m, nil
		case "i", "r":
			if !m.busy {
				m.busy = true
				return m, m.inspect()
			}
		case "?":
			m.page, m.offset = guidePage, 0
		}
		if m.busy {
			return m, nil
		}
		if m.page == homePage {
			switch key {
			case "up", "k", "shift+tab":
				m.cursor = (m.cursor + 2) % 3
			case "down", "j", "tab":
				m.cursor = (m.cursor + 1) % 3
			case "enter", " ":
				switch m.cursor {
				case 0:
					m.busy = true
					return m, m.inspect()
				case 1:
					m = m.openWorkspace()
				case 2:
					return m, tea.Quit
				}
			}
		} else {
			lineCount := len(m.pageLines(m.contentWidth(), ui.NewTheme(m.color)))
			visible := m.viewport(lineCount)
			limit := max(0, lineCount-visible)
			switch key {
			case "up", "k":
				m.offset = max(0, m.offset-1)
			case "down", "j":
				m.offset = min(limit, m.offset+1)
			case "pgdown", " ":
				m.offset = min(limit, m.offset+visible)
			case "pgup":
				m.offset = max(0, m.offset-visible)
			case "home", "g":
				m.offset = 0
			case "end", "G":
				m.offset = limit
			}
		}
	}
	return m, nil
}

func (m Model) contentWidth() int {
	return m.layout().contentWidth
}

func (m Model) scrollLimit() int {
	if m.page == homePage {
		return 0
	}
	lines := m.pageLines(m.contentWidth(), ui.NewTheme(m.color))
	return max(0, len(lines)-m.viewport(len(lines)))
}
