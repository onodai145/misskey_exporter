# misskey_exporter リライト 実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 単一ファイルのポーリング型 Exporter を、スクレイプ時取得・collector 構成・テスト付きの Exporter に書き直す。

**Architecture:** `package main` のまま責務ごとにファイルを分ける（config / client / exporter / collector_queue / main）。`exporter` が `prometheus.Collector` として全 `collector` を並列に実行し、collector ごとの success と duration を出す。設定は kingpin + exporter-toolkit + promslog の Prometheus 標準スタックで受ける。

**Tech Stack:** Go 1.26、`github.com/alecthomas/kingpin/v2`、`github.com/prometheus/client_golang`、`github.com/prometheus/common`（`promslog`, `version`）、`github.com/prometheus/exporter-toolkit`。

**Spec:** `docs/superpowers/specs/2026-10-15-exporter-rewrite-design.md`（実装者は計画と仕様書の両方を読むこと）

## Global Constraints

- Go: `go.mod` の `go` ディレクティブと builder イメージは 1.26（`golang:1.26`）。最新版のライブラリ（`client_golang` v1.25 など）が要求する。
- 依存は仕様書 5.1 のライブラリのみ。テストは標準の `testing`、`httptest`、`prometheus/testutil`。
- 環境変数名は `MISSKEY_URL`、`MISSKEY_API_KEY`、`MISSKEY_API_KEY_FILE`。既定の待ち受けは `:9100`、`--misskey.timeout` の既定は `5s`。
- メトリクスは `misskey_queue_length{queue,state}`（help は `Current length of Misskey queues`）、`misskey_scrape_collector_success{collector}`、`misskey_scrape_collector_duration_seconds{collector}`。いずれも gauge。
- API キーはログ・エラーメッセージ・コマンドライン引数に出さない。`MISSKEY_API_KEY` と `--misskey.api-key-file` の両方指定はエラー。
- client: エラーボディは先頭 512 バイト、レスポンスは 10MiB まで。client 自身はタイムアウトを持たず、呼び出し側の context で止める。
- 失敗した collector は、そのメトリクス本体を出さない（古い値や 0 を残さない）。`/metrics` は常に 200 を返す。
- 作業ディレクトリは、リポジトリのルート。ブランチは `refactor/rewrite-exporter`（作成済み）。`main` に直接コミットしない。
- コミットメッセージは件名のみ（本文なし）。`--no-verify` / `--no-gpg-sign` は使わない。`git commit` が失敗またはタイムアウトしたら、リトライせず停止して報告する。コミットには、件名の後ろに `-m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"` を別の `-m` として付ける。
- コード内のコメントは英語、README / CLAUDE.md は日本語。

## Review Focus

仕様書が暗に求めているが、タスクのテスト以外では見落としやすい入力・状態を、起きやすい順に挙げる。各行は担当タスクのテストで固定する。

1. Docker Secrets / Kubernetes Secret のファイルは末尾に改行がある。トークンから前後の空白を除き、改行込みで 401 にならないこと（Task 1 `TestParseConfigAPIKeySources/file_with_surrounding_whitespace`）。
2. Misskey がリバースプロキシのサブパス配下（`https://example.com/misskey/`）にある、または URL が `/` で終わる。`//api/...` にならず、`/misskey/api/...` に届くこと（Task 1 `TestParseConfigURLNormalization`、Task 2 `TestClientPostWithPathPrefix`）。
3. Misskey が応答しない（ハング）。スクレイプがタイムアウトで止まり、他の collector は影響を受けず、success が 0 になること（Task 2 `TestClientPostContextTimeout`、Task 3 `TestExporterTimeout`）。
4. トークンの権限が足りず 401/403 が返る、または 200 で `null` や想定外の形が返る。success が 0 になり、キューの系列が残らず、`/metrics` は 200 のままで、API キーがログとエラーに出ないこと（Task 2 `TestClientPostNon2xx`、Task 4 `TestQueueCollectorFailures` と `TestMetricsEndpointStaysUpWhenMisskeyFails`）。
5. Prometheus が2台以上から同時にスクレイプする。競合しないこと（Task 3 `TestExporterConcurrentScrapes`、`go test -race`）。

---

## ファイル構成

| ファイル | 操作 | 責務 |
|---|---|---|
| `config.go` / `config_test.go` | 作成 | フラグ・環境変数の解釈と検証 |
| `client.go` / `client_test.go` | 作成 | Misskey API クライアント |
| `exporter.go` / `exporter_test.go` | 作成 | `collector` interface と、全 collector を束ねる `prometheus.Collector` |
| `collector_queue.go` / `collector_queue_test.go` | 作成 | `admin/queue/stats` 用 collector と、`/metrics` のエンドツーエンドテスト |
| `main.go` | 作成 | 組み立て、HTTP サーバー、graceful shutdown |
| `misskey_exporter.go` | 削除（Task 5） | 旧実装。Task 5 まで残す（新しいファイルと名前が衝突しないため共存できる） |
| `Dockerfile` | 変更 | `golang:1.26`、`go build -o /misskey_exporter .` |
| `go.mod` / `go.sum` | 変更 | 依存の追加と整理 |
| `README.md` / `CLAUDE.md` | 書き直し | ドキュメント |

`dashboards/misskey-dashboard.json` は変更しない（`misskey_queue_length` のまま）。

---

### Task 1: config（フラグと環境変数）

**Files:**
- Create: `config.go`
- Create: `config_test.go`
- Modify: `go.mod`, `go.sum`（`go mod tidy`）

**Interfaces:**
- Consumes: なし
- Produces:
  - `const programName = "misskey_exporter"`、`const defaultListenAddr = ":9100"`
  - `type config struct { MisskeyURL string; APIKey string; Timeout time.Duration; Web *web.FlagConfig; Log *promslog.Config }`
  - `func parseConfig(args []string) (*config, error)`（`args` はプログラム名を含まない）。環境変数 `MISSKEY_URL`、`MISSKEY_API_KEY`、`MISSKEY_API_KEY_FILE` を `os.Getenv` 経由で読む。

