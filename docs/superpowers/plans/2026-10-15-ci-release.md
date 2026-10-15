# CI とリリース自動化 実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** PR と `main` への push でテストを回し、`v*` タグの push でバイナリ（linux/amd64、linux/arm64）とコンテナイメージ（GHCR）を GitHub Release として公開する。

**Architecture:** GitHub Actions の2つのワークフロー（`ci.yml` と `release.yml`）。リリースは GoReleaser が、ビルド、tar.gz、チェックサム、GitHub Release、GHCR へのマルチアーキテクチャイメージの push をまとめて行う。イメージ用に、ビルド済みバイナリを `COPY` するだけの `Dockerfile.release` を足し、ローカル用の `Dockerfile` は変えない。

**Tech Stack:** GitHub Actions、GoReleaser v2.18.3、docker buildx と QEMU、`prometheus/common/version`（ldflags の埋め込み先）。

**Spec:** `docs/superpowers/specs/2026-10-15-ci-release-design.md`（実装者は計画と仕様書の両方を読むこと）

## Global Constraints

- アクションは、タグではなくコミット SHA で固定し、行末のコメントにバージョンを書く。固定する SHA は次のとおり（2026-10-15 に `gh api` で取得）。

  | アクション | バージョン | SHA |
  |---|---|---|
  | `actions/checkout` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
  | `actions/setup-go` | v7.0.0 | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` |
  | `docker/setup-qemu-action` | v4.4.0 | `99012661954931238ded8c8b007157a8430204e1` |
  | `docker/setup-buildx-action` | v4.4.1 | `f87e5991a6d7451dcb8d9637bfbc97413f497069` |
  | `docker/login-action` | v4.6.0 | `dbcb813823bdd20940b903addbd779551569679f` |
  | `goreleaser/goreleaser-action` | v7.2.3 | `f06c13b6b1a9625abc9e6e439d9c05a8f2190e94` |

- GoReleaser 本体は `v2.18.3` に固定する（アクションの `version` 入力に指定。Dependabot の対象外なので、更新は手動）。
- Go のバージョンは、`setup-go` の `go-version: "1.26"` と `check-latest: true` で、1.26 系の最新を使う（最終レビューで変更。`go.mod` は `go 1.26.0` とパッチ版まで指定しているので、`go-version-file` だと 1.26.0 がそのまま入る）。公開用の `release.yml` は `cache: false`。
- 権限は最小にする。`ci.yml` は `contents: read` だけ。`release.yml` は `contents: write` と `packages: write` だけ。
- イメージ名は `ghcr.io/onodai145/misskey_exporter`。イメージのタグは `{{ .Version }}`（`v` なし、例 `0.2.0`）と `latest`。
- ビルドは `CGO_ENABLED=0`、`-trimpath`、linux の amd64 と arm64 だけ。ldflags は、既定値を置き換えて `version.Version`、`version.Revision`、`version.BuildDate`（パッケージは `github.com/prometheus/common/version`）を明示する。
- `Dockerfile`（ローカルの `docker build .` 用）は変更しない。
- 作業ディレクトリは、リポジトリのルート。ブランチは `feat/ci-release`（作成済み）。`main` に直接コミットしない。
- コミットメッセージは件名のみ（本文なし）。`--no-verify` / `--no-gpg-sign` は使わない。`git commit` が失敗またはタイムアウトしたら、リトライせず停止して報告する。コミットには、件名の後ろに `-m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"` を別の `-m` として付ける。
- コード（YAML）内のコメントは英語、README / CLAUDE.md は日本語。
- タグの作成と push、PR のマージ、パッケージの公開設定の変更は、ユーザーが指示するまで実行しない。

## Review Focus

仕様書が暗に求めているが、タスクの手順以外では見落としやすい状況を、起きやすい順に挙げる。各行は担当タスクの確認手順で固定する。

