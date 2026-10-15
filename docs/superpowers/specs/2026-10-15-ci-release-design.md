# CI とリリース自動化 設計仕様

日付: 2026-10-15
ステータス: ドラフト（レビュー待ち）

## 1. 目的

次の2つを GitHub Actions で自動化する。

- PR と `main` への push で、`go vet`、`gofmt`、`go test -race` を実行する（CI）。
- `v*` タグの push で、バイナリとコンテナイメージを公開する（リリース）。

`git tag v0.2.0 && git push origin v0.2.0` だけでリリースが完成することを成功条件とする。

## 2. 現状

- `.github/` はなく、CI は一切ない。テストは手元でしか動いていない。
- 配布物は `Dockerfile`（`golang:1.26` → `alpine` のマルチステージ）だけで、イメージの公開先もない。
- バージョンは、ldflags が空のとき `version.go` が Go のモジュールバージョン（git タグ由来）で埋める。`--version`、起動ログ、Misskey への User-Agent（`misskey_exporter/<version>`）で使われる。
- リポジトリは PUBLIC、デフォルトブランチは `main`。PR は通常のマージコミットでマージする。
- ローカルには `goreleaser` も `docker buildx` もない。

## 3. 要件

### 3.1 機能要件

- F1: PR と `main` への push で `go vet ./...`、`gofmt` の差分チェック、`go test -race ./...` を実行する。
- F2: `v*` タグの push で、次の成果物を作って公開する。
  - linux/amd64 と linux/arm64 の tar.gz。
  - チェックサムファイル `misskey_exporter_<version>_checksums.txt`（sha256）。
  - `ghcr.io/onodai145/misskey_exporter` のマルチアーキテクチャイメージ（linux/amd64、linux/arm64）。
  - GitHub Release（リリースノートは PR タイトルから自動生成）。
- F3: リリースの前に `go vet` と `go test -race` をもう一度実行し、失敗したら何も公開しない。
- F4: タグ `v0.2.0` から、アーカイブ名、イメージタグ、`--version`、User-Agent のすべてが `0.2.0` になる。
- F5: 手動実行（`workflow_dispatch`）で、タグなし・公開なしの snapshot ビルドを CI 上で回せる。本番のタグを打つ前に、イメージのビルドまで確認するため。

### 3.2 非機能要件

- N1: サードパーティのアクションは、タグではなくコミット SHA で固定し、コメントにバージョンを書く。
- N2: ワークフローの権限は最小にする。`ci.yml` は `contents: read`、`release.yml` は `contents: write` と `packages: write` だけ。
- N3: リリースのバイナリとイメージ内のバイナリは同一（GoReleaser が作った同じバイナリを使う）。
- N4: ローカルの `docker build .` は今のまま動く。

### 3.3 スコープ外

- 署名（cosign）、SBOM、deb/rpm、Homebrew。
- darwin と windows のバイナリ。必要になったら GoReleaser の `goos` に足す。
- イメージの非 root 化とベースイメージの固定。今の `Dockerfile` と同じ `alpine:latest` のまま移す。
- `LICENSE` ファイルの追加。
- Go モジュールの Dependabot。

## 4. 構成

| ファイル | 内容 |
|---|---|
| `.github/workflows/ci.yml` | 新規。テスト用 |
| `.github/workflows/release.yml` | 新規。リリース用 |
| `.github/dependabot.yml` | 新規。`github-actions` のみ、週次。SHA 固定の更新を追うため |
| `.goreleaser.yaml` | 新規。ビルド、アーカイブ、イメージ、リリースノート |
| `Dockerfile.release` | 新規。ビルド済みバイナリを `COPY` するだけ |
| `Dockerfile` | 変更なし。ローカルの `docker build .` 用 |
| `.gitignore` | `dist/` を追加 |
| `README.md`、`CLAUDE.md` | 取得方法、リリース手順、2つの Dockerfile の役割 |

## 5. ワークフロー

### 5.1 `ci.yml`

- 起動: `pull_request`、`main` への `push`。
- 権限: `contents: read`。
- 手順: checkout → `setup-go`（`go-version-file: go.mod`）→ `go vet ./...` → `gofmt -l .` が空であることの確認 → `go test -race ./...`。

### 5.2 `release.yml`

- 起動: `v*` タグの `push`、`workflow_dispatch`。
- 権限: `contents: write`、`packages: write`。
- 手順:
  1. `fetch-depth: 0` で checkout（GoReleaser がタグと履歴を必要とする）。
  2. `setup-go`（`go-version-file: go.mod`）。
  3. `go vet ./...` と `go test -race ./...`。
  4. QEMU と buildx の用意。`Dockerfile.release` の `RUN apk add` が arm64 側で実行されるため、エミュレーションが要る。
  5. `GITHUB_TOKEN` で GHCR にログイン。
  6. GoReleaser の実行。タグの push なら `release --clean`、`workflow_dispatch` なら `release --clean --snapshot`（公開しない）。

