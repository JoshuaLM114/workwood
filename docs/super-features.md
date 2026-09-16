# Super-features & worktrees

A super-feature is a named set of git worktrees across the base repos, recorded in
a committed manifest (`workwood/super-features/<slug>.yaml`). The worktrees are
real checkouts you edit code in.

## Lifecycle

```sh
workwood sf create demo "sample feature spanning services"   # shorthand defaults to "d"
workwood sf create another-feature --shorthand af             # override the branch prefix

workwood sf add demo api feature/login   # worktree of api on branch  d/feature/login
workwood sf add demo web ui              # worktree of web on branch  d/ui
workwood sf add demo lib hotfix --no-feature-prefix      # raw branch "hotfix"
workwood sf add demo api existing-branch --existing      # attach the exact branch
workwood sf add demo db upgrade --from chore/upgrade           # cut from a specific source

workwood sf status demo    # branch + dirty state per worktree
workwood sf list            # all features
workwood sf up demo        # rebuild every worktree from the manifest (idempotent)
workwood sf remove demo api feature/login [--prune-branch]   # one worktree
workwood sf down demo      # remove recorded worktrees, keep the manifest
workwood sf delete demo [--prune-branches]                   # worktrees + manifest
workwood sf rename demo "Demo work"   # your LOCAL display name; slug/branches unchanged
```

- **Branches** are `<shorthand>/<sub>` (shorthand committed in the manifest, default
  = initials of the name). The `<repo>` arg only says which base clone to cut from.
- New worktrees land at `$WORKWOOD_DATA/features/<slug>/<repo>--<branch-slug>`.
  The feature prefix is omitted from the branch suffix; slashes become underscores.
  For example, repo `api` on `d/feature/login` uses `api--feature_login`. Multiple
  branches of the same repo each get their own folder. Occupied or manifest-reserved
  names gain `--2`, `--3`, and so on. Existing numeric suffixes remain valid.
  Older names are reviewed when opening a feature in the TUI or running `sf up`.
- New branch names must be valid Git branch names and absent from local/origin
  refs and the feature's recorded or staged worktrees for that repo. Branch refs
  are refreshed before creation; offline checks use the last fetched refs. A ref
  `x` cannot coexist with `x/y` — `add` also guards that.
- `sf add ... --existing` explicitly attaches the supplied branch verbatim. It
  requires an existing local or origin branch; it never invents a missing branch.
- In the **TUI** editor, **`a`** (add a worktree) first asks for the repo and a
  **Create a new branch / From an existing branch** choice. *New* then collects the
  branch name, placement, and source (the CLI's `<sub>` / `--no-feature-prefix` /
  `--from`). *From existing* lists the repo's branches (each tagged local / remote /
  both) and checks the chosen one out **directly** — a remote-only branch becomes a
  local branch **tracking** it (no `<feature>/` prefix). The dropdown is refreshed by
  a `git fetch` when opened. (Git allows only one worktree per branch, so picking a
  branch already checked out elsewhere fails on apply.)
- **`sf up`** is how a teammate reconstructs your feature: `git pull` the super-repo,
  `workwood repos pull`, `workwood sf up <feature>`. For a worktree whose branch
  exists on origin it creates a tracking branch; if the branch is on neither local
  nor origin it **prompts before inventing a new local branch** (yes on EOF).
  The separate folder-name review requires a response when old names are found.

## Updating old folder names

Opening a super-feature in the TUI (Editor or Actions, including a launch from
inside the feature) and CLI `sf up` check every recorded folder name. An old name
such as `api` for branch `sf/fix-login` proposes `api--fix-login`. Each entry offers:

- **Rename**: move the checkout and update its manifest path. Saved working sets
  and target presets follow the move, including service paths inside the checkout.
  Git's index, uncommitted changes, and branch are preserved. If the checkout is
  absent, only its recorded path changes; `sf up` then rebuilds it there.
