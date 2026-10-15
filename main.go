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
