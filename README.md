# workwood

A **global CLI** for working across many repos at once. workwood orchestrates
**git worktrees** so a "super-feature" can span several repos — and, crucially,
hold **multiple branches of the same repo** (which submodules can't). It is
**action-driven**: workwood manages the worktrees and hands the selected target
paths to **actions** (e.g. `tmux`, `ssh`) that do whatever you want with them.

It does *not* vendor your repos. A team keeps a small **super-repo** describing
the project; each developer runs `workwood init` there once, and their personal
state lives outside the repo in a data dir they point at per project with
`WORKWOOD_DATA` (the project's UUID is recorded inside that state as an integrity
link, not as a path segment).

## Where everything lives

| | Lives in | Owned by | Committed? |
| --- | --- | --- | --- |
| **Project definition** — `id` (UUID) + `name` + repo list (each with a clone `url`) | the super-repo root: `workwood.yml` | the team | yes |
| **Super-feature manifests** — `id` + parent `project` UUID + repos/branches/paths | the super-repo: `workwood/super-features/<slug>.yaml` | the team | yes |
| **Actions** (`tmux`, `ssh`, …) | the super-repo: `workwood/actions/` | the team | yes |
| **Your per-project state** — editable names, target working sets | `$WORKWOOD_DATA/workwood-state.yml` | you | no |
| **Saved target presets** | `$WORKWOOD_DATA/targets/<name>.yml` | you | no |
| **Base clones + feature worktrees** | `$WORKWOOD_DATA/{main,features}/` (fixed, not configurable) | you | no |
| **Global app settings** — language, update-check, data-dir fallback | `~/.workwood/config.yaml` | you | no |

The split is deliberate: everything **shared and portable** is committed in the
super-repo and linked by **UUID**; everything **personal** (including your
editable display names) lives in the external data dir, so renaming a project or
feature, or re-pathing your checkouts, never disturbs the committed files or a
teammate. `~/.workwood/` holds *only* global app settings — there is no project
registry.

### Two names per thing

A project and each super-feature have an **immutable original name** (the *slug*,
committed) that drives the filename and worktree dir — and an **editable active
name** (local, in `workwood-state.yml`) that's just your display label. Renaming
changes only the active name; the slug never moves. A super-feature additionally
has a committed **shorthand** that prefixes its git branches (see Super-features).

## Install

workwood is distributed as source and built by the Go toolchain — there are no
pre-built binaries, so installing requires **Go 1.25+** and **git** on PATH. Base
repos are cloned with plain `git clone <url>`, so whatever auth your git already
uses (SSH keys, credential helper) is what workwood uses.

**One-liner** (recommended) — compiles the latest tagged release and drops
`workwood` in your Go bin dir:

```sh
curl -fsSL https://raw.githubusercontent.com/JoshuaLM114/workwood/main/install.sh | sh
```

Pin a specific version with `… | WORKWOOD_VERSION=v0.2.0 sh`. The script warns if
your Go bin dir isn't on `$PATH`.

**Directly with `go install`** (same result, no script):

```sh
go install github.com/JoshuaLM114/workwood@latest   # or @v0.2.0
```

**From a checkout:**

```sh
git clone git@github.com:JoshuaLM114/workwood.git && cd workwood
go install .
```

> `go install …@vX` stamps the release version into the binary automatically, so
> `workwood version` reports it correctly — no flags needed.

### Staying up to date

Re-run the one-liner (or `go install …@latest`) to upgrade. workwood also checks
**once a day** whether a newer release exists and, if so, prints a one-line hint
to stderr — it never modifies its own binary. Silence it with `update_check:
false` in `~/.workwood/config.yaml` (or the toggle in the **⚙ Settings** screen),
or per-invocation with `WORKWOOD_NO_UPDATE_CHECK=1`.

## Quick start

```sh
# Point workwood at a data dir for your personal, per-project state + checkouts.
export WORKWOOD_DATA=~/workwood-data        # (else init prompts once and saves it)

# 1. Clone the team super-repo and initialise workwood inside it (idempotent).
git clone git@github.com:my-org/my-super-repo.git ~/work/super
cd ~/work/super && workwood init            # back-fills the project UUID, writes your state

# 2. Pull the base reference clones.
workwood repos pull

# 3. Rebuild an existing super-feature's worktrees, or start a new one.
workwood sf up voice-pipeline               # rebuild a shared feature locally
workwood sf create my-feature "what it's for"
workwood sf add my-feature api feature/integrate     # branch my-feature/feature/integrate
workwood sf add my-feature web ui                    # branch my-feature/ui

# 4. Run an action over its targets.
workwood action tmux my-feature             # one tmux session, a window per target
workwood action ssh  my-feature             # open a shell in a chosen target
```

`workwood init` turns a cloned super-repo into a working setup; it's safe to
re-run (it only fills what's missing). If `$WORKWOOD_DATA` is unset, the first run
prompts for a path and saves it in `~/.workwood/config.yaml`.

## Selecting a project

There is no registry — workwood finds the project by **location**:

1. `-p <path>` / `--project <path>` — act on the super-repo at that path
2. otherwise, walk up from your cwd to the nearest `workwood.yml`

```sh
workwood project                 # show the current project (uuid, names, paths)
workwood project rename "Pay"    # set YOUR local display name (slug unchanged)
```

## The TUI

Running `workwood` (no args) opens a root menu with three entries:

- **Super-features** — the feature picker; create one, or open one to edit its
  worktrees (then `o` for its Actions screen).
- **Edit project** — add/remove the base repos in `workwood.yml`. **`a` add** is
  two steps: enter the repo name + its clone **URL** (required — workwood clones
  exactly that, deriving nothing), then pick its **default branch** from a dropdown
  of the remote's branches (read via `git ls-remote <url>`; falls back to free text
  if listing fails). **`e`** re-picks an existing repo's default branch — a
  dropdown of the clone's local/remote branches (each tagged `local` / `remote` /
  both) — and checks the base clone out to it (reporting any error). `d` removes. The table shows each repo's configured
  **Default** branch next to the **Active** branch actually checked out under
  `main_dir` — a mismatch is flagged (⚠) so divergences are obvious. The last
  column shows each clone's **sync position** relative to origin
  (`up to date` / `↓ N behind` / `↑ N ahead` / diverged), refreshed by the on-boot
  fetch (below); behind clones are highlighted. Repos that aren't a real clone
  (missing, or a stray worktree rather than the main git) are listed in **red**,
  and `p` fetches + pulls them all (cloning any that are missing).
