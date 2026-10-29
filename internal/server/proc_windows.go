//go:build windows

package server

import (
	"os"
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// Detach makes cmd outlive the console that started it.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}

// terminate kills the process; Windows has no SIGTERM to send.
func terminate(p *os.Process) error {
	return p.Kill()
}
