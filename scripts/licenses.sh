#!/usr/bin/env bash
# Check and collect the licenses of the dependencies compiled into the binary.
#
# Usage:
#   scripts/licenses.sh check        fail if a dependency has a license that is not allowed
#   scripts/licenses.sh save <dir>   collect the license files into <dir>
#
# Needs go-licenses: go install github.com/google/go-licenses/v2@v2.0.1
set -euo pipefail

# Work from the repository root, wherever the script is started: ./... and
# LICENSE below are relative to it, and so is the <dir> argument. Started in a
# subdirectory, go-licenses would otherwise check nothing and still succeed.
cd "$(dirname "$0")/.."

# The licenses that dependencies may have. To allow another one, add it here.
ALLOWED=MIT,Apache-2.0,BSD-3-Clause

die() {
  echo "error: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 check | save <dir>" >&2
  exit 2
}

case "${1:-}" in
  check) [ $# -eq 1 ] || usage ;;
  save) [ $# -eq 2 ] || usage ;;
  *) usage ;;
esac

# go install puts go-licenses in GOPATH/bin, which is not always on PATH
# (for example when GoReleaser runs this script from a hook).
command -v go-licenses >/dev/null 2>&1 || PATH="$(go env GOPATH)/bin:$PATH"
command -v go-licenses >/dev/null 2>&1 || die "go-licenses not found; run: go install github.com/google/go-licenses/v2@v2.0.1"

# The module itself is not a dependency.
module=$(go list -m)

case "$1" in
  check)
    go-licenses check ./... --ignore "$module" --allowed_licenses="$ALLOWED"
    ;;
  save)
    dir=$2
    # go-licenses skips the Go standard library, whose license must be shipped too.
    go_license="$(go env GOROOT)/LICENSE"
    [ -f "$go_license" ] || die "the Go license was not found: $go_license"
    rm -rf "$dir"
    go-licenses save ./... --ignore "$module" --save_path="$dir"
    mkdir -p "$dir/go" "$dir/$module"
    cp "$go_license" "$dir/go/LICENSE"
    cp LICENSE "$dir/$module/LICENSE"
    ;;
esac
