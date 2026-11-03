//go:build windows

package engine

import (
	"os/exec"
	"strconv"
)

func setProcAttr(cmd *exec.Cmd) {}

func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// taskkill /T walks the child process tree; /F forces termination.
	killer := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if err := killer.Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
