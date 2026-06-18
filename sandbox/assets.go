package sandbox

// welcomeManifest is a committed sample super-feature shipped in the sandbox's
// super-repo, so `sf list` / the TUI show something and `sf up welcome` has work.
const welcomeManifest = `version: 1
feature: welcome
description: sample shared feature — try ` + "`workwood sf up welcome`" + `
created: "2026-01-01"
worktrees:
  - repo: api
    branch: welcome/intro
    base: main
    path: welcome/api
`

// contextPlugin is a PROJECT-LOCAL plugin (lives in the super-repo's plugins/).
// It needs no tmux — it just prints the context workwood resolved, so it's a good
// way to see what plugins receive.
const contextPlugin = `#!/usr/bin/env bash
# Project-local plugin (super-repo plugins/context). Dumps the resolved context.
#   workwood compose context <feature>
set -euo pipefail
echo "project : ${WORKWOOD_PROJECT:-?}"
echo "feature : ${WORKWOOD_FEATURE:-?}"
echo "plugin  : ${WORKWOOD_PLUGIN:-?}"
echo
printf '%-10s %-9s %-18s %s\n' REPO TARGET BRANCH DIR
printf '%-10s %-9s %-18s %s\n' ---- ------ ------ ---
while IFS=$'\t' read -r repo target dir workwood branch; do
    [ -n "$repo" ] || continue
    printf '%-10s %-9s %-18s %s\n' "$repo" "$target" "${branch:-—}" "$dir"
done < "$WORKWOOD_CONTEXT"
`

// helloworldChild is each sample repo's .workwood/helloworld — the child the
// global helloworld plugin composes ("hello " + this = "hello world").
const helloworldChild = `#!/usr/bin/env bash
# .workwood/helloworld child (workwood sandbox) — the second half of the line.
echo "world"
`

// upComponent is each sample repo's .workwood/up. The tmux plugin runs it per
// repo; here it shows the handed-in context and drops into a shell.
const upComponent = `#!/usr/bin/env bash
# Sample .workwood/up child (workwood sandbox).
set -euo pipefail
cat <<BANNER
┌──────────────────────────────────────────────────────────
│ workwood sandbox · child up · ${WORKWOOD_REPO:-?}
│   target : ${WORKWOOD_TARGET:-?}
│   dir    : ${WORKWOOD_DIR:-?}
│   branch : ${WORKWOOD_BRANCH:-?}
│
│ A real repo would boot its service here. In the sandbox we
│ just open a shell in this dir so you can poke around.
│ Ctrl-D to close this window.
└──────────────────────────────────────────────────────────
BANNER
cd "${WORKWOOD_DIR:-.}" 2>/dev/null || true
exec "${SHELL:-/bin/bash}"
`

// sshComponent is each sample repo's .workwood/ssh, exercised by the ssh plugin.
const sshComponent = `#!/usr/bin/env bash
# Sample .workwood/ssh child (workwood sandbox).
set -euo pipefail
echo "[sandbox] pretending to connect to ${WORKWOOD_REPO:-?}'s dev-deployed service…"
echo "[sandbox] a real .workwood/ssh would: ssh / kubectl exec / port-forward."
echo "[sandbox] dropping you into a local shell instead. Ctrl-D to exit."
exec "${SHELL:-/bin/bash}"
`

// sampleRepoReadme is the README committed into each sample repo (%s = name, blurb).
const sampleRepoReadme = `# %s

A workwood **sandbox** sample repo — %s.

It exists only so you can practise cutting worktrees and composing plugins. Its
` + "`.workwood/`" + ` folder holds the per-repo CHILD scripts workwood hands to
plugins (` + "`up`" + `, ` + "`ssh`" + `, and ` + "`helloworld`" + `).
`

// The sandbox `activate` script and walkthrough README templates are translated
// text, so they live in the message catalog (i18n) under the keys
// "sandbox.activate_tmpl" and "sandbox.walkthrough_tmpl" rather than here.
