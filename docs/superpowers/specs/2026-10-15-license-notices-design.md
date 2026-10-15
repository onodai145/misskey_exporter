# 配布物の依存ライブラリのライセンス表示 設計仕様

日付: 2026-10-15
ステータス: ドラフト（レビュー待ち）

## 1. 目的

リリースのアーカイブと Docker イメージに、組み込まれている依存ライブラリのライセンス表示を含める。あわせて、許可していない種類のライセンスの依存が入ったときに、CI で失敗させる。

## 2. 現状

- リリースのバイナリには、27 個の Go モジュール（`go version -m` で数えた）と、Go の標準ライブラリが組み込まれている。ライセンスは MIT、Apache-2.0、BSD-3-Clause。コピーレフトは見つかっていない。
- これらのライセンスは、バイナリの配布でも、著作権表示と許諾文（MIT）、条件と免責の再掲（BSD 系）、ライセンスの写しと `NOTICE` の内容（Apache-2.0）を求める。
- 現状のアーカイブには、バイナリと README だけが入っている（`LICENSE` は、次のリリースから入る）。イメージには、バイナリだけが入っている。依存のライセンス表示は、どちらにも含まれていない。
- ライセンスの種類をチェックする仕組みはない。
- `go-licenses`（Google 製）v2.0.1 は、Go 1.27.2 で、このリポジトリに対して動くことを確認した。
  - `check` は、許可リストのとおりなら終了コード 0、許可していない種類があれば 1 で、違反したライブラリを一覧にする。
  - `save` は 34 ファイルを集める（`NOTICE` が 6 つ含まれる。モジュールの中の入れ子のライセンス、たとえば `client_golang` の `internal/.../gddo`（BSD-3）も拾う）。
  - 自分自身のモジュール（`misskey_exporter`）も一覧に出る。
  - Go の標準ライブラリは対象外。
- このマシンの Go（ディストリのパッケージ）には、`$(go env GOROOT)/LICENSE` がない。

## 3. 要件

### 3.1 機能要件

- F1: リリースのアーカイブ（tar.gz）に、依存のライセンスを含める。フォルダ `licenses/` に、依存、Go、自分自身のライセンスのファイルが揃う。
- F2: Docker イメージの `/licenses/` に、同じ内容を含める。
- F3: ライセンスのファイルは、リリース時に生成する。リポジトリにはコミットしない。
- F4: 許可するライセンスは、`MIT`、`Apache-2.0`、`BSD-3-Clause` の3つだけ。許可していない種類の依存があれば、PR の CI（`ci.yml`）とリリース（`release.yml`）で失敗する。
- F5: Go の標準ライブラリのライセンス（`GOROOT/LICENSE`）を含める。ファイルがなければ、黙って省略せず、失敗する。

### 3.2 非機能要件

- N1: 許可リストは、`scripts/licenses.sh` の1か所だけに書く。
- N2: `go-licenses` は、版を固定して `go install` する（`github.com/google/go-licenses/v2@v2.0.1`）。Go のチェックサムデータベースで改ざんを検出する。`go.mod` には入れない。
- N3: サードパーティのアクションの SHA 固定など、既存のワークフローの方針は変えない。
- N4: スクリプトは、偽の `go-licenses` でテストでき、ネットワークや実際の `go-licenses` を必要としない。

### 3.3 スコープ外

- 公開済みの `v0.2.0` への後付け。
- SBOM（attestation）へのライセンス情報の追加。
- 法務的なレビュー。
- `go-licenses` の版の自動更新（Dependabot の対象外。手動で上げる）。
- 許可する種類を増やす仕組み（増やすときは、`scripts/licenses.sh` の許可リストを書き換える）。

## 4. 構成

| ファイル | 内容 |
|---|---|
| `scripts/licenses.sh` | 新規。`check` と `save <dir>` |
| `scripts/licenses_test.sh` | 新規。偽の `go-licenses` を使うテスト |
| `.goreleaser.yaml` | `before.hooks`、アーカイブの `files`、`dockers_v2` の `extra_files` |
| `Dockerfile.release` | `COPY licenses /licenses` を追加 |
| `.github/workflows/ci.yml` | `go-licenses` のインストール、`check`、`licenses_test.sh` の実行 |
| `.github/workflows/release.yml` | `go-licenses` のインストールと `check`（GoReleaser の前） |
| `.gitignore` | `/licenses/` を追加 |
| `README.md`、`CLAUDE.md` | 配布物の `licenses/`、許可リストの場所と変え方 |

