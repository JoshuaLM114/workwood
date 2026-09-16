#!/bin/sh
# workwood installer — compiles + installs the `workwood` CLI from source via the
# Go toolchain. Meant to be piped straight from the repo:
#
#   curl -fsSL https://raw.githubusercontent.com/JoshuaLM114/workwood/main/install.sh | sh
#
# Pin a version instead of the latest tag:
#   curl -fsSL .../install.sh | WORKWOOD_VERSION=v0.2.0 sh
#
# Requires: Go (>=1.25.5) and git on PATH. Installs into `go env GOBIN` (or
# $(go env GOPATH)/bin). No root, no system files touched.
set -eu

MODULE="github.com/JoshuaLM114/workwood"
VERSION="${WORKWOOD_VERSION:-latest}"

say()  { printf '%s\n' "$*"; }
die()  { printf 'install: %s\n' "$*" >&2; exit 1; }

command -v go  >/dev/null 2>&1 || die "Go is required but not found on PATH.
  Install it from https://go.dev/dl/ (>=1.25.5), then re-run this script."
command -v git >/dev/null 2>&1 || die "git is required but not found on PATH."

say "Installing ${MODULE}@${VERSION} …"
GOFLAGS="${GOFLAGS:-}" go install "${MODULE}@${VERSION}"

# Where did it land? GOBIN wins; otherwise GOPATH/bin.
bindir="$(go env GOBIN 2>/dev/null || true)"
[ -n "$bindir" ] || bindir="$(go env GOPATH)/bin"
binpath="$bindir/workwood"

[ -x "$binpath" ] || die "build reported success but $binpath is missing — check 'go install' output above."

say ""
say "Installed: $binpath"

# Nudge the user if the install dir isn't on PATH, so `workwood` actually resolves.
case ":${PATH}:" in
  *":${bindir}:"*) ;;
  *)
    say ""
    say "NOTE: $bindir is not on your PATH. Add it, e.g.:"
    say "    echo 'export PATH=\"$bindir:\$PATH\"' >> ~/.profile && . ~/.profile"
    ;;
esac

say ""
say "Get started:"
say "    workwood version           # confirm the install"
say "    workwood init <super-repo> # register a project"
say "    workwood                   # the interactive TUI"