- [ ] **Step 1: 失敗するテストを書く**

`config_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setEnv isolates a test from the real environment. kingpin treats an empty
// environment variable as unset.
func setEnv(t *testing.T, url, key, keyFile string) {
	t.Helper()
	t.Setenv("MISSKEY_URL", url)
	t.Setenv("MISSKEY_API_KEY", key)
	t.Setenv("MISSKEY_API_KEY_FILE", keyFile)
}

func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseConfigDefaults(t *testing.T) {
	setEnv(t, "", "secret-token", "")

	cfg, err := parseConfig([]string{"--misskey.url=https://example.com"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MisskeyURL != "https://example.com" {
		t.Errorf("MisskeyURL = %q", cfg.MisskeyURL)
	}
	if cfg.APIKey != "secret-token" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
	if got := *cfg.Web.WebListenAddresses; len(got) != 1 || got[0] != ":9100" {
		t.Errorf("WebListenAddresses = %v, want [:9100]", got)
	}
}

func TestParseConfigURLFromEnv(t *testing.T) {
	setEnv(t, "https://env.example.com", "secret-token", "")

	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MisskeyURL != "https://env.example.com" {
		t.Errorf("MisskeyURL = %q", cfg.MisskeyURL)
	}
}

func TestParseConfigFlagOverridesEnv(t *testing.T) {
	setEnv(t, "https://env.example.com", "secret-token", "")

	cfg, err := parseConfig([]string{"--misskey.url=https://flag.example.com"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MisskeyURL != "https://flag.example.com" {
		t.Errorf("MisskeyURL = %q, want the flag value", cfg.MisskeyURL)
	}
}

func TestParseConfigURLNormalization(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://example.com", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{"http://localhost:3000/", "http://localhost:3000"},
		// Misskey behind a reverse proxy under a sub path.
		{"https://example.com/misskey/", "https://example.com/misskey"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			setEnv(t, "", "secret-token", "")
			cfg, err := parseConfig([]string{"--misskey.url=" + tt.in})
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if cfg.MisskeyURL != tt.want {
				t.Errorf("MisskeyURL = %q, want %q", cfg.MisskeyURL, tt.want)
			}
		})
	}
}

func TestParseConfigInvalidURL(t *testing.T) {
	tests := []string{
		"example.com",
		"ftp://example.com",
		"https://",
		"https://user:pass@example.com",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			setEnv(t, "", "secret-token", "")
			_, err := parseConfig([]string{"--misskey.url=" + in})
			if err == nil {
				t.Fatalf("parseConfig(%q) succeeded, want error", in)
			}
			if !strings.Contains(err.Error(), "misskey.url") {
				t.Errorf("error %q does not mention misskey.url", err)
			}
			if strings.Contains(err.Error(), "pass") {
				t.Errorf("error %q leaks URL credentials", err)
			}
		})
	}
}

func TestParseConfigMissingURL(t *testing.T) {
	setEnv(t, "", "secret-token", "")

	_, err := parseConfig(nil)
	if err == nil || !strings.Contains(err.Error(), "misskey.url") {
		t.Fatalf("err = %v, want an error mentioning misskey.url", err)
	}
}

func TestParseConfigTimeout(t *testing.T) {
	setEnv(t, "", "secret-token", "")

	cfg, err := parseConfig([]string{"--misskey.url=https://example.com", "--misskey.timeout=2s"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Timeout != 2*time.Second {
		t.Errorf("Timeout = %v, want 2s", cfg.Timeout)
	}

	for _, bad := range []string{"0s", "-1s", "abc"} {
		t.Run(bad, func(t *testing.T) {
			setEnv(t, "", "secret-token", "")
			_, err := parseConfig([]string{"--misskey.url=https://example.com", "--misskey.timeout=" + bad})
			if err == nil {
				t.Fatalf("timeout %q accepted, want error", bad)
			}
		})
	}
}

func TestParseConfigAPIKeySources(t *testing.T) {
	args := []string{"--misskey.url=https://example.com"}

	t.Run("env only", func(t *testing.T) {
		setEnv(t, "", "env-token", "")
		cfg, err := parseConfig(args)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.APIKey != "env-token" {
			t.Errorf("APIKey = %q", cfg.APIKey)
		}
	})

	t.Run("file only", func(t *testing.T) {
		setEnv(t, "", "", "")
		path := writeKeyFile(t, "file-token")
		cfg, err := parseConfig(append(args, "--misskey.api-key-file="+path))
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.APIKey != "file-token" {
			t.Errorf("APIKey = %q", cfg.APIKey)
		}
	})

	// Docker secrets and Kubernetes secrets are often written with a trailing
	// newline.
	t.Run("file with surrounding whitespace", func(t *testing.T) {
		setEnv(t, "", "", "")
		path := writeKeyFile(t, "  file-token\r\n")
		cfg, err := parseConfig(append(args, "--misskey.api-key-file="+path))
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.APIKey != "file-token" {
			t.Errorf("APIKey = %q, want the trimmed token", cfg.APIKey)
		}
	})

	t.Run("file path from env", func(t *testing.T) {
		path := writeKeyFile(t, "file-token")
		setEnv(t, "", "", path)
		cfg, err := parseConfig(args)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.APIKey != "file-token" {
			t.Errorf("APIKey = %q", cfg.APIKey)
		}
	})
}

func TestParseConfigAPIKeyErrors(t *testing.T) {
	args := []string{"--misskey.url=https://example.com"}

	t.Run("both set", func(t *testing.T) {
		path := writeKeyFile(t, "file-token")
		setEnv(t, "", "env-token-xyz", "")
		_, err := parseConfig(append(args, "--misskey.api-key-file="+path))
		if err == nil || !strings.Contains(err.Error(), "only one") {
			t.Fatalf("err = %v, want a 'specify only one' error", err)
		}
		for _, secret := range []string{"env-token-xyz", "file-token"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q leaks the API key", err)
			}
		}
	})

	t.Run("neither set", func(t *testing.T) {
		setEnv(t, "", "", "")
		_, err := parseConfig(args)
		if err == nil || !strings.Contains(err.Error(), "MISSKEY_API_KEY") {
			t.Fatalf("err = %v, want an error mentioning MISSKEY_API_KEY", err)
		}
	})

	t.Run("whitespace-only env", func(t *testing.T) {
		setEnv(t, "", "  \n", "")
		if _, err := parseConfig(args); err == nil {
			t.Fatal("whitespace-only MISSKEY_API_KEY accepted, want error")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		setEnv(t, "", "", "")
		missing := filepath.Join(t.TempDir(), "nope")
		_, err := parseConfig(append(args, "--misskey.api-key-file="+missing))
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Fatalf("err = %v, want an error naming the file", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		setEnv(t, "", "", "")
		path := writeKeyFile(t, " \n")
		_, err := parseConfig(append(args, "--misskey.api-key-file="+path))
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("err = %v, want an 'empty' error", err)
		}
	})
}

func TestParseConfigReportsAllProblems(t *testing.T) {
	setEnv(t, "", "", "")

	_, err := parseConfig([]string{"--misskey.url=ftp://example.com", "--misskey.timeout=0s"})
	if err == nil {
		t.Fatal("parseConfig succeeded, want error")
	}
	for _, want := range []string{"misskey.url", "misskey.timeout", "MISSKEY_API_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./... -run TestParseConfig 2>&1 | head -5`
