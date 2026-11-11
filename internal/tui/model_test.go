package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kanuwrld/launchproof/internal/project"
)

func TestViewsFitSmallAndLargeTerminals(t *testing.T) {
	for _, size := range [][2]int{{20, 8}, {24, 10}, {32, 12}, {40, 16}, {60, 24}, {80, 24}, {100, 32}, {160, 50}} {
		for _, color := range []bool{false, true} {
			for _, currentPage := range []page{homePage, reportPage, guidePage} {
				m := New(context.Background(), "/a/very-long-project-name", "0.1.0-dev", color, false)
				m.width, m.height, m.page = size[0], size[1], currentPage
				m.report = project.Report{Name: strings.Repeat("Project ", 30), Stack: []string{"Next.js"}}
				view := m.View()
				if !view.AltScreen {
					t.Fatal("interactive view must use the alternate screen")
				}
				if lipgloss.Width(view.Content) > size[0] || lipgloss.Height(view.Content) > size[1] {
					t.Fatalf("view %d overflows %dx%d: %dx%d", currentPage, size[0], size[1], lipgloss.Width(view.Content), lipgloss.Height(view.Content))
				}
				if !strings.Contains(ansi.Strip(view.Content), "q quit") {
					t.Fatalf("exit hint disappeared for view %d at %dx%d", currentPage, size[0], size[1])
				}
			}
		}
	}
}

func TestWelcomeNavigationAndInspect(t *testing.T) {
	m := New(context.Background(), t.TempDir(), "test", false, false)
	if m.Init() != nil {
		t.Fatal("reduced-motion mode should not schedule animation")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	if m.cursor != 1 {
		t.Fatal("down arrow must select the guide")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.page != guidePage {
		t.Fatal("enter should open the guide")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.page != homePage {
		t.Fatal("escape should return home")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m = next.(Model)
	if cmd == nil || !m.busy {
		t.Fatal("inspect must start a background command")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.busy || m.page != reportPage || m.err != nil {
		t.Fatalf("inspection did not finish: %+v", m)
	}
}

func TestAnimationStopsAfterGreeting(t *testing.T) {
	m := New(context.Background(), t.TempDir(), "test", true, true)
	m.frame = 59
	next, cmd := m.Update(tickMsg{})
	if next.(Model).frame != 60 || cmd != nil {
		t.Fatal("welcome animation should stop instead of consuming idle CPU")
	}
}
