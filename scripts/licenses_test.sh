#!/usr/bin/env bash
# Tests for scripts/licenses.sh. They use a fake go and a fake go-licenses, so
# they need neither network access nor the real tools.
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SCRIPT="$ROOT/scripts/licenses.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

failures=0
total=0

# PATH holds only the fakes and the system tools, so that a real go or
# go-licenses on the developer's PATH is never used.
BIN="$TMP/bin"
SYSPATH=/usr/bin:/bin
mkdir -p "$BIN"

# Fake go. GOROOT and GOPATH come from FAKE_GOROOT and FAKE_GOPATH.
cat > "$BIN/go" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "env GOROOT") echo "$FAKE_GOROOT" ;;
  "env GOPATH") echo "$FAKE_GOPATH" ;;
  "list -m") echo "example.com/mod" ;;
  *) echo "fake go: unexpected arguments: $*" >&2; exit 99 ;;
esac
EOF

# Fake go-licenses. It records its arguments in FAKE_GL_LOG. "save" creates the
# --save_path directory with one license file. FAKE_GL_EXIT is the exit code.
cat > "$BIN/go-licenses" <<'EOF'
#!/usr/bin/env bash
echo "$*" >> "$FAKE_GL_LOG"
echo "$PWD" >> "$FAKE_GL_LOG.cwd"
for arg in "$@"; do
  case "$arg" in
    --save_path=*)
      dir=${arg#--save_path=}
      mkdir -p "$dir/dep" && echo "dep license" > "$dir/dep/LICENSE"
      ;;
  esac
done
exit "${FAKE_GL_EXIT:-0}"
EOF
chmod +x "$BIN/go" "$BIN/go-licenses"

# A fake repository (with a LICENSE), a fake GOROOT (with a LICENSE) and an
# empty call log. Leaves the shell outside the repository.
setup() {
  rm -rf "$TMP/repo" "$TMP/goroot" "$TMP/gopath" "$TMP/nogl" "$TMP/log" "$TMP/log.cwd"
  mkdir -p "$TMP/repo/scripts" "$TMP/repo/sub" "$TMP/goroot" "$TMP/gopath/bin"
  cp "$SCRIPT" "$TMP/repo/scripts/licenses.sh"
  echo "repo license" > "$TMP/repo/LICENSE"
  echo "go license" > "$TMP/goroot/LICENSE"
  : > "$TMP/log"
}

# run_licenses [args...]: sets out and rc. go-licenses is found on PATH. The
# script starts in RUN_DIR (the fake repository by default).
run_licenses() {
  out=$(cd "${RUN_DIR:-$TMP/repo}" && env PATH="$BIN:$SYSPATH" FAKE_GOROOT="$TMP/goroot" FAKE_GOPATH="$TMP/gopath" FAKE_GL_LOG="$TMP/log" bash "$TMP/repo/scripts/licenses.sh" "$@" 2>&1)
  rc=$?
}

result() { # result <name> <condition result: 0 = ok>
  total=$((total + 1))
  if [ "$2" -eq 0 ]; then
    echo "ok   - $1"
  else
    echo "FAIL - $1"
    echo "       rc=$rc output: $out"
    failures=$((failures + 1))
  fi
}

# --- check
setup
run_licenses check
[ "$rc" -eq 0 ] && [ "$(cat "$TMP/log")" = "check ./... --ignore example.com/mod --allowed_licenses=MIT,Apache-2.0,BSD-3-Clause" ]
result "check passes the module and the allowed licenses to go-licenses" $?

setup
FAKE_GL_EXIT=1 run_licenses check
[ "$rc" -ne 0 ] && grep -q "^check " "$TMP/log"
result "check fails when go-licenses reports a disallowed license" $?

# --- save
setup
mkdir -p "$TMP/repo/out"
echo stale > "$TMP/repo/out/stale"
run_licenses save out
[ "$rc" -eq 0 ] && [ ! -e "$TMP/repo/out/stale" ] && [ -f "$TMP/repo/out/dep/LICENSE" ]
result "save replaces the output directory" $?

setup
run_licenses save out
[ "$rc" -eq 0 ] \
  && [ "$(cat "$TMP/repo/out/go/LICENSE")" = "go license" ] \
  && [ "$(cat "$TMP/repo/out/example.com/mod/LICENSE")" = "repo license" ] \
  && grep -q -- "save ./... --ignore example.com/mod --save_path=out" "$TMP/log"
result "save adds the Go license and the license of the module itself" $?

setup
rm "$TMP/goroot/LICENSE"
run_licenses save out
[ "$rc" -ne 0 ] && grep -q "Go license" <<<"$out" && [ ! -e "$TMP/repo/out" ] && [ ! -s "$TMP/log" ]
result "save fails without the Go license, before touching anything" $?

# --- finding go-licenses
setup
mkdir -p "$TMP/nogl"
ln -s "$BIN/go" "$TMP/nogl/go"
out=$(cd "$TMP/repo" && env PATH="$TMP/nogl:$SYSPATH" FAKE_GOROOT="$TMP/goroot" FAKE_GOPATH="$TMP/gopath" FAKE_GL_LOG="$TMP/log" bash "$TMP/repo/scripts/licenses.sh" check 2>&1)
rc=$?
[ "$rc" -ne 0 ] && grep -q "go-licenses not found" <<<"$out"
result "fails with a hint when go-licenses is not installed" $?

setup
mkdir -p "$TMP/nogl"
ln -s "$BIN/go" "$TMP/nogl/go"
cp "$BIN/go-licenses" "$TMP/gopath/bin/go-licenses"
out=$(cd "$TMP/repo" && env PATH="$TMP/nogl:$SYSPATH" FAKE_GOROOT="$TMP/goroot" FAKE_GOPATH="$TMP/gopath" FAKE_GL_LOG="$TMP/log" bash "$TMP/repo/scripts/licenses.sh" check 2>&1)
rc=$?
[ "$rc" -eq 0 ] && grep -q "^check " "$TMP/log"
result "finds go-licenses in GOPATH/bin when it is not on PATH" $?

# --- working directory
setup
RUN_DIR="$TMP/repo/sub" run_licenses check
[ "$rc" -eq 0 ] && [ "$(cat "$TMP/log.cwd")" = "$TMP/repo" ]
result "check runs go-licenses from the repository root, even when started in a subdirectory" $?

setup
RUN_DIR="$TMP/repo/sub" run_licenses save out
[ "$rc" -eq 0 ] && [ -f "$TMP/repo/out/go/LICENSE" ] && [ -f "$TMP/repo/out/example.com/mod/LICENSE" ] && [ ! -e "$TMP/repo/sub/out" ]
result "save started in a subdirectory writes under the repository root" $?

# --- usage
setup
run_licenses
[ "$rc" -eq 2 ] && grep -q "usage" <<<"$out"
result "no arguments prints the usage" $?

setup
run_licenses save
[ "$rc" -eq 2 ] && grep -q "usage" <<<"$out"
result "save without a directory prints the usage" $?

setup
run_licenses frobnicate
[ "$rc" -eq 2 ] && grep -q "usage" <<<"$out"
result "an unknown command prints the usage" $?

setup
run_licenses check extra
[ "$rc" -eq 2 ] && grep -q "usage" <<<"$out"
result "check with an extra argument prints the usage" $?

echo
echo "$((total - failures))/$total passed"
[ "$failures" -eq 0 ]
