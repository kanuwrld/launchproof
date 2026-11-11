package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kanuwrld/launchproof/internal/project"
)

func TestFrameFillsTerminalAndPinsFooter(t *testing.T) {
	// Column/row counts include 16:9, 7:6, square, portrait and short windows.
	for _, size := range [][2]int{{24, 10}, {32, 12}, {48, 40}, {80, 24}, {96, 48}, {112, 48}, {128, 36}, {160, 24}, {200, 90}, {320, 40}} {
		for _, currentPage := range []page{homePage, reportPage, guidePage} {
			for _, color := range []bool{false, true} {
				m := New(context.Background(), "/workspace/界面-project-with-a-long-name", "0.1.1", color, false)
				m.width, m.height, m.page = size[0], size[1], currentPage
				m.report = project.Report{Name: "project", Stack: []string{"Go"}}
				assertFrame(t, m)
				if currentPage == reportPage {
					m.err = errors.New(strings.Repeat("A long actionable error. ", 30))
					assertFrame(t, m)
				}
			}
		}
	}
}

func assertFrame(t *testing.T, m Model) {
	t.Helper()
	content := ansi.Strip(m.View().Content)
	lines := strings.Split(content, "\n")
	if len(lines) != m.height {
		t.Fatalf("page %d at %dx%d has %d rows", m.page, m.width, m.height, len(lines))
	}
	for i, line := range lines {
		if lipgloss.Width(line) != m.width {
			t.Fatalf("page %d row %d at %dx%d has width %d", m.page, i, m.width, m.height, lipgloss.Width(line))
		}
	}
	footer := m.height - 1 - m.layout().edge
	hint := "q quit"
	if m.page == initPage {
		hint = "ctrl+c quit"
	}
	if !strings.Contains(lines[footer], hint) {
		t.Fatalf("footer moved off row %d on page %d at %dx%d", footer, m.page, m.width, m.height)
	}
	if m.page == homePage {
		for _, item := range []string{"Inspect this project", "Project workspace", "Exit"} {
			if !strings.Contains(content, item) {
				t.Fatalf("menu item %q clipped at %dx%d", item, m.width, m.height)
			}
		}
	}
}

func TestLiveResizeReflowsWithoutResettingSelection(t *testing.T) {
	m := New(context.Background(), "/workspace/Launchproof", "test", false, false)
	m.cursor = 1
	for _, size := range [][3]int{{128, 36, 1}, {96, 48, 0}, {112, 48, 0}, {60, 70, 0}, {160, 24, 1}, {24, 10, 0}, {128, 36, 1}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		content := ansi.Strip(m.View().Content)
		if strings.Contains(content, "│") != (size[2] == 1) {
			t.Fatalf("wrong column arrangement at %dx%d", size[0], size[1])
		}
		if m.cursor != 1 || !strings.Contains(content, "› Project workspace") {
			t.Fatalf("selection lost at %dx%d", size[0], size[1])
		}
		assertFrame(t, m)
	}
}

func TestMenuSurvivesSizesBetweenBreakpoints(t *testing.T) {
	m := New(context.Background(), "/workspace/project", "test", false, false)
	m.cursor = 2
	for width := 24; width <= 176; width += 4 {
		for height := 10; height <= 42; height += 2 {
			m.width, m.height = width, height
			assertFrame(t, m)
		}
	}
}

func TestGuideAndInspectionErrorsCanScrollToEnd(t *testing.T) {
	for _, currentPage := range []page{guidePage, reportPage} {
		m := New(context.Background(), t.TempDir(), "test", false, false)
		m.width, m.height, m.page = 32, 12, currentPage
		m.err = errors.New(strings.Repeat("More detail. ", 30) + "FINAL DETAIL")
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
		m = next.(Model)
		if m.offset == 0 || m.offset != m.scrollLimit() {
			t.Fatal("End must reach the last viewport")
		}
		expected := "assumptions stay visible."
		if currentPage == reportPage {
			expected = "FINAL DETAIL"
		}
		if !strings.Contains(strings.Join(strings.Fields(ansi.Strip(m.View().Content)), " "), expected) {
			t.Fatalf("last content is unreachable on page %d: %s", currentPage, ansi.Strip(m.View().Content))
		}
		next, _ = m.Update(tea.WindowSizeMsg{Width: 180, Height: 60})
		m = next.(Model)
		if m.offset != 0 {
			t.Fatal("growing the viewport should clamp an obsolete scroll offset")
		}
		assertFrame(t, m)
	}
}

func TestTinyTerminalDoesNotPanic(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {1, 1}, {5, 2}, {20, 8}} {
		m := New(context.Background(), "/project", "test", false, false)
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := next.(Model).View()
		if lipgloss.Width(view.Content) > max(1, size[0]) || lipgloss.Height(view.Content) > max(1, size[1]) {
			t.Fatalf("tiny frame overflows %dx%d", size[0], size[1])
		}
	}
}
