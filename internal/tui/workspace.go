package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/kanuwrld/launchproof/internal/project"
	"github.com/kanuwrld/launchproof/internal/ui"
	"github.com/kanuwrld/launchproof/internal/workflow"
)

const (
	workspacePage page = iota + 3
	checksPage
	logsPage
	projectsPage
	browsePage
	documentsPage
	markdownPage
	initPage
	doctorPage
	messagePage
)

var workspaceActions = []string{"Run checks", "Project doctor", "Initialize project", "Read documentation", "Export verification report", "Prepare handoff", "Switch project", "How it works", "Back"}
var formQuestions = []string{"What should this project achieve?", "Who is it for?", "What must work before it is done?", "Any constraints or decisions to preserve?"}

type workspaceState struct {
	cursor               int
	config               workflow.Config
	run                  workflow.Run
	results              []workflow.Result
	events               <-chan workflow.Event
	cancel               context.CancelFunc
	running, quitAfter   bool
	content, title, note string
	returnPage           page
	formStep             int
	answers              []string
	input                string
	recent               []workflow.RecentProject
	entries              []string
	directory            string
	documents            []string
}

type workspaceMsg struct {
	kind              string
	text, title, path string
	err               error
	config            workflow.Config
	recent            []workflow.RecentProject
	files             []string
}
type runEventMsg struct {
	event workflow.Event
	open  bool
}

func awaitRun(events <-chan workflow.Event) tea.Cmd {
	return func() tea.Msg { event, ok := <-events; return runEventMsg{event, ok} }
}

func (m Model) workspaceTask(kind string) tea.Cmd {
	ctx, root := m.ctx, m.path
	return func() tea.Msg {
		msg := workspaceMsg{kind: kind}
		switch kind {
		case "plan":
			msg.config, msg.err = workflow.LoadConfig(root)
		case "doctor":
			var report workflow.DoctorReport
			report, msg.err = workflow.Diagnose(ctx, root)
			msg.text = workflow.DoctorText(report)
			msg.title = "PROJECT DOCTOR"
		case "report":
			msg.text, msg.err = workflow.ExportReport(ctx, root)
			msg.title = "VERIFICATION REPORT"
		case "handoff":
			msg.text, msg.err = workflow.Handoff(ctx, root)
			msg.title = "PROJECT HANDOFF"
		case "docs-check":
			var report workflow.DocsReport
			report, msg.err = workflow.CheckDocs(ctx, root)
			msg.text = workflow.DocsText(report)
			msg.title = "DOCUMENTATION CHECK"
		case "projects":
			msg.recent, msg.err = workflow.RecentProjects()
		case "documents":
			msg.files, msg.err = workflow.SourceFiles(ctx, root, nil)
			msg.files = slices.DeleteFunc(msg.files, func(name string) bool { return !strings.EqualFold(filepath.Ext(name), ".md") })
			for _, name := range []string{workflow.StateDir + "/REPORT.md", workflow.StateDir + "/HANDOFF.md"} {
				if _, err := workflow.ReadFile(root, name, 2<<20); err == nil {
					msg.files = append(msg.files, name)
				}
			}
			slices.Sort(msg.files)
			if len(msg.files) > 500 {
				msg.files = msg.files[:500]
			}
		}
		return msg
	}
}

func (m Model) startRun() (Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(m.ctx)
	events := make(chan workflow.Event, 64)
	cfg, root := m.workspace.config, m.path
	m.workspace.events, m.workspace.cancel, m.workspace.running = events, cancel, true
	m.workspace.run = workflow.Run{}
	m.workspace.note = "Running configured checks. Press x to cancel."
	m.workspace.results = make([]workflow.Result, len(cfg.Checks))
	for i, check := range cfg.Checks {
		m.workspace.results[i] = workflow.Result{ID: check.ID, Status: "pending", ExitCode: -1}
	}
	return m, func() tea.Msg {
		go func() {
			defer close(events)
			_, err := workflow.Execute(ctx, root, cfg, func(event workflow.Event) {
				if event.Kind == "output" {
					select {
					case events <- event:
					default:
					}
				} else {
					events <- event
				}
			})
			if err != nil {
				events <- workflow.Event{Kind: "error", Err: err}
			}
		}()
		event, ok := <-events
		return runEventMsg{event, ok}
	}
}

