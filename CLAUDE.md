# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Misskey の管理 API からジョブキューの情報を取得し、Prometheus 形式で公開する Exporter。実装は単一ファイル `misskey_exporter.go`（`package main`）のみ。テストとリンター設定は存在しない。

## Commands

```sh
go build -o misskey_exporter misskey_exporter.go   # ビルド（出力バイナリは .gitignore 済み）
MISSKEY_API_KEY=... MISSKEY_URL=https://example.com go run misskey_exporter.go
curl localhost:9100/metrics                         # 動作確認
docker build -t misskey_exporter .                  # Dockerfile は golang:1.24 → alpine のマルチステージ
```

## Architecture

- `main` が `/metrics`（`promhttp.Handler()`）を `:9100` で公開し、goroutine で 10 秒ごとに `fetchQueueStats()` を呼んで Gauge を更新する（スクレイプ時に取得するのではなく、バックグラウンドのポーリング方式）。リッスンポートと間隔はハードコード。
- `fetchQueueStats()` は `POST {MISSKEY_URL}/api/admin/queue/stats` を `Authorization: Bearer` で叩き、レスポンス `map[queue]map[state]int` を `misskey_queue_length{queue,state}` の GaugeVec にそのまま反映する。
- `MISSKEY_API_KEY` / `MISSKEY_URL` は `fetchQueueStats()` 内で毎回 `getEnvVariable()` により読まれ、未設定だと `os.Exit(1)` する。そのため未設定は起動直後ではなく最初のポーリングで検知される。
- HTTP エラーやデコード失敗は `fmt.Println` で出力して次の周期に進むだけ（ステータスコードは検査しない）。
- `dashboards/misskey-dashboard.json` は Grafana ダッシュボード。`misskey_queue_length{queue="..."}` のクエリに依存しているため、メトリクス名やラベルを変えるとダッシュボードも直す必要がある。

## Gotchas

- `go.mod` の依存がすべて `// indirect` になっている（直接 import している `prometheus/client_golang` も含む）。`go mod tidy` で直る。
- 必要な API 権限は「ジョブキューに関する情報を見る」。README によると Misskey v2025.3.1 時点では「絵文字を見る」権限も必要。
- README・コード内コメントは日本語。
