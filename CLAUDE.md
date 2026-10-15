# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Misskey の管理 API から情報を取得し、Prometheus 形式で公開する Exporter。すべて `package main`（リポジトリ直下）で、ファイルは責務ごとに分かれている。Go 1.26 以降が必要（依存ライブラリが要求する）。

## Commands

```sh
go build -o misskey_exporter .                      # ビルド（出力バイナリは .gitignore 済み）
go vet ./...
go test -race ./...                                 # 全テスト（スクレイプの競合検出のため -race を付ける）
go test -race -run TestClientPostSuccess ./...      # 単一テスト
MISSKEY_URL=https://example.com MISSKEY_API_KEY=... go run .
curl localhost:9100/metrics                         # 動作確認
docker build -t misskey_exporter .                  # golang:1.26 → alpine のマルチステージ
goreleaser release --snapshot --clean --skip=docker  # リリースの成果物をローカルで試す（dist/ に出る。イメージは buildx が要るので除く）
go get <module>@latest && go mod tidy              # 依存の更新（tidy だけだと古い版に解決される）
```

テストはすべて偽の Misskey（`httptest`）を相手にするので、実際の Misskey は不要。

## Architecture

スクレイプ時に取得する方式（バックグラウンドのポーリングはしない）。

```
Prometheus → /metrics → exporter.Collect()
  → 各 collector を並列実行（共通の timeout context）
  → client.Post() → Misskey API
  → collector ごとに success と duration を出力
```

- `main.go`: 設定読み込み、`collectors` の登録、HTTP サーバー（exporter-toolkit の `web.ListenAndServe`）、SIGTERM/SIGINT での graceful shutdown。
- `config.go`: kingpin によるフラグ + 環境変数の解釈と検証。アクセストークンは `MISSKEY_API_KEY` かファイル（`--misskey.api-key-file`）で渡し、値そのものをフラグでは受け取らない。検証エラーは `errors.Join` でまとめて返す。
- `client.go`: Misskey API クライアント。自前のタイムアウトは持たず、呼び出し側の context で止める。エラーにトークンを含めない。リダイレクトは追従しない（POST が GET に変わるため）。User-Agent は `misskey_exporter/<version>`（バージョンが取れないときは `unknown`）。
- `version.go`: `version.Version`（`--version`、起動ログ、User-Agent で使う）を、ldflags が空のとき Go が git タグから記録するモジュールバージョンで埋める。タグ付きコミットなら `0.2.0`、タグなしなら `0.0.0-<日時>-<コミット>` になる。`.git` を含まないソースからビルドすると取れず `unknown` になる。
- `exporter.go`: `collector` interface と、全 collector を束ねる `prometheus.Collector`。collector の panic は error に変換される（goroutine 上の panic でプロセスが落ちるのを防ぐ）。
- `collector_queue.go`: `/api/admin/queue/stats` を `misskey_queue_length{queue,state}` にする。

### collector を追加する手順

1. `collector_<name>.go` に `collector` interface（`Name()` と `Update()`）を実装する。`Update` は、失敗しうる処理（API 呼び出しとデコード）をすべて終えてから最初のメトリクスを送出すること。そうすれば、失敗した取得はそのメトリクスを一切出さない（古い値や 0 を残さない）。
2. `main.go` の `collectors` に登録する。
3. `collector_queue_test.go` を手本に、`fakeMisskey` でテストを書く。

## Gotchas

- `exporter.Describe` は意図的に何も送らない（unchecked collector）。メトリクスの一覧を事前に宣言できないため。
- `dashboards/misskey-dashboard.json`（Grafana）は `misskey_queue_length{queue="..."}` に依存している。名前やラベルを変えるときはダッシュボードも直す。
- 設定のテストは `t.Setenv` で環境変数を隔離している（kingpin の `Envar` が `os.Getenv` を直接読むため）。
- `httptest` のハンドラでクライアントの中断を待つテストは、先にリクエストボディを読むこと（`io.Copy(io.Discard, r.Body)`）。読まないとサーバーが切断に気づかず、`srv.Close` が待ち続けてテストが数秒止まる。
- `testutil.CollectAndCompare` で duration は比較できない（値が不定）。`misskey_queue_length` や `misskey_scrape_collector_success` のように、メトリクス名で絞って比較する。
- `main.go` の `run()` に自動テストはない（kingpin が `--help` / `--version` でプロセスを終了するため）。`main.go` やフラグを変えたら、`--version`、`--help`、偽の Misskey + `curl` で実バイナリを確認する。
- Dockerfile は2つある。`Dockerfile` はローカルの `docker build .` 用（マルチステージ）、`Dockerfile.release` は GoReleaser 用（ビルド済みバイナリを `COPY` するだけ）。`EXPOSE` と `ENTRYPOINT` が重複するので、片方を変えたらもう片方も直す。
- `--version` の出力は stderr に出る（kingpin）。`grep` や `head` に渡すときは `2>&1` を付ける。
- コード内のコメントは英語、README は日本語。

## Workflow

- ブランチ名は `<種別>/<内容>`（例: `docs/add-claude-md`、`refactor/rewrite-exporter`）。PR は通常のマージコミットでマージする（squash しない）。
- 設計の経緯は `docs/superpowers/specs/2026-10-15-exporter-rewrite-design.md`、実装計画は `docs/superpowers/plans/` にある。
