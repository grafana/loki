package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-kit/log"       //nolint:depguard
	"github.com/go-kit/log/level" //nolint:depguard
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/grafana/loki/v3/tools/ratestore-standalone/standalone"
)

func main() {
	logger := log.NewLogfmtLogger(os.Stdout)

	cfg := standalone.Config{}
	cfg.RegisterFlags(flag.CommandLine, logger)
	flag.Parse()

	logger = level.NewFilter(logger, cfg.LogLevel.Option)

	// Create a new registry for Kafka metrics
	reg := prometheus.NewRegistry()

	rs, err := standalone.NewRateStoreService(cfg, reg, logger)
	if err != nil {
		logger.Log("msg", "Error creating rate store standalone", "err", err)
		os.Exit(1)
	}

	if err := services.StartAndAwaitRunning(context.Background(), rs); err != nil {
		logger.Log("msg", "Error starting rate store standalone", "err", err)
		os.Exit(1)
	}

	// Create HTTP server for metrics
	httpListenAddr := fmt.Sprintf(":%d", cfg.HTTPListenPort)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.Handle("/memberlist", rs.GetMemberlist())
	mux.Handle("/ingester-ring", rs.GetIngesterRing())

	server := &http.Server{
		Addr:    httpListenAddr,
		Handler: mux,
	}

	// Start HTTP server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			level.Error(logger).Log("msg", "Error starting metrics server", "err", err)
			os.Exit(1)
		}
	}()

	level.Info(logger).Log("msg", "Started metrics server", "addr", httpListenAddr)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	// Gracefully shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		level.Error(logger).Log("msg", "Error shutting down metrics server", "err", err)
	}

	// Stop the service gracefully
	if err := services.StopAndAwaitTerminated(context.Background(), rs); err != nil {
		level.Error(logger).Log("msg", "Error stopping rate store standalone", "err", err)
		os.Exit(1)
	}
}