## 5. スクリプト（`scripts/licenses.sh`）

bash。`set -euo pipefail`。どこから起動されても、リポジトリのルート（スクリプトの1つ上）に移動してから動く（`./...`、`LICENSE`、`<dir>` はルートからの相対。サブディレクトリから起動すると、`go-licenses` が何も検査せずに成功してしまうため）。`go-licenses` が PATH にないときは、`$(go env GOPATH)/bin` を PATH に足す。

- `scripts/licenses.sh check`:
  `go-licenses check ./... --ignore misskey_exporter --allowed_licenses=MIT,Apache-2.0,BSD-3-Clause` を実行する。許可していない種類があれば、`go-licenses` の終了コードのまま失敗する。
- `scripts/licenses.sh save <dir>`:
  1. `<dir>` を削除する（古いファイルを残さない）。
  2. `go-licenses save ./... --ignore misskey_exporter --save_path=<dir>` を実行する。
  3. `$(go env GOROOT)/LICENSE` を `<dir>/go/LICENSE` にコピーする。ファイルがなければ、エラーで失敗する。
  4. リポジトリの `LICENSE` を `<dir>/misskey_exporter/LICENSE` にコピーする。

## 6. リリースへの組み込み

- `.goreleaser.yaml`:
  - `before.hooks` に `scripts/licenses.sh save licenses`。
  - アーカイブの `files` に `LICENSE`、`README.md`、`licenses/**`（`files` を指定すると、既定の `LICENSE` と `README` が置き換わるので、明示する）。
  - `dockers_v2` の `extra_files` に `licenses`。
- `Dockerfile.release`: `COPY licenses /licenses`。
- `release.yml`: `setup-go` → `go install go-licenses` → `scripts/licenses.sh check` → … → GoReleaser（生成は `before.hooks`）。
- `ci.yml`: `go install go-licenses` → `scripts/licenses.sh check`、`scripts/licenses_test.sh`。

## 7. テスト

`scripts/licenses_test.sh`（`release_test.sh` と同じ流儀）。偽の `go-licenses` を PATH に置き、次を確認する。

- `check` が、許可リストと `--ignore` を正しい引数で渡す。
- `go-licenses` が失敗したら、`check` も失敗する。
- `save` が、`<dir>` の古いファイルを消す。
- `save` が、`GOROOT/LICENSE` を `<dir>/go/LICENSE` に、リポジトリの `LICENSE` を `<dir>/misskey_exporter/LICENSE` に置く。
- `GOROOT/LICENSE` がなければ、`save` が失敗する。

## 8. 検証

1. ローカル: スクリプトのテスト、`goreleaser check`。アーカイブの構成は、手作りの `licenses/` と `goreleaser release --snapshot --clean --skip=before,docker` で確認する。ローカルの Go には `GOROOT/LICENSE` がないので、本物の生成はローカルでは確認できない。
2. CI: PR の `ci.yml` で、許可リストの `check` が通ること。マージ後に、`release.yml` の `workflow_dispatch`（snapshot）で、本物の生成と、イメージの `COPY licenses` が通ること。
3. 本番: 次のリリースの後で、`docker run --rm --entrypoint ls ghcr.io/onodai145/misskey_exporter:<版> /licenses` でイメージの中身を確認し、アーカイブを展開して `licenses/` を確認する。

## 9. 未確認の事項

公式ドキュメントでは確認できていない。実装時に検証する。

- `archives.files` の `licenses/**` が、アーカイブ内でフォルダ構成を保つか。
- `dockers_v2` の `extra_files` がフォルダを受け付けるか。
- CI の `setup-go` が入れる Go に、`GOROOT/LICENSE` があるか。
- CI の Go（1.26 系）で、`go-licenses` v2.0.1 が動くか（ローカルでは Go 1.27.2 で確認した）。

## 10. 完了条件

- `ci.yml` が PR で動き、`check` と `licenses_test.sh` が通る。
- 許可リストにない種類のライセンスの依存を足すと、`check` が失敗する（ローカルで確認する）。
- snapshot ビルドが通り、アーカイブに `licenses/` が入る。
- 次のリリースの、アーカイブとイメージに、`licenses/`（依存、`go/LICENSE`、`misskey_exporter/LICENSE`）が入っている。
- README と `CLAUDE.md` に、配布物の `licenses/` と、許可リストの変え方が書かれている。
