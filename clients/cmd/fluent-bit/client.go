package main

import (
	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client"
)

// NewClient creates a new client based on the fluentbit configuration.
func NewClient(cfg *config, logger log.Logger, metrics *client.Metrics) (client.Client, error) {
	if cfg.bufferConfig.buffer {
		return NewBuffer(cfg, logger, metrics)
	}
	return client.New(metrics, cfg.clientConfig, 0, 0, false, logger)
}