func (m Model) openWorkspace() Model {
	m.page, m.offset, m.busy = workspacePage, 0, false
	m.workspace.cursor = 0
	m.workspace.note = ""
	return m
}

func (m Model) openAction(index int) (Model, tea.Cmd) {
	m.workspace.cursor = 0
	m.offset = 0
	m.workspace.note = ""
	switch index {
	case 0:
		m.busy = true
		return m, m.workspaceTask("plan")
	case 1:
		m.busy = true
		return m, m.workspaceTask("doctor")
	case 2:
		m.page = initPage
		m.workspace.formStep = 0
		m.workspace.answers = nil
		m.workspace.input = ""
		return m, nil
	case 3:
		m.busy = true
		return m, m.workspaceTask("documents")
	case 4:
		m.busy = true
		m.workspace.returnPage = workspacePage
		return m, m.workspaceTask("report")
	case 5:
		m.busy = true
		m.workspace.returnPage = workspacePage
		return m, m.workspaceTask("handoff")
	case 6:
		m.busy = true
		return m, m.workspaceTask("projects")
	case 7:
		m.page = guidePage
		return m, nil
	default:
		m.page = homePage
		return m, nil
	}
}

func browseFolder(directory string) tea.Cmd {
	return func() tea.Msg {
		msg := workspaceMsg{kind: "browse", path: directory}
		entries, err := os.ReadDir(directory)
		msg.err = err
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != ".git" && entry.Name() != "node_modules" {
				msg.files = append(msg.files, entry.Name())
			}
			if len(msg.files) >= 500 {
				break
			}
		}
		return msg
	}
}
func openProject(directory string) tea.Cmd {
	return func() tea.Msg {
		root, err := workflow.ProjectRoot(directory)
		if err == nil {
			err = workflow.RememberProject(root)
		}
		return workspaceMsg{kind: "opened", path: root, err: err}
	}
}

func (m Model) showDocument(name string, back page) (Model, tea.Cmd) {
	m.busy = true
	m.workspace.returnPage = back
	root := m.path
	return m, func() tea.Msg {
		data, err := workflow.ReadFile(root, name, 2<<20)
		return workspaceMsg{kind: "markdown", title: name, text: workflow.Redact(string(data)), err: err}
	}
}

