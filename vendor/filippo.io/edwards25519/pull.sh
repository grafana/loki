#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
	echo "Usage: $0 <tag>"
	exit 1
fi

TAG="$1"
TMPDIR="$(mktemp -d)"

cleanup() {
	rm -rf "$TMPDIR"
}
trap cleanup EXIT

command -v git >/dev/null
command -v git-filter-repo >/dev/null

if [ -d "$HOME/go/.git" ]; then
	REFERENCE=(--reference "$HOME/go" --dissociate)
else
	REFERENCE=()
fi

git -c advice.detachedHead=false clone --no-checkout "${REFERENCE[@]}" \
	-b "$TAG" https://go.googlesource.com/go.git "$TMPDIR"

# Simplify the history graph by removing the dev.boringcrypto branches, whose
# merges end up empty after grafting anyway. This also fixes a weird quirk
# (maybe a git-filter-repo bug?) where only one file from an old path,
# src/crypto/ed25519/internal/edwards25519/const.go, would still exist in the
# filtered repo.
git -C "$TMPDIR" replace --graft f771edd7f9 99f1bf54eb
git -C "$TMPDIR" replace --graft 109c13b64f c2f96e686f
git -C "$TMPDIR" replace --graft aa4da4f189 912f075047

git -C "$TMPDIR" filter-repo --force \
	--paths-from-file /dev/stdin \
	--prune-empty always \
	--prune-degenerate always \
	--tag-callback 'tag.skip()' <<'EOF'
src/crypto/internal/fips140/edwards25519
src/crypto/internal/edwards25519
src/crypto/ed25519/internal/edwards25519
EOF

git fetch "$TMPDIR"
git update-ref "refs/heads/upstream/$TAG" FETCH_HEAD

echo
echo "Fetched upstream history up to $TAG. Merge with:"
echo -e "\tgit merge --no-ff --no-commit --allow-unrelated-histories upstream/$TAG"
