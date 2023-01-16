package main

import (
	"github.com/go-kit/log"

	"github.com/grafana/loki/clients/pkg/promtail/client"
)

// NewClient creates a new client based on the fluentbit configuration.
func NewClient(cfg *config, logger log.Logger, metrics *client.Metrics, streamLagLabels []string) (client.Client, error) {
	if cfg.bufferConfig.buffer {
		return NewBuffer(cfg, logger, metrics, streamLagLabels)
	}
	return client.New(metrics, cfg.clientConfig, streamLagLabels, 0, 0, logger)
}