- **⚙ Settings** — language, update-check, and this project's display name.

On launch the TUI does a **background `git fetch`** of every ref clone (read-only
— it never pulls or touches your working tree) and, if any clone is behind origin,
shows a one-line warning on the main menu. Pulling is never forced; use Edit
project → `p` (or `workwood repos pull`) when you choose to.

## Super-features

A super-feature is a tracked manifest recording every worktree + branch that
belongs to it. Each feature has a **shorthand** — a short branch prefix recorded
in the manifest — so worktree branches are `<shorthand>/<worktree-branch-name>`.
On `create` the shorthand defaults to the initials of the name
(`my-new-super-feature` → `mnsf`); override it with `--shorthand`. Because the
manifest stores both `feature:` and `shorthand:`, anyone seeing a branch like
`mnsf/api` can open the manifest yml and map it back to the feature.

```sh
workwood sf create voice "cross-service voice work"   # shorthand → v
workwood sf create my-new-super-feature --shorthand mns
workwood sf add voice api feature/integrate     # -> branch v/feature/integrate
workwood sf add voice db schema                 # -> branch v/schema
workwood sf add voice api web                   # 2nd worktree of api -> dir api--web
workwood sf add voice lib hotfix --no-feature-prefix   # -> raw branch hotfix
workwood sf add voice db pg17 --from chore/pg17        # cut from a specific source

workwood sf status voice         # branch + dirty state per worktree
workwood sf list                 # all super-features
workwood sf up voice             # rebuild every worktree from the manifest
workwood sf remove voice api feature/integrate [--prune-branch]
workwood sf down voice           # remove ALL worktrees, keep the manifest
workwood sf delete voice [--prune-branches]
```

Two worktrees of the same repo just need different names; the second one's folder
gets a `--<slug>` suffix so the checkouts don't collide. The one hard git rule is
that a ref `x` can't coexist with `x/y` — `add` guards that and asks for a
non-nesting name.

Commit `super-features/voice.yaml` and push it. A teammate then `git pull`s,
runs `workwood repos pull`, and `workwood sf up voice` rebuilds the exact set.

## Actions + targets

workwood is deliberately **dumb about meaning**. A **target** is just a named
absolute path; an **action** is just a script that does something with the
selected targets. The tool hands an action the enabled targets + the feature name
and gets out of the way — *the action* decides what "local", "deployed", "build",
etc. mean.

