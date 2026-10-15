# 配布物の依存ライブラリのライセンス表示 実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** リリースのアーカイブと Docker イメージに、依存ライブラリ、Go、自分自身のライセンスを含め、許可していない種類のライセンスの依存が入ったら CI で失敗させる。

**Architecture:** `scripts/licenses.sh` が `go-licenses`（Google 製）を包み、`check`（許可リストの検査）と `save <dir>`（ライセンスのファイルの収集に、Go と自分自身のライセンスを足す）を提供する。リリース時に GoReleaser の `before.hooks` で `licenses/` を生成し、アーカイブと `Dockerfile.release` のイメージに含める。

**Tech Stack:** bash、`go-licenses` v2.0.1、GoReleaser v2.18.3、GitHub Actions。

**Spec:** `docs/superpowers/specs/2026-10-15-license-notices-design.md`（実装者は計画と仕様書の両方を読むこと）

## Global Constraints

- 許可するライセンスは `MIT,Apache-2.0,BSD-3-Clause` の3つだけ。許可リストは `scripts/licenses.sh` の1か所だけに書く。
- `go-licenses` は `go install github.com/google/go-licenses/v2@v2.0.1`（版を固定）。`go.mod` には入れない。ワークフローでは、既存と同じ方針で、サードパーティのアクションを SHA で固定する（今回は新しいアクションを使わない）。
- 生成するフォルダの名前は `licenses/`（リポジトリのルート）。コミットしない（`.gitignore` の `/licenses/`）。中身は、依存のライセンス、`go/LICENSE`、`<モジュール名>/LICENSE`。
- スクリプトのテストは、偽の `go` と偽の `go-licenses` を使い、ネットワークと実際のツールを必要としない。`PATH` は、偽物とシステムのコマンドだけにする。
- Go 標準ライブラリのライセンス（`$(go env GOROOT)/LICENSE`）がなければ、黙って省略せず、失敗する。
- 作業ディレクトリは、リポジトリのルート。ブランチは `feat/license-notices`（作成済み）。`main` に直接コミットしない。
- コミットメッセージは件名のみ（本文なし）。`--no-verify` / `--no-gpg-sign` は使わない。`git commit` が失敗またはタイムアウトしたら、リトライせず停止して報告する。コミットには、件名の後ろに `-m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"` を別の `-m` として付ける。
- シェルは zsh。変数を単語に分割しない、`$PIPESTATUS` ではなく `$pipestatus` を使う、`$VAR:l` のような `:` の後の1文字が修飾子になる（`${VAR}:l` と書く）といった違いに注意する。スクリプトは bash で書き、`bash scripts/...` で実行する。
- コード（シェル、YAML）内のコメントは英語、README / CLAUDE.md は日本語。
- PR のマージ、タグの作成と push、公開物の変更は、ユーザーが指示するまで実行しない。

## Review Focus

仕様書が暗に求めているが、タスクの手順以外では見落としやすい状況を、起きやすい順に挙げる。各行は担当タスクの確認手順で固定する。

1. CI の Go に `GOROOT/LICENSE` がない、または場所が違う。リリースが黙ってライセンスを欠いたまま公開されず、失敗すること（Task 1 のテスト `save fails without the Go license`、Task 4 Step 3 の snapshot）。
2. 依存の更新で、許可していない種類のライセンス（GPL、ISC など）が入る。PR の CI が落ちること（Task 2 Step 5 の許可リストを狭めた確認）。
3. アーカイブで、同名の `LICENSE` ファイルがフォルダ構成を失って1つに潰れる。静かにライセンスが欠けること（Task 3 Step 5 のアーカイブの一覧）。
4. ローカルで再実行したとき、前回の `licenses/` の古いファイルが混ざる。削除してから保存すること（Task 1 のテスト `save replaces the output directory`）。
5. イメージに `/licenses` が入っていない、または `COPY` のパスが違う。snapshot のビルドが失敗し、リリース後に `ls /licenses` で確認できること（Task 4 Step 3、Step 6）。
6. `go install` した `go-licenses` が PATH にない（GoReleaser の `before.hooks` から呼ばれる場合）。`GOPATH/bin` を足して見つけること（Task 1 のテスト `finds go-licenses in GOPATH/bin when it is not on PATH`）。

---

## ファイル構成

