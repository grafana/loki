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

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/tools/stream-generator/generator"
)

func main() {
	logger := log.NewLogfmtLogger(os.Stdout)

	cfg := generator.Config{}
	cfg.RegisterFlags(flag.CommandLine, logger)
	flag.Parse()

	if err := cfg.Validate(); err != nil {
		level.Error(logger).Log("msg", "Error validating config", "err", err)
		os.Exit(1)
	}

	logger = level.NewFilter(logger, cfg.LogLevel.Option)

	if err := util.LogConfig(&cfg); err != nil {
		level.Error(logger).Log("msg", "Error printing config", "err", err)
		os.Exit(1)
	}

	// Create a new registry for Kafka metrics
	promReg := prometheus.NewRegistry()
	reg := prometheus.WrapRegistererWithPrefix(generator.MetricsNamespace, promReg)

	// Create and start the stream metadata generator service
	gen, err := generator.New(cfg, logger, reg)
	if err != nil {
		level.Error(logger).Log("msg", "Error creating stream generator", "err", err)
		os.Exit(1)
	}

	// Start the service and wait for it to be ready
	if err := services.StartAndAwaitRunning(context.Background(), gen); err != nil {
		level.Error(logger).Log("msg", "Error starting stream generator", "err", err)
		os.Exit(1)
	}

	// Create HTTP server for metrics
	httpListenAddr := fmt.Sprintf(":%d", cfg.HTTPListenPort)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	if cfg.PushMode == generator.PushStreamMetadataOnly {
		mux.Handle("/memberlist", gen.GetMemberlist())
		mux.Handle("/ingest-limit-frontend-ring", gen.GetFrontendRing())
	}

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
	if err := services.StopAndAwaitTerminated(context.Background(), gen); err != nil {
		level.Error(logger).Log("msg", "Error stopping stream generator", "err", err)
		os.Exit(1)
	}
}
