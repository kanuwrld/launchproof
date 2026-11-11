package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, root, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
func configFile(t *testing.T, root string, cfg Config) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, ConfigName, string(data))
}
func helperCheck(id, mode string) Check {
	return Check{ID: id, Command: []string{os.Args[0], "-test.run=^TestCheckHelper$", "--", mode}, TimeoutSeconds: 5}
}

func TestCheckHelper(t *testing.T) {
	if os.Getenv("LAUNCHPROOF_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "pass":
		os.Stdout.WriteString("check completed\n")
		os.Exit(0)
	case "fail":
		os.Stderr.WriteString("failed deliberately\n")
		os.Exit(7)
	case "secret":
		os.Stdout.WriteString("\x1b[31m" + os.Getenv("TEST_LAUNCHPROOF_SECRET") + "\x1b[0m\n")
		os.Exit(0)
	case "large":
		os.Stdout.WriteString(strings.Repeat("x", 100<<10))
		os.Exit(0)
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(9)
}

func TestRunRecordsFailuresRedactionAndStaleness(t *testing.T) {
	t.Setenv("LAUNCHPROOF_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	t.Setenv("TEST_LAUNCHPROOF_SECRET", "private-value-must-not-leak")
	root := t.TempDir()
	put(t, root, "README.md", "# Example\n")
	cfg := Config{SchemaVersion: 1, Checks: []Check{helperCheck("pass", "pass"), helperCheck("fail", "fail"), helperCheck("secret", "secret"), {ID: "missing", SkipReason: "No tests declared."}}}
	configFile(t, root, cfg)
	events := 0
	run, err := Execute(context.Background(), root, cfg, func(event Event) { events++ })
	if err != nil {
		t.Fatal(err)
	}
	if run.Success() || run.Stale || run.Results[1].ExitCode != 7 || run.Results[3].Status != "skipped" || events < 5 {
		t.Fatalf("incorrect evidence: %+v", run)
	}
	data, _ := ReadFile(root, StateDir+"/latest.json", 4<<20)
	if strings.Contains(string(data), "private-value-must-not-leak") || strings.Contains(run.Results[2].Output, "\x1b") {
		t.Fatal("secrets or terminal control codes escaped into the run")
	}
	if !strings.Contains(run.Results[2].Output, "[REDACTED]") {
		t.Fatal("missing redaction evidence")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(root, StateDir, "latest.json"))
		if info.Mode().Perm() != 0o600 {
			t.Fatal("run record must be private")
		}
	}
	if _, err = ExportReport(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	current, err := Latest(context.Background(), root)
	if err != nil || current.Stale {
		t.Fatalf("generated state must not invalidate source: %v %+v", err, current)
	}
	put(t, root, "README.md", "# Changed\n")
	current, err = Latest(context.Background(), root)
	if err != nil || !current.Stale {
		t.Fatal("source edits must invalidate evidence")
	}
	handoff, err := Handoff(context.Background(), root)
	if err != nil || !strings.Contains(handoff, "stale: true") || !strings.Contains(handoff, "fail: failed") {
		t.Fatalf("handoff lost verification: %v %s", err, handoff)
	}
}

func TestCancellationAndTimeoutArePersisted(t *testing.T) {
	t.Setenv("LAUNCHPROOF_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	for _, timeout := range []bool{false, true} {
		root := t.TempDir()
		check := helperCheck("wait", "sleep")
		check.TimeoutSeconds = 1
		cfg := Config{SchemaVersion: 1, Checks: []Check{check, helperCheck("after", "pass")}}
		configFile(t, root, cfg)
		ctx := context.Background()
		cancel := func() {}
		if !timeout {
			ctx, cancel = context.WithCancel(ctx)
		}
		start := time.Now()
		run, err := Execute(ctx, root, cfg, func(event Event) {
			if !timeout && event.Kind == "started" && event.Index == 0 {
				time.AfterFunc(150*time.Millisecond, cancel)
			}
		})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		expected := "cancelled"
		if timeout {
			expected = "timed_out"
		}
		if run.Results[0].Status != expected || run.Success() || time.Since(start) > 10*time.Second {
			t.Fatalf("cancellation failed: %+v", run)
		}
		if !timeout && run.Results[1].Status != "cancelled" {
			t.Fatal("a cancelled run must not start another command")
		}
		if _, err := ReadFile(root, StateDir+"/latest.json", 4<<20); err != nil {
			t.Fatal("cancelled runs must remain inspectable")
		}
	}
}

func TestOutputIsBounded(t *testing.T) {
	t.Setenv("LAUNCHPROOF_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	root := t.TempDir()
	cfg := Config{SchemaVersion: 1, Checks: []Check{helperCheck("large", "large")}}
	configFile(t, root, cfg)
	run, err := Execute(context.Background(), root, cfg, nil)
	if err != nil || !run.Results[0].Truncated || len(run.Results[0].Output) > 64<<10 {
		t.Fatalf("unbounded output: %v", err)
	}
}

func TestInitializationPreservesDocuments(t *testing.T) {
	root := t.TempDir()
	put(t, root, "README.md", "# Existing\n")
	put(t, root, "AGENTS.md", "Keep my instructions.\n")
	put(t, root, "package.json", `{"name":"portal","packageManager":"npm@10","scripts":{"build":"build","test":"test"}}`)
	result, err := Initialize(context.Background(), root, Definition{Goal: "Book appointments", Audience: "Customers", Acceptance: "A booking can be cancelled", Constraints: "Keep the existing database"})
	if err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if string(agents) != "Keep my instructions.\n" {
		t.Fatal("existing instructions were overwritten")
	}
	cfg, err := LoadConfig(root)
	if err != nil || cfg.Project.Goal != "Book appointments" || len(result.Preserved) < 1 {
		t.Fatalf("bad initialization: %+v %v", result, err)
	}
	for _, check := range cfg.Checks {
		if check.ID == "build" && strings.Join(check.Command, " ") != "npm run build" {
			t.Fatal("did not derive the actual package manager")
		}
	}
	spec, _ := ReadFile(root, "SPEC.md", 1<<20)
	if !strings.Contains(string(spec), "A booking can be cancelled") {
		t.Fatal("spec lost the user's acceptance criteria")
	}
	result, err = Initialize(context.Background(), root, Definition{Goal: "Overwrite?"})
	if err != nil || len(result.Created) != 0 {
		t.Fatalf("init should be idempotent: %+v %v", result, err)
	}
}

func TestDocsUsesMarkdownStructureAndChecksAnchors(t *testing.T) {
	root := t.TempDir()
	put(t, root, "package.json", `{"name":"docs","packageManager":"npm@10","scripts":{"build":"build"}}`)
	put(t, root, "target file.md", "# Hello **world**\n\n# Hello world\n\n## Русский раздел\n")
	put(t, root, "README.md", "# Docs\n\n[valid](target%20file.md#hello-world)\n[duplicate](target%20file.md#hello-world-1)\n[unicode](target%20file.md#русский-раздел)\n[reference][guide]\n[guide]: target%20file.md\n[broken](missing.md)\n[anchor](target%20file.md#absent)\n<img src=\"missing.png\">\n\n```md\n[example](not-a-link.md)\n```\n\n```sh\nnpm run build\nnpm run absent\n```\n")
	report, err := CheckDocs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 4 {
		t.Fatalf("expected only missing file, anchor, image and script: %+v", report)
	}
	for _, issue := range report.Issues {
		if strings.Contains(issue.Message, "not-a-link") || strings.Contains(issue.Message, "hello-world") || strings.Contains(issue.Message, "русский") {
			t.Fatalf("Markdown parsed incorrectly: %+v", issue)
		}
	}
}

func TestScopedWritesRejectSymlinksAndTraversal(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := WriteFile(root, "../escape.md", []byte("bad"), true); err == nil {
		t.Fatal("output escaped the project")
	}
	if err := os.Symlink(outside, filepath.Join(root, StateDir)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := WriteFile(root, StateDir+"/report.md", []byte("bad"), true); err == nil {
		t.Fatal("output followed a symlink")
	}
}

func TestRecentProjectsDeduplicateAndLimit(t *testing.T) {
	t.Setenv("LAUNCHPROOF_HOME", t.TempDir())
	root := t.TempDir()
	if err := RememberProject(root); err != nil {
		t.Fatal(err)
	}
	if err := RememberProject(root); err != nil {
		t.Fatal(err)
	}
	recent, err := RecentProjects()
	if err != nil || len(recent) != 1 {
		t.Fatalf("invalid recents: %v %+v", err, recent)
	}
}

func TestConfigRejectsAmbiguousCommands(t *testing.T) {
	for _, cfg := range []Config{{SchemaVersion: 2, Checks: []Check{{ID: "x", Command: []string{"echo"}}}}, {SchemaVersion: 1, Checks: []Check{{ID: "x", Command: []string{"echo"}, SkipReason: "skip"}}}, {SchemaVersion: 1, Checks: []Check{{ID: "x", Command: []string{"echo"}}, {ID: "x", Builtin: "docs"}}}, {SchemaVersion: 1, Checks: []Check{{ID: "x", Builtin: "shell"}}}} {
		if cfg.Validate() == nil {
			t.Fatalf("invalid configuration accepted: %+v", cfg)
		}
	}
	root := t.TempDir()
	put(t, root, ConfigName, `{"schema_version":1,"checks":[{"id":"docs","builtin":"docs"}],"typo":true}`)
	if _, err := LoadConfig(root); err == nil {
		t.Fatal("unknown fields silently accepted")
	}
}

func TestLiveOutputArrivesBeforeCheckFinishes(t *testing.T) {
	t.Setenv("LAUNCHPROOF_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	root := t.TempDir()
	cfg := Config{SchemaVersion: 1, Checks: []Check{helperCheck("stream", "pass")}}
	configFile(t, root, cfg)
	outputSeen, finished := false, false
	_, err := Execute(context.Background(), root, cfg, func(event Event) {
		if event.Kind == "output" {
			if finished {
				t.Error("output arrived after completion")
			}
			outputSeen = strings.Contains(event.Result.Output, "check completed")
		}
		if event.Kind == "finished" {
			finished = true
		}
	})
	if err != nil || !outputSeen || !finished {
		t.Fatalf("live log missing: %v", err)
	}
}

func TestIgnoredProjectStillInspectsItsOwnFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	parent := t.TempDir()
	if out, err := exec.Command("git", "init", parent).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	put(t, parent, ".gitignore", "scratch/\n")
	root := filepath.Join(parent, "scratch", "project")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	put(t, root, "README.md", "# Nested project\n")
	report, err := CheckDocs(context.Background(), root)
	if err != nil || report.Files != 1 {
		t.Fatalf("ignored child lost its documents: %v %+v", err, report)
	}
	before, err := Fingerprint(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "README.md", "# Changed project\n")
	after, err := Fingerprint(context.Background(), root, nil)
	if err != nil || before.Digest == after.Digest || before.Files != 1 {
		t.Fatalf("nested project has empty evidence: %v %+v %+v", err, before, after)
	}
}