1. `gofmt` をかけ忘れたコミットを PR に出す。CI が落ちること（Task 1 Step 5）。
2. テストが落ちる状態のコミットにタグを打つ。何も公開されないこと。`release.yml` でテストが GoReleaser より前にあること（Task 3 Step 3）。
3. checkout が浅くてタグが見えない。バージョンが `0.0.0-...` や `unknown` になること。`fetch-depth: 0` があり、公開物の `--version` が `0.2.0` になること（Task 3 Step 3、Task 4 Step 5）。
4. arm64 のイメージが作れない（QEMU 上の `RUN apk`、`COPY $TARGETPLATFORM/...` のパス）。amd64 はローカルの手作りコンテキストで、amd64 と arm64 の両方は CI の snapshot ビルドで確認すること（Task 2 Step 8、Task 4 Step 2）。
5. GHCR のパッケージが初回の push で private になる。プレリリースタグ（`v0.2.0-rc.1`）で `latest` や prerelease の扱いが想定と違う。公開状態を確認し、プレリリースタグは検証するまで使わないこと（Task 4 Step 6）。

## 最終レビューでの変更

Task 1〜3 の実装後、ブランチ全体のレビューを受けて、次を変えた（Task 1 と 3 の YAML は、この変更を反映した内容が最終版）。

- `ci.yml` と `release.yml`: `go-version-file: go.mod` を `go-version: "1.26"` と `check-latest: true` に変更。`release.yml` には `cache: false` を追加。
- `.goreleaser.yaml`: `release: prerelease: auto` を追加。イメージのタグ `latest` を `{{ if not .Prerelease }}latest{{ end }}` に変更。
- SBOM の attestation は、GoReleaser の既定のまま付ける（仕様書 3.3 に明記）。

---

## ファイル構成

| ファイル | 操作 | 責務 |
|---|---|---|
| `.github/workflows/ci.yml` | 作成 | PR と `main` への push で `vet`、`gofmt`、`test` |
| `.github/dependabot.yml` | 作成 | アクションの SHA 固定を週次で更新 |
| `.goreleaser.yaml` | 作成 | ビルド、アーカイブ、チェックサム、リリースノート、イメージ |
| `Dockerfile.release` | 作成 | ビルド済みバイナリを `COPY` するだけのイメージ定義 |
| `.gitignore` | 変更 | `dist/` を追加 |
| `.github/workflows/release.yml` | 作成 | タグでリリース、`workflow_dispatch` で snapshot |
| `README.md` | 変更 | 公開イメージとバイナリの取得方法 |
| `CLAUDE.md` | 変更 | 2つの Dockerfile の役割、リリース手順 |

---

### Task 1: CI ワークフローと Dependabot

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `.github/dependabot.yml`

**Interfaces:**
- Consumes: `go.mod` の `go` ディレクティブ（`go-version-file` が読む）。
- Produces: `main` と PR で動くテスト。Task 4 が、PR で緑になることを確認する。

- [ ] **Step 1: actionlint を用意する**

```sh
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
"$(go env GOPATH)/bin/actionlint" -version
```

Expected: `1.7.12` で始まるバージョンが出る。

- [ ] **Step 2: `ci.yml` を書く**

`.github/workflows/ci.yml`:

```yaml
name: ci

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - name: Vet
        run: go vet ./...
      - name: Check formatting
        run: |
          unformatted=$(gofmt -l .)
          if [ -n "$unformatted" ]; then
            echo "Run gofmt on:"
            echo "$unformatted"
            exit 1
          fi
      - name: Test
        run: go test -race ./...
```

- [ ] **Step 3: actionlint で検査する**

Run: `"$(go env GOPATH)/bin/actionlint" .github/workflows/ci.yml; echo "exit=$?"`
Expected: 出力なしで `exit=0`。

- [ ] **Step 4: `dependabot.yml` を書く**

`.github/dependabot.yml`:

```yaml
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
```

- [ ] **Step 5: `gofmt` の検査が、整形漏れで失敗することを確かめる**

`ci.yml` の `Check formatting` の中身を、そのままローカルで動かす。まず整形漏れのファイルを作って失敗を確認する。

```sh
printf 'package main\nfunc   zz(){}\n' > zz_fmt_check.go
bash -c 'unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "Run gofmt on:"; echo "$unformatted"; exit 1; fi'; echo "exit=$?"
rm zz_fmt_check.go
```

Expected: `Run gofmt on:` と `zz_fmt_check.go` が出て、`exit=1`。

