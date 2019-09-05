package main

import (
	"bytes"

	"github.com/docker/docker/daemon/logger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

var jobName = "docker"

type loki struct {
	client  client.Client
	handler api.EntryHandler
	labels  model.LabelSet
	logger  log.Logger
}

// New create a new Loki logger that forward logs to Loki instance
func New(logCtx logger.Info, logger log.Logger) (logger.Logger, error) {
	logger = log.With(logger, "container_id", logCtx.ContainerID)
	cfg, err := parseConfig(logCtx)
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg.clientConfig, logger)
	if err != nil {
		return nil, err
	}
	var handler api.EntryHandler = c
	if len(cfg.pipeline.PipelineStages) != 0 {
		pipeline, err := stages.NewPipeline(logger, cfg.pipeline.PipelineStages, &jobName, prometheus.DefaultRegisterer)
		if err != nil {
			return nil, err
		}
		handler = pipeline.Wrap(c)
	}
	return &loki{
		client:  c,
		labels:  cfg.labels,
		logger:  logger,
		handler: handler,
	}, nil
}

// Log implements `logger.Logger`
func (l *loki) Log(m *logger.Message) error {
	if len(bytes.Fields(m.Line)) == 0 {
		level.Info(l.logger).Log("msg", "ignoring empty line", "line", string(m.Line))
		return nil
	}
	lbs := l.labels.Clone()
	if m.Source != "" {
		lbs["source"] = model.LabelValue(m.Source)
	}
	return l.handler.Handle(lbs, m.Timestamp, string(m.Line))
}

// Log implements `logger.Logger`
func (l *loki) Name() string {
	return driverName
}

// Log implements `logger.Logger`
func (l *loki) Close() error {
	l.client.Stop()
	return nil
}