Expected: ビルド失敗。`undefined: parseConfig`（旧 `misskey_exporter.go` はそのまま残っていて、ビルドを妨げない）

- [ ] **Step 3: 実装する**

`config.go`:

```go
package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/common/promslog"
	promslogflag "github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

const (
	programName       = "misskey_exporter"
	defaultListenAddr = ":9100"
)

// config is the validated runtime configuration.
type config struct {
	MisskeyURL string        // normalized base URL, without a trailing slash
	APIKey     string        // access token; never log it
	Timeout    time.Duration // timeout of one scrape's API requests
	Web        *web.FlagConfig
	Log        *promslog.Config
}

// parseConfig parses command line arguments (without the program name) and the
// environment, then validates the result. It reports every problem it finds.
func parseConfig(args []string) (*config, error) {
	app := kingpin.New(programName, "Prometheus exporter for Misskey.")
	app.HelpFlag.Short('h')
	app.Version(version.Print(programName))

	rawURL := app.Flag("misskey.url", "Base URL of the Misskey instance (http or https).").
		Envar("MISSKEY_URL").Required().String()
	keyFile := app.Flag("misskey.api-key-file",
		"Path to a file that contains the Misskey access token. "+
			"Alternative to the MISSKEY_API_KEY environment variable.").
		Envar("MISSKEY_API_KEY_FILE").String()
	timeout := app.Flag("misskey.timeout",
		"Timeout of the Misskey API requests of one scrape. Keep it shorter than the Prometheus scrape interval.").
		Default("5s").Duration()

	cfg := &config{Log: &promslog.Config{}}
	cfg.Web = webflag.AddFlags(app, defaultListenAddr)
	promslogflag.AddFlags(app, cfg.Log)

	if _, err := app.Parse(args); err != nil {
		return nil, err
	}

	var errs []error

	baseURL, err := normalizeBaseURL(*rawURL)
	if err != nil {
		errs = append(errs, err)
	}
	if *timeout <= 0 {
		errs = append(errs, errors.New("--misskey.timeout must be greater than 0"))
	}
	apiKey, err := resolveAPIKey(os.Getenv("MISSKEY_API_KEY"), *keyFile)
	if err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	cfg.MisskeyURL = baseURL
	cfg.APIKey = apiKey
	cfg.Timeout = *timeout
	return cfg, nil
}

// normalizeBaseURL checks that raw is an http(s) URL and strips a trailing
// slash so that "<base>/api/..." never contains a double slash.
func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid --misskey.url: cannot be parsed as a URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("invalid --misskey.url: scheme must be http or https")
	}
	if u.Host == "" {
		return "", errors.New("invalid --misskey.url: host is missing")
	}
	if u.User != nil {
		return "", errors.New("invalid --misskey.url: must not contain credentials")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// resolveAPIKey returns the access token from either the environment value or
// the key file. Specifying both is an error so that there is no precedence
// rule to remember. Error messages never contain the token.
func resolveAPIKey(envValue, keyFile string) (string, error) {
	envValue = strings.TrimSpace(envValue)
	switch {
	case envValue != "" && keyFile != "":
		return "", errors.New("both MISSKEY_API_KEY and --misskey.api-key-file are set; specify only one")
	case envValue != "":
		return envValue, nil
	case keyFile != "":
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return "", fmt.Errorf("read API key file: %w", err)
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("API key file %q is empty", keyFile)
		}
		return key, nil
	default:
		return "", errors.New("API key is not set; set MISSKEY_API_KEY or --misskey.api-key-file")
	}
}
```

- [ ] **Step 4: 依存を解決してテストを通す**

Run:

```bash
go get github.com/alecthomas/kingpin/v2@latest github.com/prometheus/client_golang@latest \
  github.com/prometheus/common@latest github.com/prometheus/exporter-toolkit@latest
go mod tidy && go vet ./... && go test -race -count=1 ./...
```

Expected: PASS。`go.mod` の `go` ディレクティブが `go 1.26.0` に上がり、`kingpin/v2`、`client_golang`、`common`、`exporter-toolkit` が直接依存として入る（`go.mod` を確認すること）。

`@latest` を明示するのは、`go mod tidy` だけだと既存の古い `client_golang` / `common` に引きずられて `go 1.25.0` に解決されるため。Go 1.27 が出ている現在、1.25 はサポート対象外なので、サポート対象の最も古い 1.26 を下限にする。

- [ ] **Step 5: コミットする**