次に、ファイルを消した状態で成功を確認する。

```sh
bash -c 'unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "Run gofmt on:"; echo "$unformatted"; exit 1; fi'; echo "exit=$?"
git status --short
```

Expected: 出力なしで `exit=0`。`git status --short` には `.github/` だけが出る（`zz_fmt_check.go` が残っていないこと）。

- [ ] **Step 6: CI と同じコマンドがローカルで通ることを確かめる**

Run: `go vet ./... && go test -race ./...`
Expected: `ok  	misskey_exporter`。

- [ ] **Step 7: Commit**

```sh
git add .github/workflows/ci.yml .github/dependabot.yml
git commit -m "Add CI workflow and Dependabot for GitHub Actions" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 2: GoReleaser の設定と Dockerfile.release

**Files:**
- Create: `.goreleaser.yaml`
- Create: `Dockerfile.release`
- Modify: `.gitignore`（末尾に追記）
- Modify: `CLAUDE.md`（Commands と Gotchas に追記）

**Interfaces:**
- Consumes: `github.com/prometheus/common/version` の変数 `Version`、`Revision`、`BuildDate`（`version.go` と `prometheus/common` が使う）。
- Produces: リポジトリ直下の `.goreleaser.yaml` と `Dockerfile.release`。ビルド成果物は `dist/`。アーカイブは `misskey_exporter_<version>_linux_<arch>.tar.gz`、チェックサムは `misskey_exporter_<version>_checksums.txt`。Task 3 の `release.yml` が、この設定を `goreleaser release` で使う。

- [ ] **Step 1: GoReleaser を用意する**

```sh
go install github.com/goreleaser/goreleaser/v2@v2.18.3
"$(go env GOPATH)/bin/goreleaser" --version
```

Expected: `GitVersion:    2.18.3` を含む出力。

- [ ] **Step 2: `dist/` を `.gitignore` に追加する**

`.gitignore` は末尾に改行がないので、先頭の `\n` でその行を閉じる。

```sh
printf '\n\n# GoReleaser output\ndist/\n' >> .gitignore
tail -4 .gitignore
```

Expected: 末尾が `# End of https://www.toptal.com/developers/gitignore/api/go`、空行、`# GoReleaser output`、`dist/`。

- [ ] **Step 3: `Dockerfile.release` を書く**

`Dockerfile.release`（今の `Dockerfile` の2段目と同じ内容。GoReleaser が、ビルド済みバイナリを `linux/amd64/` と `linux/arm64/` に並べたコンテキストで実行する）:

```dockerfile
FROM alpine:latest
RUN apk --no-cache add ca-certificates
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/misskey_exporter /misskey_exporter

EXPOSE 9100

ENTRYPOINT ["/misskey_exporter"]
```

- [ ] **Step 4: `.goreleaser.yaml` を書く**

`.goreleaser.yaml`:

```yaml
version: 2

project_name: misskey_exporter

builds:
  - id: misskey_exporter
    main: .
    binary: misskey_exporter
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    goos:
      - linux
    goarch:
      - amd64
      - arm64
    # Replace the default ldflags: they only set main.* variables, but the
    # version shown by --version, the logs and the User-Agent lives in
    # prometheus/common/version.
    ldflags:
      - -s -w
      - -X github.com/prometheus/common/version.Version={{ .Version }}
      - -X github.com/prometheus/common/version.Revision={{ .FullCommit }}
      - -X github.com/prometheus/common/version.BuildDate={{ .Date }}

archives:
  - id: misskey_exporter
    formats:
      - tar.gz
    name_template: "misskey_exporter_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

# Release notes come from the merged pull requests, not from the commit list,
# because pull requests are merged with merge commits.
changelog:
  use: github-native

dockers_v2:
  - id: misskey_exporter
    images:
      - ghcr.io/onodai145/misskey_exporter
    # {{ .Version }} has no leading "v", unlike the default {{ .Tag }}.
    tags:
      - "{{ .Version }}"
      - latest
    dockerfile: Dockerfile.release
    platforms:
      - linux/amd64
      - linux/arm64
    labels:
      org.opencontainers.image.source: "https://github.com/onodai145/misskey_exporter"
```