## 6. GoReleaser の設定（`.goreleaser.yaml`）

- `version: 2`、`project_name: misskey_exporter`。
- `builds`:
  - `CGO_ENABLED=0`、`-trimpath`。
  - `goos: linux`、`goarch: amd64, arm64`。
  - `ldflags` は既定値を置き換え、次の3つを明示する。
    - `-X github.com/prometheus/common/version.Version={{.Version}}`
    - `-X github.com/prometheus/common/version.Revision={{.FullCommit}}`
    - `-X github.com/prometheus/common/version.BuildDate={{.Date}}`
  - `{{.Version}}` は先頭の `v` を外した値になる。
- `archives`: tar.gz、`misskey_exporter_{{.Version}}_linux_{{.Arch}}`。
- `checksum`: 既定（`<project>_<version>_checksums.txt`、sha256）。
- `changelog`: `use: github-native`（マージコミット運用なので、コミット一覧ではなく PR タイトルを使う）。
- `dockers_v2`:
  - `images: ghcr.io/onodai145/misskey_exporter`。
  - `tags: "{{ .Version }}"` と `latest`。
  - `platforms: linux/amd64, linux/arm64`。
  - `dockerfile: Dockerfile.release`。
  - `labels` に `org.opencontainers.image.source`（リポジトリとの紐付け）。

## 7. `Dockerfile.release`

今の `Dockerfile` の2段目と同じ内容にする。

```dockerfile
FROM alpine:latest
RUN apk --no-cache add ca-certificates
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/misskey_exporter /misskey_exporter

EXPOSE 9100

ENTRYPOINT ["/misskey_exporter"]
```

`EXPOSE` と `ENTRYPOINT` は `Dockerfile` と重複する。ずれないように、`CLAUDE.md` に注意書きを残す。

## 8. バージョンの扱い

- リリース: GoReleaser の ldflags が `version.Version` を埋める（`0.2.0`）。`version.go` のフォールバックは `Version` が空のときだけ働くので、ldflags が優先される。
- ローカル: これまでどおり、タグ付き checkout からの `go build` で `version.go` が埋める。
- イメージタグ: `{{ .Version }}` を使うので `0.2.0`（`v` なし）。GoReleaser の既定の `{{ .Tag }}` は `v` 付きなので、明示的に指定する。

## 9. 検証

実装後、次を確認する。

1. ローカル: `goreleaser` を用意できれば、`goreleaser release --snapshot --clean --skip=publish` で、アーカイブの中身、チェックサムファイル、バイナリの `--version` を確認する。イメージは公開フェーズで作られるので、この実行には含まれない。ローカルに `docker buildx` もないので、イメージのビルドはローカルでは確認できない。
2. CI: `workflow_dispatch` の snapshot ビルドで、イメージのビルド（amd64 と arm64）まで通ることを確認する。`workflow_dispatch` は、ワークフローがデフォルトブランチにないと起動できないと理解しているので、`main` へのマージ後に実行する（実際に動かして確認する）。
3. 本番: `main` にマージしたあと、`v0.2.0` を打って次を確認する。
   - Release に tar.gz 2つとチェックサムファイルがあること。
   - GHCR にイメージがあり、`docker run --rm ghcr.io/onodai145/misskey_exporter:0.2.0 --version` が `0.2.0` を表示すること。
   - GHCR のパッケージが public であること（最初の push では private になる場合があり、その場合は初回だけ GitHub 上で public にする手作業が要る）。

## 10. 未確認の事項

公式ドキュメントでは確認できなかった。実装時に検証する。

- `ldflags` を上書きすると既定値が置き換わること、`-X` を `main` 以外のパッケージに使えること。
- `dockers_v2` の `dockerfile` と `labels` のオプション名。
- プレリリースタグ（`v0.2.0-rc.1` など）のとき、Release が prerelease になるか、`latest` が動くか。
- GHCR パッケージの初回の可視性。
- 各アクションの SHA とバージョンは、実装時に `gh api` で調べて固定する。

## 11. 完了条件

- `ci.yml` が PR で動き、`vet`、`gofmt`、`test` が通る。
- `workflow_dispatch` の snapshot ビルドが通る。
- `v0.2.0` のタグで、Release、アーカイブ、チェックサム、GHCR のイメージが公開される。
- 公開された成果物の `--version` と User-Agent が `0.2.0` になる。
- README と `CLAUDE.md` に、取得方法とリリース手順が書かれている。
