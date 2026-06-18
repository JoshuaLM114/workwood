# workwood

A **global CLI** for working across many repos at once. workwood orchestrates
**git worktrees** so a "super-feature" can span several repos — and, crucially,
hold **multiple branches of the same repo** (which submodules can't). It is
**plugin-driven**: workwood manages the worktrees and hands a resolved context to
**plugins** (e.g. `tmux`, `ssh`) that compose whatever workflow you want on top.

It does *not* vendor your repos. A team keeps a small **super-repo** describing
the project; each developer registers it locally and chooses where their clones
and worktrees live on disk.

## The two halves

| | Lives in | Owned by | Committed? |
| --- | --- | --- | --- |
| **Project definition** — org + repo list + default branches | the team super-repo: `workwood.yaml` | the team | yes |
| **Super-feature manifests** — repos + branches + worktree paths | the super-repo: `super-features/<name>.yaml` | the team | yes |
| **Where clones/worktrees sit on disk** | your `~/.workwood/config.yaml` | you | no |
| **Run-target overrides** (additive list per repo: ignore/main/worktree/custom) | `~/.workwood/state/<project>/<feature>.yaml` | you | no |
| **Plugins** (`tmux`, `ssh`, …) | `~/.workwood/plugins/` (global) + `<super-repo>/plugins/` (project) | you / team | local + committed |

The split is deliberate: **shared, portable definitions** are committed in the
super-repo; **machine-specific paths** stay in your local registry, so the same
manifest rebuilds into any layout on any machine.

## Install

```sh
git clone git@github.com:JoshuaLM114/workwood.git
cd workwood
go install .          # puts `workwood` on your $PATH (needs Go 1.25+)
```

Requires `git` and `gh` (authenticated) for cloning base repos.

## Try it in a sandbox

The fastest way to understand workwood is to generate a throwaway playground —
isolated home, sample repos (with local git origins), a team super-repo, and a
copy of the binary, all in one folder. It touches neither GitHub nor your real
`~/.workwood`:

```sh
workwood sandbox ~/workwood-sandbox     # …or from a checkout: example/sandbox/run.sh
source ~/workwood-sandbox/activate      # isolated home + bundled binary on PATH
workwood sf list                        # a shared sample feature: welcome
workwood compose helloworld welcome     # parent 'hello ' + each repo's child 'world'
workwood                                # the TUI
```

The sandbox's own `README.md` is a full walkthrough. `rm -rf` the folder to clean
up. See [`example/sandbox/`](example/sandbox).

## Quick start

```sh
# 1. A teammate already has a super-repo on GitHub with workwood.yaml +
#    super-features/.  Clone it, then register it locally:
git clone git@github.com:my-org/my-super-repo.git ~/work/super
workwood init ~/work/super              # prompts for where YOU keep clones/worktrees
#   …or non-interactively:
workwood init ~/work/super --name super --main ~/work/main --features ~/work/features

# 2. Pull the base reference clones into your main_dir.
workwood repos pull

# 3. Rebuild an existing super-feature's worktrees, or start a new one.
workwood sf up voice-pipeline           # rebuild a shared feature locally
workwood sf create my-feature "what it's for"
workwood sf add my-feature api feature/integrate     # branch my-feature/feature/integrate
workwood sf add my-feature web ui                    # branch my-feature/ui

# 4. Compose a workflow over it.
workwood compose tmux my-feature        # one tmux session, a window per repo
workwood compose ssh  my-feature        # jump into a dev-deployed service
```

`workwood init` is the moment a developer turns a shared super-repo into a local
working setup; everything afterward is selected with `-p <name>`, auto-detected
when your cwd is inside the project, or taken from your default project.

## Selecting a project

Most commands act on one project. workwood picks it in this order:

1. `-p <name>` / `--project <name>` (may go before or after the subcommand)
2. the project whose `path` / `main_dir` / `features_dir` contains your cwd
3. your default project (`workwood project use <name>`)

```sh
workwood project list           # registered projects ( * = default )
workwood project use super      # set the default
workwood project remove super   # forget it (on-disk files untouched)
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

- a **PARENT** script in `~/.workwood/plugins/` (global) or a project's `plugins/`
  dir (project-local, which **overrides** a global of the same name), and
- a **CHILD** script in each referenced repo's **`.workwood/`** folder, which the
  parent runs per repo and composes the results of.

```sh
workwood compose helloworld voice    # run the helloworld plugin against the feature
workwood compose tmux voice          # the tmux plugin: a window per repo
workwood compose tmux voice --init   # scaffold each repo's .workwood child
workwood plugins                     # list available plugins
```

workwood ships three default plugins (seeded into `~/.workwood/plugins/` on first
run, never overwriting your edits):

- **`helloworld`** — the teaching example: the parent prints `hello `, each repo's
  `.workwood/helloworld` child prints `world` → `hello world` once per repo.
- **`tmux`** — one tmux session, one window ("tab") per context row, each running
  that repo's `.workwood/tmux` (or `.workwood/up`) child.
- **`ssh`** — lists the repos that provide a `.workwood/ssh` child and drops you
  into the one you pick (a shell, `kubectl exec`, port-forward — the repo decides).

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
| `WORKWOOD_PROJECT` | project name |
| `WORKWOOD_FEATURE` | feature name |
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
templates and [`example/workwood.yaml`](example/workwood.yaml) for a project def.

### Targets — additive, per repo

Each repo carries an **ordered list of setups** (stored per-developer in
`~/.workwood/state/<project>/<feature>.yaml`); composing emits **one context row
per setup**, so the same repo can take part several ways at once. The built-in
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
| `workwood sandbox [path]` | scaffold a self-contained playground to try the tool |
| `workwood init [path] [--name n] [--main d] [--features d]` | register a super-repo (scaffolds `workwood.yaml` if missing) |
| `workwood project list\|use\|remove` | manage the project registry |
| `workwood repos pull` | clone/refresh every base repo into `main_dir` |
| `workwood repos list` | list base repos + whether they're cloned |
| `workwood sf create <name> [desc]` | create a super-feature manifest |
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

`super-feature` and `sf` are interchangeable. `-p/--project <name>` works on any
project-scoped command.

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

You can also change the language (and the default project + the active project's
paths) interactively in the TUI's **⚙ Settings** screen — pick it from the
feature list; submitting writes `~/.workwood/config.yaml` and applies immediately.

Technical/CLI terms (worktree, super-feature, plugin, repo, the target names
`ignore`/`main`/`worktree`) stay in English in every language so commands remain
literal. Plugin scripts and repo content are not translated; the sandbox's
`activate` script and walkthrough README are.

## Code layout

| Path | Responsibility |
| --- | --- |
| `main.go` | CLI dispatch + `init`/`project`/`lang` registry commands |
| `i18n/` | message catalog (`locales/*.json`) + `T`/`Err` helpers + language resolver |
| `config/` | resolve `~/.workwood`, the project registry, and a selected project's paths |
| `projectdef/` | parse/scaffold a super-repo's `workwood.yaml` |
| `manifest/` | load/save super-feature manifests |
| `gitx/` | thin wrappers over the `git` / `gh` CLIs |
| `targets/` | per-developer run targets (`~/.workwood/state/`) |
| `plugin/` | resolve targets → context TSV → exec a plugin (layered dirs) |
| `superfeature/` | create/add/up/down/remove/status/list + compose |
| `repos/` | clone/refresh the base reference clones |
| `plugins/` | embedded default plugins (`helloworld`, `tmux`, `ssh`) + first-run seeding |
| `sandbox/` | scaffold a self-contained playground (`workwood sandbox`) |
| `version/` | build + on-disk schema version and the compatibility check |

## Notes

- A new branch's source defaults to its repo's `default_branch` from
  `workwood.yaml`; override with `--from <source>`. New branches are created
  `--no-track`, so the source is a starting point, not an upstream.
- `add` / `up` attach to an existing branch (local or `origin/*`) if one matches,
  otherwise create a new branch from the source — so teammates' pushed branches
  are picked up automatically.
- Manifests carry a `version:` schema number. A manifest written by a **newer**
  workwood than your build is refused with an upgrade message rather than
  misread; legacy files without it are accepted and re-stamped on the next write.
- Plugins are language-agnostic executables. Edit a seeded one freely — re-seeding
  never clobbers it. Drop a project-specific plugin in the super-repo's `plugins/`
  to share it with the team (it shadows a global of the same name).
