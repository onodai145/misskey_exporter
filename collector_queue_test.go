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