func (m Model) workspaceUpdate(message tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case workspaceMsg:
		m.busy = false
		m.offset = 0
		if msg.err != nil {
			m.page = messagePage
			m.workspace.title = "ACTION COULD NOT COMPLETE"
			m.workspace.content = workflow.Redact(msg.err.Error())
			return m, nil, true
		}
		switch msg.kind {
		case "plan":
			m.page = checksPage
			m.workspace.config = msg.config
			m.workspace.cursor = 0
			m.workspace.run = workflow.Run{}
			m.workspace.results = nil
			m.workspace.note = "Review commands below. Enter runs them; esc goes back."
		case "doctor", "docs-check":
			m.page = doctorPage
			m.workspace.title = msg.title
			m.workspace.content = msg.text
		case "report", "handoff", "markdown":
			m.page = markdownPage
			m.workspace.title = msg.title
			m.workspace.content = msg.text
		case "projects":
			m.page = projectsPage
			m.workspace.recent = msg.recent
			m.workspace.cursor = 0
		case "browse":
			m.page = browsePage
			m.workspace.directory = msg.path
			m.workspace.entries = msg.files
			m.workspace.cursor = 0
		case "opened":
			m.path = msg.path
			m.report = project.Report{}
			m.err = nil
			m.page = homePage
			m.cursor = 0
			m.workspace = workspaceState{}
		case "documents":
			m.page = documentsPage
			m.workspace.documents = msg.files
			m.workspace.cursor = 0
		case "initialized":
			m.page = messagePage
			m.workspace.title = "PROJECT INITIALIZED"
			m.workspace.content = msg.text
		}
		return m, nil, true
	case runEventMsg:
		if !msg.open {
			m.workspace.running = false
			return m, nil, true
		}
		event := msg.event
		switch event.Kind {
		case "started", "output", "finished":
			if event.Index >= 0 && event.Index < len(m.workspace.results) {
				m.workspace.results[event.Index] = event.Result
			}
			if m.page == logsPage && event.Index == m.workspace.cursor {
				m.workspace.content = checkLog(event.Result)
			}
		case "completed":
			m.workspace.run = event.Run
			m.workspace.running = false
			m.workspace.cancel()
			m.workspace.note = "Run saved. Enter opens logs; r reruns; e exports a report."
			if event.Run.Stale {
				m.workspace.note = "STALE: source changed during this run. e exports evidence."
			}
			if m.workspace.quitAfter {
				return m, tea.Quit, true
			}
			return m, nil, true
		case "error":
			m.workspace.running = false
			m.workspace.cancel()
			m.workspace.note = workflow.Redact(event.Err.Error())
			if m.workspace.quitAfter {
				return m, tea.Quit, true
			}
			return m, nil, true
		}
		return m, awaitRun(m.workspace.events), true
	case tea.KeyPressMsg:
		key := msg.String()
		if m.page == initPage {
			return m.formUpdate(msg)
		}
		if m.workspace.running && (key == "q" || key == "ctrl+c" || key == "x") {
			m.workspace.cancel()
			m.workspace.note = "Cancelling the command and its child processes…"
			m.workspace.quitAfter = key != "x"
			return m, nil, true
		}
		if m.page == homePage {
			shortcuts := map[string]int{"w": -1, "c": 0, "d": 1, "n": 2, "m": 3, "h": 5, "p": 6}
			if action, ok := shortcuts[key]; ok {
				m = m.openWorkspace()
				if action < 0 {
					return m, nil, true
				}
				m, cmd := m.openAction(action)
				return m, cmd, true
			}
			return m, nil, false
		}
		if m.page < workspacePage {
			return m, nil, false
		}
		if key == "q" || key == "ctrl+c" {
			return m, tea.Quit, true
		}
		if key == "esc" || key == "backspace" {
			if m.workspace.running {
				if m.page == logsPage {
					m.page = checksPage
					m.offset = 0
				}
				return m, nil, true
			}
			switch m.page {
			case workspacePage:
				m.page = homePage
			case logsPage:
				m.page = checksPage
			case markdownPage:
				m.page = m.workspace.returnPage
			case browsePage:
				m.page = projectsPage
			default:
				m.page = workspacePage
			}
			m.offset = 0
			m.busy = false
			return m, nil, true
		}
		if m.busy {
			return m, nil, true
		}
		if m.page == checksPage {
			if (key == "enter" && m.workspace.run.ID == "" && !m.workspace.running) || key == "r" && !m.workspace.running {
				m, cmd := m.startRun()
				return m, cmd, true
			}
			if key == "e" && !m.workspace.running && m.workspace.run.ID != "" {
				m.workspace.returnPage = checksPage
				m.busy = true
				return m, m.workspaceTask("report"), true
			}
			if key == "enter" && len(m.workspace.results) > m.workspace.cursor {
				result := m.workspace.results[m.workspace.cursor]
				m.workspace.content = checkLog(result)
				m.workspace.title = "CHECK LOG / " + result.ID
				m.page = logsPage
				m.offset = 0
				return m, nil, true
			}
		}
		if m.page == documentsPage && key == "v" {
			m.busy = true
			return m, m.workspaceTask("docs-check"), true
		}
		if m.page == browsePage && key == "~" {
			directory, err := os.UserHomeDir()
			if err == nil {
				m.busy = true
				return m, browseFolder(directory), true
			}
			return m, nil, true
		}
		count := m.listCount()
		if count > 0 {
			switch key {
			case "up", "k", "shift+tab":
				m.workspace.cursor = (m.workspace.cursor + count - 1) % count
			case "down", "j", "tab":
				m.workspace.cursor = (m.workspace.cursor + 1) % count
			case "home":
				m.workspace.cursor = 0
			case "end":
				m.workspace.cursor = count - 1
			case "enter":
				m, cmd := m.chooseItem()
				return m, cmd, true
			}
			m = m.revealSelection()
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m Model) listCount() int {
	switch m.page {
	case workspacePage:
		return len(workspaceActions)
	case checksPage:
		return len(m.workspace.config.Checks)
	case projectsPage:
		return len(m.workspace.recent) + 1
	case browsePage:
		return len(m.workspace.entries) + 2
	case documentsPage:
		return len(m.workspace.documents)
	}
	return 0
}

func (m Model) chooseItem() (Model, tea.Cmd) {
	index := m.workspace.cursor
	switch m.page {
	case workspacePage:
		return m.openAction(index)
	case projectsPage:
		m.busy = true
		if index == 0 {
			return m, browseFolder(m.path)
		}
		return m, openProject(m.workspace.recent[index-1].Path)
	case browsePage:
		m.busy = true
		if index == 0 {
			return m, openProject(m.workspace.directory)
		}
		directory := filepath.Dir(m.workspace.directory)
		if index > 1 {
			directory = filepath.Join(m.workspace.directory, m.workspace.entries[index-2])
		}
		return m, browseFolder(directory)
	case documentsPage:
		if len(m.workspace.documents) > index {
			return m.showDocument(m.workspace.documents[index], documentsPage)
		}
	}
	return m, nil
}

func (m Model) formUpdate(key tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	if m.busy {
		return m, nil, true
	}
	if m.workspace.formStep >= 4 {
		switch key.String() {
		case "up", "k":
			m.offset = max(0, m.offset-1)
			return m, nil, true
		case "down", "j":
			m.offset = min(m.scrollLimit(), m.offset+1)
			return m, nil, true
		case "pgdown":
			m.offset = min(m.scrollLimit(), m.offset+m.layout().bodyHeight-1)
			return m, nil, true
		case "pgup":
			m.offset = max(0, m.offset-m.layout().bodyHeight+1)
			return m, nil, true
		}
	}

	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit, true
	case "esc":
		return m.openWorkspace(), nil, true
	case "backspace":
		runes := []rune(m.workspace.input)
		if len(runes) > 0 {
			m.workspace.input = string(runes[:len(runes)-1])
		}
	case "enter":
		if m.workspace.formStep < 4 {
			m.workspace.answers = append(m.workspace.answers, strings.TrimSpace(m.workspace.input))
			m.workspace.formStep++
			m.workspace.input = ""
			m.offset = 0
		} else if !m.busy {
			answers := m.workspace.answers
			root, ctx := m.path, m.ctx
			m.busy = true
			return m, func() tea.Msg {
				result, err := workflow.Initialize(ctx, root, workflow.Definition{Goal: answers[0], Audience: answers[1], Acceptance: answers[2], Constraints: answers[3]})
				return workspaceMsg{kind: "initialized", text: "Created:\n" + strings.Join(result.Created, "\n") + "\n\nPreserved:\n" + strings.Join(result.Preserved, "\n") + "\n\nReview launchproof.json, then open Run checks.", err: err}
			}, true
		}
	default:
		if m.workspace.formStep < 4 && len(m.workspace.input) < 4000 {
			m.workspace.input += strings.Map(func(r rune) rune {
				if unicode.IsPrint(r) {
					return r
				}
				return -1
			}, key.Text)
		}
	}
	return m, nil, true
}

