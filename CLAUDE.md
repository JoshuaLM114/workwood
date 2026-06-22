# workwood — how to use it (agent guide)

This file is for **using the `workwood` CLI/TUI** to drive a project — not for
developing workwood itself (for internals see `README.md` and the Go packages).
It's the index: skim it, then open the one `docs/` file for the task at hand.

## What workwood is

It orchestrates **git worktrees across several repos at once** as named
**super-features**, and runs **actions** (scripts) against a chosen set of
**targets** (paths). One feature = a coordinated branch + worktree in each repo it
touches.

Mental model (memorize this):

- **super-repo** — a git repo with `workwood.yml` at its root: the committed
  project definition (`id`, `name`, `repos[]` each with a `url`). Alongside it,
  `workwood/actions/` (action scripts) and `workwood/super-features/<slug>.yaml`
  (one committed manifest per feature). All committed + team-shared.
- **base repos** — reference clones at `$WORKWOOD_DATA/main/<repo>`. Read-only
  scaffolding; never edit these. `workwood repos pull` creates/refreshes them.
- **super-feature** — a named set of worktrees (one per repo it spans), recorded
  in a manifest. Worktrees are **real checkouts** at
  `$WORKWOOD_DATA/features/<slug>/<repo>` — **this is where you edit code.**
- **branches** — `<shorthand>/<sub>`, where the shorthand defaults to the feature
  name's initials (`my-feature` → `mf`) and is committed in the manifest.
- **targets / working set** — a per-feature `key → /abs/path` map (the base repos +
  this feature's worktrees, + monorepo services). The *enabled* subset is what an
  action acts on. Reusable named **presets** live at `$WORKWOOD_DATA/targets/`.
- **action** — a bash script in `workwood/actions/` defining `Run` + `Validate`
  functions, run against the working set.
- **`$WORKWOOD_DATA`** — the per-project data dir (clones, worktrees, state,
  presets). **Never committed**; set per project via the env var.

## Invoking it

- `workwood` with no args opens the **TUI**; subcommands run headless. `-p <path>`
  targets a project explicitly.
- The project is found by walking **up** from cwd to a `workwood.yml`, **or** —
  when you're inside a feature folder — by its `.workwood/link.yml` back-link, which
  also supplies `$WORKWOOD_DATA`. So from inside `…/features/<feat>/<repo>/…` the
  `[feature]` argument and the env var are both optional. See
  `docs/super-features.md`.

## Commands at a glance

```
workwood init [path]                         scaffold/sync a super-repo
workwood repos pull | list                   clone/refresh base repos
workwood sf create <name> [desc] [--shorthand s]
workwood sf add <name> <repo> <wt-branch> [--from <src>] [--no-feature-prefix]
workwood sf up | status | down | delete | relink [feature]   (feature defaults to cwd)
workwood sf rename <slug> <new-name>         local display name only
workwood action <name> [feature] [--targets <preset|file>]
workwood actions                             list discovered actions
workwood targets list | show [feature] | generate [feature]
workwood project [info] | rename <name>      ·   workwood lang [en|ja]   ·   workwood version
```

## Hard rules (not preferences)

- **Edit code only inside a feature's worktrees** (`$WORKWOOD_DATA/features/…`).
  Treat `$WORKWOOD_DATA/main/…` as read-only reference clones.
- A repo entry in `workwood.yml` **must have a `url`** — workwood clones exactly
  that (it derives nothing; no org/host).
- An **action must define both `Run` and `Validate`** bash functions (no top-level
  code) — workwood *sources* the script and calls one. Missing either ⇒ it can't
  run. See `docs/actions.md`.
- Manifests, `workwood.yml`, and actions are **committed**; everything under
  `$WORKWOOD_DATA` is **local + git-ignored** (state, clones, worktrees, presets,
  the feature back-link).

## When you want to… → read

- **Set up / onboard a project** (init, `workwood.yml`, repos, `$WORKWOOD_DATA`) →
  `docs/setup.md`
- **Create/rebuild features & worktrees**, or **run from a feature folder** →
  `docs/super-features.md`
- **Write or run an action** (the `Run`/`Validate` contract, env, validation) →
  `docs/actions.md`
- **Choose targets, save/load presets, or wire a monorepo** (`.workwood/targets.yml`) →
  `docs/targets.md`
