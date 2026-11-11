package workflow

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func commandFor(ctx context.Context, args []string) (*exec.Cmd, error) {
	resolved, err := exec.LookPath(args[0])
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, resolved, args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext == ".cmd" || ext == ".bat" {
		values := append([]string{resolved}, args[1:]...)
		for _, value := range values {
			if strings.ContainsAny(value, "\"\r\n&|<>^%!\x00") {
				return nil, errors.New("batch-file arguments contain unsupported shell characters; use a native executable instead")
			}
		}
		shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		cmd = exec.CommandContext(ctx, shell)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200, CmdLine: syscall.EscapeArg(shell) + " /d /s /c \"\"" + strings.Join(values, "\" \"") + "\"\""}
	}
	cmd.Cancel = func() error {
		killCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		killer := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
		_ = exec.CommandContext(killCtx, killer, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 4 * time.Second
	return cmd, nil
}