- [ ] **Step 5: 設定を検査する**

Run: `"$(go env GOPATH)/bin/goreleaser" check; echo "exit=$?"`
Expected: 設定が検証されて `exit=0`。

もし非推奨の警告（例: `formats`）や未知のオプションのエラーが出たら、メッセージと公式ドキュメント（https://goreleaser.com/customization/package/dockers_v2/ ほか）に従って直す。仕様書 10章が「未確認」とした `dockerfile`、`labels`、ldflags の上書きは、ここで確定する。直した内容は、Step 9 のコミット前に、このプランの該当箇所と矛盾しないか見直す。

- [ ] **Step 6: snapshot ビルドをローカルで実行する（公開とイメージは除く）**

snapshot モードでも、`--skip=publish`（`--snapshot` が暗黙に含む）ではイメージのビルドが走る。ローカルには buildx がないので、`--skip=docker` でイメージを除く。

```sh
"$(go env GOPATH)/bin/goreleaser" release --snapshot --clean --skip=docker; echo "exit=$?"
```

Expected: `exit=0`。`dist/` に、linux/amd64 と linux/arm64 のバイナリ、2つの tar.gz、チェックサムファイルができる。

もし docker や buildx のエラーで失敗したら、設定を曲げずに止めて報告する。代わりに `goreleaser build --snapshot --clean` でバイナリだけを確認し、アーカイブとチェックサムは Task 4 の CI の snapshot で確認する。

- [ ] **Step 7: 成果物を確かめる**

```sh
ls dist/*.tar.gz dist/*_checksums.txt
V=$(python3 -I -c 'import json;print(json.load(open("dist/metadata.json"))["version"])')
echo "metadata version: $V"
AMD=$(find dist -type f -name misskey_exporter -path '*linux_amd64*')
ARM=$(find dist -type f -name misskey_exporter -path '*linux_arm64*')
"$AMD" --version 2>&1 | head -1
"$AMD" --version 2>&1 | grep -c "revision: $(git rev-parse HEAD)"
go version -m "$ARM" | grep -E 'GOARCH|-trimpath'
(cd dist && sha256sum -c ./*_checksums.txt)
tar -tzf dist/misskey_exporter_*_linux_amd64.tar.gz
```

Expected:
- `ls` が `..._linux_amd64.tar.gz`、`..._linux_arm64.tar.gz`、`..._checksums.txt` を出す。
- `--version` の1行目が `misskey_exporter, version <metadata version> (...)`。`unknown` や空ではない。
- revision の一致が `1`（HEAD のコミット）。
- arm64 のバイナリに `GOARCH=arm64` と `-trimpath=true` が出る。
- `sha256sum -c` が2つとも `OK`。
- tar の一覧に `misskey_exporter` がある。

- [ ] **Step 8: `Dockerfile.release` を手作りのコンテキストで確かめる（amd64）**

```sh
CTX=$(mktemp -d)
mkdir -p "$CTX/linux/amd64"
cp "$(find dist -type f -name misskey_exporter -path '*linux_amd64*')" "$CTX/linux/amd64/misskey_exporter"
docker build --build-arg TARGETPLATFORM=linux/amd64 -f Dockerfile.release -t misskey_exporter:release-check "$CTX"
docker run --rm misskey_exporter:release-check --version 2>&1 | head -1
docker rmi misskey_exporter:release-check
rm -rf "$CTX"
```

Expected: ビルドが成功し、`--version` が Step 7 と同じバージョンを出す。arm64 は、ローカルに buildx がないので Task 4 の CI で確認する。

- [ ] **Step 9: `CLAUDE.md` に追記する**

`CLAUDE.md` の Commands のコードブロックで、`docker build -t misskey_exporter .` の行の下に1行足す。

```
goreleaser release --snapshot --clean --skip=docker  # リリースの成果物をローカルで試す（dist/ に出る。イメージは buildx が要るので除く）
```

Gotchas の末尾（`コード内のコメントは英語、README は日本語。` の行の前）に1項目足す。