func (m Model) workspaceLines(width int, t ui.Theme) []string {
	list := func(title string, items []string) []string {
		lines := append(wrapped(t.Muted.Render(ui.SafeText(title)), width), "")
		for i, item := range items {
			prefix, style := "  ", t.Text
			if i == m.workspace.cursor {
				prefix, style = "› ", t.Selected
			}
			lines = append(lines, style.Render(ui.Fit(prefix+ui.SafeText(item), width)))
		}
		if m.busy {
			lines = append(lines, "", t.Dim.Render("Loading…"))
		}
		return lines
	}
	switch m.page {
	case workspacePage:
		return list("Choose a project action", workspaceActions)
	case projectsPage:
		items := []string{"Browse folders…"}
		for _, item := range m.workspace.recent {
			items = append(items, ui.DisplayPath(item.Path))
		}
		return list("Recent projects — stored only on this machine", items)
	case browsePage:
		return list(ui.DisplayPath(m.workspace.directory), append([]string{"Use this folder", ".."}, m.workspace.entries...))
	case documentsPage:
		if len(m.workspace.documents) == 0 {
			return []string{"No Markdown documents found.", "Initialize the project to create SPEC.md and TASKS.md."}
		}
		return list("Enter reads a document · v validates documentation", m.workspace.documents)
	case checksPage:
		lines := wrapped(t.Muted.Render(m.workspace.note), width)
		lines = append(lines, "")
		for i, check := range m.workspace.config.Checks {
			status := "ready"
			if check.SkipReason != "" {
				status = "skipped"
			}
			duration := ""
			if len(m.workspace.results) > i {
				status = m.workspace.results[i].Status
				duration = fmt.Sprintf(" %.2fs", float64(m.workspace.results[i].DurationMS)/1000)
			}
			prefix, style := "  ", t.Text
			if i == m.workspace.cursor {
				prefix, style = "› ", t.Selected
			}
			lines = append(lines, style.Render(ui.Fit(fmt.Sprintf("%s%-14s %s%s", prefix, check.ID, status, duration), width)))
			detail := workflow.CommandText(check.Command)
			if check.Builtin != "" {
				detail = "builtin: " + check.Builtin
			}
			if check.SkipReason != "" {
				detail = check.SkipReason
			}
			lines = append(lines, t.Dim.Render(ui.Fit("    "+detail, width)))
		}
		return lines
	case initPage:
		if m.workspace.formStep < 4 {
			return append([]string{t.Accent.Render(fmt.Sprintf("PROJECT SETUP · %d / 4", m.workspace.formStep+1)), ""}, append(wrapped(formQuestions[m.workspace.formStep], width), append([]string{""}, append(wrapped(t.Text.Render("> "+m.workspace.input+"▏"), width), "", "Enter continues. Empty answers remain explicitly undecided.")...)...)...)
		}
		lines := []string{t.Accent.Render("REVIEW PROJECT SETUP"), ""}
		for i, answer := range m.workspace.answers {
			if answer == "" {
				answer = "To be decided"
			}
			lines = append(lines, wrapped(t.Muted.Render(formQuestions[i]), width)...)
			lines = append(lines, wrapped(answer, width)...)
			lines = append(lines, "")
		}
		lines = append(lines, "Create launchproof.json, SPEC.md, TASKS.md and AGENTS.md.", "Existing files are preserved. .gitignore gains /.launchproof/.", "", "Press Enter to create; Esc goes back.")
		return lines
	case markdownPage:
		return markdownLines(m.workspace.content, width, t)
	default:
		return wrapped(t.Text.Render(workflow.Redact(m.workspace.content)), width)
	}
}

