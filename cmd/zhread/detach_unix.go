//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach starts c in a session of its own, away from zhread's terminal.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