- **Remove manifest entry**: exclude it from the super-feature, keeping the checkout
  and branch. Independently configured action targets and presets stay intact.
  The retained checkout can appear as an orphan in `sf doctor`; adopting it again
  brings it back into the feature and subjects its name to the same review.

The CLI accepts `y`/Enter to rename, `n` to remove the manifest entry, or `cancel`
to stop. TUI Escape, CLI cancellation, and EOF during folder review leave all
entries unchanged, even after earlier choices. All choices are collected before
applying them. With no old names, `sf up` needs no folder-review input.

Moves use [`git worktree move`](https://git-scm.com/docs/git-worktree#Documentation/git-worktree.txt-move)
without forcing locked worktrees or worktrees containing submodules. Move or
metadata-write failures roll back earlier changes; any rollback failure reports
the affected paths for repair. Occupied destinations are never overwritten.
After a rename, terminals and editors opened at the old path should reopen the
new path shown in the result.

`sf down` and `sf delete` operate on recorded worktrees and preserve files or
checkouts left outside the manifest, including entries removed during this review.

## Deleting a super-feature (guided)

In the TUI super-features list, **`x`** on a feature opens a guided, **irreversible**
delete. After an acknowledgement it walks **one page per repo**, asking three
things independently:

- **remove the worktree** — `git worktree remove --force` (deletes the checkout +
  unregisters it from git),
- **delete the local files** — force-removes the directory (+ prunes) if anything
  remains (e.g. the base clone is gone),
- **delete the branch** — `git branch -D` in the base clone.

Worktree/files default **on**, the branch defaults **off** (branches may be
pushed). It then removes the feature record (back-link, state entry, manifest); any
worktree/branch you chose to keep is left in place. The non-interactive equivalent
is `workwood sf delete <name> [--prune-branches]` (removes everything; prunes
branches only with the flag).

## Keeping the manifest in sync (doctor)

A worktree's existence lives in two places — the **manifest** (the committed record)
and the **disk** (the actual checkout + git worktree registration). `add`/`apply`
keep them in lock-step: **`ApplyEdit` persists the manifest after each worktree it
creates/removes**, so a mid-batch failure can't leave a worktree on disk that the
manifest doesn't know about — what succeeded is saved, and the failed item is
reported (in the TUI it stays staged to fix + retry).

When they *do* drift (an interrupted run, a manual `git worktree`/`rm`), reconcile
it:

- **`workwood sf doctor [feature]`** reports **orphans** (a worktree on disk, not in
  the manifest) and **missing** (a manifest entry with no checkout). With no flags it
  asks per item: orphan → **adopt** into the manifest / **remove** from disk / skip;
  missing → **rebuild** the checkout / **drop** from the manifest / skip. Bulk flags
  skip the prompts: `--adopt` / `--remove-orphans`, `--rebuild` / `--drop`.
- In the **TUI** editor a desync shows a banner (`⚠ N orphan(s) · M missing`);
  **`D`** opens the same per-item walkthrough. Adopted/rebuilt worktrees then appear
  as normal rows.

## Run from inside a feature folder

Each feature folder gets a back-link at
`$WORKWOOD_DATA/features/<slug>/.workwood/link.yml` pointing at the super-repo +
data dir. So from a feature folder — or any worktree inside it — workwood resolves
the parent project **even with `$WORKWOOD_DATA` unset**, and the `[feature]` arg
defaults to that feature:

```sh
cd "$WORKWOOD_DATA/features/demo/api--feature_login"
workwood sf status        # no feature arg → "demo"
workwood action tmux      # same
workwood                  # TUI opens straight into demo's Actions panel (esc → menu)
```

The link is written on `sf create`/`sf up` and back-filled by `workwood init`. If
it's missing/stale, `workwood sf relink [feature]` regenerates it (all features
with no arg). The TUI validates links on launch and offers a fix (`r`).

→ Once worktrees exist, pick targets & run actions: `docs/targets.md`, `docs/actions.md`.
