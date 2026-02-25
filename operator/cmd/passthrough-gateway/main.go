package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/grafana/loki/operator/internal/passthroughgateway"
)

func main() {
	logger := log.NewLogger("lokistack-gateway")

	cfg := &passthroughgateway.Config{}
	f := flag.NewFlagSet("lokistack-gateway", flag.ExitOnError)
	cfg.RegisterFlags(f)

	if err := f.Parse(os.Args[1:]); err != nil {
		logger.Error(err, "failed to parse flags")
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		logger.Error(err, "invalid configuration")
		os.Exit(1)
	}

	logger.Info("starting lokistack gateway",
		"listen-addr", cfg.ListenAddr,
		"admin-addr", cfg.AdminAddr,
		"loki-distributor-endpoint", cfg.Loki.DistributorEndpoint,
		"loki-query-frontend-endpoint", cfg.Loki.QueryFrontendEndpoint,
	)

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	server, err := passthroughgateway.NewServer(cfg, logger, reg)
	if err != nil {
		logger.Error(err, "failed to create server")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := server.Run(ctx); err != nil {
		logger.Error(err, "server error")
		os.Exit(1)
	}

	logger.Info("lokistack gateway stopped")
}
