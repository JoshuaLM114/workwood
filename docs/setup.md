# Setting up / onboarding a project

Goal: get from "a git super-repo with a `workwood.yml`" to "base repos cloned,
ready to create features."

## 1. The data dir — `$WORKWOOD_DATA`

Per-project, never committed; it holds clones, worktrees, state, and presets. Set
it before running anything (it differs per project):

```sh
export WORKWOOD_DATA=/abs/path/to/this-project-data
```

If unset, the CLI uses the registered data directory, then the saved fallback in
`~/.workwood/config.yaml`, then prompts once (interactive) or errors
(non-interactive). (Running from inside a feature folder
supplies it automatically — see `docs/super-features.md`.)

## 2. `workwood.yml` (committed, at the super-repo root)

```yaml
id: <uuid>          # filled by `workwood init`; commit it — it's the shared identity
name: my-project    # canonical name (immutable)
repos:
  - name: api               # local dir name + the key used everywhere
    default_branch: main    # branch the base clone is parked on / features cut from
    url: git@example.invalid:team/api.git   # REQUIRED — cloned verbatim (git clone <url>)
```

Replace the placeholder clone URL with your repository's URL.

There is **no `org`/`host`** — the `url` is the whole truth. Cloning uses plain
`git clone`, so your normal git/ssh/credential-helper auth applies.

## 3. `workwood init [path] [--data-dir <path>]`

Idempotent. Back-fills the project `id`, creates `workwood/{actions,super-features}/`,
writes your local state file, and (re)writes feature back-links. Run it after
cloning a super-repo or editing `workwood.yml`. Commit the `id` it adds.

Successful initialization also registers the UUID, super-repo root and data dir
in `~/.workwood/projects.yml` (`WORKWOOD_HOME` overrides this directory).
`workwood project list` and the MCP `projects_list` tool discover these entries.
For existing setups, use `workwood project register <root> --data-dir <data>`
without initializing again. Re-register after relocating a project.
`workwood project unregister <uuid>` removes only the registry entry.
See [MCP setup](mcp.md) to connect an agent.

### Check an existing setup

```sh
workwood project check [path-or-uuid] [--data-dir /existing/project-data]
workwood init [path-or-uuid] [--data-dir /existing/project-data]
workwood project check [path-or-uuid]
```

`project check` prints a read-only JSON report. It locates `workwood.yml` by
walking up from the supplied path or cwd, or follows a feature folder's back-link.
It recognizes unregistered projects and older definitions without UUIDs. The
report names missing metadata and returns `ready`, `needs_init`,
`needs_data_dir`, `not_found`, or `blocked`. `blocked` also exits with an error;
other statuses describe a successful check, not necessarily a ready project.

`init` updates the detected root, including when run from a nested directory or
feature worktree. It fills missing identities, tracks features, refreshes local
back-links, registers the project, and records `setup_version` in local state.
Existing scripts, presets, working sets, names, checkouts, and branches are
preserved. Identity conflicts and newer unsupported setup versions stop the
upgrade. Worktree folder renaming remains a separate review during `sf up` or
opening a feature in the TUI.

For these setup commands, the data directory comes from the feature back-link,
explicit `--data-dir`, registration, `WORKWOOD_DATA`, then the saved fallback.
If none is known, `project check` reports `needs_data_dir`; give `init` the
existing data path. Interactive initialization can prompt for it. A `ready`
setup can still need `repos pull` to populate or refresh source clones.

## 4. `workwood repos pull`

Clones each repo from its `url` into `$WORKWOOD_DATA/main/<name>` on first run,
fetches on later runs, and fast-forwards each on its `default_branch`.
Fetch, checkout and pull failures are reported; divergence is never force-reset. `workwood repos
list` shows clone state. **You can't create features until the base repos are real
clones** — `sf create` refuses otherwise.

In the TUI, **Edit project** does the same interactively: `a` add a repo (name +
required URL, then pick the default branch from a dropdown of the remote's
branches), `e` change a repo's default branch (checks the clone out to it), `p`
fetch+pull all. The table shows each clone's active branch and how far it is
ahead/behind origin (the TUI also does a background fetch on launch and warns if
anything is behind).

## Install

Source + Go toolchain only (no prebuilt binaries): `go install .` in the workwood
repo, or the project's `install.sh`. Requires **Go 1.25.5+**, **git**, and **bash**
(actions are sourced with bash).

→ Next: create a feature in `docs/super-features.md`.
