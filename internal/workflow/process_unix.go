//go:build !windows

package workflow

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

func commandFor(ctx context.Context, args []string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}
