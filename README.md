# workwood

A **global CLI** for working across many repos at once. workwood orchestrates
**git worktrees** so a "super-feature" can span several repos — and, crucially,
hold **multiple branches of the same repo** (which submodules can't). It is
**plugin-driven**: workwood manages the worktrees and hands a resolved context to
**plugins** (e.g. `tmux`, `ssh`) that compose whatever workflow you want on top.

It does *not* vendor your repos. A team keeps a small **super-repo** describing
the project; each developer runs `workwood init` there once, and their personal
state lives outside the repo in a data dir keyed by the project's UUID.

## Where everything lives

| | Lives in | Owned by | Committed? |
| --- | --- | --- | --- |
| **Project definition** — `id` (UUID) + `name` + org + repo list | the super-repo root: `workwood.yml` | the team | yes |
| **Super-feature manifests** — `id` + parent `project` UUID + repos/branches/paths | the super-repo: `workwood/super-features/<slug>.yaml` | the team | yes |
| **Plugins** (`tmux`, `ssh`, …) | the super-repo: `workwood/plugins/` | the team | yes |
| **Your per-project state** — editable names, run-target setups, path overrides | `$WORKWOOD_DATA/<project-uuid>/workwood-state.yml` | you | no |
| **Base clones + feature worktrees** | `$WORKWOOD_DATA/<project-uuid>/{main,features}/` (overridable) | you | no |
| **Global app settings** — language, update-check, data-dir fallback | `~/.workwood/config.yaml` | you | no |

The split is deliberate: everything **shared and portable** is committed in the
super-repo and linked by **UUID**; everything **personal** (including your
editable display names) lives in the external data dir, so renaming a project or
feature, or re-pathing your checkouts, never disturbs the committed files or a
teammate. `~/.workwood/` holds *only* global app settings — there is no project
registry.

### Two names per thing

A project and each super-feature have an **immutable original name** (the *slug*,
committed) that drives every filename, git branch prefix, and worktree dir — and
an **editable active name** (local, in `workwood-state.yml`) that's just your
display label. Renaming changes only the active name; the slug never moves.

## Install

workwood is distributed as source and built by the Go toolchain — there are no
pre-built binaries, so installing requires **Go 1.25+** and **git** on PATH
(plus `gh`, authenticated, for cloning base repos).

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

# 4. Compose a workflow over it.
workwood compose tmux my-feature            # one tmux session, a window per repo
workwood compose ssh  my-feature            # jump into a dev-deployed service
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

## Super-features

A super-feature is a tracked manifest recording every worktree + branch that
belongs to it. The branch is always `<feature>/<worktree-branch-name>`; the
`<repo>` argument only says which base clone to cut from.

```sh
workwood sf create voice "cross-service voice work"
workwood sf add voice api feature/integrate     # -> branch voice/feature/integrate
workwood sf add voice db schema                 # -> branch voice/schema
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

## Plugins + `.workwood/`

A **plugin is just a script** (any language). workwood never composes behaviour
itself — it resolves each repo to a target + source dir and hands that to the
plugin. A plugin is two halves it glues together:

- a **PARENT** script committed in the super-repo's **`workwood/plugins/`** folder
  (the only plugin source — shared with the whole team), and
- a **CHILD** script in each referenced repo's **`.workwood/`** folder, which the
  parent runs per repo and composes the results of.

```sh
workwood compose helloworld voice    # run the helloworld plugin against the feature
workwood compose tmux voice          # the tmux plugin: a window per repo
workwood compose tmux voice --init   # scaffold each repo's .workwood child
workwood plugins                     # list available plugins
```

`workwood init` creates an **empty** `workwood/plugins/` — add your own scripts
there and commit them. Three worked reference plugins ship in
[`example/workwood/plugins/`](example/workwood/plugins): **`helloworld`** (the
teaching example — parent prints `hello `, each repo's child prints `world`),
**`tmux`** (one session, a window per context row), and **`ssh`** (pick a repo
that ships a `.workwood/ssh` child and connect). Copy any of them into your
project's `workwood/plugins/` to use them.

### Two modes: run and init

A plugin is invoked in one of two modes (it reads `WORKWOOD_MODE`):

- **run** (default) — compose behaviour from the children.
- **init** (`workwood compose <plugin> <feature> --init`) — the plugin's utility
  for scaffolding a starter **child** script into each repo's `.workwood/` folder.

### The context handed to a plugin

workwood resolves each repo to a **target** + source dir, writes a TSV context
file, and execs the parent with it in the environment:

| Env | Meaning |
| --- | --- |
| `WORKWOOD_PROJECT` | project slug (the immutable original name, not your local label) |
| `WORKWOOD_FEATURE` | feature slug (the immutable original name) |
| `WORKWOOD_PLUGIN` | the plugin's name |
| `WORKWOOD_MODE` | `run` or `init` |
| `WORKWOOD_SESSION` | suggested session name (= feature) |
| `WORKWOOD_CONTEXT` | path to a TSV, one row per **target setup** |
| `WORKWOOD_LANG` | the active UI language (`en`/`ja`) — localize your plugin's own output if you like |
| `WORKWOOD_VAR_<KEY>` | each entry of the manifest's free-form `vars:` map |

Each TSV row is tab-separated: `repo  target  dir  workwoodDir  branch`, where
`dir` is the absolute source dir and `workwoodDir` is `<dir>/.workwood`. `branch`
is last because it's the only field that can be empty. Because targets are
additive, a repo can produce **several rows**. Read it without `jq`:

```sh
while IFS=$'\t' read -r repo target dir workwood branch; do
    "$workwood"/helloworld     # run this repo's child, honouring $target
done < "$WORKWOOD_CONTEXT"
```

A child receives `WORKWOOD_REPO`, `WORKWOOD_TARGET`, `WORKWOOD_DIR`, and
`WORKWOOD_BRANCH`. See [`example/.workwood/`](example/.workwood) for worked child
templates and [`example/workwood.yml`](example/workwood.yml) for a project def.

### Targets — additive, per repo

Each repo carries an **ordered list of setups** (stored per-developer in your
`workwood-state.yml`, keyed by the feature's UUID); composing emits **one context
row per setup**, so the same repo can take part several ways at once. The built-in
setups are:

| Setup | Source dir handed to the plugin |
| --- | --- |
| `worktree` | one of the feature's worktrees (name a path when there are several) |
| `main` | the base reference clone in your `main_dir` |
| `ignore` | excluded from composition — but only when it's the repo's **sole** setup |
| *any other string* (e.g. `deploy`) | a **custom** target — workwood passes the raw string + the base clone dir to the repo's `.workwood` child, which decides what it means |

A repo with **no** setups falls back to automatic (checked-out worktree, else main).

```sh
workwood sf target add    voice api worktree           # source api from its worktree
workwood sf target add    voice api deploy             # ALSO add a custom 'deploy' setup
workwood sf target add    voice --all main             # bulk: add 'main' to every repo
workwood sf target add    voice --repo api --repo web ignore
workwood sf target remove voice api deploy             # drop one setup
workwood sf target clear  voice api                    # reset api to auto
workwood sf target list   voice                        # show each repo's setups
```

In the TUI editor: `t` adds a setup to the highlighted repo, `T` adds one to many
repos at once (multi-select), `c` clears a repo back to auto.

## Command reference

| Command | What it does |
| --- | --- |
| `workwood init [path]` | scaffold/sync this super-repo (idempotent): back-fill the project UUID, create `workwood/{plugins,super-features}`, write your `workwood-state.yml` |
| `workwood project [info]` | show the current project (uuid, original + active name, paths) |
| `workwood project rename <name>` | set YOUR local display name (slug/branches unchanged) |
| `workwood repos pull` | clone/refresh every base repo into `main_dir` |
| `workwood repos list` | list base repos + whether they're cloned |
| `workwood sf create <name> [desc]` | create a super-feature manifest (mints its UUID) |
| `workwood sf rename <slug> <name>` | set a super-feature's local display name (slug unchanged) |
| `workwood sf add <name> <repo> <wt-branch> [--from <src>] [--no-feature-prefix]` | add a worktree on `<name>/<wt-branch>` |
| `workwood sf up <name>` | rebuild all worktrees from the manifest |
| `workwood sf target add\|remove\|clear\|list <name> …` | manage a repo's additive target setups (`--all`/`--repo` for bulk) |
| `workwood sf status <name>` | branch + dirty state per worktree |
| `workwood sf list` | list all super-features |
| `workwood sf remove <name> <repo> [wt-branch] [--prune-branch]` | remove ONE worktree (+ optionally its branch) |
| `workwood sf down <name>` | remove ALL worktrees, keep the manifest |
| `workwood sf delete <name> [--prune-branches]` | remove worktrees + manifest (+ branches) |
| `workwood compose <plugin> <feature> [--init]` | run a plugin (or `--init` to scaffold its `.workwood` children) |
| `workwood plugins` | list available plugins |
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

You can also change the language (and the update-check toggle, this project's
active name, and its checkout-path overrides) interactively in the TUI's **⚙
Settings** screen — pick it from the feature list. App settings write to
`~/.workwood/config.yaml`; the project name/paths write to your
`workwood-state.yml`. Both apply immediately.

Technical/CLI terms (worktree, super-feature, plugin, repo, the target names
`ignore`/`main`/`worktree`) stay in English in every language so commands remain
literal. Plugin scripts and repo content are not translated.

## Code layout

| Path | Responsibility |
| --- | --- |
| `main.go` | CLI dispatch + `init` / `project` / `lang` / data-dir resolution |
| `i18n/` | message catalog (`locales/*.json`) + `T`/`Err` helpers + language resolver |
| `config/` | locate the super-repo, resolve app settings + the data dir, and load `workwood-state.yml` (`config.go`, `state.go`) |
| `projectdef/` | parse/scaffold a super-repo's `workwood.yml` (id + name + repos) |
| `manifest/` | load/save super-feature manifests (id + parent project) |
| `gitx/` | thin wrappers over the `git` / `gh` CLIs |
| `targets/` | the run-target data type + label helpers (persisted in `config`'s state) |
| `plugin/` | resolve targets → context TSV → exec a plugin from `workwood/plugins` |
| `superfeature/` | create/add/up/down/remove/status/list/rename + compose |
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
- Plugins are language-agnostic executables committed in `workwood/plugins/`. Each
  plugin receives `WORKWOOD_LANG` so it can localize its *own* output if it wants;
  workwood never translates plugin text.
- The project's `id` (and each manifest's `id` + `project`) get written into the
  committed files by `init`/`sf create` — **commit them** so teammates share the
  same identity and their state links up correctly.
