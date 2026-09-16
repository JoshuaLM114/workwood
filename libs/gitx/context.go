package gitx

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/JoshuaLM114/workwood/libs/process"
)

// RunContext runs Git with captured output and no terminal credential prompts.
// An empty directory allows commands such as clone before a checkout exists.
func RunContext(ctx context.Context, dir string, args ...string) (string, error) {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := process.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), ctx.Err()
	}
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimRight(string(out), "\n"), nil
}
