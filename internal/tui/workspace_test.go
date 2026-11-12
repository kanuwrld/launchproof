package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kanuwrld/launchproof/internal/workflow"
)

func key(m Model, code rune, text string) (Model, tea.Cmd) {
	next, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: text})
	return next.(Model), cmd
}
func resolve(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func TestWorkspaceExecutionAndDocuments(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LAUNCHPROOF_HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# A working project\n\nRead this locally."), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.Initialize(context.Background(), root, workflow.Definition{Goal: "Ship a usable CLI"}); err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), root, "test", false, false)
	m, cmd := key(m, 'c', "c")
	m = resolve(m, cmd)
	if m.page != checksPage || m.workspace.running {
		t.Fatal("preview must not execute commands")
	}
	m, cmd = key(m, tea.KeyEnter, "")
	if !m.workspace.running || cmd == nil {
		t.Fatal("enter should begin the reviewed run")
	}
	for cmd != nil {
		next, nextCmd := m.Update(cmd())
		m = next.(Model)
		cmd = nextCmd
	}
	if m.workspace.running || m.workspace.run.ID == "" || !m.workspace.run.Success() {
		t.Fatalf("did not save successful built-in checks: %+v", m.workspace.run)
	}
	m, cmd = key(m, tea.KeyEnter, "")
	m = resolve(m, cmd)
	if m.page != logsPage || !strings.Contains(m.workspace.content, "passed") {
		t.Fatal("cannot read check logs")
	}
	m, _ = key(m, tea.KeyEscape, "")
	m, cmd = key(m, 'e', "e")
	m = resolve(m, cmd)
	if m.page != markdownPage || !strings.Contains(m.workspace.content, "verification report") {
		t.Fatalf("cannot export report: %s", m.workspace.content)
	}
	m, _ = key(m, tea.KeyEscape, "")
	m, _ = key(m, tea.KeyEscape, "")
	m, _ = key(m, tea.KeyEscape, "")
	m, cmd = key(m, 'm', "m")
	m = resolve(m, cmd)
	if m.page != documentsPage || len(m.workspace.documents) < 3 {
		t.Fatal("document list is empty")
	}
	m, cmd = key(m, tea.KeyEnter, "")
	m = resolve(m, cmd)
	if m.page != markdownPage {
		t.Fatal("cannot open Markdown")
	}
}

func TestProjectPickerAndInitForm(t *testing.T) {
	t.Setenv("LAUNCHPROOF_HOME", t.TempDir())
	root := t.TempDir()
	child := filepath.Join(root, "nested-project")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), root, "test", false, false)
	m, cmd := key(m, 'p', "p")
	m = resolve(m, cmd)
	m, cmd = key(m, tea.KeyEnter, "")
	m = resolve(m, cmd)
	if m.page != browsePage {
		t.Fatal("folder picker did not open")
	}
	m.workspace.cursor = 2
	m, cmd = key(m, tea.KeyEnter, "")
	m = resolve(m, cmd)
	m, cmd = key(m, tea.KeyEnter, "")
	m = resolve(m, cmd)
	resolvedChild, _ := workflow.ProjectRoot(child)
	if m.page != homePage || m.path != resolvedChild {
		t.Fatal("did not switch to selected folder")
	}
	m, _ = key(m, 'n', "n")
	for _, answer := range []string{"Quality shipping", "Developers", "All checks pass", "Preserve existing data"} {
		for _, r := range answer {
			m, _ = key(m, r, string(r))
		}
		m, _ = key(m, tea.KeyEnter, "")
	}
	if m.workspace.formStep != 4 || m.workspace.answers[0] != "Quality shipping" {
		t.Fatal("form lost input or treated q as quit")
	}
	m.width, m.height = 32, 12
	for i := 0; i < 100; i++ {
		m, _ = key(m, tea.KeyDown, "")
	}
	if m.offset != m.scrollLimit() {
		t.Fatal("setup review cannot scroll to its end")
	}
	m, cmd = key(m, tea.KeyEnter, "")
	m = resolve(m, cmd)
	cfg, err := workflow.LoadConfig(child)
	if err != nil || cfg.Project.Goal != "Quality shipping" {
		t.Fatalf("setup failed: %v", err)
	}
}

