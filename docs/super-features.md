# Super-features & worktrees

A super-feature is a named set of git worktrees across the base repos, recorded in
a committed manifest (`workwood/super-features/<slug>.yaml`). The worktrees are
real checkouts you edit code in.

## Lifecycle

```sh
workwood sf create voice "cross-service voice work"   # shorthand defaults to "v"
workwood sf create billing --shorthand bs             # override the branch prefix

workwood sf add voice api feature/login   # worktree of api on branch  v/feature/login
workwood sf add voice web ui              # worktree of web on branch  v/ui
workwood sf add voice lib hotfix --no-feature-prefix      # raw branch "hotfix"
workwood sf add voice db pg17 --from chore/pg17           # cut from a specific source

workwood sf status voice    # branch + dirty state per worktree
workwood sf list            # all features
workwood sf up voice        # rebuild every worktree from the manifest (idempotent)
workwood sf remove voice api feature/login [--prune-branch]   # one worktree
workwood sf down voice      # remove ALL worktrees, keep the manifest
workwood sf delete voice [--prune-branches]                   # worktrees + manifest
workwood sf rename voice "Voice work"   # your LOCAL display name; slug/branches unchanged
```

- **Branches** are `<shorthand>/<sub>` (shorthand committed in the manifest, default
  = initials of the name). The `<repo>` arg only says which base clone to cut from.
- Worktrees land at `$WORKWOOD_DATA/features/<slug>/<repo>` (a 2nd worktree of the
  same repo gets a `--<sub>` dir suffix). A ref `x` can't coexist with `x/y` — `add`
  guards that.
- **`sf up`** is how a teammate reconstructs your feature: `git pull` the super-repo,
  `workwood repos pull`, `workwood sf up <feature>`. For a worktree whose branch
  exists on origin it creates a tracking branch; if the branch is on neither local
  nor origin it **prompts before inventing a new local branch** (yes on EOF, so
  scripts aren't blocked).

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

## Run from inside a feature folder

Each feature folder gets a back-link at
`$WORKWOOD_DATA/features/<slug>/.workwood/link.yml` pointing at the super-repo +
data dir. So from a feature folder — or any worktree inside it — workwood resolves
the parent project **even with `$WORKWOOD_DATA` unset**, and the `[feature]` arg
defaults to that feature:

```sh
cd "$WORKWOOD_DATA/features/voice/api"
workwood sf status        # no feature arg → "voice"
workwood action tmux      # same
workwood                  # TUI opens straight into voice's Actions panel (esc → menu)
```

The link is written on `sf create`/`sf up` and back-filled by `workwood init`. If
it's missing/stale, `workwood sf relink [feature]` regenerates it (all features
with no arg). The TUI validates links on launch and offers a fix (`r`).

→ Once worktrees exist, pick targets & run actions: `docs/targets.md`, `docs/actions.md`.