func markdownLines(content string, width int, t ui.Theme) []string {
	var lines []string
	inCode := false
	fence := ""
	for _, line := range strings.Split(workflow.Redact(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inCode && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			inCode = true
			fence = trimmed[:3]
			lines = append(lines, t.Dim.Render("── code ──"))
			continue
		}
		if inCode && strings.HasPrefix(trimmed, fence) {
			inCode = false
			lines = append(lines, "")
			continue
		}
		style := t.Text
		if inCode {
			style = t.Muted
		} else if strings.HasPrefix(trimmed, "#") {
			style = t.Accent
			line = strings.TrimLeft(trimmed, "# ")
		}
		lines = append(lines, wrapped(style.Render(line), width)...)
	}
	return lines
}

func (m Model) workspaceTitle() string {
	switch m.page {
	case workspacePage:
		return "PROJECT WORKSPACE"
	case checksPage:
		return "CHECKS / EXECUTION"
	case projectsPage:
		return "SWITCH PROJECT"
	case browsePage:
		return "PROJECT FOLDER"
	case documentsPage:
		return "PROJECT DOCUMENTS"
	case initPage:
		return "PROJECT SETUP"
	default:
		if m.workspace.title != "" {
			return ui.SafeText(m.workspace.title)
		}
	}
	return "LAUNCHPROOF"
}

