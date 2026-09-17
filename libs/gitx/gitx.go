// Package gitx is a thin wrapper over the `git` CLI. Shelling out (rather than an
// in-process git library) keeps behaviour identical to plain command-line git and
// inherits the user's existing git/ssh/credential-helper auth.
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

// ValidBranchName checks a literal branch name without expanding checkout aliases.
func ValidBranchName(repo, branch string) bool {
	return branch != "HEAD" && !strings.HasPrefix(branch, "-") &&
		quiet(repo, "check-ref-format", "refs/heads/"+branch)
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

// AheadBehindRefs reports how many commits local is ahead of and behind remote.
// ok is false when either ref cannot be compared.
func AheadBehindRefs(dir, local, remote string) (ahead, behind int, ok bool) {
	out, err := output(dir, "rev-list", "--left-right", "--count", local+"..."+remote)
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, false
	}
	ahead, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false
	}
	behind, err = strconv.Atoi(fields[1])
	return ahead, behind, err == nil
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

// LsRemoteHeads lists a repo's branch names straight from a clone URL (no local
// clone needed), via `git ls-remote --heads <url>`. The url is passed to git
// opaquely — git/ssh/gh credential handling applies. Returns an error on any
// failure (offline, no auth, bad URL) so callers can fall back.
func LsRemoteHeads(url string) ([]string, error) {
	out, err := exec.Command("git", "ls-remote", "--heads", url).Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// each line: "<sha>\trefs/heads/<branch>"
		if i := strings.Index(line, "refs/heads/"); i >= 0 {
			names = append(names, line[i+len("refs/heads/"):])
		}
	}
	return names, nil
}

// AddWorktreeExistingLocal checks out an existing local branch into abs.
func AddWorktreeExistingLocal(repo, abs, branch string) error {
	return run(repo, "worktree", "add", abs, branch)
}

// MoveWorktree moves a linked checkout and updates Git's worktree registration.
// Locked worktrees and worktrees with submodules retain Git's normal protections.
func MoveWorktree(repo, from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("worktree destination %q already exists", to)
	} else if !os.IsNotExist(err) {
		return err
	}
	return run(repo, "worktree", "move", from, to)
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
	return run(repo, "branch", "-D", "--", branch)
}

// PruneWorktrees clears stale worktree registrations (after a directory was
// removed out-of-band, e.g. with rm -rf rather than `git worktree remove`).
func PruneWorktrees(repo string) error {
	return run(repo, "worktree", "prune")
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

// Clone clones url into dest with git. The url is passed opaquely (any host,
// ssh/https/local) — auth comes from the user's git/ssh/credential setup.
func Clone(url, dest string) error {
	cmd := exec.Command("git", "clone", url, dest)
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

// CloneQuiet clones url into dest with git, output captured (for the TUI, which
// owns the terminal — inherited stdio would corrupt the alt-screen).
func CloneQuiet(url, dest string) error {
	out, err := exec.Command("git", "clone", url, dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s: %w\n%s", url, err, strings.TrimSpace(string(out)))
	}
	return nil
}
