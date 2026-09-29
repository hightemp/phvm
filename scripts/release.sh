#!/bin/sh
# Publish the version from VERSION to the existing tag-triggered workflow.
set -eu

fail() {
    printf 'Release failed: %s\n' "$*" >&2
    exit 1
}

cd "$(git rev-parse --show-toplevel)"
[ -f VERSION ] || fail "VERSION file is missing"
version=$(cat VERSION)
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' ||
    fail "VERSION must contain X.Y.Z without a v prefix"
# Reject extra lines as well as invalid individual lines.
case "$version" in
    *'
'*) fail "VERSION must contain a single version" ;;
esac

tag="v$version"
tag_ref="refs/tags/$tag"
branch=$(git symbolic-ref --quiet --short HEAD) || fail "check out a branch before releasing"
[ -z "$(git diff --name-only --diff-filter=U)" ] || fail "resolve merge conflicts before releasing"
git remote get-url origin >/dev/null || fail "origin remote is missing"
if git show-ref --verify --quiet "$tag_ref"; then
    fail "tag $tag already exists locally; update VERSION for a new release"
fi

# Check the remote before staging or committing any changes.
if git ls-remote --exit-code --tags origin "$tag_ref" >/dev/null; then
    fail "tag $tag already exists on origin; update VERSION for a new release"
else
    status=$?
    [ "$status" -eq 2 ] || fail "cannot check tags on origin"
fi

printf 'Releasing %s from branch %s\n' "$tag" "$branch"
printf 'Changes included by git add -A:\n'
git status --short --untracked-files=all
source_state=$(python3 scripts/release_state.py)
make release-check || fail "release preflight failed; no commit or tag created"
[ "$(cat VERSION)" = "$version" ] || fail "VERSION changed during preflight"
[ "$(python3 scripts/release_state.py)" = "$source_state" ] || fail "source, index or HEAD changed during preflight; retry checks"
git add -A
git commit --allow-empty -m "chore: release $tag"
git tag -a "$tag" -m "Release $tag"

# A rejected branch update must not publish the tag and trigger a release.
if ! git push --atomic origin "HEAD:refs/heads/$branch" "$tag_ref"; then
    printf 'Release commit and tag are saved locally. After resolving the push error, retry:\n' >&2
    printf '  git push --atomic origin HEAD:refs/heads/%s %s\n' "$branch" "$tag_ref" >&2
    exit 1
fi

printf 'Published %s; the GitHub Actions release workflow will run.\n' "$tag"
