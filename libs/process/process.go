// Package process prepares commands whose lifetime follows a request.
package process

import (
	"context"
	"os/exec"
	"time"
)

// CommandContext closes inherited pipes promptly and terminates child processes
// on platforms with process groups when a request is cancelled.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	configureCancellation(cmd)
	return cmd
}
