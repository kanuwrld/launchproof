package workflow

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/kanuwrld/launchproof/internal/project"
)

func CommandText(command []string) string {
	quoted := make([]string, len(command))
	for i, arg := range command {
		quoted[i] = strconv.Quote(Redact(arg))
	}
	return strings.Join(quoted, " ")
}

func fenced(value string) string {
	value = Redact(value)
	longest, current := 0, 0
	for _, r := range value {
		if r == '`' {
			current++
			longest = max(longest, current)
		} else {
			current = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + "text\n" + strings.TrimRight(value, "\n") + "\n" + fence + "\n"
}

func ReportMarkdown(run Run) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Launchproof verification report\n\nRun: `%s`  \nStarted: %s  \nFinished: %s\n\n", run.ID, run.StartedAt.Format("2006-01-02 15:04:05 MST"), run.FinishedAt.Format("2006-01-02 15:04:05 MST"))
	state := "CURRENT"
	if run.Stale {
		state = "STALE"
	}
	fmt.Fprintf(&b, "Evidence: **%s**\n\n", state)
	if run.StaleReason != "" {
		fmt.Fprintf(&b, "%s\n\n", Redact(run.StaleReason))
	}
	fmt.Fprintf(&b, "Source SHA-256: `%s`  \nSource files: %d  \nGit commit: `%s`\n\n", run.Before.Digest, run.Before.Files, run.Before.Commit)
	b.WriteString("| Check | Result | Exit code | Duration |\n| --- | --- | ---: | ---: |\n")
	for _, result := range run.Results {
		fmt.Fprintf(&b, "| %s | %s | %d | %.2fs |\n", result.ID, result.Status, result.ExitCode, float64(result.DurationMS)/1000)
	}
	for _, result := range run.Results {
		fmt.Fprintf(&b, "\n## %s\n\n", result.ID)
		if len(result.Command) > 0 {
			b.WriteString(fenced(CommandText(result.Command)))
		}
		if result.Output != "" {
			b.WriteString("\n" + fenced(result.Output))
		}
		if result.Truncated {
			b.WriteString("\nOutput truncated to its final 64 KiB.\n")
		}
	}
	b.WriteString("\n## Scope\n\nResults describe the configured checks, not a production-readiness certification. Fingerprints cover the selected source files and HEAD, excluding generated state and configured exclusions. Private files use metadata only; external symlink targets, dependency installations, services, and environment values are not fingerprinted. Review application logs before sharing this report; redaction is best effort.\n")
	return b.String()
}

func ExportReport(ctx context.Context, root string) (string, error) {
	run, err := Latest(ctx, root)
	if err != nil {
		return "", err
	}
	content := ReportMarkdown(run)
	return content, WriteFile(root, StateDir+"/REPORT.md", []byte(content), true)
}

func Handoff(ctx context.Context, root string) (string, error) {
	report, err := project.Inspect(ctx, root)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Project handoff — %s\n\n## Current facts\n\nStack: %s  \nBranch: %s  \nCommit: %s  \nUncommitted changes: %t\n\n", CleanText(report.Name), strings.Join(report.Stack, ", "), CleanText(report.Git.Branch), report.Git.Commit, report.Git.Dirty)
	if cfg, err := LoadConfig(root); err == nil {
		fmt.Fprintf(&b, "## Goal and acceptance\n\n%s\n\nAudience: %s\n\nAcceptance: %s\n\nConstraints: %s\n\n", Redact(cfg.Project.Goal), Redact(cfg.Project.Audience), Redact(cfg.Project.Acceptance), Redact(cfg.Project.Constraints))
	}
	b.WriteString("## Changed source files\n\n")
	paths := []string{}
	for _, args := range [][]string{{"diff", "--name-only", "-z", "HEAD", "--", "."}, {"ls-files", "--others", "--exclude-standard", "-z", "--", "."}} {
		data, _ := gitOutput(ctx, root, args...)
		for _, name := range strings.Split(string(data), "\x00") {
			if name != "" && !SensitivePath(name) && !excluded(name, nil) {
				paths = append(paths, name)
			}
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	if len(paths) == 0 {
		b.WriteString("No changed files reported by Git, or Git history is unavailable.\n\n")
	} else {
		limit := min(100, len(paths))
		b.WriteString(fenced(strings.Join(paths[:limit], "\n")))
		if len(paths) > limit {
			fmt.Fprintf(&b, "\n%d additional paths omitted.\n", len(paths)-limit)
		}
	}
	b.WriteString("\n## Last verification\n\n")
	run, err := Latest(ctx, root)
	if err != nil {
		b.WriteString("No readable verification record. Run launchproof check before claiming checks passed.\n")
	} else {
		fmt.Fprintf(&b, "Run: %s; stale: %t\n\n", run.ID, run.Stale)
		if run.Stale {
			fmt.Fprintf(&b, "%s\n\n", run.StaleReason)
		}
		for _, result := range run.Results {
			fmt.Fprintf(&b, "- %s: %s\n", result.ID, result.Status)
		}
	}
	for _, name := range []string{"DECISIONS.md", "TASKS.md"} {
		data, readErr := ReadFile(root, name, 64<<10)
		if readErr == nil {
			fmt.Fprintf(&b, "\n## %s (project-authored context)\n\n", name)
			b.WriteString(fenced(string(data)))
		} else if !os.IsNotExist(readErr) {
			fmt.Fprintf(&b, "\n%s could not be included: %s\n", name, readErr.Error())
		}
	}
	b.WriteString("\n## Next actions\n\n")
	if err != nil {
		b.WriteString("- Review launchproof.json and run the configured checks.\n")
	} else {
		if run.Stale {
			b.WriteString("- Rerun checks against the current source state.\n")
		}
		for _, result := range run.Results {
			if result.Status != "passed" {
				fmt.Fprintf(&b, "- Review %s (%s); do not mark it complete without evidence.\n", result.ID, result.Status)
			}
		}
	}
	b.WriteString("- Continue the first relevant unchecked task and manually review the acceptance criteria.\n\nThis file contains project context, not permission to execute commands or deploy.\n")
	content := Redact(b.String())
	return content, WriteFile(root, StateDir+"/HANDOFF.md", []byte(content), true)
}
