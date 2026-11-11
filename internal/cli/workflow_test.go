package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kanuwrld/launchproof/internal/workflow"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(strings.NewReader(""), &out, &errOut, "test")
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}
func TestWorkflowCLIContracts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, "init", root, "--json", "--goal", "Ship a CLI")
	var initialized workflow.InitResult
	if err != nil || json.Unmarshal([]byte(out), &initialized) != nil {
		t.Fatalf("init JSON: %v %s", err, out)
	}
	out, err = execute(t, "check", root, "--json", "--dry-run")
	var checks []workflow.Check
	if err != nil || json.Unmarshal([]byte(out), &checks) != nil || len(checks) != 1 {
		t.Fatalf("preview: %v %s", err, out)
	}
	if _, err = os.Stat(filepath.Join(root, ".launchproof", "latest.json")); !os.IsNotExist(err) {
		t.Fatal("dry-run executed checks")
	}
	out, err = execute(t, "check", root, "--json")
	var run workflow.Run
	if err != nil || json.Unmarshal([]byte(out), &run) != nil || !run.Success() {
		t.Fatalf("check JSON: %v %s", err, out)
	}
	out, err = execute(t, "report", root, "--json")
	if err != nil || json.Unmarshal([]byte(out), &run) != nil || run.Stale {
		t.Fatalf("report JSON: %v %s", err, out)
	}
	if _, err = execute(t, "report", root, "--output", ".launchproof/../README.md"); err == nil {
		t.Fatal("a managed-looking path overwrote an existing document")
	}
	content, _ := os.ReadFile(filepath.Join(root, "README.md"))
	if string(content) != "# Existing\n" {
		t.Fatal("README was overwritten")
	}
	if _, err = execute(t, "view", "../outside.md", "--project", root); err == nil {
		t.Fatal("view escaped the project")
	}
	if err = os.WriteFile(filepath.Join(root, "README.md"), []byte("[Broken](missing.md)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = execute(t, "docs", "check", root, "--json")
	var docs workflow.DocsReport
	if err == nil || json.Unmarshal([]byte(out), &docs) != nil || len(docs.Issues) != 1 {
		t.Fatalf("broken docs must exit nonzero with valid JSON: %v %s", err, out)
	}
	out, err = execute(t, "check", root, "--json")
	if err == nil || json.Unmarshal([]byte(out), &run) != nil || run.Success() {
		t.Fatalf("failed checks must exit nonzero with valid JSON: %v %s", err, out)
	}
}