func TestWorkspacePagesStayInsideFrame(t *testing.T) {
	for _, p := range []page{workspacePage, checksPage, logsPage, projectsPage, browsePage, documentsPage, markdownPage, initPage, doctorPage, messagePage, statusPage} {
		for _, size := range [][2]int{{24, 10}, {32, 12}, {60, 40}, {80, 24}, {120, 50}, {180, 50}, {240, 24}} {
			m := New(context.Background(), "/project", "test", true, false)
			m.page = p
			m.width, m.height = size[0], size[1]
			m.workspace.content = strings.Repeat("Long details with Unicode 界 and link descriptions.\n", 60)
			m.workspace.note = strings.Repeat("Review the commands. ", 10)
			m.workspace.documents = []string{"README.md", strings.Repeat("very-long-name", 30) + ".md"}
			m.workspace.entries = []string{strings.Repeat("folder", 40)}
			m.workspace.directory = strings.Repeat("/project", 40)
			m.workspace.config = workflow.Config{SchemaVersion: 1, Checks: []workflow.Check{{ID: "docs", Builtin: "docs"}}}
			m.workspace.answers = []string{strings.Repeat("Long goal ", 40), "Audience", "Acceptance", "Constraints"}
			m.workspace.formStep = 4
			assertFrame(t, m)
			if !strings.Contains(ansi.Strip(m.View().Content), "quit") {
				t.Fatalf("quit hint missing on %d %v", p, size)
			}
		}
	}
}

func TestCancellationWaitsForEvidence(t *testing.T) {
	m := New(context.Background(), t.TempDir(), "test", false, false)
	m.page = checksPage
	cancelled := false
	m.workspace.running = true
	m.workspace.cancel = func() { cancelled = true }
	m, cmd := key(m, 'q', "q")
	if !cancelled || !m.workspace.quitAfter || cmd != nil {
		t.Fatal("quit must cancel first and wait for persisted evidence")
	}
	next, cmd := m.Update(runEventMsg{event: workflow.Event{Kind: "completed", Run: workflow.Run{ID: "saved"}}, open: true})
	m = next.(Model)
	if m.workspace.running || cmd == nil {
		t.Fatal("completion should release the terminal")
	}
}

func TestResizeKeepsWorkspaceSelectionVisible(t *testing.T) {
	m := New(context.Background(), t.TempDir(), "test", false, false).openWorkspace()
	m.workspace.cursor = len(workspaceActions) - 1
	next, _ := m.Update(tea.WindowSizeMsg{Width: 32, Height: 12})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "› Back") {
		t.Fatal("selected action disappeared after shrinking")
	}
}

func TestStatusShortcutRefreshAndCheckPreview(t *testing.T) {
	root := t.TempDir()
	if _, err := workflow.Initialize(context.Background(), root, workflow.Definition{}); err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), root, "test", false, false)
	m, cmd := key(m, 's', "s")
	m = resolve(m, cmd)
	if m.page != statusPage || !strings.Contains(m.workspace.content, "Evidence: none") {
		t.Fatalf("status shortcut failed: %s", m.workspace.content)
	}
	if _, err := os.Stat(filepath.Join(root, workflow.StateDir)); !os.IsNotExist(err) {
		t.Fatal("opening status executed checks")
	}
	cfg, err := workflow.LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.Execute(context.Background(), root, cfg, nil); err != nil {
		t.Fatal(err)
	}
	m, cmd = key(m, 'r', "r")
	m = resolve(m, cmd)
	if m.page != statusPage || !strings.Contains(m.workspace.content, "Evidence: current") {
		t.Fatal("status refresh lost current evidence")
	}
	m, cmd = key(m, 'c', "c")
	m = resolve(m, cmd)
	if m.page != checksPage || m.workspace.running || m.workspace.run.ID != "" {
		t.Fatal("status shortcut must open a plan, not start checks")
	}
}
