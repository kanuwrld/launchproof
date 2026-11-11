package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kanuwrld/launchproof/internal/project"
)

func TestInspectJSONIsOneDocumentWithoutANSI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"demo","scripts":{"build":"next build"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	root := NewRoot(strings.NewReader(""), &output, &output, "test")
	root.SetArgs([]string{"inspect", dir, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var report project.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("stdout must contain exactly one JSON document: %s: %v", output.String(), err)
	}
	if report.Name != "demo" || report.ChecksExecuted || strings.Contains(output.String(), "\x1b") {
		t.Fatalf("invalid JSON contract: %s", output.String())
	}
}

func TestNonTerminalWelcomeDoesNotStartTUI(t *testing.T) {
	var output bytes.Buffer
	root := NewRoot(strings.NewReader(""), &output, &output, "test")
	root.SetArgs([]string{"--plain", t.TempDir()})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "launchproof inspect") || strings.Contains(output.String(), "\x1b") {
		t.Fatalf("plain welcome should be readable and actionable: %q", output.String())
	}
}

func TestInspectMissingPathReturnsError(t *testing.T) {
	var output bytes.Buffer
	root := NewRoot(strings.NewReader(""), &output, &output, "test")
	root.SetArgs([]string{"inspect", filepath.Join(t.TempDir(), "missing")})
	if err := root.Execute(); err == nil {
		t.Fatal("expected a nonzero result for a missing project")
	}
}
