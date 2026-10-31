//go:build !unix

package main

import "os/exec"

func detach(c *exec.Cmd) {}
