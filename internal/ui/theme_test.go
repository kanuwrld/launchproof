package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/kanuwrld/launchproof/internal/project"
)

func TestSafeTextRemovesTerminalAndDirectionControls(t *testing.T) {
	input := "\x1b[31mred\x1b[0m\x1b]52;c;c2VjcmV0\x07\n\r\t\u202Ename"
	result := SafeText(input)
	if strings.ContainsAny(result, "\x1b\x07\n\r\t\u202E") || strings.Contains(result, "c2VjcmV0") {
		t.Fatalf("unsafe terminal text survived: %q", result)
	}
	if !strings.Contains(result, "red") || !strings.Contains(result, "name") {
		t.Fatalf("ordinary text lost: %q", result)
	}
}

func TestLongReportFitsTerminalWidth(t *testing.T) {
	report := project.Report{
		Name: strings.Repeat("项目", 30), Path: "/projects/" + strings.Repeat("long", 80),
		Stack: []string{"Next.js", "TypeScript"}, Scripts: []string{strings.Repeat("test", 100)},
		Documents: []project.Document{{Name: "README.md", Present: true}},
		Findings:  []project.Finding{{Level: "note", Message: strings.Repeat("A useful next step. ", 30)}},
	}
	for _, width := range []int{20, 40, 80, 120} {
		for _, color := range []bool{false, true} {
			for _, line := range ReportLines(report, width, NewTheme(color)) {
				if lipgloss.Width(line) > width {
					t.Fatalf("line overflows width %d: %q", width, line)
				}
			}
		}
	}
}
