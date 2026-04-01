package main

import (
	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/clients/pkg/util"
)

// NewClient creates a new client based on the fluentbit configuration.
func NewClient(cfg *config, logger log.Logger, metrics *util.Metrics) (util.Client, error) {
	if cfg.bufferConfig.buffer {
		return NewBuffer(cfg, logger, metrics)
	}
	return util.New(metrics, cfg.clientConfig, 0, 0, false, logger)
}
