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
| **Feature back-link** — lets you run from a feature folder | `$WORKWOOD_DATA/features/<feature>/.workwood/link.yml` | you | no |
| **Local project registry** — UUID, root and data directory | `~/.workwood/projects.yml` | you | no |
| **Global app settings** — language, update-check, data-dir fallback | `~/.workwood/config.yaml` | you | no |

The split is deliberate: everything **shared and portable** is committed in the
super-repo and linked by **UUID**; everything **personal** (including your
editable display names) lives in the external data dir, so renaming a project or
feature, or re-pathing your checkouts, never disturbs the committed files or a
teammate. `~/.workwood/` holds global app settings and a local project registry
for CLI selection and MCP discovery. `workwood init` registers projects automatically.

### Two names per thing

A project and each super-feature have an **immutable original name** (the *slug*,
committed) that drives the filename and worktree dir — and an **editable active
name** (local, in `workwood-state.yml`) that's just your display label. Renaming
changes only the active name; the slug never moves. A super-feature additionally
has a committed **shorthand** that prefixes its git branches (see Super-features).

## Install

workwood is distributed as source and built by the Go toolchain — there are no
pre-built binaries, so installing requires **Go 1.25.5+** and **git** on PATH. Base
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

The examples use placeholder names and clone URLs. Replace them with your own.

```sh
# Point workwood at a data dir for your personal, per-project state + checkouts.
export WORKWOOD_DATA=~/workwood-data        # (else init prompts once and saves it)

# 1. Clone the team super-repo and initialise workwood inside it (idempotent).
git clone git@example.invalid:team/super-repo.git ~/work/super
cd ~/work/super && workwood init            # back-fills the project UUID, writes your state

# 2. Pull the base reference clones.
workwood repos pull

# 3. Rebuild an existing super-feature's worktrees, or start a new one.
workwood sf up demo                         # rebuild a shared feature locally
workwood sf create my-feature "what it's for"
workwood sf add my-feature api feature/integrate     # branch my-feature/feature/integrate
workwood sf add my-feature web ui                    # branch my-feature/ui

# 4. Run an action over its targets.
workwood action tmux my-feature             # one tmux session, a window per target
workwood action ssh  my-feature             # open a shell in a chosen target
```

`workwood init` turns a cloned super-repo into a working setup; it's safe to
re-run (it fills missing setup metadata). When no data path is known, interactive
initialization prompts for one and saves it in `~/.workwood/config.yaml`.

## Selecting a project

workwood finds projects by location or a registered UUID:

1. `-p <path-or-uuid>` / `--project <path-or-uuid>` selects a super-repo
2. otherwise, walk up from cwd to `workwood.yml` or a feature back-link

`workwood init` records the project's root and data directory in
`~/.workwood/projects.yml`. Use `workwood project list` to see registrations,
`workwood project register <path> --data-dir <path>` for existing projects, and
`workwood project unregister <uuid>` to forget an entry while keeping its files.

