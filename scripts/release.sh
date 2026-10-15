#!/usr/bin/env bash
# Create and push a release tag after checking that it is safe to do so.
# Pushing the tag starts .github/workflows/release.yml, which publishes the
# release. Release tags cannot be moved or deleted afterwards (see the
# release-tags-immutable ruleset), so every check happens before the tag exists.
#
# There is no confirmation prompt: run it with --dry-run first to see the
# commit and the release notes, then run it again without --dry-run.
#
# Usage: scripts/release.sh vX.Y.Z [--dry-run]
set -euo pipefail

die() {
  echo "error: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 vX.Y.Z [--dry-run]" >&2
  exit 2
}

tag=""
dry_run=false
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=true ;;
    -*) usage ;;
    *)
      [ -z "$tag" ] || usage
      tag=$arg
      ;;
  esac
done
[ -n "$tag" ] || usage

# Prerelease tags are not supported: how GoReleaser treats them is unverified.
[[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "the version must look like vX.Y.Z (no prerelease part): $tag"

[ "$(git branch --show-current)" = main ] || die "releases are made from main, but the current branch is '$(git branch --show-current)'"
[ -z "$(git status --porcelain)" ] || die "the working tree is not clean; commit or stash your changes first"

git fetch --quiet origin main
sha=$(git rev-parse HEAD)
[ "$sha" = "$(git rev-parse origin/main)" ] || die "main is not the same commit as origin/main; run 'git pull --ff-only origin main' (or push your commits through a pull request)"

if git rev-parse --quiet --verify "refs/tags/$tag" >/dev/null || [ -n "$(git ls-remote --tags origin "refs/tags/$tag")" ]; then
  die "the tag $tag already exists"
fi

repo=$(gh repo view --json nameWithOwner -q .nameWithOwner)
ci=$(gh api "repos/$repo/commits/$sha/check-runs" --jq '[.check_runs[] | select(.name == "test") | .conclusion] | first // "none"')
[ "$ci" = success ] || die "CI (the 'test' check) has not succeeded for $sha: $ci"

echo "Release $tag from $sha"
echo
echo "Release notes preview:"
gh api -X POST "repos/$repo/releases/generate-notes" -f tag_name="$tag" -f target_commitish="$sha" --jq .body
echo

if $dry_run; then
  echo "dry run: no tag was created. Run it again without --dry-run to publish $tag."
  exit 0
fi

echo "Creating and pushing $tag (a release tag cannot be moved or deleted afterwards)"
git tag -a "$tag" -m "Release $tag" "$sha"
if ! git push origin "refs/tags/$tag"; then
  git tag -d "$tag" >/dev/null
  die "pushing the tag failed; the local tag was removed"
fi

echo
echo "Pushed $tag. Watch the release: https://github.com/$repo/actions/workflows/release.yml"