```bash
git add config.go config_test.go go.mod go.sum
git commit -m "Add config parsing with flags and API key file" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Misskey API クライアント

**Files:**
- Create: `client.go`
- Create: `client_test.go`

**Interfaces:**
- Consumes: なし
- Produces:
  - `const maxErrorBodyBytes = 512`、`const defaultMaxResponseBytes = 10 << 20`
  - `type client struct { baseURL, apiKey string; http *http.Client; maxResponseBytes int64 }`
  - `func newClient(baseURL, apiKey string) *client`（`baseURL` は末尾 `/` なし。`parseConfig` が正規化済み）
  - `func (c *client) Post(ctx context.Context, path string, out any) error`（`path` は `/api/...`。JSON ボディ `{}` を `POST` し、`out` にデコードする）
  - テスト用の `const testAPIKey`（`client_test.go` 内。Task 4 のテストが使う）

- [ ] **Step 1: 失敗するテストを書く**

`client_test.go`:

```go
package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "secret-token-123"

func TestClientPostSuccess(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		io.WriteString(w, `{"deliver":{"waiting":2}}`)
	}))
	defer srv.Close()

	var out map[string]map[string]float64
	c := newClient(srv.URL, testAPIKey)
	if err := c.Post(context.Background(), "/api/admin/queue/stats", &out); err != nil {
		t.Fatalf("Post: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/admin/queue/stats" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer "+testAPIKey {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if gotBody != "{}" {
		t.Errorf("body = %q, want {}", gotBody)
	}
	if out["deliver"]["waiting"] != 2 {
		t.Errorf("decoded = %v", out)
	}
}

// Misskey behind a reverse proxy under a sub path (see config normalization).
func TestClientPostWithPathPrefix(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	var out map[string]any
	c := newClient(srv.URL+"/misskey", testAPIKey)
	if err := c.Post(context.Background(), "/api/x", &out); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotPath != "/misskey/api/x" {
		t.Errorf("path = %q, want /misskey/api/x", gotPath)
	}
}

// The token lacking a permission (see README) makes Misskey answer 4xx.
func TestClientPostNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"message":"Insufficient permissions."}}`)
	}))
	defer srv.Close()

	var out map[string]any
	err := newClient(srv.URL, testAPIKey).Post(context.Background(), "/api/x", &out)
	if err == nil {
		t.Fatal("Post succeeded, want error")
	}
	for _, want := range []string{"403", "Insufficient permissions."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("error %q leaks the API key", err)
	}
}

func TestClientPostTruncatesErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, strings.Repeat("z", 5000))
	}))
	defer srv.Close()

	var out map[string]any
	err := newClient(srv.URL, testAPIKey).Post(context.Background(), "/api/x", &out)
	if err == nil {
		t.Fatal("Post succeeded, want error")
	}
	if n := strings.Count(err.Error(), "z"); n != maxErrorBodyBytes {
		t.Errorf("error contains %d body bytes, want %d", n, maxErrorBodyBytes)
	}
}

func TestClientPostInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html>not json</html>`)
	}))
	defer srv.Close()

	var out map[string]any
	err := newClient(srv.URL, testAPIKey).Post(context.Background(), "/api/x", &out)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err = %v, want a decode error", err)
	}
}

func TestClientPostResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"a":"`+strings.Repeat("a", 100)+`"}`)
	}))
	defer srv.Close()

	c := newClient(srv.URL, testAPIKey)
	c.maxResponseBytes = 16
	var out map[string]any
	err := c.Post(context.Background(), "/api/x", &out)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want a size limit error", err)
	}
}

// A hung Misskey must not hang the scrape: the caller's context decides.
func TestClientPostContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server only notices a closed connection after the body is read.
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	var out map[string]any
	err := newClient(srv.URL, testAPIKey).Post(ctx, "/api/x", &out)
	if err == nil {
		t.Fatal("Post succeeded, want error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Post took %v, want it to stop at the context deadline", elapsed)
	}
}

// Transport errors embed the request URL; the token must never be part of it.
func TestClientPostConnectionErrorDoesNotLeakKey(t *testing.T) {
	var out map[string]any
	err := newClient("http://127.0.0.1:1", testAPIKey).Post(context.Background(), "/api/x", &out)
	if err == nil {
		t.Fatal("Post succeeded, want error")
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("error %q leaks the API key", err)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./... -run TestClient 2>&1 | head -5`
Expected: ビルド失敗。`undefined: newClient`、`undefined: maxErrorBodyBytes` など

- [ ] **Step 3: 実装する**

`client.go`:

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	defaultMaxResponseBytes = 10 << 20 // 10 MiB
	maxErrorBodyBytes       = 512
)

// client calls the Misskey API. It has no timeout of its own: callers bound
// each request with the context they pass in.
type client struct {
	baseURL          string // without a trailing slash, see normalizeBaseURL
	apiKey           string
	http             *http.Client
	maxResponseBytes int64
}

func newClient(baseURL, apiKey string) *client {
	return &client{
		baseURL:          baseURL,
		apiKey:           apiKey,
		http:             &http.Client{},
		maxResponseBytes: defaultMaxResponseBytes,
	}
}

