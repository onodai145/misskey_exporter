# misskey_exporter

ChatGPTに教えてもらいながらすべてをコピペででっち上げたMisskey用のExporterです。

今のところ、キューの情報を取得できるだけです。

## 使い方

```sh
docker build -t misskey_exporter .
docker run -d --name misskey_exporter \
  -e MISSKEY_API_KEY="YOUR_API_KEY" \
  -e MISSKEY_URL="https://example.com" \
  -p 9100:9100 \
  misskey_exporter
```
### 起動に必要な環境変数

必要な環境変数が設定されていない場合、起動に失敗します。

1. MISSKEY_APY_KEY

    `ジョブキューに関する情報を見る` の権限を有効にしたアクセストークンを発行してください。  
    ただし、v2025.3.1の時点ではバグっていて `絵文字を見る` の権限が必要っぽいです。

1. MISSKEY_URL

    MisskeyのURLです。

### Prometheusの設定

```yaml
scrape_configs:
  - job_name: "misskey"
    scrape_interval: 10s
    static_configs:
      - targets:
        - misskey_exporter:9100
```
