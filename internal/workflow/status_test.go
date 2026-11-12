package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatusTracksRequirementsAndSourceWithoutWriting(t *testing.T) {
	root := t.TempDir()
	put(t, root, "README.md", "# Working documentation\n")
	cfg := Config{SchemaVersion: 1, Checks: []Check{{ID: "docs", Builtin: "docs"}}, Requirements: []Requirement{
		{ID: "documentation", Description: "Local documentation links resolve", Checks: []string{"docs"}},
		{ID: "acceptance", Description: "The user can complete the main flow", Checks: []string{}},
	}}
	configFile(t, root, cfg)
	status, err := Status(context.Background(), root)
	if err != nil || status.Evidence.State != "none" || status.Passes() || status.ChecksExecuted || status.Checks[0].Status != "not_run" {
		t.Fatalf("unexpected initial status: %v %+v", err, status)
	}
	if _, err := os.Stat(filepath.Join(root, StateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only status created state")
	}
	if _, err := Execute(context.Background(), root, cfg, nil); err != nil {
		t.Fatal(err)
	}
	status, err = Status(context.Background(), root)
	if err != nil || status.Passes() || status.Requirements[0].Status != "evidenced" || status.Requirements[1].Status != "unmapped" {
		t.Fatalf("unmapped acceptance must stay unresolved: %v %+v", err, status)
	}
	cfg.Requirements[1].Checks = []string{"docs"}
	configFile(t, root, cfg)
	status, err = Status(context.Background(), root)
	if err != nil || status.Evidence.State != "stale" || status.Requirements[0].Status != "unverified" {
		t.Fatalf("mapping edits must invalidate evidence: %v %+v", err, status)
	}
	if _, err := Execute(context.Background(), root, cfg, nil); err != nil {
		t.Fatal(err)
	}
	status, err = Status(context.Background(), root)
	if err != nil || !status.Passes() {
		t.Fatalf("current complete evidence did not pass: %v %+v", err, status)
	}
	put(t, root, "README.md", "# Changed after verification\n")
	status, err = Status(context.Background(), root)
	if err != nil || status.Passes() || status.Checks[0].Status != "stale" || status.Checks[0].LastResult != "passed" {
		t.Fatalf("old success must not certify changed source: %v %+v", err, status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Status(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was lost: %v", err)
	}
}

func TestStatusRejectsIncompleteAndInvalidResults(t *testing.T) {
	for _, test := range []struct {
		name    string
		results []Result
		want    string
	}{
		{"missing", nil, "not_run"},
		{"skipped", []Result{{ID: "docs", Status: "skipped", ExitCode: -1}}, "skipped"},
		{"failed", []Result{{ID: "docs", Status: "failed", ExitCode: 1}}, "failed"},
		{"cancelled", []Result{{ID: "docs", Status: "cancelled", ExitCode: -1}}, "cancelled"},
		{"timed_out", []Result{{ID: "docs", Status: "timed_out", ExitCode: -1}}, "timed_out"},
		{"contradictory", []Result{{ID: "docs", Status: "passed", ExitCode: 1}}, "unverified"},
		{"unknown", []Result{{ID: "docs", Status: "running", ExitCode: 0}}, "unverified"},
		{"duplicate", []Result{{ID: "docs", Status: "failed", ExitCode: 1}, {ID: "docs", Status: "passed", ExitCode: 0}}, "unverified"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := Config{SchemaVersion: 1, Checks: []Check{{ID: "docs", Builtin: "docs"}}, Requirements: []Requirement{{ID: "links", Description: "Links resolve", Checks: []string{"docs"}}}}
			configFile(t, root, cfg)
			snapshot, err := Fingerprint(context.Background(), root, nil)
			if err != nil {
				t.Fatal(err)
			}
			run := Run{SchemaVersion: 1, ID: "fixture", Before: snapshot, After: snapshot, FinishedAt: time.Now().UTC(), Results: test.results}
			if err := SaveRun(root, run); err != nil {
				t.Fatal(err)
			}
			status, err := Status(context.Background(), root)
			if err != nil || status.Passes() || status.Checks[0].Status != test.want || status.Requirements[0].Status != "unverified" {
				t.Fatalf("incomplete evidence accepted: %v %+v", err, status)
			}
		})
	}
}

func TestStatusHandlesMissingInvalidAndUnreadableState(t *testing.T) {
	root := t.TempDir()
	status, err := Status(context.Background(), root)
	if err != nil || status.Configuration != "missing" || status.Passes() {
		t.Fatalf("missing config: %v %+v", err, status)
	}
	put(t, root, ConfigName, "{broken")
	status, err = Status(context.Background(), root)
	if err != nil || status.Configuration != "invalid" || status.Passes() {
		t.Fatalf("invalid config: %v %+v", err, status)
	}
	configFile(t, root, Config{SchemaVersion: 1, Checks: []Check{{ID: "never", Command: []string{"this-command-must-not-be-executed"}}}})
	if err := WriteFile(root, StateDir+"/latest.json", []byte("{broken"), false); err != nil {
		t.Fatal(err)
	}
	status, err = Status(context.Background(), root)
	if err != nil || status.Evidence.State != "unreadable" || status.Passes() {
		t.Fatalf("unreadable state: %v %+v", err, status)
	}
	data, _ := ReadFile(root, StateDir+"/latest.json", 1<<20)
	if string(data) != "{broken" {
		t.Fatal("status overwrote the saved evidence")
	}
}

func TestStatusGitScopeAndPrivacy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	parent := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", parent}, args...)...)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init")
	root := filepath.Join(parent, "app")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	put(t, root, "original.txt", "before")
	put(t, parent, "outside.txt", "outside")
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--no-gpg-sign", "-m", "fixture")
	git("mv", "app/original.txt", "app/renamed.txt")
	put(t, root, "renamed.txt", "after staged rename")
	put(t, root, "space 界.txt", "untracked")
	put(t, root, ".env.local", "SECRET=never-export-this")
	put(t, root, "excluded.txt", "excluded")
	put(t, parent, "outside.txt", "changed outside scope")
	if err := WriteFile(root, StateDir+"/private.md", []byte("state"), false); err != nil {
		t.Fatal(err)
	}
	changes := changedFiles(context.Background(), root, []string{"excluded.txt"})
	if !changes.Available || changes.Note != "" || changes.Hidden != 1 || len(changes.Files) != 3 {
		t.Fatalf("incorrect scoped Git changes: %+v", changes)
	}
	paths := map[string]ChangedFile{}
	for _, file := range changes.Files {
		paths[file.Path] = file
	}
	if paths["original.txt"].Index != "D" || paths["renamed.txt"].Index != "A" || paths["renamed.txt"].Worktree != "M" || paths["space 界.txt"].Index != "?" {
		t.Fatalf("lost staged/worktree/untracked changes: %+v", paths)
	}
	data, _ := json.Marshal(changes)
	if strings.Contains(string(data), "SECRET") || strings.Contains(string(data), ".env") || strings.Contains(string(data), "outside.txt") {
		t.Fatal("out-of-scope or private data exported")
	}
	put(t, parent, ".gitignore", "ignored/\n")
	ignored := filepath.Join(parent, "ignored")
	if err := os.Mkdir(ignored, 0700); err != nil {
		t.Fatal(err)
	}
	if changes := changedFiles(context.Background(), ignored, nil); !strings.Contains(changes.Note, "ignored") {
		t.Fatalf("ignored parent scope was silently called clean: %+v", changes)
	}
}