// Post sends an empty JSON object to path (for example "/api/admin/queue/stats")
// and decodes the JSON response into out. Errors never contain the API key.
func (c *client) Post(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("create request for %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("request %s: unexpected status %d: %s", path, resp.StatusCode, bytes.TrimSpace(snippet))
	}

	// Read one byte more than allowed to tell "exactly the limit" from "over".
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response of %s: %w", path, err)
	}
	if int64(len(data)) > c.maxResponseBytes {
		return fmt.Errorf("response of %s exceeds %d bytes", path, c.maxResponseBytes)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response of %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: テストを通す**

Run: `go vet ./... && go test -race -count=1 -run TestClient -v ./...`
Expected: 8 件すべて PASS。`TestClientPostContextTimeout` は 0.1 秒未満で終わる（5 秒かかる場合は、テストのハンドラがリクエストボディを読んでいない）。

- [ ] **Step 5: コミットする**

```bash
git add client.go client_test.go
git commit -m "Add Misskey API client" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 3: exporter と collector interface

**Files:**
- Create: `exporter.go`
- Create: `exporter_test.go`
- Modify: `go.mod`, `go.sum`（`prometheus/testutil` が使う依存が増える）

**Interfaces:**
- Consumes: `*client`（Task 2）
- Produces:
  - `type collector interface { Name() string; Update(ctx context.Context, c *client, ch chan<- prometheus.Metric) error }`
  - `func newExporter(c *client, collectors []collector, timeout time.Duration, logger *slog.Logger) *exporter`（`*exporter` は `prometheus.Collector`）
  - `scrapeSuccessDesc`、`scrapeDurationDesc`（ラベルは `collector`）

- [ ] **Step 1: 失敗するテストを書く**

`exporter_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeCollector is a collector whose behavior is supplied by the test.
type fakeCollector struct {
	name   string
	update func(ctx context.Context, ch chan<- prometheus.Metric) error
}

func (f fakeCollector) Name() string { return f.name }

func (f fakeCollector) Update(ctx context.Context, _ *client, ch chan<- prometheus.Metric) error {
	return f.update(ctx, ch)
}

var fakeDesc = prometheus.NewDesc("test_value", "A test metric.", []string{"collector"}, nil)

func okCollector(name string) fakeCollector {
	return fakeCollector{name: name, update: func(_ context.Context, ch chan<- prometheus.Metric) error {
		ch <- prometheus.MustNewConstMetric(fakeDesc, prometheus.GaugeValue, 1, name)
		return nil
	}}
}

func failingCollector(name string, err error) fakeCollector {
	return fakeCollector{name: name, update: func(context.Context, chan<- prometheus.Metric) error {
		return err
	}}
}

func newTestExporter(timeout time.Duration, logger *slog.Logger, cols ...collector) *exporter {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return newExporter(nil, cols, timeout, logger)
}

func TestExporterReportsSuccessPerCollector(t *testing.T) {
	exp := newTestExporter(time.Second, nil,
		okCollector("ok"),
		failingCollector("bad", errors.New("boom")),
	)

	expected := `
# HELP misskey_scrape_collector_success Whether the last scrape of a collector succeeded (1) or failed (0).
# TYPE misskey_scrape_collector_success gauge
misskey_scrape_collector_success{collector="bad"} 0
misskey_scrape_collector_success{collector="ok"} 1
`
	if err := testutil.CollectAndCompare(exp, strings.NewReader(expected), "misskey_scrape_collector_success"); err != nil {
		t.Error(err)
	}
	if n := testutil.CollectAndCount(exp, "misskey_scrape_collector_duration_seconds"); n != 2 {
		t.Errorf("duration series = %d, want 2 (one per collector)", n)
	}
}

// A failing collector must not take the others down, and the metrics of the
// healthy one must still be exposed.
func TestExporterFailureDoesNotAffectOtherCollectors(t *testing.T) {
	exp := newTestExporter(time.Second, nil,
		failingCollector("bad", errors.New("boom")),
		okCollector("ok"),
	)

	expected := `
# HELP test_value A test metric.
# TYPE test_value gauge
test_value{collector="ok"} 1
`
	if err := testutil.CollectAndCompare(exp, strings.NewReader(expected), "test_value"); err != nil {
		t.Error(err)
	}
}

func TestExporterLogsFailure(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	exp := newTestExporter(time.Second, logger, failingCollector("bad", errors.New("kaboom")))

	testutil.CollectAndCount(exp, "misskey_scrape_collector_success")

	for _, want := range []string{"collector=bad", "kaboom"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log %q does not contain %q", buf.String(), want)
		}
	}
}

// A hung API must not hang the scrape: the exporter's timeout cancels the
// collectors, success is reported as 0 and the healthy collector is unaffected.
func TestExporterTimeout(t *testing.T) {
	hang := fakeCollector{name: "hang", update: func(ctx context.Context, _ chan<- prometheus.Metric) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	exp := newTestExporter(50*time.Millisecond, nil, hang, okCollector("ok"))

	start := time.Now()
	expected := `
# HELP misskey_scrape_collector_success Whether the last scrape of a collector succeeded (1) or failed (0).
# TYPE misskey_scrape_collector_success gauge
misskey_scrape_collector_success{collector="hang"} 0
misskey_scrape_collector_success{collector="ok"} 1
`
	if err := testutil.CollectAndCompare(exp, strings.NewReader(expected), "misskey_scrape_collector_success"); err != nil {
		t.Error(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("scrape took %v, want it to stop at the timeout", elapsed)
	}
}

// Two Prometheus servers may scrape at the same time. Run with -race.
func TestExporterConcurrentScrapes(t *testing.T) {
	exp := newTestExporter(time.Second, nil, okCollector("a"), okCollector("b"))

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if n := testutil.CollectAndCount(exp, "misskey_scrape_collector_success"); n != 2 {
				t.Errorf("success series = %d, want 2", n)
			}
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go mod tidy && go test ./... -run TestExporter 2>&1 | head -5`
Expected: ビルド失敗。`undefined: collector`、`undefined: newExporter` など（`go mod tidy` は `prometheus/testutil` の依存を解決するために先に実行する）

- [ ] **Step 3: 実装する**

`exporter.go`:

```go
package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// collector scrapes one Misskey API and turns the result into metrics.
//
// To add a metric source, implement this interface in a new collector_<name>.go
// file and register it in main.go.
type collector interface {
	// Name is the value of the "collector" label of the scrape metrics.
	Name() string
	// Update fetches the data and sends metrics to ch. It must finish all
	// fallible work before sending the first metric, so that a failed scrape
	// exposes none of the collector's own metrics.
	Update(ctx context.Context, c *client, ch chan<- prometheus.Metric) error
}

var (
	scrapeSuccessDesc = prometheus.NewDesc(
		"misskey_scrape_collector_success",
		"Whether the last scrape of a collector succeeded (1) or failed (0).",
		[]string{"collector"}, nil,
	)
	scrapeDurationDesc = prometheus.NewDesc(
		"misskey_scrape_collector_duration_seconds",
		"Duration of the last scrape of a collector in seconds.",
		[]string{"collector"}, nil,
	)
)

// exporter runs all collectors on every scrape. It implements
// prometheus.Collector.
type exporter struct {
	client     *client
	collectors []collector
	timeout    time.Duration
	logger     *slog.Logger
}

func newExporter(c *client, collectors []collector, timeout time.Duration, logger *slog.Logger) *exporter {
	return &exporter{client: c, collectors: collectors, timeout: timeout, logger: logger}
}

// Describe sends nothing on purpose: the metrics of the collectors are not
// known up front, which makes this an "unchecked" collector.
func (e *exporter) Describe(chan<- *prometheus.Desc) {}

// Collect runs the collectors concurrently, bounded by one shared timeout.
func (e *exporter) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, c := range e.collectors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.execute(ctx, c, ch)
		}()
	}
	wg.Wait()
}

