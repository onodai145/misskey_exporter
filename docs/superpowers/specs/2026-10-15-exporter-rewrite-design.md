# misskey_exporter リライト 設計仕様

日付: 2026-10-15
ステータス: 承認済み（実装計画の作成中に、Go 1.26 の要求など実装上の詳細を追記）

## 1. 目的

Misskey の管理 API から取得した情報を Prometheus 形式で公開する Exporter を、次の観点でリライトする。

- コード品質の改善（エラー処理、設定の検証、テスト可能な構造）
- 機能拡張の土台作り（管理系とキュー系のメトリクスを追加しやすい構成）
- Prometheus のベストプラクティス準拠（スクレイプ時取得、失敗の可観測性、命名規則）
- ドキュメント（README.md、CLAUDE.md）の読みやすさ

## 2. 現状

- 実装は `misskey_exporter.go` 1ファイル（約100行）。
- 10秒ごとのバックグラウンドポーリングで `misskey_queue_length{queue,state}` の GaugeVec を更新する。
- 環境変数は毎回のポーリングで読み、未設定なら `os.Exit(1)` する。
- HTTP エラーやデコード失敗は `fmt.Println` で出すだけで、ステータスコードは検査しない。
- テストはない。`go.mod` の依存はすべて `// indirect`。
- `dashboards/misskey-dashboard.json`（Grafana）が `misskey_queue_length` に依存している。

## 3. 要件

### 3.1 機能要件

- F1: `POST {MISSKEY_URL}/api/admin/queue/stats` の結果を `misskey_queue_length{queue,state}` として公開する（既存と同名・同ラベル）。
- F2: メトリクスはスクレイプ時に取得する（バックグラウンドポーリングは廃止）。
- F3: collector ごとに成功/失敗と所要時間を公開する。
- F4: 新しい API の追加は、collector の実装と `main.go` への登録だけで済む。
- F5: 設定はコマンドラインフラグと環境変数で受け取り、TLS と Basic 認証は exporter-toolkit の `--web.config.file` で設定できる。

### 3.2 非機能要件

- N1: 既存との互換性は必須ではない。ただし、変える理由がないもの（環境変数名 `MISSKEY_URL` / `MISSKEY_API_KEY`、既定ポート `:9100`、`misskey_queue_length{queue,state}`、Grafana ダッシュボード）は維持する。
- N2: 配布は当面 Docker（`golang:1.26` → `alpine`）。採用ライブラリの最新版（`client_golang` / `prometheus/common`）が Go 1.26 を要求し、Go 1.27 が出ている現在 1.25 以前はサポート対象外なので、`go.mod` の `go` ディレクティブと builder イメージを 1.24 から 1.26 に上げる。バイナリ配布は将来（任意のタイミング）で、今回は実装しないが、妨げない設計にする（cgo なし、`--version` を持つ）。
- N3: 外部依存を増やしてよい。Prometheus の Exporter で標準的なライブラリを採用する（5.1 参照）。
- N4: API キーをログやエラーメッセージに出さない。値そのものをコマンドライン引数では受け取らない（プロセス一覧に出るため）。環境変数に加えて、Docker Secrets や Kubernetes Secret のようなファイル経由でも渡せる。

### 3.3 スコープ外

- 管理系 collector 本体の追加（今回は土台と既存キュー collector の移行まで）。
- CI の追加。

## 4. アーキテクチャ

すべて `package main` のまま、責務ごとにファイルを分ける。

| ファイル | 責務 |
|---|---|
| `main.go` | 設定の読み込み、client と collector の組み立て、HTTP サーバー起動、SIGTERM/SIGINT での graceful shutdown |
| `config.go` | コマンドラインフラグと環境変数の解釈と検証 |
| `client.go` | Misskey API クライアント |
| `exporter.go` | 複数の collector をまとめて実行する `prometheus.Collector` |
| `collector_queue.go` | `admin/queue/stats` 用 collector |

`misskey_exporter.go` は廃止する。

### 4.1 データフロー

```
Prometheus scrape → promhttp → exporter.Collect()
  → 各 collector を並列実行（共通の timeout context）
  → client.Post(ctx, path, out) → ステータス検査 → JSON デコード
  → ConstMetric を ch に送出
  → collector ごとに success(0/1) と duration を送出
```

デフォルトレジストリを使い、`go_*` と `process_*` も引き続き公開する。

