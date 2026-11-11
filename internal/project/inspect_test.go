package project

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInspectNextProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package.json", `{"name":"portal","packageManager":"pnpm@10.0.0","dependencies":{"next":"16.0.0","react":"19.0.0"},"devDependencies":{"typescript":"5.9.0"},"scripts":{"test:unit":"vitest","build":"next build","dev":"next dev"}}`)
	writeFile(t, root, "pnpm-lock.yaml", "")
	writeFile(t, root, "README.md", "# Portal")
	report, err := Inspect(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Name != "portal" || report.PackageManager != "pnpm" {
		t.Fatalf("unexpected project: %+v", report)
	}
	if !slices.Equal(report.Stack, []string{"Next.js", "TypeScript"}) || !slices.Equal(report.Scripts, []string{"build", "dev", "test:unit"}) {
		t.Fatalf("unexpected stack/scripts: %+v", report)
	}
	if !report.Documents[0].Present || report.ChecksExecuted || len(report.Findings) != 0 {
		t.Fatalf("incorrect inspection evidence: %+v", report)
	}
}

func TestInspectDoesNotExecuteOrExportScriptCommands(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "should-not-exist")
	manifest := map[string]any{
		"name": "safe", "scripts": map[string]string{"test": "touch " + marker + " SUPER_PRIVATE_VALUE"},
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "package.json", string(content))
	writeFile(t, root, ".env", "TOKEN=ENV_PRIVATE_VALUE")
	report, err := Inspect(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PRIVATE_VALUE") || report.ChecksExecuted {
		t.Fatalf("private values or execution claims in report: %s", data)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("inspection executed a project script")
	}
}

func TestRejectInvalidManifests(t *testing.T) {
	for name, content := range map[string]string{
		"null":      "null",
		"malformed": "{broken", "wrong-script-type": `{"scripts":{"test":42}}`,
		"trailing-data": "{} {}", "oversized": strings.Repeat(" ", maxManifestSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "package.json", content)
			if _, err := Inspect(context.Background(), root); err == nil {
				t.Fatal("expected an actionable manifest error")
			}
		})
	}
}

func TestRejectSymlinkManifest(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeFile(t, outside, "private.json", `{"name":"outside"}`)
	if err := os.Symlink(filepath.Join(outside, "private.json"), filepath.Join(root, "package.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Inspect(context.Background(), root); err == nil {
		t.Fatal("symlinked manifest must not be read")
	}
}

func TestInspectGoAndUnsupportedProjects(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/project\n\ngo 1.26.0\n")
	report, err := Inspect(context.Background(), root)
	if err != nil || report.Name != "example.com/project" || !slices.Equal(report.Stack, []string{"Go"}) {
		t.Fatalf("unexpected Go report: %+v, %v", report, err)
	}
	report, err = Inspect(context.Background(), t.TempDir())
	if err != nil || len(report.Stack) != 0 || len(report.Findings) == 0 {
		t.Fatalf("unsupported directory should return honest findings: %+v, %v", report, err)
	}
	if _, err := Inspect(context.Background(), filepath.Join(root, "go.mod")); err == nil {
		t.Fatal("a file is not a project directory")
	}
	if _, err := Inspect(context.Background(), filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing directory should fail")
	}
}

func TestConflictingLockfiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package.json", `{"packageManager":"pnpm@10","scripts":{"build":"build","test":"test"}}`)
	writeFile(t, root, "package-lock.json", "{}")
	writeFile(t, root, "yarn.lock", "")
	writeFile(t, root, "README.md", "# App")
	report, err := Inspect(context.Background(), root)
	if err != nil || report.PackageManager != "pnpm" || len(report.Findings) != 2 {
		t.Fatalf("lockfile conflicts should be visible: %+v, %v", report, err)
	}
}

func TestGitStateIncludesUntrackedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-b", "main", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	report, err := Inspect(context.Background(), root)
	if err != nil || !report.Git.Available || report.Git.Dirty || report.Git.Branch != "main" {
		t.Fatalf("unexpected unborn repository: %+v, %v", report.Git, err)
	}
	writeFile(t, root, "README.md", "# App")
	report, err = Inspect(context.Background(), root)
	if err != nil || !report.Git.Dirty {
		t.Fatalf("untracked file must make the tree dirty: %+v, %v", report.Git, err)
	}
}