func (e *exporter) execute(ctx context.Context, c collector, ch chan<- prometheus.Metric) {
	start := time.Now()
	err := c.Update(ctx, e.client, ch)
	duration := time.Since(start).Seconds()

	success := 1.0
	if err != nil {
		success = 0
		e.logger.Error("collector failed", "collector", c.Name(), "err", err)
	}
	ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, duration, c.Name())
	ch <- prometheus.MustNewConstMetric(scrapeSuccessDesc, prometheus.GaugeValue, success, c.Name())
}
```

- [ ] **Step 4: テストを通す**

Run: `go vet ./... && go test -race -count=1 -run TestExporter -v ./...`
Expected: 5 件すべて PASS（`TestExporterTimeout` は約 0.05 秒）。

- [ ] **Step 5: コミットする**

```bash
git add exporter.go exporter_test.go go.mod go.sum
git commit -m "Add exporter that runs collectors per scrape" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 4: queue collector と `/metrics` のエンドツーエンドテスト

**Files:**
- Create: `collector_queue.go`
- Create: `collector_queue_test.go`

**Interfaces:**
- Consumes: `collector`、`newExporter`（Task 3）、`newClient`、`testAPIKey`（Task 2）
- Produces:
  - `type queueCollector struct{}`（`collector` を実装。`Name()` は `"queue"`）
  - `var queueLengthDesc`（`misskey_queue_length`、ラベル `queue`, `state`）

- [ ] **Step 1: 失敗するテストを書く**

`collector_queue_test.go`:

```go
package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeMisskey serves body with status for POST /api/admin/queue/stats.
func fakeMisskey(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/queue/stats" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newQueueExporter(srv *httptest.Server) *exporter {
	return newExporter(
		newClient(srv.URL, testAPIKey),
		[]collector{queueCollector{}},
		time.Second,
		slog.New(slog.DiscardHandler),
	)
}

func TestQueueCollector(t *testing.T) {
	srv := fakeMisskey(t, http.StatusOK,
		`{"deliver":{"active":1,"waiting":2,"delayed":0},"inbox":{"active":0,"waiting":5.5}}`)
	exp := newQueueExporter(srv)

	expected := `
# HELP misskey_queue_length Current length of Misskey queues
# TYPE misskey_queue_length gauge
misskey_queue_length{queue="deliver",state="active"} 1
misskey_queue_length{queue="deliver",state="delayed"} 0
misskey_queue_length{queue="deliver",state="waiting"} 2
misskey_queue_length{queue="inbox",state="active"} 0
misskey_queue_length{queue="inbox",state="waiting"} 5.5
`
	if err := testutil.CollectAndCompare(exp, strings.NewReader(expected), "misskey_queue_length"); err != nil {
		t.Error(err)
	}

	successExpected := `
# HELP misskey_scrape_collector_success Whether the last scrape of a collector succeeded (1) or failed (0).
# TYPE misskey_scrape_collector_success gauge
misskey_scrape_collector_success{collector="queue"} 1
`
	if err := testutil.CollectAndCompare(exp, strings.NewReader(successExpected), "misskey_scrape_collector_success"); err != nil {
		t.Error(err)
	}
}

// Failures must expose success=0 and no stale or zeroed queue series.
func TestQueueCollectorFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"missing permission", http.StatusForbidden, `{"error":{"message":"Insufficient permissions."}}`},
		{"server error", http.StatusInternalServerError, `oops`},
		{"not json", http.StatusOK, `<html></html>`},
		{"null", http.StatusOK, `null`},
		{"unexpected shape", http.StatusOK, `{"error":{"message":"x"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := newQueueExporter(fakeMisskey(t, tt.status, tt.body))

			if n := testutil.CollectAndCount(exp, "misskey_queue_length"); n != 0 {
				t.Errorf("queue series = %d, want 0", n)
			}
			expected := `
# HELP misskey_scrape_collector_success Whether the last scrape of a collector succeeded (1) or failed (0).
# TYPE misskey_scrape_collector_success gauge
misskey_scrape_collector_success{collector="queue"} 0
`
			if err := testutil.CollectAndCompare(exp, strings.NewReader(expected), "misskey_scrape_collector_success"); err != nil {
				t.Error(err)
			}
		})
	}
}

// An empty object is a valid answer: no queues, but the scrape succeeded.
func TestQueueCollectorEmptyObject(t *testing.T) {
	exp := newQueueExporter(fakeMisskey(t, http.StatusOK, `{}`))

	if n := testutil.CollectAndCount(exp, "misskey_queue_length"); n != 0 {
		t.Errorf("queue series = %d, want 0", n)
	}
	if n := testutil.CollectAndCount(exp, "misskey_scrape_collector_success"); n != 1 {
		t.Errorf("success series = %d, want 1", n)
	}
}