| ファイル | 操作 | 責務 |
|---|---|---|
| `scripts/licenses.sh` | 作成 | `check` と `save <dir>` |
| `scripts/licenses_test.sh` | 作成 | 偽の `go` と `go-licenses` を使ったテスト |
| `.github/workflows/ci.yml` | 変更 | `go-licenses` のインストール、`check`、テストの実行 |
| `.goreleaser.yaml` | 変更 | `before.hooks`、アーカイブの `files`、`dockers_v2` の `extra_files` |
| `Dockerfile.release` | 変更 | `COPY licenses /licenses` |
| `.gitignore` | 変更 | `/licenses/` |
| `.github/workflows/release.yml` | 変更 | `go-licenses` のインストールと `check` |
| `README.md`、`CLAUDE.md` | 変更 | 配布物の `licenses/`、許可リストの変え方 |

---

### Task 1: ライセンスのスクリプトとテスト

**Files:**
- Create: `scripts/licenses.sh`
- Create: `scripts/licenses_test.sh`

**Interfaces:**
- Consumes: `go-licenses` の `check ./... --ignore <module> --allowed_licenses=<list>` と `save ./... --ignore <module> --save_path=<dir>`、`go list -m`、`go env GOROOT`、`go env GOPATH`、リポジトリ直下の `LICENSE`。
- Produces: `scripts/licenses.sh check`（失敗時は非ゼロで終了）と `scripts/licenses.sh save <dir>`（`<dir>` に依存のライセンス、`go/LICENSE`、`<モジュール名>/LICENSE`）。Task 2 と 3 が使う。

- [ ] **Step 1: テストを書く**

`scripts/licenses_test.sh`:

````bash
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
  rm -rf "$TMP/repo" "$TMP/goroot" "$TMP/gopath" "$TMP/nogl" "$TMP/log"
  mkdir -p "$TMP/repo" "$TMP/goroot" "$TMP/gopath/bin"
  echo "repo license" > "$TMP/repo/LICENSE"
  echo "go license" > "$TMP/goroot/LICENSE"
  : > "$TMP/log"
}