func TestStatusLimitsChangesAndRedactsText(t *testing.T) {
	t.Setenv("TEST_LAUNCHPROOF_SECRET", "private-status-value")
	var data strings.Builder
	for i := 0; i < 205; i++ {
		fmt.Fprintf(&data, "?? file-%03d\x00", i)
	}
	changes := parseChanges([]byte(data.String()), "", nil, Changes{Files: []ChangedFile{}})
	if len(changes.Files) != 200 || changes.Omitted != 5 {
		t.Fatalf("unbounded file list: %+v", changes)
	}
	root := t.TempDir()
	configFile(t, root, Config{SchemaVersion: 1, Checks: []Check{{ID: "manual", SkipReason: "private-status-value"}}, Requirements: []Requirement{{ID: "acceptance", Description: "private-status-value", Checks: []string{}}}})
	status, err := Status(context.Background(), root)
	encoded, _ := json.Marshal(status)
	if err != nil || strings.Contains(string(encoded)+StatusText(status), "private-status-value") {
		t.Fatal("status leaked a known secret")
	}
}

func TestRequirementsValidateReferencesAndPreserveUnknownAcceptance(t *testing.T) {
	valid := Requirement{ID: "links", Description: "Links resolve", Checks: []string{"docs"}}
	for _, requirements := range [][]Requirement{
		{{ID: "invalid id", Description: "Description"}},
		{{ID: "empty", Description: " "}},
		{{ID: "unknown", Description: "Description", Checks: []string{"missing"}}},
		{{ID: "duplicate-check", Description: "Description", Checks: []string{"docs", "docs"}}},
		{valid, valid},
	} {
		cfg := Config{SchemaVersion: 1, Checks: []Check{{ID: "docs", Builtin: "docs"}}, Requirements: requirements}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("invalid requirements accepted: %+v", requirements)
		}
	}
	root := t.TempDir()
	if _, err := Initialize(context.Background(), root, Definition{Acceptance: "A user can book a room"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(root)
	if err != nil || len(cfg.Requirements) != 1 || len(cfg.Requirements[0].Checks) != 0 {
		t.Fatalf("init invented verification coverage: %v %+v", err, cfg.Requirements)
	}
}
