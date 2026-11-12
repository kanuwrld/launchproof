package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ChangedFile struct {
	Path     string `json:"path"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
}

type Changes struct {
	Available bool          `json:"available"`
	Branch    string        `json:"branch,omitempty"`
	Commit    string        `json:"commit,omitempty"`
	Files     []ChangedFile `json:"files"`
	Hidden    int           `json:"hidden"`
	Omitted   int           `json:"omitted"`
	Note      string        `json:"note,omitempty"`
}

type EvidenceStatus struct {
	State      string     `json:"state"`
	RunID      string     `json:"run_id,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Reason     string     `json:"reason,omitempty"`
}

type CheckStatus struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	LastResult string `json:"last_result,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type RequirementStatus struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Checks      []string `json:"checks"`
	Status      string   `json:"status"`
}

type ProjectStatus struct {
	SchemaVersion  int                 `json:"schema_version"`
	Name           string              `json:"name"`
	Configuration  string              `json:"configuration"`
	ConfigError    string              `json:"config_error,omitempty"`
	Changes        Changes             `json:"changes"`
	Evidence       EvidenceStatus      `json:"evidence"`
	Checks         []CheckStatus       `json:"checks"`
	Requirements   []RequirementStatus `json:"requirements"`
	NextActions    []string            `json:"next_actions"`
	ChecksExecuted bool                `json:"checks_executed"`
}

// Passes describes the configured scope only, never release approval.
func (s ProjectStatus) Passes() bool {
	if s.Configuration != "valid" || s.Evidence.State != "current" || len(s.Checks) == 0 {
		return false
	}
	for _, check := range s.Checks {
		if check.Status != "passed" {
			return false
		}
	}
	for _, requirement := range s.Requirements {
		if requirement.Status != "evidenced" {
			return false
		}
	}
	return true
}

// Status reads configuration, Git metadata and saved evidence without running checks or writing files.
func Status(ctx context.Context, root string) (ProjectStatus, error) {
	s := ProjectStatus{
		SchemaVersion: 1, Name: Redact(filepath.Base(root)), Configuration: "valid",
		Evidence: EvidenceStatus{State: "none"}, Checks: []CheckStatus{},
		Requirements: []RequirementStatus{}, NextActions: []string{},
	}
	cfg, configErr := LoadConfig(root)
	var excludes []string
	if configErr == nil {
		excludes = cfg.Exclude
	}
	s.Changes = changedFiles(ctx, root, excludes)
	if err := ctx.Err(); err != nil {
		return s, err
	}
	if configErr != nil {
		s.Configuration, s.ConfigError = "invalid", Redact(configErr.Error())
		s.NextActions = append(s.NextActions, "Fix launchproof.json before running checks.")
		if errors.Is(configErr, os.ErrNotExist) {
			s.Configuration = "missing"
			s.NextActions = []string{"Run launchproof init, then review launchproof.json."}
		}
		return s, nil
	}
	run, runErr := Latest(ctx, root)
	if err := ctx.Err(); err != nil {
		return s, err
	}
	if runErr != nil {
		s.Evidence.Reason = Redact(runErr.Error())
		if !errors.Is(runErr, os.ErrNotExist) {
			s.Evidence.State = "unreadable"
		}
	} else {
		s.Evidence = EvidenceStatus{State: "current", RunID: Redact(run.ID), FinishedAt: &run.FinishedAt}
		if run.Stale || run.SnapshotError != "" {
			s.Evidence.State, s.Evidence.Reason = "stale", Redact(run.StaleReason)
		}
	}
	results := map[string]Result{}
	duplicates := map[string]bool{}
	for _, result := range run.Results {
		if _, exists := results[result.ID]; exists {
			duplicates[result.ID] = true
		}
		results[result.ID] = result
	}
	states := map[string]string{}
	for _, check := range cfg.Checks {
		status := CheckStatus{ID: check.ID, Status: "not_run"}
		if check.SkipReason != "" {
			status.Status, status.Reason = "skipped", Redact(check.SkipReason)
		}
		if result, exists := results[check.ID]; exists {
			status.LastResult = Redact(result.Status)
			switch {
			case duplicates[check.ID]:
				status.Status, status.Reason = "unverified", "Duplicate results in the saved run."
			case s.Evidence.State == "stale":
				status.Status, status.Reason = "stale", s.Evidence.Reason
			case result.Status == "passed" && result.ExitCode == 0 && check.SkipReason == "":
				status.Status = "passed"
			case result.Status == "skipped":
				status.Status, status.Reason = "skipped", "Not executed in the latest run."
			case result.Status == "failed" || result.Status == "cancelled" || result.Status == "timed_out":
				status.Status, status.Reason = result.Status, "Read launchproof report for the saved output."
			default:
				status.Status, status.Reason = "unverified", "The saved result is not a completed passing check."
			}
		}
		states[check.ID] = status.Status
		s.Checks = append(s.Checks, status)
	}
	for _, requirement := range cfg.Requirements {
		status := RequirementStatus{ID: requirement.ID, Description: Redact(requirement.Description), Checks: append([]string{}, requirement.Checks...), Status: "evidenced"}
		if len(requirement.Checks) == 0 {
			status.Status = "unmapped"
		} else {
			for _, id := range requirement.Checks {
				if states[id] != "passed" {
					status.Status = "unverified"
					break
				}
			}
		}
		s.Requirements = append(s.Requirements, status)
	}
	if s.Evidence.State != "current" {
		s.NextActions = append(s.NextActions, "Review launchproof check --dry-run, then run launchproof check for current evidence.")
	} else {
		for _, check := range s.Checks {
			switch check.Status {
			case "passed":
			case "skipped", "not_run":
				s.NextActions = append(s.NextActions, fmt.Sprintf("Resolve skipped or missing check %s in launchproof.json, then run the full launchproof check.", check.ID))
			default:
				s.NextActions = append(s.NextActions, fmt.Sprintf("Review %s with launchproof report, fix its cause, then rerun launchproof check.", check.ID))
			}
		}
	}
	for _, requirement := range s.Requirements {
		if requirement.Status == "unmapped" {
			s.NextActions = append(s.NextActions, fmt.Sprintf("Link requirement %s to checks that exercise it in launchproof.json.", requirement.ID))
		}
	}
	if len(s.Requirements) == 0 {
		s.NextActions = append(s.NextActions, "Add requirements and their check IDs to launchproof.json to track acceptance evidence.")
	}
	if s.Passes() {
		s.NextActions = append(s.NextActions, "Review acceptance beyond the configured checks before release.")
	}
	return s, nil
}

func changedFiles(ctx context.Context, root string, excludes []string) Changes {
	changes := Changes{Files: []ChangedFile{}}
	if data, err := gitOutput(ctx, root, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(string(data)) != "true" {
		changes.Note = "No Git worktree detected; source verification is still available."
		return changes
	}
	changes.Available = true
	branch, err := gitOutput(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	changes.Branch = Redact(strings.TrimSpace(string(branch)))
	if err != nil {
		changes.Branch = "HEAD (detached)"
	}
	commit, _ := gitOutput(ctx, root, "rev-parse", "--verify", "HEAD")
	changes.Commit = strings.TrimSpace(string(commit))
	prefix, err := gitOutput(ctx, root, "rev-parse", "--show-prefix")
	if err != nil {
		changes.Note = "Could not determine the project scope in Git."
		return changes
	}
	if _, err := gitOutput(ctx, root, "check-ignore", "--quiet", "."); err == nil {
		changes.Note = "This directory is ignored by its parent repository; Git cannot list its source changes."
		return changes
	}
	data, err := gitOutput(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames", "--", ".")
	if err != nil {
		changes.Note = "Could not read Git changes."
		return changes
	}
	return parseChanges(data, strings.TrimSuffix(string(prefix), "\n"), excludes, changes)
}

func parseChanges(data []byte, prefix string, excludes []string, changes Changes) Changes {
	for _, entry := range strings.Split(string(data), "\x00") {
		if len(entry) < 4 || entry[2] != ' ' || !strings.HasPrefix(entry[3:], prefix) {
			continue
		}
		path := strings.TrimPrefix(entry[3:], prefix)
		if !filepath.IsLocal(path) || excluded(path, excludes) {
			continue
		}
		if SensitivePath(path) {
			changes.Hidden++
			continue
		}
		if len(changes.Files) >= 200 {
			changes.Omitted++
			continue
		}
		changes.Files = append(changes.Files, ChangedFile{Path: Redact(path), Index: entry[:1], Worktree: entry[1:2]})
	}
	return changes
}

func StatusText(s ProjectStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", s.Name)
	if s.Passes() {
		b.WriteString("Configured checks passed with current evidence.\n")
	} else {
		b.WriteString("Verification needs attention.\n")
	}
	fmt.Fprintf(&b, "Configuration: %s\nEvidence: %s\n", s.Configuration, s.Evidence.State)
	if s.ConfigError != "" {
		fmt.Fprintf(&b, "%s\n", s.ConfigError)
	}
	if s.Evidence.RunID != "" {
		fmt.Fprintf(&b, "Latest run: %s\n", s.Evidence.RunID)
	}
	if s.Evidence.Reason != "" {
		fmt.Fprintf(&b, "%s\n", s.Evidence.Reason)
	}
	b.WriteString("\nNEXT ACTIONS\n")
	for _, action := range s.NextActions {
		fmt.Fprintf(&b, "  - %s\n", action)
	}
	b.WriteString("\nCHECKS\n")
	for _, check := range s.Checks {
		fmt.Fprintf(&b, "  %-16s %s", check.ID, check.Status)
		if check.Status == "stale" {
			fmt.Fprintf(&b, " (last: %s)", check.LastResult)
		}
		b.WriteByte('\n')
		if check.Reason != "" && check.Status != "stale" {
			fmt.Fprintf(&b, "    %s\n", check.Reason)
		}
	}
	b.WriteString("\nREQUIREMENTS\n")
	if len(s.Requirements) == 0 {
		b.WriteString("No requirement-to-check mapping configured.\n")
	}
	for _, requirement := range s.Requirements {
		fmt.Fprintf(&b, "  %s: %s\n    %s\n", requirement.ID, requirement.Status, requirement.Description)
		if len(requirement.Checks) > 0 {
			fmt.Fprintf(&b, "    Checks: %s\n", strings.Join(requirement.Checks, ", "))
		}
	}
	b.WriteString("\nGIT CHANGES\n")
	if s.Changes.Available {
		fmt.Fprintf(&b, "Branch: %s\n", s.Changes.Branch)
	}
	if s.Changes.Note != "" {
		fmt.Fprintln(&b, s.Changes.Note)
	} else if len(s.Changes.Files) == 0 {
		b.WriteString("No visible source changes.\n")
	}
	if len(s.Changes.Files) > 0 {
		b.WriteString("Index / worktree (? = untracked):\n")
		for _, file := range s.Changes.Files {
			fmt.Fprintf(&b, "  %s%s  %q\n", file.Index, file.Worktree, file.Path)
		}
	}
	if s.Changes.Hidden > 0 || s.Changes.Omitted > 0 {
		fmt.Fprintf(&b, "%d sensitive paths hidden; %d additional paths omitted.\n", s.Changes.Hidden, s.Changes.Omitted)
	}
	b.WriteString("\nRead-only. No checks executed. Passing linked checks is evidence of their configured scope, not proof that a requirement is fully implemented.\n")
	return b.String()
}
