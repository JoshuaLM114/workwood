// Package gitx is a thin wrapper over the `git` and `gh` CLIs. Shelling out
// (rather than an in-process git library) keeps behaviour identical to plain
// command-line git and inherits the user's existing git/gh credentials.
package gitx

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// run executes git -C dir args..., returning a useful error on failure.
func run(dir string, args ...string) error {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// output executes git -C dir args... and returns trimmed stdout.
func output(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// quiet returns true when git -C dir args... exits 0 (used for show-ref checks).
func quiet(dir string, args ...string) bool {
	full := append([]string{"-C", dir}, args...)
	return exec.Command("git", full...).Run() == nil
}

// IsRepo reports whether dir is a git repository (has a .git entry).
func IsRepo(dir string) bool {
	if _, err := os.Stat(dir + "/.git"); err == nil {
		return true
	}
	return false
}

// HasLocalBranch reports whether refs/heads/<branch> exists in repo.
func HasLocalBranch(repo, branch string) bool {
	return quiet(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
}

// HasRemoteBranch reports whether refs/remotes/origin/<branch> exists in repo.
func HasRemoteBranch(repo, branch string) bool {
	return quiet(repo, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch)
}

// CurrentBranch returns the short name of the branch checked out at dir (e.g.
// "main"). Returns "HEAD" for a detached head; errors when dir isn't a git repo.
func CurrentBranch(dir string) (string, error) {
	return output(dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// AheadBehind reports how far the current branch is ahead of / behind its
// upstream (origin tracking branch). ok is false when there's no upstream or HEAD
// is detached. Run after a fetch for accurate numbers.
func AheadBehind(dir string) (ahead, behind int, ok bool) {
	out, err := output(dir, "rev-list", "--left-right", "--count", "@{u}...HEAD")
	if err != nil {
		return 0, 0, false
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return 0, 0, false
	}
	behind, _ = strconv.Atoi(f[0]) // commits in @{u} not HEAD
	ahead, _ = strconv.Atoi(f[1])  // commits in HEAD not @{u}
	return ahead, behind, true
}

// LocalBranches lists short names of every local branch in repo.
func LocalBranches(repo string) ([]string, error) {
	out, err := output(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// RemoteBranches lists short names (without the origin/ prefix) of every remote
// branch in repo.
func RemoteBranches(repo string) ([]string, error) {
	out, err := output(repo, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimPrefix(line, "origin/")
		if line == "" || line == "HEAD" {
			continue
		}
		names = append(names, line)
	}
	return names, nil
}

// Fetch refreshes origin refs, pruning deleted ones. Best-effort: network/auth
// failures are swallowed so offline use still works.
func Fetch(repo string) {
	_ = run(repo, "fetch", "origin", "--prune")
}

// AddWorktreeExistingLocal checks out an existing local branch into abs.
func AddWorktreeExistingLocal(repo, abs, branch string) error {
	return run(repo, "worktree", "add", abs, branch)
}

// AddWorktreeTrackRemote creates a local branch tracking origin/<branch> in abs.
func AddWorktreeTrackRemote(repo, abs, branch string) error {
	return run(repo, "worktree", "add", "--track", "-b", branch, abs, "origin/"+branch)
}

// AddWorktreeNewBranch creates a NEW branch from baseref into abs with --no-track
// (the source is only a starting point, not an upstream).
func AddWorktreeNewBranch(repo, abs, branch, baseref string) error {
	return run(repo, "worktree", "add", "--no-track", "-b", branch, abs, baseref)
}

// RemoveWorktree removes the worktree at abs (forced, to drop dirty trees).
func RemoveWorktree(repo, abs string) error {
	return run(repo, "worktree", "remove", "--force", abs)
}

// DeleteBranch force-deletes a local branch (work may be unmerged).
func DeleteBranch(repo, branch string) error {
	return run(repo, "branch", "-D", branch)
}

// Checkout switches repo to branch (best-effort).
func Checkout(repo, branch string) error { return run(repo, "checkout", branch) }

// PullFFOnly fast-forwards repo (best-effort).
func PullFFOnly(repo string) error { return run(repo, "pull", "--ff-only") }

// StatusLine returns the first line of `git status -sb` for a worktree.
func StatusLine(abs string) (string, error) {
	out, err := output(abs, "status", "-sb")
	if err != nil {
		return "", err
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		return out[:i], nil
	}
	return out, nil
}

// DirtyCount returns the number of changed (porcelain) entries in a worktree.
func DirtyCount(abs string) (int, error) {
	out, err := output(abs, "status", "--porcelain")
	if err != nil {
		return 0, err
	}
	if out == "" {
		return 0, nil
	}
	return len(strings.Split(out, "\n")), nil
}

// Clone clones slug into dest using gh (so SSO/host auth is handled for us).
func Clone(slug, dest string) error {
	cmd := exec.Command("gh", "repo", "clone", slug, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// FetchAll refreshes all remotes for a base clone (used by `repos pull`).
func FetchAll(repo string) error {
	cmd := exec.Command("git", "-C", repo, "fetch", "--all", "--prune")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// FetchAllQuiet is FetchAll with output captured (used by the TUI, which owns the
// terminal — inherited stdio would corrupt the alt-screen).
func FetchAllQuiet(repo string) error { return run(repo, "fetch", "--all", "--prune") }

// CloneQuiet clones slug into dest via gh with output captured (for the TUI).
func CloneQuiet(slug, dest string) error {
	cmd := exec.Command("gh", "repo", "clone", slug, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh repo clone %s: %w\n%s", slug, err, strings.TrimSpace(string(out)))
	}
	return nil
}
