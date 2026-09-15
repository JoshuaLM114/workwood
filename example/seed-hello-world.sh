#!/usr/bin/env bash
# seed-hello-world.sh — a STANDALONE helper, not tied to workwood in any way.
#
# Given the path to a super-feature's worktrees, it drops a
# .workwood/hello-world.txt file into each worktree, so the `hello-world` action
# has something to print. Run it once, by hand, before running the action.
#
# Usage:
#   ./seed-hello-world.sh <path-to-the-super-feature's-worktrees>
#
# The path is the directory that holds the feature's worktrees — with the default
# layout that's:
#   $WORKWOOD_DATA/features/<feature>
#
# Example:
#   ./seed-hello-world.sh "$WORKWOOD_DATA/features/demo"
set -euo pipefail

root="${1:?usage: ./seed-hello-world.sh <path-to-super-feature-worktrees>}"
[ -d "$root" ] || { echo "not a directory: $root" >&2; exit 1; }

count=0
for wt in "$root"/*/; do          # each immediate subdirectory is a worktree
    [ -d "$wt" ] || continue
    wt="${wt%/}"
    name="$(basename "$wt")"
    mkdir -p "$wt/.workwood"
    printf 'Hello, world! \xf0\x9f\x91\x8b  from %s\n' "$name" > "$wt/.workwood/hello-world.txt"
    echo "seeded $wt/.workwood/hello-world.txt"
    count=$((count + 1))
done

echo "seeded $count worktree(s) under $root"