```
- Dockerfile は2つある。`Dockerfile` はローカルの `docker build .` 用（マルチステージ）、`Dockerfile.release` は GoReleaser 用（ビルド済みバイナリを `COPY` するだけ）。`EXPOSE` と `ENTRYPOINT` が重複するので、片方を変えたらもう片方も直す。
```

- [ ] **Step 10: 既存の `Dockerfile` が変わっていないことと、作業ツリーの状態を確かめる**

```sh
git diff --exit-code Dockerfile; echo "exit=$?"
git status --short
```

Expected: `exit=0`。`git status --short` に出るのは `.goreleaser.yaml`、`Dockerfile.release`、`.gitignore`、`CLAUDE.md` だけ（`dist/` は出ない）。

- [ ] **Step 11: Commit**

```sh
git add .goreleaser.yaml Dockerfile.release .gitignore CLAUDE.md
git commit -m "Add GoReleaser config and release Dockerfile" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 3: リリースワークフローと文書

**Files:**
- Create: `.github/workflows/release.yml`
- Modify: `README.md`（`### Docker` の冒頭と、`### ソースから実行` の前）
- Modify: `CLAUDE.md`（Workflow に追記）

**Interfaces:**
- Consumes: Task 2 の `.goreleaser.yaml`（`goreleaser release` で使う）と `Dockerfile.release`。GoReleaser v2.18.3。
- Produces: `v*` タグの push で動く `release.yml`。`workflow_dispatch` では `--snapshot` で動く（公開しない）。Task 4 が、この2つの動作を確認する。

- [ ] **Step 1: `release.yml` を書く**

`.github/workflows/release.yml`:

