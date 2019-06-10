package main

import (
	"bytes"

	"github.com/docker/docker/daemon/logger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/prometheus/common/model"
)

type loki struct {
	client client.Client
	labels model.LabelSet
	logger log.Logger
}

func New(logCtx logger.Info, logger log.Logger) (logger.Logger, error) {
	logger = log.With(logger, "container_id", logCtx.ContainerID)
	cfg, err := parseConfig(logCtx)
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg.clientConfig, nil)
	if err != nil {
		return nil, err
	}
	return &loki{
		client: c,
		labels: cfg.labels,
		logger: logger,
	}, nil
}

func (l *loki) Log(m *logger.Message) error {
	if len(bytes.Fields(m.Line)) == 0 {
		level.Info(l.logger).Log("msg", "ignoring empty line")
		return nil
	}
	return l.client.Handle(l.labels.Clone(), m.Timestamp, string(m.Line))
}
func (l *loki) Name() string {
	return driverName
}
func (l *loki) Close() error {
	l.client.Stop()
	return nil
}
