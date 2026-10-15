#!/usr/bin/env bash
# Tests for scripts/release.sh. They use throwaway git repositories and a fake
# gh, so they need neither network access nor a real GitHub repository.
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT="$ROOT/scripts/release.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

failures=0
total=0

# Fake gh. FAKE_CI is the conclusion reported for the "test" check run.
mkdir "$TMP/bin"
cat > "$TMP/bin/gh" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "repo view --json nameWithOwner -q .nameWithOwner") echo "example/repo" ;;
  "api repos/example/repo/commits/"*"/check-runs "*) echo "${FAKE_CI:-success}" ;;
  "api -X POST repos/example/repo/releases/generate-notes "*) echo "## What's Changed (fake release notes)" ;;
  *) echo "fake gh: unexpected arguments: $*" >&2; exit 99 ;;
esac
EOF
chmod +x "$TMP/bin/gh"

# A bare origin and a clone on main with one commit. Leaves the shell in the clone.
setup() {
  rm -rf "$TMP/origin.git" "$TMP/work" "$TMP/other"
  # Name the branch explicitly: the default depends on init.defaultBranch, and
  # a clone of an origin whose HEAD points to a missing branch has no HEAD.
  git init -q --bare --initial-branch=main "$TMP/origin.git"
  git clone -q "$TMP/origin.git" "$TMP/work" 2>/dev/null
  cd "$TMP/work" || exit 1
  git config user.email t@example.com
  git config user.name t
  git config commit.gpgsign false
  git config tag.gpgsign false
  git checkout -q -B main
  echo a > a
  git add a
  git commit -q -m init
  git push -q -u origin main
}

# A second clone of origin, to move origin ahead of the work clone.
other_clone() {
  git clone -q "$TMP/origin.git" "$TMP/other" 2>/dev/null
  git -C "$TMP/other" config user.email t@example.com
  git -C "$TMP/other" config user.name t
  git -C "$TMP/other" config commit.gpgsign false
  git -C "$TMP/other" config tag.gpgsign false
}

# run_release [args...]: sets out and rc. stdin is closed: the script must not prompt.
run_release() {
  out=$(PATH="$TMP/bin:$PATH" bash "$SCRIPT" "$@" 2>&1 </dev/null)
  rc=$?
}

check() { # check <name> <condition result: 0 = ok>
  total=$((total + 1))
  if [ "$2" -eq 0 ]; then
    echo "ok   - $1"
  else
    echo "FAIL - $1"
    echo "       rc=$rc output: $out"
    failures=$((failures + 1))
  fi
}

no_tag_anywhere() {
  [ -z "$(git -C "$TMP/work" tag -l)" ] && [ -z "$(git ls-remote --tags "$TMP/origin.git")" ]
}

expect_refusal() { # expect_refusal <name> <expected text in output> [args...]
  local name=$1 text=$2
  shift 2
  run_release "$@"
  [ "$rc" -ne 0 ] && grep -q -- "$text" <<<"$out" && no_tag_anywhere
  check "$name" $?
}

# --- version format
setup
expect_refusal "rejects a version without v" "vX.Y.Z" 1.0.0
expect_refusal "rejects a two-part version" "vX.Y.Z" v1.0
expect_refusal "rejects a prerelease version" "vX.Y.Z" v1.0.0-rc.1

# --- repository state
setup
git checkout -q -b side
expect_refusal "rejects a branch other than main" "main" v1.0.0

setup
echo x >> a
expect_refusal "rejects uncommitted changes" "clean" v1.0.0

setup
other_clone
echo b > "$TMP/other/b"; git -C "$TMP/other" add b; git -C "$TMP/other" commit -q -m ahead; git -C "$TMP/other" push -q origin HEAD:main
expect_refusal "rejects a main that is behind origin/main" "origin/main" v1.0.0

setup
git tag v1.0.0
run_release v1.0.0
[ "$rc" -ne 0 ] && grep -q "already exists" <<<"$out" && [ -z "$(git ls-remote --tags "$TMP/origin.git")" ]
check "rejects a tag that exists locally" $?

setup
other_clone
git -C "$TMP/other" tag v1.0.0; git -C "$TMP/other" push -q origin v1.0.0
run_release v1.0.0
[ "$rc" -ne 0 ] && grep -q "already exists" <<<"$out" && [ -z "$(git tag -l)" ]
check "rejects a tag that exists only on origin" $?

# --- CI status
setup
FAKE_CI=failure expect_refusal "rejects a commit whose CI failed" "CI" v1.0.0
FAKE_CI=none expect_refusal "rejects a commit with no CI result" "CI" v1.0.0

# --- dry run
setup
run_release v1.0.0 --dry-run
[ "$rc" -eq 0 ] && grep -q "fake release notes" <<<"$out" && grep -q "dry run" <<<"$out" && grep -q -- "without --dry-run" <<<"$out" && no_tag_anywhere
check "dry run shows the release notes, says how to publish and creates nothing" $?

# --- success
setup
run_release v1.0.0
head=$(git rev-parse HEAD)
[ "$rc" -eq 0 ] \
  && [ "$(git cat-file -t v1.0.0 2>/dev/null)" = "tag" ] \
  && [ "$(git rev-parse 'v1.0.0^{commit}')" = "$head" ] \
  && [ "$(git ls-remote "$TMP/origin.git" 'refs/tags/v1.0.0^{}' | cut -f1)" = "$head" ]
check "creates an annotated tag on HEAD and pushes it" $?

echo
echo "$((total - failures))/$total passed"
[ "$failures" -eq 0 ]