### 4.2 collector interface

```go
type collector interface {
    Name() string
    Update(ctx context.Context, c *client, ch chan<- prometheus.Metric) error
}
```

`exporter` が各 collector を並列に実行し、`Update` の戻り値から success と duration を出す。collector の panic は error に変換して `success=0` として扱い、プロセスは落とさない。collector 同士は独立で、1つの失敗が他に影響しない。

## 5. コンポーネント詳細

### 5.1 config

Prometheus の Exporter で標準的なスタックを使う。

| 用途 | ライブラリ |
|---|---|
| フラグ解釈 | `github.com/alecthomas/kingpin/v2` |
| HTTP サーバー（TLS、Basic 認証、systemd ソケット） | `github.com/prometheus/exporter-toolkit/web`（`web/kingpinflag`） |
| ログ（`log/slog` ベース、`--log.level` / `--log.format`） | `github.com/prometheus/common/promslog`（`promslog/flag`） |
| `--version` | `github.com/prometheus/common/version` |

| 項目 | 指定方法 | 必須 | 既定値 | 説明 |
|---|---|---|---|---|
| Misskey の URL | `--misskey.url` または環境変数 `MISSKEY_URL` | 必須 | なし | `http` または `https` の URL。末尾の `/` は除く |
| API キー | 環境変数 `MISSKEY_API_KEY`、または `--misskey.api-key-file`（環境変数 `MISSKEY_API_KEY_FILE` でも指定可）でファイルのパス | どちらか一方が必須 | なし | アクセストークン。値そのものをフラグでは受け取らない |
| リクエストタイムアウト | `--misskey.timeout` | 任意 | `5s` | 1回の API リクエストのタイムアウト。スクレイプ間隔より短くすること |
| 待ち受けアドレス | `--web.listen-address` | 任意 | `:9100` | exporter-toolkit のフラグ |
| TLS / Basic 認証 | `--web.config.file` | 任意 | なし | exporter-toolkit のフラグ |
| ログ | `--log.level`, `--log.format` | 任意 | `info`, `logfmt` | promslog のフラグ |
| バージョン | `--version` | 任意 | なし | |

- 起動時に検証し、問題を報告して終了コード 1 で終了する。必須フラグの欠落は kingpin が報告し、API キーの欠落、不正な URL、`--misskey.timeout` が 0 以下の場合は検証処理がエラーにする。検証で見つかった問題は、最初の1件で止めずにまとめて報告する。
- `--misskey.url` は、スキームが `http` / `https` で、ホストがあり、認証情報（`user:pass@`）、クエリ、フラグメントを含まない URL だけを受け付ける。パスは許可する（サブパス配下のリバースプロキシ向け）。API のパスを URL の末尾に連結するため、クエリやフラグメントがあると URL が壊れる。
- API キーの解決規則:
  - `MISSKEY_API_KEY` と API キーファイルの両方が指定された場合は、曖昧さを避けるためエラーにする（優先順位は設けない）。
  - どちらも指定されなかった場合もエラーにする。
  - ファイルは起動時に1回だけ読み、前後の空白と改行を除く。読めない場合や、空の場合はエラーにする（ローテーションへの追従は対象外。更新時は再起動する）。
  - エラーメッセージには、ファイルのパスは含めてよいが、内容は含めない。

### 5.2 client

- `http.Client` は起動時に1つ作って使い回す。
- `Post(ctx, path, out)` は JSON ボディ `{}` を送り、`Authorization: Bearer` を付ける（現行の認証方式を維持）。
- 2xx 以外はエラーとし、ステータスコードとボディ先頭 512 バイトを含める。
- レスポンスボディは `io.LimitReader` で 10MB に制限する。
- リダイレクトは追従しない。追従すると POST が GET に変わり、紛らわしいステータスで失敗するため。3xx は非 2xx として報告し、エラーに `--misskey.url` の確認を促す一文を付ける。
- API キーはエラーメッセージに含めない。

### 5.3 queue collector

- レスポンスを `map[string]map[string]float64` でデコードする（現行の `int` では数値型の想定外でデコード全体が失敗するため）。
- レスポンスが JSON の `null` の場合は、失敗として扱う（空のオブジェクト `{}` は、キューが0件の成功として扱う）。
- `misskey_queue_length{queue,state}` を `ConstMetric`（gauge）で出力する。すべてのメトリクスを組み立ててから送出し、途中でエラーになった場合は1つも出さない。

