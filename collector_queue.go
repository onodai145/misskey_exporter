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
