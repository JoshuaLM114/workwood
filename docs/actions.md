# Actions

An action is a **bash script** in the super-repo's `workwood/actions/` folder that
workwood **sources** and calls into. Workwood is designed to be entirely flexible with it's actions.
It merely passes the selected targets and the data repo which also allows for actions to save their data if required to do so.

## The contract (required)

An action script must:

1. carry the marker comment `# workwood-action: <optional label>`,
2. be **executable** (`chmod +x`),
3. define the bash functions — **`Run`** and **`Validate`** are required; **`Init`**
   is an optional bootstrap (below) — and **no top-level work** (workwood `source`s
   the file and calls one function).

```bash
#!/usr/bin/env bash
# workwood-action: print each target's hello file
set -euo pipefail

# Validate: return non-zero if this action can't act on the target(s) in context.
Validate() {
  while IFS= read -r line; do
    key=${line%%:*}; path=${line#*: }
    [ -n "$key" ] && [ "$key" != "$line" ] || continue
    [ -f "$path/.workwood/hello-world.txt" ] || return 1
  done < "${WORKWOOD_TARGETS:?}"
}

# Run: do the thing.
Run() {
  while IFS= read -r line; do
    key=${line%%:*}; path=${line#*: }
    [ -n "$key" ] && [ "$key" != "$line" ] || continue
    cat "$path/.workwood/hello-world.txt"
  done < "$WORKWOOD_TARGETS"
}

# Init (optional): create the minimal files the action needs, in each selected
# target. MUST be idempotent — a re-run no-ops on targets already set up.
Init() {
  while IFS= read -r line; do
    key=${line%%:*}; path=${line#*: }
    [ -n "$key" ] && [ "$key" != "$line" ] || continue
    file="$path/.workwood/hello-world.txt"
    [ -f "$file" ] || { mkdir -p "$path/.workwood"; echo "hello, $key" > "$file"; }
  done < "${WORKWOOD_TARGETS:?}"
}
```

A script missing `Run` or `Validate` is shown but **cannot be run** (TUI or CLI).

## Environment workwood provides

# TODO: Alter this behaviour, I am not a fan of needing to pass via a file.

- `WORKWOOD_TARGETS` — path to a `context.yml`: a YAML `key: /abs/path` map of the
  enabled targets. Parse it with the tiny `key/path` loop above (no `yq`/`jq`).
- `WORKWOOD_ACTION_DATA` — a private per-feature state **dir** for this action
  (`…/<feature>/.workwood/action-data/<action>/`). workwood creates it empty and
  passes the path; it never reads/writes inside — persist + source your own state
  files here (config, last-run choices) across runs.
- `WORKWOOD_FEATURE` — the active super-feature slug.
- `WORKWOOD_ACTION` — this action's name. `WORKWOOD_LANG` — UI language.
- `WORKWOOD_VAR_<KEY>` — each `vars:` entry from the manifest, upper-cased.

Interactive actions are fine — `Run` may `exec tmux`/a shell; the TUI releases the
terminal and resumes when you exit. (See the bundled `tmux`, `ssh`, `helloworld`,
`hello-world` examples in `example/workwood/actions/`.)

## Running

```sh
workwood actions                         # list discovered actions (+ availability)
workwood action <name> [feature]         # run against the feature's working set
workwood action <name> [feature] --targets <preset|file.yml>   # override the set
workwood action <name> [feature] --init  # run the action's Init (bootstrap files)
```

`[feature]` defaults to the feature you're standing in (`docs/super-features.md`).

## Bootstrap (`Init`)

`Init` creates the **minimal files an action needs to run** — e.g. the
`hello-world.txt` that `Validate` looks for — in the **currently
selected** targets. It **must be idempotent**: a target already set up is left
untouched, so re-running is safe.

- TUI Actions screen: **`i`** runs the selected action's `Init` against the working
  set, then re-validates so the **✓ / ✗** marks update.
- CLI: `workwood action <name> [feature] --init`.
- Actions that need no per-target files should still define a no-op `Init`.

## Validation (`Validate`)

`Validate`'s exit code says whether the action can act on a target — its body is
yours (e.g. "the target has the child script it needs"). In the **TUI Actions
screen**:

- validation runs **on entry**, on **`V`**, and when you switch action (`d`);
- it runs `Validate` **once per available target** (each target as the sole
  context) and marks each **✓ / ✗** in the tree;
- an **enabled** target's text is green (passed) / red (failed), and **`R` (run) is
  blocked while any enabled target is failing**;
- the picker greys out actions missing `Run`/`Validate`.

Keep `Validate` fast — it runs once per target, synchronously.

→ Targets/presets that feed an action: `docs/targets.md`.
