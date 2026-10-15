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