// End to end: /metrics keeps answering 200 when Misskey is broken, and says so
// through misskey_scrape_collector_success.
func TestMetricsEndpointStaysUpWhenMisskeyFails(t *testing.T) {
	exp := newQueueExporter(fakeMisskey(t, http.StatusInternalServerError, `oops`))
	reg := prometheus.NewRegistry()
	reg.MustRegister(exp)

	rec := httptest.NewRecorder()
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `misskey_scrape_collector_success{collector="queue"} 0`) {
		t.Errorf("body does not report the failure:\n%s", body)
	}
	if strings.Contains(body, "misskey_queue_length{") {
		t.Errorf("body contains queue series although the scrape failed:\n%s", body)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `go test ./... -run 'Queue|MetricsEndpoint' 2>&1 | head -5`
Expected: ビルド失敗。`undefined: queueCollector`

- [ ] **Step 3: 実装する**

`collector_queue.go`:

```go
package main

import (
	"context"
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var queueLengthDesc = prometheus.NewDesc(
	"misskey_queue_length",
	"Current length of Misskey queues",
	[]string{"queue", "state"}, nil,
)

// queueCollector exposes the job queue sizes of POST /api/admin/queue/stats.
type queueCollector struct{}

func (queueCollector) Name() string { return "queue" }

func (queueCollector) Update(ctx context.Context, c *client, ch chan<- prometheus.Metric) error {
	// {"<queue>": {"<state>": <count>}}
	var stats map[string]map[string]float64
	if err := c.Post(ctx, "/api/admin/queue/stats", &stats); err != nil {
		return err
	}
	if stats == nil { // the JSON literal null
		return errors.New("queue stats response is null")
	}

	// Build every metric first so that an error exposes none of them.
	metrics := make([]prometheus.Metric, 0, len(stats))
	for queue, states := range stats {
		for state, count := range states {
			m, err := prometheus.NewConstMetric(queueLengthDesc, prometheus.GaugeValue, count, queue, state)
			if err != nil {
				return err
			}
			metrics = append(metrics, m)
		}
	}
	for _, m := range metrics {
		ch <- m
	}
	return nil
}
```

- [ ] **Step 4: テストを通す**

Run: `go vet ./... && go test -race -count=1 ./...`
Expected: すべての既存テストを含めて PASS。`TestQueueCollectorFailures` の5ケース（`missing permission`, `server error`, `not json`, `null`, `unexpected shape`）が通ること。

- [ ] **Step 5: コミットする**

```bash
git add collector_queue.go collector_queue_test.go
git commit -m "Add queue collector" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 5: main.go への組み立てと旧実装の削除

**Files:**
- Create: `main.go`
- Delete: `misskey_exporter.go`
- Modify: `Dockerfile`
- Modify: `go.mod`, `go.sum`（`go mod tidy`）

**Interfaces:**
- Consumes: `parseConfig`（Task 1）、`newClient`（Task 2）、`newExporter`（Task 3）、`queueCollector`（Task 4）
- Produces: 実行可能なバイナリ。`main()` は `os.Exit(run(os.Args[1:]))` だけを呼ぶ。

`run` の単体テストは書かない。`--help` / `--version` が kingpin によりプロセスを終了させるためで、代わりに Step 4 で実バイナリを動かして確認する。

- [ ] **Step 1: 旧実装を削除し、main.go を作る**

```bash
git rm misskey_exporter.go
```

`main.go`:

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
)

const shutdownTimeout = 10 * time.Second

func main() {
	os.Exit(run(os.Args[1:]))
}

// run returns the process exit code.
func run(args []string) int {
	cfg, err := parseConfig(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", programName, err)
		return 1
	}
	logger := promslog.New(cfg.Log)
	logger.Info("Starting "+programName, "version", version.Info(), "build_context", version.BuildContext())

	// Register new collectors here.
	collectors := []collector{
		queueCollector{},
	}
	prometheus.MustRegister(newExporter(newClient(cfg.MisskeyURL, cfg.APIKey), collectors, cfg.Timeout, logger))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- web.ListenAndServe(server, cfg.Web, logger) }()

	select {
	case err := <-serveErr:
		logger.Error("HTTP server failed", "err", err)
		return 1
	case <-ctx.Done():
	}

	logger.Info("Shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Graceful shutdown failed", "err", err)
		return 1
	}
	return 0
}
```

- [ ] **Step 2: Dockerfile を更新する**

`Dockerfile`:

```dockerfile
FROM golang:1.26 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /misskey_exporter .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /misskey_exporter /misskey_exporter

EXPOSE 9100

ENTRYPOINT ["/misskey_exporter"]
```

- [ ] **Step 3: ビルドとテストを通す**

Run: `go mod tidy && gofmt -l . && go build ./... && go vet ./... && go test -race -count=1 ./...`
Expected: `gofmt -l .` は何も出力しない。ビルド、vet、テストがすべて成功する。

- [ ] **Step 4: 実バイナリで動作を確認する**

偽の Misskey を用意する（リポジトリの外に置く）。

```bash
T=$(mktemp -d)
```

`$T/fake_misskey.py`:

```python
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

class H(BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if self.path != "/api/admin/queue/stats":
            self.send_response(404); self.end_headers(); return
        if self.headers.get("Authorization") != "Bearer test-token":
            self.send_response(401); self.end_headers(); self.wfile.write(b'{"error":"unauthorized"}'); return
        body = json.dumps({"deliver": {"active": 1, "waiting": 2, "delayed": 0}, "inbox": {"active": 0, "waiting": 5}}).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers(); self.wfile.write(body)
    def log_message(self, *a): pass

HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
```

```bash
python3 "$T/fake_misskey.py" 18080 & FAKE=$!
printf 'test-token\n' > "$T/keyfile"
go build -o "$T/me" .

"$T/me" --version; echo "exit=$?"
"$T/me"; echo "exit=$?"

# 正しいトークンをファイルで渡す（末尾に改行がある）
"$T/me" --misskey.url=http://127.0.0.1:18080/ --misskey.api-key-file="$T/keyfile" \
  --web.listen-address=127.0.0.1:19100 > "$T/ok.log" 2>&1 & ME=$!
sleep 1.5
curl -s -w "http=%{http_code}\n" http://127.0.0.1:19100/metrics | grep -E "^misskey_|http="
kill -TERM $ME; wait $ME; echo "exit=$?"

# 誤ったトークンを環境変数で渡す
MISSKEY_API_KEY=wrong "$T/me" --misskey.url=http://127.0.0.1:18080 \
  --web.listen-address=127.0.0.1:19101 > "$T/bad.log" 2>&1 & ME=$!
sleep 1.5
curl -s -w "http=%{http_code}\n" http://127.0.0.1:19101/metrics | grep -E "^misskey_|http="
kill -TERM $ME; wait $ME; echo "exit=$?"
cat "$T/bad.log"

kill $FAKE; rm -rf "$T"
```

Expected:
- `--version`: `misskey_exporter, version ...` を表示して `exit=0`
- 引数なし: `misskey_exporter: required flag(s) '--misskey.url' not provided` と `exit=1`
- 正しいトークン: `misskey_queue_length` が5行（`deliver` の `active`/`delayed`/`waiting`、`inbox` の `active`/`waiting`）、`misskey_scrape_collector_success{collector="queue"} 1`、`http=200`。SIGTERM で `exit=0`
- 誤ったトークン: `misskey_queue_length` は出ず、`misskey_scrape_collector_success{collector="queue"} 0`、`http=200`。`bad.log` に `collector failed ... unexpected status 401` があり、トークン（`wrong`）は含まれない。SIGTERM で `exit=0`

- [ ] **Step 5: Docker ビルドを確認する**

Run: `docker build -t misskey_exporter:plancheck . && docker run --rm misskey_exporter:plancheck --version; docker image rm misskey_exporter:plancheck`
Expected: ビルドが成功し、`misskey_exporter, version ...` が表示される。

- [ ] **Step 6: コミットする**

```bash
git add main.go Dockerfile go.mod go.sum
git commit -m "Wire up main and remove the old single-file exporter" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

（`git rm` により `misskey_exporter.go` の削除はステージ済み）

---

### Task 6: ドキュメントと最終確認

**Files:**
- Modify: `README.md`（全面書き直し）
- Modify: `CLAUDE.md`（全面書き直し）

**Interfaces:**
- Consumes: Task 1〜5 で確定したフラグ名、環境変数名、メトリクス名、ファイル構成
- Produces: なし

- [ ] **Step 1: README.md を書き直す**

`README.md`:

````markdown
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
````

- [ ] **Step 2: CLAUDE.md を書き直す**

`CLAUDE.md`:

````markdown
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
- `client.go`: Misskey API クライアント。自前のタイムアウトは持たず、呼び出し側の context で止める。エラーにトークンを含めない。
- `exporter.go`: `collector` interface と、全 collector を束ねる `prometheus.Collector`。
- `collector_queue.go`: `/api/admin/queue/stats` を `misskey_queue_length{queue,state}` にする。

### collector を追加する手順

1. `collector_<name>.go` に `collector` interface（`Name()` と `Update()`）を実装する。`Update` は、失敗しうる処理（API 呼び出しとデコード）をすべて終えてから最初のメトリクスを送出すること。そうすれば、失敗した取得はそのメトリクスを一切出さない（古い値や 0 を残さない）。
2. `main.go` の `collectors` に登録する。
3. `collector_queue_test.go` を手本に、`fakeMisskey` でテストを書く。

## Gotchas

- `exporter.Describe` は意図的に何も送らない（unchecked collector）。メトリクスの一覧を事前に宣言できないため。
- `dashboards/misskey-dashboard.json`（Grafana）は `misskey_queue_length{queue="..."}` に依存している。名前やラベルを変えるときはダッシュボードも直す。
- 設定のテストは `t.Setenv` で環境変数を隔離している（kingpin の `Envar` が `os.Getenv` を直接読むため）。
- コード内のコメントは英語、README は日本語。
````

- [ ] **Step 3: ドキュメントと実装が一致していることを確認する**

Run:

```bash
go build -o misskey_exporter . && ./misskey_exporter --help 2>&1 | grep -E "misskey\.(url|api-key-file|timeout)|web\.(listen-address|config\.file)|log\.(level|format)"
grep -c 'misskey_queue_length' dashboards/misskey-dashboard.json
```

Expected: README の設定表にあるフラグ（`--misskey.url`、`--misskey.api-key-file`、`--misskey.timeout`、`--web.listen-address`、`--web.config.file`、`--log.level`、`--log.format`）がすべて表示される。ダッシュボードには `misskey_queue_length` が残っている（1以上）。

- [ ] **Step 4: 最終確認（仕様書の完了条件）**

Run: `gofmt -l . && go build ./... && go vet ./... && go test -race -count=1 ./... && git status --short`
Expected: `gofmt -l .` は何も出力しない。ビルド、vet、テストが成功する。`git status` に出るのは `README.md` と `CLAUDE.md` の変更だけ（`misskey_exporter` バイナリは `.gitignore` 済み）。

仕様書 11 の残りの条件（`docker build`、`--version` / `--help`）は Task 5 で確認済み。**実際の Misskey に対する確認**（`MISSKEY_URL` と `MISSKEY_API_KEY` を渡して `go run .` し、`curl localhost:9100/metrics` で `misskey_queue_length` と `misskey_scrape_collector_success 1` を見る）は、利用者の環境で行う。この計画では実施できない。

- [ ] **Step 5: コミットする**

```bash
git add README.md CLAUDE.md
git commit -m "Rewrite README and CLAUDE.md for the new structure" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Self-Review（計画の作成者による確認）

- **仕様書との対応:** F1〜F5、N1〜N4、5.1〜5.4、6、7、8、9、10、11 に対応するタスクがある（F1/F2/5.3/5.4 は Task 3〜5、F3 は Task 3、F4 は Task 3 の interface と Task 6 の CLAUDE.md、F5/5.1 は Task 1 と Task 5、6 は Task 3〜4、7 は各タスクのテスト、8 は Task 5、9 は Task 6、11 は Task 5〜6）。
- **仕様書との差分:** 仕様書の「5.2 `http.Client` は起動時に1つ」は `newClient` 内で1つ作る形で満たしている。タイムアウトは仕様書 4.1 の「共通の timeout context」に従い、`exporter.Collect` が掛ける。
- **プレースホルダー:** なし。コードはすべて、scratch で `go vet` / `go test -race` / `gofmt` / 実バイナリ / `docker build` を通したものと同一。
- **型の一貫性:** `newClient(baseURL, apiKey string)`、`(*client).Post(ctx, path, out)`、`newExporter(c, collectors, timeout, logger)`、`collector.Update(ctx, c, ch)` は全タスクで一致している。
