#!/usr/bin/env bash
# Build workwood from this checkout and scaffold a throwaway sandbox to play in.
#
# Usage:
#   example/sandbox/run.sh [dest-dir]
#
# dest-dir defaults to ./workwood-sandbox under the repo root. The sandbox is
# fully self-contained (its own home + sample repos + a copy of the binary) and
# touches neither your real ~/.workwood nor GitHub. Delete the folder to clean up.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dest="${1:-$repo_root/workwood-sandbox}"
bin="$repo_root/bin/workwood"

echo "==> building workwood"
( cd "$repo_root" && go build -o "$bin" . )

echo "==> creating sandbox at $dest"
"$bin" sandbox "$dest"

cat <<EOF

Next:
  source $dest/activate
  workwood sf list
  workwood compose hello welcome
EOF