### Targets — a named set of paths

Each feature has a **working set**: a map of editable `key → absolute path`,
stored per-developer in your `workwood-state.yml`. A **clean** set is seeded from
every reference repo (key = repo name) and the feature's worktrees (key = the
sub-branch). You edit it on the **Actions screen** in the TUI (press `o` in the
feature editor):

- **space** toggles a candidate on/off (on = in the working set).
- **p** adds an arbitrary path — point it anywhere (even a repo outside the
  feature). If the path is a git repo, the key defaults to its current branch;
  otherwise you type a key.
- **r** renames a key; **S** saves the working set as a named **preset**; **L**
  loads one. Presets live at `$WORKWOOD_DATA/targets/<name>.yml`
  (plain `key: /abs/path` YAML) and are reusable across features.

**Multi-service repos:** if a repo (or worktree) ships a committed
`.workwood/targets.yml` (a `serviceName → subpath` map), the tree expands it into
one toggleable sub-node per service — so a single super-repo can mix single-service
and multi-service repos. See [`example/.workwood/targets.yml`](example/.workwood/targets.yml).

CLI: `workwood targets list` (presets) and `workwood targets show <feature>` (the
working set).

### Actions — scripts in `workwood/actions/`

An **action is just a script** (any language) committed in the super-repo's
**`workwood/actions/`** folder that **opts in** with a marker comment near the top:

```sh
#!/usr/bin/env bash
# workwood-action: deploy to dev      # the text after ':' is an optional label
```

