#!/usr/bin/env bash
#
# Carry the RGOClient patches onto a new upstream Fyne.
#
#   ./update-fyne.sh v2.9.0     bump to v2.9.0, leaving the result unpushed
#   ./update-fyne.sh            verify the tree against its upstream base
#
# The patches are three commits on `main` sitting on top of one pristine commit
# on `upstream`. A bump replaces the tree on `upstream` with the new release and
# rebases those three onto it, so anything upstream has moved arrives as an
# ordinary git conflict with three-way context -- not as a .rej file beside the
# code, and not as a patch that has silently gone stale.

set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$repo"

marker='RGOClient patch'
run_build=1

die() { echo "error: $*" >&2; exit 1; }

target=""
for arg in "$@"; do
    case "$arg" in
        --no-build) run_build=0 ;;
        -*)         die "unknown flag: $arg" ;;
        *)          target="${arg#v}" ;;
    esac
done

count_markers() { git grep -c "$marker" "$1" -- '*.go' 2>/dev/null | awk -F: '{n += $NF} END {print n+0}'; }

changed_files() { git diff --name-only upstream main; }

######## verify: what does main change, and is all of it marked

echo "upstream base: $(git describe --tags --exact-match upstream 2>/dev/null || git rev-parse --short upstream)"
echo "patches on main:"
git log --format='  %h %s' upstream..main

unmarked=()
while IFS= read -r f; do
    [ -z "$f" ] && continue
    case "$f" in .gitattributes|PATCHES.md|README.md|update-fyne.sh) continue ;; esac
    git grep -q "$marker" main -- "$f" || unmarked+=("$f")
done < <(changed_files)

echo "changed vs upstream: $(changed_files | wc -l) files, $(count_markers main) '$marker' markers"
if [ ${#unmarked[@]} -gt 0 ]; then
    printf 'error: changed but carries no %s comment:\n' "$marker" >&2
    printf '  %s\n' "${unmarked[@]}" >&2
    exit 1
fi

if [ -z "$target" ]; then
    echo "verify only -- pass a version to bump."
    exit 0
fi

######## bump

[ -n "$(git status --porcelain)" ] && die "working tree is dirty -- commit or stash first"

current="$(git describe --tags --exact-match upstream 2>/dev/null | sed 's|^upstream/v||' || true)"
[ "$current" = "$target" ] && echo "note: upstream is already at v$target; rebuilding it anyway"

markers_before="$(count_markers main)"

# This module *is* fyne.io/fyne/v2, so it cannot download itself. The pristine
# copy comes from a throwaway module outside the repo.
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
( cd "$work" && go mod init pristineprobe >/dev/null 2>&1 && go mod download "fyne.io/fyne/v2@v$target" ) \
    || die "could not download fyne.io/fyne/v2@v$target -- is that a real release?"
pristine="$(go env GOMODCACHE)/fyne.io/fyne/v2@v$target"
[ -d "$pristine" ] || die "expected $pristine to exist after download"

echo "==> replacing the upstream branch with v$target"
git checkout -q upstream
git rm -rq --ignore-unmatch .
# git rm leaves untracked strays behind; the branch must be exactly upstream
find . -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
cp -r "$pristine"/. .
chmod -R u+w .

cat > .gitattributes <<'EOF'
# Upstream Fyne is LF throughout and this tree is diffed against pristine
# upstream on every version bump. Any EOL conversion would make every line of
# every file look changed, so nothing here is converted, ever.
* -text
EOF

git add -A
# --allow-empty so re-running the same version is a no-op rather than a failure
git commit -q --allow-empty -m "Fyne v$target (pristine upstream)

Unmodified fyne.io/fyne/v2 v$target as published. This branch carries upstream
and nothing else; the RGOClient patches live on main."
git tag -f "upstream/v$target" >/dev/null

echo "==> rebasing the patches onto v$target"
git checkout -q main
if ! git rebase upstream; then
    cat >&2 <<EOF

The rebase stopped on a conflict. Upstream has moved code one of the patches
sits on, and what the patch should now mean is a judgement call.

  git status                 what conflicts
  <edit>                     resolve, keeping the '$marker' comments
  git add <file>
  git rebase --continue

  git rebase --abort         to back out; the upstream branch keeps v$target

Then re-run this script with no arguments to verify.
EOF
    exit 1
fi

markers_after="$(count_markers main)"
echo "==> markers: $markers_before before, $markers_after after"
if [ "$markers_before" != "$markers_after" ]; then
    die "marker count changed -- a patch was dropped or doubled. Inspect 'git diff upstream main'."
fi

if [ "$run_build" -eq 1 ]; then
    echo "==> go build ./..."
    go build ./... || die "build failed against v$target"
    echo "==> go vet ./..."
    go vet ./... || echo "warning: vet reported problems (upstream's own, possibly)"
fi

# -rgo.N restarts at 1 for each upstream version, and never reuses a number
n=1
while git rev-parse -q --verify "refs/tags/v$target-rgo.$n" >/dev/null; do n=$((n + 1)); done
git tag "v$target-rgo.$n"

cat <<EOF

Done. main is now v$target plus $markers_after marked changes, tagged v$target-rgo.$n.

  git push --force-with-lease origin main
  git push origin upstream "upstream/v$target" "v$target-rgo.$n"

Then in rgoclient:

  go mod edit -replace=fyne.io/fyne/v2=github.com/sentinelb51/rgoclient-fyne/v2@v$target-rgo.$n
  go mod tidy && go build ./... && go test ./...

main's history is rewritten by the rebase, which is why the push is forced. The
previous tag still points at the previous release, so nothing already built
moves under anyone -- and do not re-point one that has been fetched, or the
checksum database will fail every later build with a mismatch.
EOF