```yaml
name: release

on:
  push:
    tags:
      - "v*"
  # Manual runs build everything with --snapshot and publish nothing.
  workflow_dispatch:

permissions:
  contents: write
  packages: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          # GoReleaser needs the tags and the history to derive the version.
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: go.mod
      - name: Vet
        run: go vet ./...
      - name: Test
        run: go test -race ./...
      # QEMU runs the "RUN apk add" step of Dockerfile.release for linux/arm64.
      - uses: docker/setup-qemu-action@99012661954931238ded8c8b007157a8430204e1 # v4.4.0
      - uses: docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4.4.1
      - uses: docker/login-action@dbcb813823bdd20940b903addbd779551569679f # v4.6.0
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94 # v7.2.3
        with:
          distribution: goreleaser
          version: v2.18.3
          args: release --clean ${{ github.event_name == 'workflow_dispatch' && '--snapshot' || '' }}
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 2: actionlint で検査する**

Run: `"$(go env GOPATH)/bin/actionlint" .github/workflows/ci.yml .github/workflows/release.yml; echo "exit=$?"`
Expected: 出力なしで `exit=0`。

- [ ] **Step 3: 構造を確かめる**

Review Focus 2、3 と、仕様書の N1、N2 を固定する。

```sh
# 1. every "uses:" is pinned to a 40-character SHA followed by a version comment
grep -hE 'uses:' .github/workflows/*.yml | grep -vE '@[0-9a-f]{40} # v[0-9]' ; echo "unpinned_exit=$?"

# 2. tests run before GoReleaser in release.yml
awk '/go test -race/{t=NR} /goreleaser-action/{g=NR} END{exit !(t && g && t<g)}' .github/workflows/release.yml; echo "order_exit=$?"

# 3. full history is fetched
grep -c 'fetch-depth: 0' .github/workflows/release.yml

# 4. permissions
grep -A2 '^permissions:' .github/workflows/ci.yml .github/workflows/release.yml
```

Expected:
- 1 は、何も出力されず `unpinned_exit=1`（SHA 固定でない行がない）。
- 2 は `order_exit=0`。
- 3 は `1`。
- 4 は、`ci.yml` が `contents: read` だけ、`release.yml` が `contents: write` と `packages: write` だけ。

- [ ] **Step 4: `README.md` に取得方法を足す**

`### Docker` の見出しの直後に、次の段落を足す（既存のコードブロックの前）。

````markdown
リリースされたイメージは GHCR にあります（linux/amd64 と linux/arm64）。以下の例の `misskey_exporter` を `ghcr.io/onodai145/misskey_exporter:<バージョン>` に置き換えてください（例: `ghcr.io/onodai145/misskey_exporter:0.2.0`）。自分でビルドする場合は、次の `docker build` を使います。

````

`### ソースから実行` の見出しの前に、次の節を足す。

````markdown
### バイナリ

[Releases](https://github.com/onodai145/misskey_exporter/releases) から、linux/amd64 と linux/arm64 の tar.gz とチェックサムファイル（`misskey_exporter_<バージョン>_checksums.txt`）をダウンロードできます。

```sh
sha256sum -c --ignore-missing misskey_exporter_<バージョン>_checksums.txt
tar -xzf misskey_exporter_<バージョン>_linux_amd64.tar.gz
MISSKEY_URL=https://example.com MISSKEY_API_KEY=... ./misskey_exporter
```

````

- [ ] **Step 5: `CLAUDE.md` の Workflow にリリース手順を足す**

`## Workflow` の箇条書きの末尾に足す。

```
- リリース: `main` で `git tag v<X.Y.Z> && git push origin v<X.Y.Z>`。`release.yml` が `vet` と `test` を回してから、GoReleaser でバイナリ、チェックサム、GitHub Release、GHCR のイメージ（`<X.Y.Z>` と `latest`）を公開する。タグを打つ前に、Actions の release を `main` で手動実行すると、公開なしの snapshot ビルド（イメージのビルドまで）を試せる。プレリリースタグ（`v0.2.0-rc.1` など）の扱いは未検証。
- CI とリリースの設計は `docs/superpowers/specs/2026-10-15-ci-release-design.md`。アクションは SHA で固定しており、更新は Dependabot の PR で行う。GoReleaser 本体の版は `release.yml` の `version` で固定していて、手動で上げる。
```

- [ ] **Step 6: 全体を確かめる**

```sh
go vet ./... && go test -race ./...
git status --short
```

Expected: テストが `ok`。`git status --short` に出るのは `.github/workflows/release.yml`、`README.md`、`CLAUDE.md` だけ。

- [ ] **Step 7: Commit**

```sh
git add .github/workflows/release.yml README.md CLAUDE.md
git commit -m "Add release workflow and document release usage" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 4: PR、マージ後の確認、最初のリリース（ユーザー操作を含む）

コードの変更はない。外部への公開を伴う操作を含むので、各ステップの前に、ユーザーの指示を確認する。

**Files:** なし

**Interfaces:**
- Consumes: Task 1〜3 のコミットを含む `feat/ci-release`。
- Produces: `v0.2.0` のリリース（Release、アーカイブ、チェックサム、GHCR のイメージ）。

- [ ] **Step 1: push して PR を作り、CI が緑になることを確かめる**

```sh
git push -u origin feat/ci-release
gh pr create --base main --head feat/ci-release --title "Add CI and release automation"
gh pr checks --watch
```

PR の本文は、日本語で概要（`ci.yml`、`release.yml`、GoReleaser、Dependabot）と、Task 4 の確認手順を書き、末尾に `🤖 Generated with [Claude Code](https://claude.com/claude-code)` を付ける。バッククォートをエスケープしないこと。

Expected: `ci` の `test` ジョブが成功する（Review Focus 1 の仕組みが、リモートでも動く）。

- [ ] **Step 2: マージ後に、snapshot ビルドを CI 上で回す**

ユーザーが PR をマージしたら（通常のマージコミット）、`main` を取り込んで実行する。

```sh
git switch main && git pull --ff-only origin main
gh workflow run release.yml --ref main
sleep 5
gh run watch "$(gh run list --workflow=release.yml --limit 1 --json databaseId -q '.[0].databaseId')" --exit-status
```

Expected: 成功する。ログで、linux/amd64 と linux/arm64 のイメージのビルド（`RUN apk add` が arm64 でも通る）と、`COPY $TARGETPLATFORM/...` が通ることを確認する。何も公開されないこと（Releases とパッケージに変化がない）も確認する。

もし `workflow_dispatch` が、ワークフローがデフォルトブランチにないという理由で拒否された場合は、マージ済みかを確認する。

失敗したら、原因をログから特定し、別のブランチと PR で直す。

- [ ] **Step 3: タグを打つ（ユーザー操作）**

ユーザーが次を実行する。または、明示的に指示を受けたときだけ私が実行する。

```sh
git tag v0.2.0 "$(git rev-parse origin/main)"
git push origin v0.2.0
```

- [ ] **Step 4: リリースの完了を待つ**

```sh
gh run watch "$(gh run list --workflow=release.yml --limit 1 --json databaseId -q '.[0].databaseId')" --exit-status
```

Expected: 成功する。

- [ ] **Step 5: 公開物を確かめる**

```sh
gh release view v0.2.0 --json assets,isPrerelease -q '.assets[].name, .isPrerelease'
docker pull ghcr.io/onodai145/misskey_exporter:0.2.0
docker run --rm ghcr.io/onodai145/misskey_exporter:0.2.0 --version 2>&1 | head -1
docker manifest inspect ghcr.io/onodai145/misskey_exporter:0.2.0 | grep -E '"architecture"'
```

Expected:
- アセットに `misskey_exporter_0.2.0_linux_amd64.tar.gz`、`misskey_exporter_0.2.0_linux_arm64.tar.gz`、`misskey_exporter_0.2.0_checksums.txt` がある。`isPrerelease` は `false`。
- `--version` が `misskey_exporter, version 0.2.0 (...)`（Review Focus 3）。
- マニフェストに `amd64` と `arm64` の両方がある。SBOM と provenance の attestation のエントリ（`unknown/unknown`）も入っていてよい。

- [ ] **Step 6: GHCR のパッケージの公開状態を確かめる**

```sh
gh api /users/onodai145/packages/container/misskey_exporter --jq .visibility
```

Expected: `public`。`private` の場合は、ユーザーが GitHub の Package settings → Change visibility で public にする（最初の push の後に1回だけ必要になる場合がある）。変更後に、認証なしで `docker pull` できることを確認する。

プレリリースタグ（`v0.2.0-rc.1` など）は、`latest` の動きと Release の prerelease 扱いが未検証なので、検証するまで使わない（Review Focus 5）。

- [ ] **Step 7: ブランチを片付ける（ユーザーの指示があるとき）**

```sh
git branch -d feat/ci-release
git push origin --delete feat/ci-release
git fetch --prune
```

---

## Self-Review

**1. 仕様書との対応**

| 仕様書 | 担当 |
|---|---|
| F1（CI） | Task 1 |
| F2（成果物と Release） | Task 2（設定）、Task 3（ワークフロー）、Task 4 Step 5（公開物の確認） |
| F3（リリース前のテスト） | Task 3 Step 1、Step 3 |
| F4（バージョンの一貫性） | Task 2 Step 4、Step 7、Task 4 Step 5 |
| F5（`workflow_dispatch` の snapshot） | Task 3 Step 1、Task 4 Step 2 |
| N1（SHA 固定） | Global Constraints、Task 3 Step 3 |
| N2（権限） | Task 1 Step 2、Task 3 Step 1、Step 3 |
| N3（同一バイナリ） | GoReleaser が `dist/` のバイナリを、アーカイブとイメージの両方に使う（Task 2 Step 4） |
| N4（`Dockerfile` 不変） | Task 2 Step 10 |
| §4（ファイル一覧） | ファイル構成の表。`.gitignore`、README、CLAUDE.md、Dependabot を含む |
| §9（検証） | Task 2 Step 6〜8、Task 4 |
| §10（未確認） | Task 2 Step 5（オプション名と ldflags）、Task 4 Step 6（可視性とプレリリース） |

**2. プレースホルダ:** 手順に TBD や「あとで」はない。実行時に確定するもの（`goreleaser check` の警告への対応、`dispatch` の挙動）は、手順と期待値を書いてある。

**3. 名前の一貫性:** イメージ名（`ghcr.io/onodai145/misskey_exporter`）、バイナリ名（`misskey_exporter`）、アーカイブ名、チェックサム名、GoReleaser の版（`v2.18.3`）、アクションの SHA は、全タスクで同じ。

**4. Review Focus:** 5項目とも、担当ステップを割り当てた。