The marker is how workwood tells an action from a stray helper executable —
there's no way to validate a script's argument signature, so an explicit opt-in is
the safe substitute (it's grepped, never run). A file without it is ignored.

The file must also be **executable** (`chmod +x`), since workwood runs it directly
— a marked-but-non-executable file is reported so you know to `chmod +x` it.

`workwood init` creates the folder empty; add your own and commit them. Three
worked references ship in [`example/workwood/actions/`](example/workwood/actions):
**`helloworld`** (greets each target), **`tmux`** (one window per target), and
**`ssh`** (pick a target, open a shell there). Copy any into your project.

```sh
workwood actions                         # list available (marked) actions
workwood action helloworld voice         # run against voice's working set
workwood action tmux voice --targets api-only   # …or against a saved preset
```

In the TUI Actions screen (`o` from a feature) the top panel is an action
**dropdown** — `d` to choose, `R` to run it, `f` to re-scan the folder; the bottom
panel is the target tree. If the folder has no marked actions you'll see a notice,
but you can still edit targets. A terminal-takeover action like tmux suspends the
TUI and resumes when it exits.

### The context handed to an action

workwood writes the enabled targets to a YAML file, passes its path as **`$1`**,
and also execs the action with:

| Env | Meaning |
| --- | --- |
| `WORKWOOD_TARGETS` | path to `context.yml` (same as `$1`), a `key: /abs/path` map of the enabled targets |
| `WORKWOOD_FEATURE` | the active super-feature slug |
| `WORKWOOD_ACTION` | the action's name |
| `WORKWOOD_LANG` | the active UI language (`en`/`ja`) — localize your own output if you like |
| `WORKWOOD_VAR_<KEY>` | each entry of the manifest's free-form `vars:` map |

`context.yml` is a plain map — read it without `jq`:

```sh
while IFS= read -r line; do
    key=${line%%:*}; path=${line#*: }
    [ -n "$key" ] && [ "$key" != "$line" ] || continue
    echo "do something in $key → $path"
done < "$WORKWOOD_TARGETS"
```

That's the whole contract: paths + the feature name. No modes, no per-repo child
scripts, no tool-imposed semantics.

## Command reference

| Command | What it does |
| --- | --- |
| `workwood init [path]` | scaffold/sync this super-repo (idempotent): back-fill the project UUID, create `workwood/{actions,super-features}`, write your `workwood-state.yml` |
| `workwood project [info]` | show the current project (uuid, original + active name, paths) |
| `workwood project rename <name>` | set YOUR local display name (slug/branches unchanged) |
| `workwood repos pull` | clone/refresh every base repo into `main_dir` |
| `workwood repos list` | list base repos + whether they're cloned |
| `workwood sf create <name> [desc]` | create a super-feature manifest (mints its UUID) |
| `workwood sf rename <slug> <name>` | set a super-feature's local display name (slug unchanged) |
| `workwood sf add <name> <repo> <wt-branch> [--from <src>] [--no-feature-prefix]` | add a worktree on `<name>/<wt-branch>` |
| `workwood sf up <name>` | rebuild all worktrees from the manifest |
| `workwood sf status <name>` | branch + dirty state per worktree |
| `workwood sf list` | list all super-features |
| `workwood sf remove <name> <repo> [wt-branch] [--prune-branch]` | remove ONE worktree (+ optionally its branch) |
| `workwood sf down <name>` | remove ALL worktrees, keep the manifest |
| `workwood sf delete <name> [--prune-branches]` | remove worktrees + manifest (+ branches) |
| `workwood action <name> <feature> [--targets <preset\|file>]` | run an action against the feature's working set (or a preset) |
| `workwood actions` | list available actions |
| `workwood targets list\|show <feature>` | list saved presets / show a feature's working set |
| `workwood lang [en\|ja]` | show or set the UI language |
| `workwood version` | print the build + file-schema version |

`super-feature` and `sf` are interchangeable. `-p/--project <path>` works on any
project-scoped command; super-feature commands take the feature's **slug**.

## Languages

The UI runs in **English** or **Japanese**. Every user-facing string is loaded
from a JSON message catalog — `i18n/locales/en.json` (the base) and
`i18n/locales/ja.json` — which are the single source of the wording (embedded in
the binary, so it stays self-contained). To add a language, drop in another
`<code>.json` and list it in `i18n.Supported`.

The active language is resolved by precedence:

1. `$WORKWOOD_LANG` (e.g. `WORKWOOD_LANG=ja`)
2. the `language:` field in `~/.workwood/config.yaml` (set it with `workwood lang ja`)
3. your `$LC_ALL` / `$LC_MESSAGES` / `$LANG`
4. English

```sh
workwood lang            # show the active language + supported codes
workwood lang ja         # persist Japanese in your config
```

You can also change the language, the update-check toggle, and this project's
active name interactively in the TUI's **⚙ Settings** screen (from the root menu).
App settings write to `~/.workwood/config.yaml`; the project name writes to your
`workwood-state.yml`. Both apply immediately. (Checkout paths aren't editable —
they're fixed under `WORKWOOD_DATA`.)

Technical/CLI terms (worktree, super-feature, action, target, repo) stay in
English in every language so commands remain literal. Action scripts and repo
content are not translated.

## Code layout

| Path | Responsibility |
| --- | --- |
| `main.go` | CLI dispatch + `init` / `project` / `lang` / data-dir resolution |
| `i18n/` | message catalog (`locales/*.json`) + `T`/`Err` helpers + language resolver |
| `config/` | locate the super-repo, resolve app settings + the data dir, and load `workwood-state.yml` (`config.go`, `state.go`) |
| `projectdef/` | parse/scaffold a super-repo's `workwood.yml` (id + name + repos) |
| `manifest/` | load/save super-feature manifests (id + parent project) |
| `gitx/` | thin wrappers over the `git` CLI |
| `targetcfg/` | the target working set + presets + candidate tree (`.workwood/targets.yml` expansion) |
| `action/` | write the targets `context.yml` → exec an action from `workwood/actions` |
| `superfeature/` | create/add/up/down/remove/status/list/rename + run-action |
| `repos/` | clone/refresh the base reference clones |
| `update/` | once-a-day "newer release available" check (passive, stderr) |
| `version/` | build + on-disk schema version and the compatibility check |

## Notes

- A new branch's source defaults to its repo's `default_branch` from
  `workwood.yml`; override with `--from <source>`. New branches are created
  `--no-track`, so the source is a starting point, not an upstream.
- `add` / `up` attach to an existing branch (local or `origin/*`) if one matches,
  otherwise create a new branch from the source — so teammates' pushed branches
  are picked up automatically.
- Manifests, `workwood-state.yml`, and `~/.workwood/config.yaml` carry a
  `version:` schema number. A file written by a **newer** workwood than your build
  is refused with an upgrade message rather than misread; legacy files without it
  are accepted and re-stamped on the next write.
- Actions are language-agnostic executables committed in `workwood/actions/`. Each
  action receives `WORKWOOD_LANG` so it can localize its *own* output if it wants;
  workwood never translates action text.
- The project's `id` (and each manifest's `id` + `project`) get written into the
  committed files by `init`/`sf create` — **commit them** so teammates share the
  same identity and their state links up correctly.
