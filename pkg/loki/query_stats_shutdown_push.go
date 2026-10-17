package loki

import (
	"context"
	"errors"
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	dto "github.com/prometheus/client_model/go"

	"github.com/grafana/loki/v3/pkg/querier/worker"
)

const queryStatsBytesProcessedMetricName = "loki_logql_querystats_bytes_processed_total"

var errMetricFamilyNotFound = errors.New("metric family not found")

func flushShutdownQueryStats(cfg worker.Config, gatherer prometheus.Gatherer, logger log.Logger) {
	if cfg.ShutdownQueryStatsPushGatewayURL == "" {
		return
	}

	metricFamily, err := gatherMetricFamily(gatherer, queryStatsBytesProcessedMetricName)
	if err != nil {
		if errors.Is(err, errMetricFamilyNotFound) {
			level.Debug(logger).Log("msg", "skipping shutdown query-stats push because metric family is not present", "metric", queryStatsBytesProcessedMetricName)
			return
		}

		level.Warn(logger).Log("msg", "failed to gather shutdown query-stats metric", "metric", queryStatsBytesProcessedMetricName, "err", err)
		return
	}

	jobName := cfg.ShutdownQueryStatsPushJobName
	pusher := push.New(cfg.ShutdownQueryStatsPushGatewayURL, jobName).
		Gatherer(metricFamilyGatherer{metricFamily: metricFamily}).
		Grouping("component", "querier")

	querierID := cfg.QuerierID
	if querierID == "" {
		hostname, hostnameErr := os.Hostname()
		if hostnameErr == nil && hostname != "" {
			querierID = hostname
		}
	}
	if querierID != "" {
		pusher = pusher.Grouping("instance", querierID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownQueryStatsPushTimeout)
	defer cancel()

	if err := pusher.PushContext(ctx); err != nil {
		level.Warn(logger).Log("msg", "failed to push shutdown query-stats metric", "metric", queryStatsBytesProcessedMetricName, "url", cfg.ShutdownQueryStatsPushGatewayURL, "err", err)
		return
	}

	level.Info(logger).Log("msg", "pushed shutdown query-stats metric", "metric", queryStatsBytesProcessedMetricName, "url", cfg.ShutdownQueryStatsPushGatewayURL)
}

type metricFamilyGatherer struct {
	metricFamily *dto.MetricFamily
}

func (g metricFamilyGatherer) Gather() ([]*dto.MetricFamily, error) {
	return []*dto.MetricFamily{g.metricFamily}, nil
}

func gatherMetricFamily(gatherer prometheus.Gatherer, metricName string) (*dto.MetricFamily, error) {
	families, err := gatherer.Gather()
	if err != nil {
		return nil, err
	}

	for _, family := range families {
		if family.GetName() == metricName {
			return family, nil
		}
	}

	return nil, errMetricFamilyNotFound
}
