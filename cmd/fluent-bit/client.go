package main

import (
	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/promtail/client"
)

// NewClient creates a new client based on the fluentbit configuration.
func NewClient(cfg *config, logger log.Logger) (client.Client, error) {
	if cfg.bufferConfig.buffer {
		return NewBuffer(cfg, logger)
	}
	return client.New(prometheus.DefaultRegisterer, cfg.clientConfig, logger)
}
