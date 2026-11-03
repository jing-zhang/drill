//go:build !windows

package engine

import (
	"os/exec"
	"syscall"
)

// setProcAttr puts the child in its own process group so the whole tree can be
// killed on timeout.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// A negative pid targets the process group created by Setpgid.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
