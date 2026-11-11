package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchCommandsWithSpaces(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "sample check.cmd")
	if err := os.WriteFile(script, []byte("@echo off\r\necho %~1\r\nexit /b 0\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd, err := commandFor(context.Background(), []string{script, "two words"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "two words" {
		t.Fatalf("batch quoting failed: %v %s", err, out)
	}
	if _, err = commandFor(context.Background(), []string{script, "safe & unsafe"}); err == nil {
		t.Fatal("batch metacharacters must be rejected")
	}
}
