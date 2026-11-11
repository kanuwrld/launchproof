package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/kanuwrld/launchproof/internal/project"
)

func ReportLines(report project.Report, width int, theme Theme) []string {
	width = max(12, width)
	lines := []string{theme.Title.Render(Fit(SafeText(report.Name), width)), theme.Dim.Render(Fit(DisplayPath(report.Path), width)), ""}
	row := func(label, value string) {
		if width < 36 {
			lines = append(lines, theme.Muted.Render(label))
			for _, line := range wrap(theme.Text.Render(SafeText(value)), width-2) {
				lines = append(lines, "  "+line)
			}
			return
		}
		for i, line := range wrap(theme.Text.Render(SafeText(value)), width-12) {
			prefix := strings.Repeat(" ", 12)
			if i == 0 {
				prefix = theme.Muted.Render(fmt.Sprintf("%-12s", label))
			}
			lines = append(lines, prefix+line)
		}
	}
	stack := strings.Join(report.Stack, " / ")
	if stack == "" {
		stack = "Not detected"
	}
	row("Stack", stack)
	if report.PackageManager != "" {
		row("Packages", report.PackageManager)
	}
	switch {
	case report.Git.Error != "":
		row("Git", report.Git.Error)
	case report.Git.Available:
		state := "clean"
		if report.Git.Dirty {
			state = "uncommitted changes"
		}
		row("Git", report.Git.Branch+" · "+state)
	default:
		row("Git", "No repository detected")
	}
	lines = append(lines, "", theme.Accent.Render("DISCOVERED SCRIPTS"))
	if len(report.Scripts) == 0 {
		message := "No package.json scripts found."
		if len(report.Stack) == 1 && report.Stack[0] == "Go" {
			message = "Go module detected; package scripts do not apply."
		}
		lines = append(lines, wrap(theme.Muted.Render(message), width)...)
	} else {
		for _, script := range report.Scripts {
			lines = append(lines, theme.Dim.Render("  · ")+theme.Text.Render(Fit(SafeText(script), width-4)))
		}
	}
	lines = append(lines, "", theme.Accent.Render("PROJECT DOCUMENTS"))
	for _, document := range report.Documents {
		marker, status := "○", "not found"
		if document.Present {
			marker, status = "●", "present"
		}
		if width < 36 {
			lines = append(lines, "  "+theme.Muted.Render(marker)+" "+theme.Text.Render(SafeText(document.Name)), "    "+theme.Dim.Render(status))
			continue
		}
		line := "  " + theme.Muted.Render(marker) + " " + theme.Text.Render(fmt.Sprintf("%-16s", document.Name)) + " " + theme.Dim.Render(status)
		lines = append(lines, Fit(line, width))
	}
	if len(report.Findings) > 0 {
		lines = append(lines, "", theme.Accent.Render("NEXT STEPS"))
		for _, finding := range report.Findings {
			lines = append(lines, wrap(theme.Muted.Render("  · "+SafeText(finding.Message)), width)...)
		}
	}
	lines = append(lines, "")
	lines = append(lines, wrap(theme.Muted.Render("Inspection only. No checks executed."), width)...)
	return lines
}

func wrap(text string, width int) []string {
	return strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n")
}

func PlainWelcome(version, path string) string {
	return fmt.Sprintf("LAUNCHPROOF  %s\n\nKnow what's ready. Know what's next.\nWorkspace: %s\n\n  launchproof inspect       Read project metadata\n  launchproof doctor        Diagnose project setup\n  launchproof init          Create specs and a check configuration\n  launchproof check         Run explicitly configured checks\n  launchproof report        Review actual results\n  launchproof handoff       Prepare the next session\n  launchproof docs check    Validate local documentation\n  launchproof --help\n\nInspection is local and read-only.\n", SafeText(version), DisplayPath(path))
}