# run_licenses [args...]: sets out and rc. go-licenses is found on PATH.
run_licenses() {
  out=$(cd "$TMP/repo" && env PATH="$BIN:$SYSPATH" FAKE_GOROOT="$TMP/goroot" FAKE_GOPATH="$TMP/gopath" FAKE_GL_LOG="$TMP/log" bash "$SCRIPT" "$@" 2>&1)
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
[ "$rc" -ne 0 ]
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
out=$(cd "$TMP/repo" && env PATH="$TMP/nogl:$SYSPATH" FAKE_GOROOT="$TMP/goroot" FAKE_GOPATH="$TMP/gopath" FAKE_GL_LOG="$TMP/log" bash "$SCRIPT" check 2>&1)
rc=$?
[ "$rc" -ne 0 ] && grep -q "go-licenses not found" <<<"$out"
result "fails with a hint when go-licenses is not installed" $?

setup
mkdir -p "$TMP/nogl"
ln -s "$BIN/go" "$TMP/nogl/go"
cp "$BIN/go-licenses" "$TMP/gopath/bin/go-licenses"
out=$(cd "$TMP/repo" && env PATH="$TMP/nogl:$SYSPATH" FAKE_GOROOT="$TMP/goroot" FAKE_GOPATH="$TMP/gopath" FAKE_GL_LOG="$TMP/log" bash "$SCRIPT" check 2>&1)
rc=$?
[ "$rc" -eq 0 ] && grep -q "^check " "$TMP/log"
result "finds go-licenses in GOPATH/bin when it is not on PATH" $?

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
````

- [ ] **Step 2: 失敗することを確かめる（RED）**

Run: `chmod +x scripts/licenses_test.sh && bash scripts/licenses_test.sh 2>&1 | tail -6; echo "exit=${pipestatus[1]}"`
Expected: スクリプトがないため、全ケースが `FAIL`（`rc=127`）で、`0/11 passed`。

- [ ] **Step 3: スクリプトを書く**

`scripts/licenses.sh`:

```bash
#!/usr/bin/env bash
# Check and collect the licenses of the dependencies compiled into the binary.
#
# Usage:
#   scripts/licenses.sh check        fail if a dependency has a license that is not allowed
#   scripts/licenses.sh save <dir>   collect the license files into <dir>
#
# Needs go-licenses: go install github.com/google/go-licenses/v2@v2.0.1
set -euo pipefail

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
```

- [ ] **Step 4: テストが通ることを確かめる（GREEN）**

Run: `chmod +x scripts/licenses.sh && bash -n scripts/licenses.sh && bash scripts/licenses_test.sh 2>&1 | tail -16`
Expected: 全ケースが `ok` で、`11/11 passed`。

- [ ] **Step 5: テストが空振りでないことを確かめる（変異）**

許可リストと `rm -rf` を、それぞれ壊して、対応するテストだけが落ちることを確認する。

```sh
S=<scratchpad のパス>
rm -rf $S/m && mkdir -p $S/m/scripts && cp scripts/licenses.sh scripts/licenses_test.sh $S/m/scripts/ && cp LICENSE $S/m/
sed -i 's/^ALLOWED=.*/ALLOWED=MIT/' $S/m/scripts/licenses.sh && echo "== allow list changed:" && bash $S/m/scripts/licenses_test.sh 2>&1 | grep -E '^FAIL|passed'
cp scripts/licenses.sh $S/m/scripts/ && sed -i '/rm -rf "\$dir"/d' $S/m/scripts/licenses.sh && echo "== rm -rf removed:" && bash $S/m/scripts/licenses_test.sh 2>&1 | grep -E '^FAIL|passed'
rm -rf $S/m
```

Expected: 1つ目は `check passes the module and the allowed licenses to go-licenses` だけが `FAIL`（`10/11 passed`）。2つ目は `save replaces the output directory` だけが `FAIL`（`10/11 passed`）。

- [ ] **Step 6: Commit**

```sh
git add scripts/licenses.sh scripts/licenses_test.sh
git commit -m "Add a script to check and collect dependency licenses" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 2: CI への組み込み

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: Task 1 の `scripts/licenses.sh check` と `scripts/licenses_test.sh`。
- Produces: PR と `main` への push で、許可リストの検査とスクリプトのテストが動く CI。Task 4 が、PR で緑になることを確認する。

- [ ] **Step 1: `ci.yml` に、ステップを足す**

`Test scripts` ステップの後ろに、次を足す。

```yaml
      - name: Test license scripts
        run: bash scripts/licenses_test.sh
      - name: Install go-licenses
        run: go install github.com/google/go-licenses/v2@v2.0.1
      - name: Check licenses
        run: scripts/licenses.sh check
```

- [ ] **Step 2: actionlint で検査する**

Run: `"$(go env GOPATH)/bin/actionlint" .github/workflows/*.yml; echo "exit=$?"`
Expected: 出力なしで `exit=0`。

- [ ] **Step 3: 実際の `go-licenses` で `check` が通ることを確かめる**

ローカルには `go-licenses` v2.0.1 が入っている（`$(go env GOPATH)/bin/go-licenses`）。入っていなければ、`go install github.com/google/go-licenses/v2@v2.0.1`。

Run: `scripts/licenses.sh check; echo "exit=$?"`
Expected: `exit=0`（出力は、非 Go コードの警告だけ。違反の行は出ない）。

- [ ] **Step 4: スクリプトのテストが、CI と同じ方法で通ることを確かめる**

Run: `bash scripts/licenses_test.sh 2>&1 | tail -2 && bash scripts/release_test.sh 2>&1 | tail -1`
Expected: `11/11 passed` と `12/12 passed`。

- [ ] **Step 5: 許可リストにない種類で、`check` が失敗することを確かめる（Review Focus 2）**

scratch にスクリプトのコピーを作り、許可リストから `MIT` を外して、実際の `go-licenses` で失敗することを確認する。

```sh
S=<scratchpad のパス>
rm -rf $S/m && mkdir -p $S/m && cp scripts/licenses.sh $S/m/licenses.sh
sed -i 's/^ALLOWED=.*/ALLOWED=Apache-2.0,BSD-3-Clause/' $S/m/licenses.sh
bash $S/m/licenses.sh check >/dev/null 2>&1; echo "MIT not allowed -> exit $?"
bash $S/m/licenses.sh check 2>&1 | grep 'Not allowed' | head -3
rm -rf $S/m
```

Expected: `exit 1`。`Not allowed license 'MIT' found for library ...` の行が出る。

- [ ] **Step 6: Commit**

```sh
git add .github/workflows/ci.yml
git commit -m "Check dependency licenses in CI" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 3: リリースへの組み込みと文書

**Files:**
- Modify: `.goreleaser.yaml`
- Modify: `Dockerfile.release`
- Modify: `.gitignore`
- Modify: `.github/workflows/release.yml`
- Modify: `README.md`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: Task 1 の `scripts/licenses.sh save <dir>` と `check`。GoReleaser v2.18.3。
- Produces: リリースのアーカイブの `licenses/`、イメージの `/licenses/`。Task 4 が確認する。

- [ ] **Step 1: `.gitignore` に追加する**

```sh
printf '/licenses/\n' >> .gitignore && tail -4 .gitignore
```

Expected: 末尾に `dist/` と `/licenses/` がある（`.gitignore` の末尾に改行があること。なければ、`printf '\n/licenses/\n'`）。

- [ ] **Step 2: `.goreleaser.yaml` を変える**

`project_name: misskey_exporter` の後ろ（`builds:` の前）に、次を足す。

```yaml
before:
  hooks:
    # Collect the licenses of the dependencies; the archives and the image ship them.
    - scripts/licenses.sh save licenses
```

`archives` を、次のように変える（`files` を足す。指定すると既定の `LICENSE` と `README` が置き換わるので、明示する）。

```yaml
archives:
  - id: misskey_exporter
    formats:
      - tar.gz
    name_template: "misskey_exporter_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - README.md
      - licenses/**
```

`dockers_v2` の `dockerfile: Dockerfile.release` の次の行に、次を足す。

```yaml
    extra_files:
      - licenses
```

- [ ] **Step 3: `Dockerfile.release` を変える**

`COPY $TARGETPLATFORM/misskey_exporter /misskey_exporter` の次の行に、次を足す。

```dockerfile
COPY licenses /licenses
```

- [ ] **Step 4: 設定を検査する**

Run: `"$(go env GOPATH)/bin/goreleaser" check; echo "exit=$?"`
Expected: 設定が検証されて `exit=0`（`files` と `extra_files` と `before` が、未知のキーとして拒否されないこと）。

- [ ] **Step 5: アーカイブの構成を確かめる（Review Focus 3）**

ローカルの Go には `GOROOT/LICENSE` がないので、本物の生成（`before`）は動かせない。手作りの `licenses/` を置き、`before` と `docker` を除いて snapshot を作る。

```sh
mkdir -p licenses/example.com/dep licenses/go licenses/misskey_exporter
printf 'dummy\n' > licenses/example.com/dep/LICENSE
printf 'dummy\n' > licenses/go/LICENSE
printf 'dummy\n' > licenses/misskey_exporter/LICENSE
"$(go env GOPATH)/bin/goreleaser" release --snapshot --clean --skip=before,docker > /dev/null 2>&1; echo "exit=$?"
tar -tzf dist/misskey_exporter_*_linux_amd64.tar.gz
rm -rf licenses dist
git status --short
```

Expected: `exit=0`。tar の一覧に、`LICENSE`、`README.md`、`misskey_exporter`（バイナリ）、`licenses/example.com/dep/LICENSE`、`licenses/go/LICENSE`、`licenses/misskey_exporter/LICENSE` が、**別々のパスで**ある（フォルダ構成が保たれる）。`git status --short` には、変更したファイルだけが出て、`licenses/` と `dist/` は出ない。

もし `LICENSE` が1つに潰れている（フォルダ構成が失われる）場合は、`files` を次のオブジェクト形式に変えて、やり直す。

```yaml
    files:
      - LICENSE
      - README.md
      - src: licenses/**
        dst: licenses
```

- [ ] **Step 6: `release.yml` に、検査を足す**

`Test` ステップ（`go test -race ./...`）の後ろ、QEMU の前に、次を足す。

```yaml
      - name: Install go-licenses
        run: go install github.com/google/go-licenses/v2@v2.0.1
      - name: Check licenses
        run: scripts/licenses.sh check
```

- [ ] **Step 7: 構造を確かめる**

```sh
"$(go env GOPATH)/bin/actionlint" .github/workflows/*.yml; echo "actionlint_exit=$?"
echo "--- check before goreleaser:"; awk '/Check licenses/{c=NR} /goreleaser-action/{g=NR} END{exit !(c && g && c<g)}' .github/workflows/release.yml; echo "order_exit=$?"
echo "--- unpinned uses:"; grep -hE 'uses:' .github/workflows/*.yml | grep -vE '@[0-9a-f]{40} # v[0-9]'; echo "unpinned_exit=$?"
```

Expected: `actionlint_exit=0`、`order_exit=0`、`unpinned_exit=1`（SHA 固定でない行がない）。

- [ ] **Step 8: `README.md` に追記する**

`## ライセンス` の節を、次の内容にする（既存の `[MIT License](LICENSE)` の行の後ろに足す）。

```markdown
リリースのアーカイブと Docker イメージ（`/licenses/`）には、依存ライブラリと Go のライセンスが、フォルダ `licenses/` に入っています。
```

- [ ] **Step 9: `CLAUDE.md` に追記する**

Commands のコードブロックで、`bash scripts/release_test.sh` の行の下に足す。

```
bash scripts/licenses_test.sh                     # scripts/licenses.sh のテスト（偽の go と go-licenses を使う。CI でも動く）
scripts/licenses.sh check                         # 依存のライセンスが許可リストの範囲か（要 go-licenses: go install github.com/google/go-licenses/v2@v2.0.1）
```

Gotchas の `Dockerfile は2つある。` の項目の前に、次を足す。

```
- 配布する依存のライセンスは、リリース時に `scripts/licenses.sh save licenses` が生成し（GoReleaser の `before.hooks`）、アーカイブの `licenses/` とイメージの `/licenses/` に入る。`licenses/` はコミットしない。許可するライセンスの種類（`MIT`、`Apache-2.0`、`BSD-3-Clause`）は `scripts/licenses.sh` の `ALLOWED` の1か所だけにあり、新しい種類の依存が入ると CI が落ちる。許可するときは `ALLOWED` に足す。`go-licenses` の版（`v2.0.1`）は `ci.yml` と `release.yml` の `go install` で固定していて、手動で上げる。Go 標準ライブラリのライセンスは `$(go env GOROOT)/LICENSE` から取るので、ディストリのパッケージの Go など、そのファイルがない環境では `save` が失敗する。
```

- [ ] **Step 10: 全体を確かめて、Commit する**

```sh
go vet ./... && go test -race -count=1 ./... 2>&1 | tail -1
bash scripts/licenses_test.sh 2>&1 | tail -1
bash scripts/release_test.sh 2>&1 | tail -1
git status --short
git add .goreleaser.yaml Dockerfile.release .gitignore .github/workflows/release.yml README.md CLAUDE.md
git commit -m "Ship dependency licenses in the archives and the image" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

Expected: テストが `ok`、`11/11 passed`、`12/12 passed`。`git status --short` に出るのは、変更した6ファイルだけ。

---

### Task 4: PR、CI での確認、リリース後の確認（ユーザー操作を含む）

コードの変更はない。外部への公開を伴う操作を含むので、各ステップの前に、ユーザーの指示を確認する。

**Files:** なし

**Interfaces:**
- Consumes: Task 1〜3 のコミットを含む `feat/license-notices`。
- Produces: マージ済みの変更と、次のリリースの、ライセンス入りのアーカイブとイメージ。

- [ ] **Step 1: push して PR を作り、CI が緑になることを確かめる**

```sh
git push -u origin feat/license-notices
gh pr create --base main --head feat/license-notices --title "Ship dependency licenses in the archives and the image"
gh pr checks --watch
```

PR の本文は、日本語で概要と確認手順を書き、末尾に `🤖 Generated with [Claude Code](https://claude.com/claude-code)` を付ける。バッククォートをエスケープしないこと。

Expected: `ci` の `test` ジョブが成功する。ログで、`Install go-licenses`、`Check licenses`、`Test license scripts` が成功している（CI の Go 1.26 系で、`go-licenses` が動くことの確認。未確認事項の1つ）。

- [ ] **Step 2: マージする（ユーザーの指示のあと）**

ユーザーが指示したら、通常のマージコミットでマージし、`main` を取り込む。

```sh
gh pr merge <PR番号> --merge
git switch main && git pull --ff-only origin main
```

- [ ] **Step 3: snapshot ビルドを CI 上で回す**

```sh
gh workflow run release.yml --ref main
sleep 8
ID=$(gh run list --workflow=release.yml --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$ID" --exit-status
```

Expected: 成功する。ログで、`scripts/licenses.sh save licenses` の実行（`before.hooks`）が成功している（`GOROOT/LICENSE` が、CI の Go にあることの確認。Review Focus 1）。イメージのビルド（`COPY licenses /licenses` と `extra_files` がフォルダを受け付けること。Review Focus 5）も成功している。何も公開されない（Releases とパッケージに変化がない）。

失敗したら、ログから原因を特定し、別のブランチと PR で直す。

- [ ] **Step 4: 次のバージョンを打つ（ユーザー操作）**

ユーザーが `scripts/release.sh v<X.Y.Z> --dry-run` で内容を確認し、`--dry-run` を外して実行する。このリリースが、ライセンス入りの最初のリリースになる。

- [ ] **Step 5: アーカイブの中身を確かめる**

```sh
D=$(mktemp -d) && cd "$D" && gh release download v<X.Y.Z> --repo onodai145/misskey_exporter --pattern '*_linux_amd64.tar.gz' && tar -tzf misskey_exporter_*_linux_amd64.tar.gz | grep -E 'LICENSE|NOTICE' | sort | head -50; cd - >/dev/null; rm -rf "$D"
```

Expected: `LICENSE`、`licenses/go/LICENSE`、`licenses/misskey_exporter/LICENSE`、依存の `LICENSE` と、`NOTICE`（`licenses/github.com/prometheus/...` など6つ）、入れ子のライセンス（`.../client_golang/internal/.../gddo/LICENSE`）が、別々のパスで並ぶ。

- [ ] **Step 6: イメージの中身を確かめる**

```sh
I=ghcr.io/onodai145/misskey_exporter
docker run --rm --entrypoint ls "${I}:<X.Y.Z>" /licenses
docker run --rm --entrypoint ls "${I}:<X.Y.Z>" /licenses/go /licenses/misskey_exporter
docker rmi "${I}:<X.Y.Z>" "${I}:latest"
```

Expected: `/licenses` に、依存のフォルダ、`go`、`misskey_exporter` がある。`/licenses/go` に `LICENSE`、`/licenses/misskey_exporter` に `LICENSE` がある。確認で pull したイメージは、`docker rmi` で消す。

- [ ] **Step 7: ブランチを片付ける（ユーザーの指示があるとき）**

```sh
git branch -d feat/license-notices
git push origin --delete feat/license-notices
git fetch --prune
```

---

## Self-Review

**1. 仕様書との対応**

| 仕様書 | 担当 |
|---|---|
| F1（アーカイブに `licenses/`） | Task 3 Step 2、Step 5、Task 4 Step 5 |
| F2（イメージの `/licenses/`） | Task 3 Step 2、Step 3、Task 4 Step 3、Step 6 |
| F3（リリース時に生成、コミットしない） | Task 3 Step 1、Step 2（`before.hooks`） |
| F4（許可リストの検査、CI とリリース） | Task 1、Task 2、Task 3 Step 6 |
| F5（Go のライセンス、なければ失敗） | Task 1（`save` とテスト）、Task 4 Step 3 |
| N1（許可リストは1か所） | Task 1 Step 3 の `ALLOWED` |
| N2（`go-licenses` の版の固定） | Task 2 Step 1、Task 3 Step 6 |
| N3（既存の方針を変えない） | Task 3 Step 7（SHA 固定の確認） |
| N4（偽物でテスト） | Task 1 |
| §4（ファイル一覧） | ファイル構成の表（すべて担当あり） |
| §8（検証） | Task 1〜4 |
| §9（未確認） | `archives.files` は Task 3 Step 5、`extra_files` は Task 4 Step 3、`GOROOT/LICENSE` と Go 1.26 系は Task 4 Step 1、Step 3 |
| §10（完了条件） | Task 4 |

**2. プレースホルダ:** 手順に TBD や「あとで」はない。`<scratchpad のパス>`、`<X.Y.Z>`、`<PR番号>` は、実行時に決まる値で、手順に書いてある。実行時に確定するもの（`files` の挙動）は、手順と期待値と、代わりの書き方を書いてある。

**3. 名前の一貫性:** スクリプト名（`scripts/licenses.sh`、`scripts/licenses_test.sh`）、サブコマンド（`check`、`save <dir>`）、フォルダ（`licenses/`、イメージの `/licenses`）、許可リスト（`MIT,Apache-2.0,BSD-3-Clause`）、`go-licenses` の版（`v2.0.1`）は、全タスクで同じ。

**4. Review Focus:** 6項目とも、担当ステップを割り当てた。