For older setups, run `workwood project check [path-or-uuid]` for a read-only JSON
report, then `workwood init [path-or-uuid]` when it reports `needs_init`. Supply
`--data-dir /existing/project-data` if the path is unknown. Both commands detect
the project from nested directories or feature back-links. Initialization records
the setup version and registration while preserving checkouts and local settings.
See [existing project setup](docs/setup.md#check-an-existing-setup).

```sh
workwood project                          # show the current project (uuid, names, paths)
workwood project rename "Workwood demo"    # set YOUR local display name (slug unchanged)
```

## MCP for agents

Run `workwood mcp` as a local stdio server. Agents can discover registered and
current projects and use 45 tools for project/repo management, super-features, folder
review and repair, targets/presets, actions and settings. Each call can choose a
project by UUID or path. The server uses [mcp-go](https://github.com/mark3labs/mcp-go).

See [MCP setup and tool reference](docs/mcp.md) for client configuration,
project discovery, tool inputs and execution behavior.

## The TUI

Running `workwood` (no args) opens a root menu with three entries:

- **Super-features** — the feature picker; create one, `enter` to open one and edit
  its worktrees (then `o` for its Actions screen), or **`x`** to **delete** one — a
  guided, irreversible walkthrough that asks, per repo, whether to remove the
  worktree, delete the local files, and delete the branch (see Super-features).
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
workwood sf create demo "sample feature spanning services"   # shorthand → d
workwood sf create my-new-super-feature --shorthand mns
workwood sf add demo api feature/integrate     # -> branch d/feature/integrate
workwood sf add demo db schema                 # -> branch d/schema
workwood sf add demo api web                   # 2nd worktree of api -> dir api--web
workwood sf add demo lib hotfix --no-feature-prefix   # -> raw branch hotfix
workwood sf add demo db upgrade --from chore/upgrade        # cut from a specific source

workwood sf status demo         # branch + dirty state per worktree
workwood sf list                 # all super-features
workwood sf up demo             # rebuild every worktree from the manifest
workwood sf remove demo api feature/integrate [--prune-branch]
workwood sf down demo           # remove recorded worktrees, keep the manifest
workwood sf delete demo [--prune-branches]
```

Every new worktree folder is `<repo>--<branch-slug>`: branch `d/feature/integrate`
in repo `api` becomes `api--feature_integrate`. The feature prefix is omitted;
remaining slashes become underscores. Multiple branches of the same repo get
separate folders from the first worktree onward. If a name is already occupied
on disk or reserved in the manifest, a numeric suffix (`--2`, `--3`, …) is added.
Opening a feature in the TUI or running `sf up` reviews existing folders that do
not follow this naming rule. Choose **Rename** to move the checkout and update
saved target paths, or **Remove manifest entry** to keep the checkout and branch
outside the super-feature. All choices apply after the last prompt; cancellation
or EOF during this review leaves everything unchanged. Valid numeric suffixes
stay as they are. See [folder-name review](docs/super-features.md#updating-old-folder-names)
for missing checkouts, Git move restrictions, and recovery behavior.

Creating a new branch rejects names already present locally or on `origin`, or
already recorded/staged for that repo. Remote refs are refreshed before creation;
when offline, validation uses the last fetched refs. Use **From an existing branch**
in the TUI, or `workwood sf add demo api existing-branch --existing`, to explicitly
attach an existing branch. `--existing` uses the exact branch name without a feature
prefix and fails if it does not exist. The one hard git rule is
that a ref `x` can't coexist with `x/y` — `add` guards that and asks for a
non-nesting name.

In the **TUI** editor, **`a`** first asks for the repo and a **Create a new branch
/ From an existing branch** choice. *From existing* lists the repo's branches
(tagged local / remote / both) and checks the chosen one out directly — a
remote-only branch becomes a local branch **tracking** it — instead of cutting a
new `<shorthand>/…` branch.

Commit `super-features/demo.yaml` and push it. A teammate then `git pull`s,
runs `workwood repos pull`, and `workwood sf up demo` rebuilds the exact set.

Applying a batch of staged adds/removes **persists the manifest after each one**, so
an error partway can't leave a worktree on disk that the manifest doesn't record —
the successes are saved and the failed item is reported. If the two ever drift
anyway (an interrupted run, a manual `git worktree`/`rm`), `workwood sf doctor
[feature]` — or **`D`** in the TUI editor — reports **orphans** (on disk, untracked)
and **missing** (tracked, no checkout) and lets you adopt, rebuild, remove, or drop
each.

### Working from inside a feature folder

You don't have to keep a terminal in the super-repo. Each feature folder
(`$WORKWOOD_DATA/features/<feature>/`) gets a back-link at
`.workwood/link.yml` pointing at your super-repo + data dir. So from a feature
folder — or any worktree inside it — workwood resolves the parent project on its
own, **even with `$WORKWOOD_DATA` unset**, and **defaults the feature** from where
you are:

```sh
cd "$WORKWOOD_DATA/features/demo/api--feature_integrate"   # a worktree inside the feature
workwood action tmux                     # no feature arg — uses "demo"
workwood targets show                    # same
workwood                                 # the TUI opens straight into demo's Actions panel
```

In the TUI, launching from a feature folder jumps into that feature's **Actions
panel**; pressing **esc** drops to the full menu, so you can still edit the parent
project's repos, manifests, and settings. The link is written on `sf create` /
`sf up` and back-filled by `workwood init`.

The link file carries its own `version`. The **TUI** validates every built
feature's link on launch and, if any are missing/stale/moved, shows a warning on
the root-menu status bar — press **`r`** to repair them. The **CLI** `workwood
action` self-heals the link as it runs, and `workwood sf relink [feature]`
validates a feature's folder + link (all features with no arg) and regenerates any
that need it.

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
  loads one; **g** regenerates this feature's `<feature>.yml` preset from the
  current repos + worktrees. Presets live at `$WORKWOOD_DATA/targets/<name>.yml`
  (plain `key: /abs/path` YAML) and are reusable across features.

Creating a super-feature seeds a starting preset named after it
(`$WORKWOOD_DATA/targets/<feature>.yml`); `g` (TUI) or `workwood targets generate
<feature>` (CLI) refresh it once you've added worktrees. The **Save** dialog
remembers the last preset you loaded and jumps to that name, suggests existing
preset names, and asks before overwriting one.

**Multi-service repos:** if a repo (or worktree) ships a committed
`.workwood/targets.yml` (a `serviceName → subpath` map), the tree expands it into
one toggleable sub-node per service — so a single super-repo can mix single-service
and multi-service repos. See [`example/.workwood/targets.yml`](example/.workwood/targets.yml).

CLI: `workwood targets list` (presets) and `workwood targets show <feature>` (the
working set).

### Actions — scripts in `workwood/actions/`

An **action is a bash script** committed in the super-repo's
**`workwood/actions/`** folder that **opts in** with a marker comment and defines
**`Run` and `Validate`** (and an optional **`Init`** bootstrap):

```sh
#!/usr/bin/env bash
# workwood-action: build each service       # text after ':' is an optional label

# Validate: return non-zero if the action can't act on the current targets.
Validate() { while read -r k p; do [ -x "$p/build.sh" ] || return 1; done < <(…); }

# Run: do the thing.
Run() { while read -r k p; do "$p/build.sh"; done < <(…); }
```

workwood **sources** the script and calls one function: **`Run`** when you run the
action, **`Validate`** as a pre-flight check. The marker tells an action from a
stray helper (it's grepped); the file must be **executable** (`chmod +x`) — a
marked-but-non-executable file is reported so you know to fix it.

**Validation.** The TUI Actions screen validates **on entry**, on **`V`**, and
when you switch action (`d`). workwood first checks the script defines both `Run` +
`Validate`, then runs its `Validate` **once per available target** — each target
handed in as the *sole* context — and marks every target **✓ / ✗** in the tree by
whether its `Validate` passed. An **enabled** target's text is coloured by its
result (**green** = passed, **red** = failed), and **a run is blocked while any
enabled target is failing**. The picker greys out scripts missing a method, the top
panel shows a `pass/total` summary, and a script with no `Run` + `Validate` can't
be run (TUI **or** CLI). `Validate`'s job is yours — e.g. confirm the target has
the "child script" it needs; the bundled `hello-world` only passes a target that
has a `.workwood/hello-world.txt`.

**Bootstrap (`Init`).** An optional `Init` function creates the **minimal files an
action needs** — e.g. that `.workwood/hello-world.txt` — in the **currently selected** targets, and must
be **idempotent** (a re-run no-ops). Trigger it with **`i`** in the Actions screen
(which then re-validates) or `workwood action <name> [feature] --init`.

`workwood init` creates the folder empty; add your own and commit them. Worked
references ship in [`example/workwood/actions/`](example/workwood/actions):
**`helloworld`** (greets each target), **`tmux`** (one window per target),
**`ssh`** (open a shell in a target), and **`hello-world`** (whose `Validate` only
marks it available when a target actually has a `.workwood/hello-world.txt`, which
its `Init` creates).

```sh
workwood actions                         # list available (marked) actions
workwood action helloworld demo         # run against demo's working set
workwood action tmux demo --targets api-only   # …or against a saved preset
workwood action hello-world demo --init        # create the files the action needs
```

In the TUI Actions screen (`o` from a feature) the top panel is an action
**dropdown** — `d` to choose, `R` to run it, `i` to bootstrap it (`Init`), `V` to
validate, `f` to re-scan the folder; the bottom panel is the target tree. If the folder has no marked actions you'll see a notice,
but you can still edit targets. A terminal-takeover action like tmux suspends the
TUI and resumes when it exits.

### The context handed to an action

workwood writes the enabled targets to a YAML file, passes its path as **`$1`**,
and also execs the action with:

| Env | Meaning |
| --- | --- |
| `WORKWOOD_TARGETS` | path to `context.yml`, a `key: /abs/path` map of the enabled targets |
| `WORKWOOD_ACTION_DATA` | a private per-feature state dir for this action (`…/<feature>/.workwood/action-data/<action>/`); workwood creates it empty and never touches the contents — the action owns its state files |
| `WORKWOOD_FEATURE` | the active super-feature slug |
| `WORKWOOD_ACTION` | the action's name |
| `WORKWOOD_LANG` | the active UI language (`en`/`ja`) — localize your own output if you like |
| `WORKWOOD_VAR_<KEY>` | each entry of the manifest's free-form `vars:` map |

`context.yml` is a plain map — read it with a shell loop:

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
| `workwood sf add <name> <repo> <wt-branch> [--from <src>] [--no-feature-prefix] [--existing]` | add a worktree on a new branch, or attach an existing branch explicitly |
| `workwood sf up <name>` | review folder names, then rebuild recorded worktrees |
| `workwood sf status <name>` | branch + dirty state per worktree |
| `workwood sf list` | list all super-features |
| `workwood sf remove <name> <repo> [wt-branch] [--prune-branch]` | remove ONE worktree (+ optionally its branch) |
| `workwood sf down <name>` | remove recorded worktrees, keep the manifest |
| `workwood sf delete <name> [--prune-branches]` | remove worktrees + manifest (+ branches) |
| `workwood sf relink [feature]` | validate a feature folder's back-link + regenerate it if missing/stale (all features if omitted) |
| `workwood sf doctor [feature] [--adopt\|--remove-orphans] [--rebuild\|--drop]` | report + resolve manifest↔disk drift (orphan / missing worktrees); per-item prompts without flags |
| `workwood action <name> [feature] [--targets <preset\|file>]` | run an action against the feature's working set (or a preset) |
| `workwood action <name> [feature] --init` | run the action's `Init` to bootstrap the files it needs in the selected targets |
| `workwood actions` | list available actions |
| `workwood targets list` | list saved presets |
| `workwood targets show [feature]` | show a feature's working set |
| `workwood targets generate [feature]` | (re)write `<feature>.yml` from current repos + worktrees |
| `workwood lang [en\|ja]` | show or set the UI language |
| `workwood mcp [-p <path-or-uuid>] [--data-dir <path>]` | serve MCP tools over local stdio |
| `workwood project list / register / unregister` | manage the local discovery registry |
| `workwood project check [path-or-uuid] [--data-dir <path>]` | detect an existing project and report setup readiness without changing files |
| `workwood version` | print the build + file-schema version |

`super-feature` and `sf` are interchangeable. `-p/--project <path>` works on any
project-scoped command; super-feature commands take the feature's **slug**. The
`[feature]` arg is optional when you run from inside a feature folder — it defaults
to that feature (likewise `sf up`/`status`/`down`/`delete`).

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
| `libs/gitx/` | thin wrappers over the `git` CLI |
| `targetcfg/` | the target working set + presets + candidate tree (`.workwood/targets.yml` expansion) |
| `action/` | write the targets `context.yml` → exec an action from `workwood/actions` |
| `superfeature/` | create/add/up/down/remove/status/list/rename + run-action |
| `repos/` | clone/refresh the base reference clones |
| `update/` | once-a-day "newer release available" check (passive, stderr) |
| `mcpserver/` | stdio MCP server, project discovery and structured tool handlers |
| `version/` | build + on-disk schema version and the compatibility check |

## Notes

- A new branch's source defaults to its repo's `default_branch` from
  `workwood.yml`; override with `--from <source>`. New branches are created
  `--no-track`, so the source is a starting point, not an upstream.
- `add` creates a new branch and rejects existing names. `add --existing` attaches
  an existing local or `origin` branch. `up` reviews outdated folder names before
  rebuilding recorded worktrees and attaches existing branches automatically.
- `down` and `delete` preserve unrecorded files and checkouts, including worktrees
  whose manifest entries were removed during folder-name review.
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
