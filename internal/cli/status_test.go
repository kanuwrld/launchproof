package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kanuwrld/launchproof/internal/workflow"
)

func TestStatusCLIReadOnlyAndStrictScope(t *testing.T) {
	root := t.TempDir()
	readStatus := func(strict, wantError bool) workflow.ProjectStatus {
		t.Helper()
		args := []string{"status", root, "--json"}
		if strict {
			args = append(args, "--strict")
		}
		output, err := execute(t, args...)
		var report workflow.ProjectStatus
		if (err != nil) != wantError || json.Unmarshal([]byte(output), &report) != nil || report.ChecksExecuted {
			t.Fatalf("status exit/JSON contract: %v %s", err, output)
		}
		return report
	}
	if status := readStatus(false, false); status.Configuration != "missing" {
		t.Fatal("uninitialized status is not actionable")
	}
	readStatus(true, true)
	if _, err := execute(t, "init", root, "--defaults"); err != nil {
		t.Fatal(err)
	}
	cfg, err := workflow.LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Checks = append(cfg.Checks, workflow.Check{ID: "other", Builtin: "docs"})
	cfg.Requirements = []workflow.Requirement{{ID: "links", Description: "All local links resolve", Checks: []string{"docs", "other"}}}
	encoded, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(root, workflow.ConfigName), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	readStatus(false, false)
	if _, err := os.Stat(filepath.Join(root, workflow.StateDir)); !os.IsNotExist(err) {
		t.Fatal("status created evidence before any check ran")
	}
	if _, err := execute(t, "check", root, "--only", "docs", "--json"); err != nil {
		t.Fatal(err)
	}
	if status := readStatus(true, true); status.Evidence.State != "current" || status.Checks[1].Status != "skipped" || status.Requirements[0].Status != "unverified" {
		t.Fatalf("partial run was treated as complete: %+v", status)
	}
	if _, err := execute(t, "check", root, "--json"); err != nil {
		t.Fatal(err)
	}
	if status := readStatus(true, false); !status.Passes() {
		t.Fatal("complete run was not accepted")
	}
	output, err := execute(t, "status", root)
	if err != nil || !strings.Contains(output, "Configured checks passed") || !strings.Contains(output, "Read-only. No checks executed.") {
		t.Fatalf("plain status missing: %v %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "new-source.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if status := readStatus(true, true); status.Evidence.State != "stale" {
		t.Fatalf("stale evidence was not detected: %+v", status)
	}
}
