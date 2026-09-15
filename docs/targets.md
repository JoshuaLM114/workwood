# Targets & presets

A **target** is just a named absolute path. A feature's **working set** is the
enabled `key → /abs/path` map that actions act on. workwood seeds a clean set from
the base repos (key = repo name) and the feature's worktrees (key = the sub-branch),
plus any monorepo services (below). You toggle/edit it in the TUI Actions screen.

## CLI

```sh
workwood targets list                 # saved presets (project-wide)
workwood targets show [feature]       # the feature's current working set
workwood targets generate [feature]   # (re)write <feature>.yml from repos + worktrees
```

`[feature]` defaults to the feature you're standing in.

## Presets

Reusable named sets at `$WORKWOOD_DATA/targets/<name>.yml` — plain `key: /abs/path`
YAML, editable by hand, shared across features. Creating a super-feature seeds a
starting preset named after it; `workwood targets generate` (CLI) or `g` (TUI)
refresh it once worktrees exist. Pass one to an action with `--targets <name>`.

## The Actions screen (TUI)

Open it from a feature (or land on it when launching from a feature folder). It's a
toggleable tree of candidate targets:

- `space` toggle a target into/out of the working set · `enter` expand a service
  group · `p` add an arbitrary path (even an external repo) · `r` rename a key
- `S` save the set as a preset (pre-fills the last-loaded name, confirms overwrite)
  · `L` load one · `g` regenerate `<feature>.yml`
- `d` pick the action · `V` validate · `R` run · `f` re-scan actions · `esc` back

## Monorepo repos — `.workwood/targets.yml`

If a repo (or worktree) ships a committed `.workwood/targets.yml`, workwood expands
it into one toggleable **sub-target per service**. Commit it inside the repo:

```yaml
# <repo>/.workwood/targets.yml  — serviceName: subpath
api:    services/api
web:    services/web
worker: services/worker
```

Relative paths resolve against the repo/worktree root (so the file is portable
across teammates' `$WORKWOOD_DATA` locations); absolute paths are used as-is. This
is distinct from a feature folder's `.workwood/link.yml` (the back-link, one level
up) — same dir name, different purpose.

→ How an action consumes these paths: `docs/actions.md`.
