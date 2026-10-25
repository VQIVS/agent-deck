#!/bin/sh
# Check exactly the tree being committed in a throwaway worktree, so other members' uncommitted
# edits in a shared checkout neither fail the check nor hide behind it.
set -eu
tree=$(git write-tree)
unset GIT_INDEX_FILE GIT_DIR GIT_WORK_TREE GIT_PREFIX GIT_COMMON_DIR
dir=$(mktemp -d "${TMPDIR:-/tmp}/agent-deck-verify-XXXXXX")
clean() {
	git worktree remove --force "$dir" >/dev/null 2>&1 || true
	rm -rf "$dir" "$dir.lintcache"
}
trap clean EXIT
git worktree add --quiet --detach "$dir" HEAD
cd "$dir"
git read-tree --reset -u "$tree"
# A shared lint cache replays issues recorded for earlier throwaway paths; keep it per run.
export GOLANGCI_LINT_CACHE="$dir.lintcache"
golangci-lint fmt --diff
golangci-lint run ./...
go test ./...
