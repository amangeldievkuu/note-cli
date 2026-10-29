//go:build !windows

package server

import (
	"os"
	"os/exec"
	"syscall"
)

// Detach makes cmd outlive the terminal that started it.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func terminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
