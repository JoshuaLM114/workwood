# Setting up / onboarding a project

Goal: get from "a git super-repo with a `workwood.yml`" to "base repos cloned,
ready to create features."

## 1. The data dir — `$WORKWOOD_DATA`

Per-project, never committed; it holds clones, worktrees, state, and presets. Set
it before running anything (it differs per project):

```sh
export WORKWOOD_DATA=/abs/path/to/this-project-data
```

If unset, the CLI prompts once (interactive) or errors (non-interactive). A saved
fallback lives in `~/.workwood/config.yaml`. (Running from inside a feature folder
supplies it automatically — see `docs/super-features.md`.)

## 2. `workwood.yml` (committed, at the super-repo root)

```yaml
id: <uuid>          # filled by `workwood init`; commit it — it's the shared identity
name: my-project    # canonical name (immutable)
repos:
  - name: api               # local dir name + the key used everywhere
    default_branch: main    # branch the base clone is parked on / features cut from
    url: git@github.com:org/api.git   # REQUIRED — cloned verbatim (git clone <url>)
```

There is **no `org`/`host`** — the `url` is the whole truth. Cloning uses plain
`git clone`, so your normal git/ssh/credential-helper auth applies.

## 3. `workwood init [path]`

Idempotent. Back-fills the project `id`, creates `workwood/{actions,super-features}/`,
writes your local state file, and (re)writes feature back-links. Run it after
cloning a super-repo or editing `workwood.yml`. Commit the `id` it adds.

## 4. `workwood repos pull`

Clones each repo from its `url` into `$WORKWOOD_DATA/main/<name>` on first run,
fetches on later runs, and parks each on its `default_branch`. `workwood repos
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
repo, or the project's `install.sh`. Requires **Go 1.25+**, **git**, and **bash**
(actions are sourced with bash).

→ Next: create a feature in `docs/super-features.md`.
