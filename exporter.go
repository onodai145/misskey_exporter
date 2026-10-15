package main

import (
	"context"
	"fmt"
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
	err := e.update(ctx, c, ch)
	duration := time.Since(start).Seconds()

	success := 1.0
	if err != nil {
		success = 0
		e.logger.Error("collector failed", "collector", c.Name(), "err", err)
	}
	ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, duration, c.Name())
	ch <- prometheus.MustNewConstMetric(scrapeSuccessDesc, prometheus.GaugeValue, success, c.Name())
}

// update runs the collector and turns a panic into an error: a panic in a
// goroutine started by Collect would otherwise crash the whole process.
func (e *exporter) update(ctx context.Context, c collector, ch chan<- prometheus.Metric) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return c.Update(ctx, e.client, ch)
}