func (m Model) workspaceFooter(width int) string {
	if m.page == documentsPage {
		return helpText(width, "↑/↓ select    enter read    v validate    esc back    q quit", "enter read · v check · q quit", "esc back · q quit")
	}
	if (m.page == logsPage || m.page == markdownPage || m.page == doctorPage || m.page == messagePage) && !m.workspace.running {
		return helpText(width, "↑/↓ scroll    pgup/pgdown page    esc back    q quit", "↑/↓ scroll · esc · q quit", "esc back · q quit")
	}
	if m.page == initPage {
		return helpText(width, "enter continue    esc cancel    ctrl+c quit", "enter · esc back · ctrl+c quit", "ctrl+c quit")
	}
	if m.workspace.running {
		return helpText(width, "↑/↓ select    enter logs    x cancel    q quit", "x cancel · q quit")
	}
	if m.page == checksPage {
		return helpText(width, "enter run/logs    r rerun    e export    esc back    q quit", "enter run/logs · esc · q quit", "esc back · q quit")
	}
	if m.page == browsePage {
		return helpText(width, "↑/↓ select    enter open/use    ~ home    esc back    q quit", "enter open · esc · q quit", "esc back · q quit")
	}
	return helpText(width, "↑/↓ navigate    enter open    esc back    q quit", "↑/↓ · enter · esc · q quit", "esc back · q quit")
}

func checkLog(result workflow.Result) string {
	if result.Output == "" {
		result.Output = "This check produced no output."
		if result.Status == "running" || result.Status == "pending" {
			result.Output = "Waiting for command output…"
		}
	}
	return fmt.Sprintf("%s · %s\n\n%s\n\n%s", result.ID, result.Status, workflow.CommandText(result.Command), result.Output)
}

func (m Model) selectionLine() int {
	width := m.contentWidth()
	title := "Choose a project action"
	switch m.page {
	case checksPage:
		return len(wrapped(m.workspace.note, width)) + 1 + 2*m.workspace.cursor
	case projectsPage:
		title = "Recent projects — stored only on this machine"
	case browsePage:
		title = ui.DisplayPath(m.workspace.directory)
	case documentsPage:
		title = "Enter reads a document · v validates documentation"
	}
	return len(wrapped(title, width)) + 1 + m.workspace.cursor
}

func (m Model) revealSelection() Model {
	line := m.selectionLine()
	visible := m.viewport(len(m.pageLines(m.contentWidth(), ui.NewTheme(m.color))))
	if line < m.offset {
		m.offset = line
	}
	if line >= m.offset+visible {
		m.offset = line - visible + 1
	}
	m.offset = min(m.offset, m.scrollLimit())
	return m
}
