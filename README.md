# misskey_exporter

Misskey の管理 API から情報を取得し、Prometheus 形式で公開する Exporter です。

現在取得できる情報は、ジョブキューの長さ（`admin/queue/stats`）です。

メトリクスはスクレイプされるたびに Misskey から取得します。

## 使い方

### Docker

```sh
docker build -t misskey_exporter .
docker run -d --name misskey_exporter \
  -e MISSKEY_URL="https://example.com" \
  -e MISSKEY_API_KEY="YOUR_API_KEY" \
  -p 9100:9100 \
  misskey_exporter
```

Docker Secrets や Kubernetes Secret など、ファイルでアクセストークンを渡す場合は、`MISSKEY_API_KEY` の代わりに `MISSKEY_API_KEY_FILE` でファイルのパスを指定します。

```sh
docker run -d --name misskey_exporter \
  -e MISSKEY_URL="https://example.com" \
  -e MISSKEY_API_KEY_FILE=/run/secrets/misskey_api_key \
  -v "$PWD/misskey_api_key:/run/secrets/misskey_api_key:ro" \
  -p 9100:9100 \
  misskey_exporter
```

### ソースから実行

Go 1.26 以降が必要です。

```sh
go run . --misskey.url=https://example.com
```

アクセストークンは環境変数 `MISSKEY_API_KEY` で渡します。

## 設定

フラグはコンテナの場合 `docker run` のイメージ名の後ろに渡せます。

| 項目 | 指定方法 | 既定値 | 説明 |
|---|---|---|---|
| Misskey の URL（必須） | `--misskey.url` または `MISSKEY_URL` | なし | `http` または `https` の URL。末尾の `/` は無視されます。サブパス配下の Misskey も指定できます |
| アクセストークン（必須） | `MISSKEY_API_KEY`、または `--misskey.api-key-file` / `MISSKEY_API_KEY_FILE` でファイルのパス | なし | どちらか一方だけを指定します。両方指定するとエラーです。ファイルは起動時に1回だけ読むので、更新したら再起動してください |
| タイムアウト | `--misskey.timeout` | `5s` | 1回のスクレイプで Misskey API を待つ時間。Prometheus の `scrape_interval` より短くしてください |
| 待ち受けアドレス | `--web.listen-address` | `:9100` | |
| TLS / Basic 認証 | `--web.config.file` | なし | 書式は [exporter-toolkit の web-configuration](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md) を参照してください |
| ログ | `--log.level`, `--log.format` | `info`, `logfmt` | |

必須の設定が足りない場合や、値が不正な場合は、起動に失敗します。`--help` で全フラグを確認できます。

アクセストークンの値そのものをコマンドライン引数で渡すことはできません（プロセス一覧に出てしまうため）。

### アクセストークンの権限

`ジョブキューに関する情報を見る` の権限を有効にしたアクセストークンを発行してください。
ただし、v2025.3.1 の時点ではバグっていて `絵文字を見る` の権限も必要なようです。

## メトリクス

| 名前 | ラベル | 内容 |
|---|---|---|
| `misskey_queue_length` | `queue`, `state` | ジョブキューの長さ |
| `misskey_scrape_collector_success` | `collector` | 直近の取得が成功したら 1、失敗したら 0 |
| `misskey_scrape_collector_duration_seconds` | `collector` | 直近の取得にかかった時間（秒） |

Misskey から取得できなかった場合も `/metrics` は 200 を返します。そのときは `misskey_scrape_collector_success` が 0 になり、`misskey_queue_length` は出力されません（古い値は残りません）。失敗の理由は標準エラー出力のログに出ます。

アラートの例:

```yaml
- alert: MisskeyScrapeFailing
  expr: misskey_scrape_collector_success == 0
  for: 5m
```

## Prometheus の設定

```yaml
scrape_configs:
  - job_name: "misskey"
    scrape_interval: 10s
    static_configs:
      - targets:
        - misskey_exporter:9100
```

スクレイプのたびに Misskey の API を呼ぶので、Misskey への負荷はスクレイプの頻度に比例します。

## Grafana

`dashboards/misskey-dashboard.json` をインポートしてください。

## 開発

```sh
go test -race ./...
```

構成と、メトリクスを追加する手順は [CLAUDE.md](CLAUDE.md) に書いてあります。
