//go:build !unix

package process

import "os/exec"

func configureCancellation(cmd *exec.Cmd) {}