### 5.4 メトリクス

| 名前 | ラベル | 種別 | 内容 |
|---|---|---|---|
| `misskey_queue_length` | `queue`, `state` | gauge | キュー長（既存・維持） |
| `misskey_scrape_collector_success` | `collector` | gauge | 1=成功、0=失敗 |
| `misskey_scrape_collector_duration_seconds` | `collector` | gauge | 取得にかかった時間 |

## 6. エラー処理

- collector が失敗した場合、promslog の logger（`log/slog`）で collector 名とエラーを標準エラー出力に記録し、`success=0` と `duration` だけを出す。メトリクス本体は出さない（古い値や 0 を残さない）。
- `/metrics` 自体は常に 200 を返す。失敗は `misskey_scrape_collector_success` で表現する。
- 起動時の設定エラーのみ、即終了する。

## 7. テスト

標準の `testing`、`httptest`、`prometheus/testutil` を使う。実装は TDD（テスト → 実装）で進める。

- config: 引数のスライスと環境変数を与えて解釈できる形にし、必須の欠落（URL、API キー）、不正な URL、既定値、不正な `--misskey.timeout`、フラグと環境変数の優先順位を確認する。API キーは、環境変数のみ、ファイルのみ、末尾改行付きファイル、両方指定（エラー）、どちらもなし（エラー）、存在しないファイル、空のファイルを確認する
- client: 成功、非 2xx、エラーボディの切り詰め、タイムアウト（呼び出し側の context で止まること）、不正な JSON、サイズ上限、Bearer ヘッダー、サブパス付き URL、接続エラーに API キーが含まれないこと
- queue collector: 偽 Misskey サーバー相手に `testutil.CollectAndCompare`
- exporter: 1つの collector が失敗しても他が出ること、collector ごとに success と duration が出ること、タイムアウトで止まること、同時スクレイプが競合しないこと（`go test -race`）、失敗がログに出ること
- `/metrics` のエンドツーエンド: Misskey が壊れていても 200 を返し、`misskey_scrape_collector_success` が 0 になり、キューの系列が残らないこと

## 8. ビルドと配布

- Dockerfile の `go build` を、単一ファイル指定から `go build -o /misskey_exporter .` に変更する（複数ファイル化で現行の指定は壊れるため必須）。builder イメージは `golang:1.26` に上げる（N2 参照）。実行用イメージ、`EXPOSE 9100`、ENTRYPOINT は維持する。フラグは `docker run` の引数（CMD）で渡せる。
- `go mod tidy` で `// indirect` を整理し、5.1 の依存を追加する。
- バイナリ配布（将来）: `CGO_ENABLED=0` でクロスコンパイルできる構成を保つ。バージョン情報は `prometheus/common/version` の変数に、ビルド時の `-ldflags` で埋め込める。リリース自動化（GoReleaser など）は今回はスコープ外。

## 9. ドキュメント

- `README.md`: 日本語で書き直す。概要、起動方法（環境変数とファイル指定の例）、フラグと環境変数の表、メトリクス一覧、Prometheus 設定、必要な API 権限（v2025.3.1 では `絵文字を見る` も必要という注記を維持）。
- `CLAUDE.md`: 新しいファイル構成、`go test ./...`（単体テスト実行例を含む）、collector の追加手順に合わせて書き直す。

## 10. 互換性

- 互換性は必須ではないが、既存の利用者が影響を受けないよう、環境変数名（`MISSKEY_URL`、`MISSKEY_API_KEY`）、既定ポート、`misskey_queue_length{queue,state}`、Grafana ダッシュボードは維持する。メトリクス名を見直したが、変える理由がなかった。
- 変更: 取得失敗時に古い値が残らなくなる、`misskey_scrape_collector_*` が増える、スクレイプ時取得になる（Misskey への負荷はスクレイプ間隔に比例する）。

## 11. 完了条件

- `go build ./...`、`go vet ./...`、`go test ./...` が通る。
- `docker build` が通る。
- `--version` と `--help` が動く。
- 実 Misskey に対して `MISSKEY_URL` と `MISSKEY_API_KEY` を渡して `go run .` を実行し、`curl localhost:9100/metrics` で `misskey_queue_length` と `misskey_scrape_collector_success 1` を確認する（実 Misskey は利用者の環境で確認する）。
