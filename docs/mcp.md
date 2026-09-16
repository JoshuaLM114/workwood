# MCP server

`workwood mcp` exposes workwood's project operations to an agent over local
stdio, using [mcp-go](https://github.com/mark3labs/mcp-go). The client launches
the process; there is no listening port or HTTP service.

## Connect

Build the current checkout with `go build -o bin/workwood .` or install it with
`go install .`. Use an absolute executable path when the client does not inherit
your shell's PATH. A client configuration using the common `mcpServers` format:

```json
{
  "mcpServers": {
    "workwood": {
      "command": "/absolute/path/to/workwood",
      "args": ["mcp"]
    }
  }
}
```

The server can start outside any project. It never asks for terminal input,
opens the TUI, or performs the CLI's startup update check. Standard output is
reserved for JSON-RPC; action and Git output is captured in tool results.

To give calls a default project, launch with:

```sh
workwood mcp -p /absolute/path/to/super-repo
# An unregistered project can supply its data directory explicitly:
workwood mcp -p /absolute/path/to/super-repo --data-dir /absolute/path/to/data
```

## Automatic registration and discovery

`workwood init` and the `project_init` tool automatically register a project in
`~/.workwood/projects.yml`. `WORKWOOD_HOME` overrides that directory for both
the CLI and MCP. Each entry records the project's committed UUID, local display
name, super-repo root, and data directory. Registry writes are atomic and locked
across processes. The registry is local and never belongs in a super-repo.

Register an existing installation without reinitializing or cloning it:

```sh
workwood project register /absolute/path/to/super-repo --data-dir /absolute/path/to/data
workwood project list
workwood project unregister <project-uuid>
```

Agents use `projects_list`, `project_register` and `project_unregister` for the
same operations. `projects_list` also includes `detected_project`: the setup
status of the startup/cwd project, even when it has no registration or UUID yet.
Each registered row includes its own `setup` report.
Re-registering a UUID updates its local locations after a move.
Unregistering keeps all project files, checkouts and branches. Registrations with
missing roots or changed identities are reported as unavailable, and are never
silently selected as a different project. Existing projects are registered on
their next `init`, or explicitly; discovery does not scan the entire disk.

### Detect and upgrade an existing project

1. Call `project_detect` with the project's path or registered UUID. Omit
   `project` to inspect the server's startup project or cwd. Detection walks up
   to `workwood.yml` or a feature back-link, including from nested directories.
   It can read older definitions without a UUID.
2. Inspect `status`, `issues`, `data_dir_source`, `setup_version`, and
   `required_setup_version`:

   | Status | Next step |
   | --- | --- |
   | `ready` | Use the project tools; setup metadata and registration are current. |
   | `needs_init` | Pass the returned `init_arguments` to `project_init`. |
   | `needs_data_dir` | Supply the existing clone/worktree/state directory as `data_dir`, then check again. |
   | `not_found` | Supply an existing project path, or an explicit new path to `project_init`. |
   | `blocked` | Resolve the reported parse, identity, or unsupported-version error before initializing. |

3. `project_init` fills missing project/feature identities, tracks committed
   features in local state, creates metadata directories, refreshes back-links
   for existing feature folders, and registers the project. It records the
   completed setup generation and returns a fresh status report.
4. Call `repos_pull` separately when you need current source repositories.

Detection and listing never change project files or register projects. A setup
version records completed initialization steps independently of software releases
and YAML format versions. The check also inspects required metadata, so a version
marker alone cannot hide a missing directory, registration, or feature back-link.

For an existing detected project, `project_init` can omit both `path` and
`data_dir` when their locations are already known. It reuses the feature link,
registration, environment, or saved fallback. A supplied `path` can also be a
nested directory or registered UUID. Running again preserves identities, local
names, working sets, presets, scripts, branches, checkout paths, and checkout
contents. Legacy folder naming still uses the separate folder-review workflow.

Setup detection defaults to the startup/cwd project; it does not select an
unrelated registration when no local project is found. This differs from the
sole-registration fallback used by ordinary project tools below. Supply a path
for older projects elsewhere on disk; they cannot be discovered from the registry
until registered.

Most tools accept these optional parameters:

| Parameter | Meaning |
| --- | --- |
| `project` | Registered UUID, super-repo path, or path inside a linked feature |
| `data_dir` | Explicit data directory, mainly for an unregistered project |
| `feature` | Feature slug; inferred only from a feature-folder path |

If `project` is omitted, workwood uses the startup project (explicit `-p` or cwd
discovery), then the sole registered project. Several registered projects with no
startup default require an explicit selection. Each tool call can select a
different project. Data-directory precedence is: feature back-link, explicit
`data_dir`/startup `--data-dir`, registration, `WORKWOOD_DATA`, saved global fallback.

## Typical workflow

1. Call `projects_list` and inspect setup readiness. For an older project, use
   `project_detect` and `project_init` as above. Pass the selected UUID as `project`.
2. Call `repos_list` and `repos_pull`. Check errors before assuming source repos
   are current. Pull clones missing repos and fast-forwards default branches;
   it does not update feature branches.
3. Call `feature_create` with `name`, optional `shorthand`, and `description`.
4. Call `feature_add_worktree` for each repo/branch. For example:

   ```json
   {
     "project": "<project-uuid>",
     "feature": "login",
     "repo": "api",
     "branch": "fix-login"
   }
   ```

   For shorthand `sf`, this creates branch `sf/fix-login` in `api--fix-login`.
   Additional branches of `api` get separate folders. A new branch name must not
   already exist. To attach an existing branch, set `existing_branch: true` and
   supply its full name. `no_feature_prefix: true` creates a literal new name.
5. Use `feature_get` to find the absolute checkout paths. Edit those worktrees;
   base clones are reference scaffolding.
6. Inspect `targets_get`, replace the enabled map with `targets_set`, or load a
   preset. An empty map disables every target and persists across restarts.
7. Inspect `actions_list`. Call `action_execute` with `action`, `feature` and an
   explicit `mode`: `validate`, `init`, or `run`.

The resource `workwood://guide` supplies this workflow to clients that read MCP
resources. Tool schemas and descriptions are available through `tools/list`.

## Tool catalog

| Area | Tools |
| --- | --- |
| Discovery and setup | `projects_list`, `project_detect`, `project_register`, `project_unregister`, `project_init`, `project_info`, `project_rename` |
| Source repositories | `repos_list`, `repo_add`, `repo_remove`, `repo_set_default_branch`, `repo_branches`, `repos_fetch`, `repos_pull` |
| Super-features | `features_list`, `feature_get`, `feature_create`, `feature_update`, `feature_add_worktree`, `feature_remove_worktree`, `feature_up`, `feature_down`, `feature_delete`, `feature_teardown`, `feature_relink` |
| Folder review and repair | `feature_folders_check`, `feature_folders_apply`, `feature_diagnose`, `feature_reconcile` |
| Targets | `targets_get`, `targets_set`, `targets_generate`, `targets_prune`, `targets_expand` |
| Presets | `presets_list`, `preset_get`, `preset_save`, `preset_load`, `preset_delete` |
| Actions | `actions_list`, `action_enable`, `action_execute` |
| Settings | `settings_get`, `settings_update`, `workwood_version` |

`feature_update` edits the shared description and action variables, and the local
display name. `targets_set` covers enabling/disabling targets, adding arbitrary
absolute paths and renaming target keys. `targets_expand` reads monorepo service
definitions. Preset replacement requires `overwrite: true`.

## Explicit repair and removal choices

`feature_up` refuses old folder names. Call `feature_folders_check`, then echo
each returned change in `feature_folders_apply.decisions`, adding `rename`:

```json
{
  "feature": "login",
  "decisions": [
    {
      "worktree": {"repo": "api", "branch": "sf/fix-login", "base": "main", "path": "login/api"},
      "path": "login/api--fix-login",
      "missing": false,
      "rename": true
    }
  ]
}
```

All current changes must have a decision. A stale or incomplete plan fails.
`rename: true` moves the checkout and updates saved target/preset paths.
`rename: false` drops only the manifest entry, keeping checkout and branch.

`feature_diagnose` reports orphan and missing paths. `feature_reconcile.choices`
contains `{ "path": "login/api--fix-login", "action": "adopt" }` entries.
Orphans support `adopt`/`remove`; missing checkouts support `rebuild`/`drop`.
Unselected paths remain unchanged. Rebuilding can recreate a missing branch.
`feature_up` skips absent branches unless `create_missing_branches: true`.

`feature_remove_worktree` identifies a checkout by repo and exact full branch.
`feature_down` removes recorded checkouts but retains their manifests and branches.
`feature_delete` removes the feature and takes an explicit `delete_branches` flag.
`feature_teardown` instead requires one choice for every recorded path, with
explicit `remove_worktree`, `delete_files` and `delete_branch` booleans. False
choices preserve those artifacts. Checkout removal can discard dirty files, and
branch deletion can discard unmerged commits. Inspect `feature_get` first.

## Execution and result semantics

- Results expose `{ "data": ... }` or `{ "data": ..., "error": "..." }` as
  structured content, with a JSON text fallback. Tool failures set `isError`;
  inspect partial progress before retrying. Invalid input schemas also fail.
- `actions_list` only inspects scripts. It does not source them or invoke
  `Validate`. `action_enable` marks a discovered script executable.
- `action_execute` calls only the selected lifecycle function; `run` requires
  both Run and Validate definitions but does not invoke Validate first. Any
  lifecycle function can have side effects, so all executions carry destructive
  and external-effect hints.
- Execution uses the saved targets unless `targets` or `preset` overrides them
  for that call. These overrides do not modify the saved selection. Scripts get
  the same context file, action-data directory and variables as the CLI/TUI.
- Actions have no terminal. Optional `input` supplies stdin text. Interactive
  terminal actions require the CLI/TUI; agents can run headless action variants.
- Combined output is limited to 1 MiB while the remaining output is drained.
  Results include `output`, `truncated`, `exit_code` and `duration_ms`.
  `timeout_seconds` defaults to 300 and accepts 1–3600. Git synchronization and
  action commands support request cancellation; Unix cancellation terminates
  their process group. Cancellation does not roll back effects already applied.
- Feature creation/rebuilding fetches refs best-effort for offline use, as the
  CLI does. Use `repos_pull` or `repos_fetch` when remote freshness is required;
  these report network and Git failures.
- Calls are serialized within each server process to protect local state and
  action context files. Avoid simultaneous mutations of one project from
  separate workwood processes. Registry writes have their own process lock.
- Tools modify the same local data and shared definitions as the CLI/TUI. They
  do not stage, commit, push, or publish repository changes. Script effects are
  determined by the project's action code.
