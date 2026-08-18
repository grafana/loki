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

type queryStatsShutdownPushReceiver struct {
	cfg      worker.Config
	gatherer prometheus.Gatherer
	logger   log.Logger
}

func newQueryStatsShutdownPushReceiver(cfg worker.Config, gatherer prometheus.Gatherer, logger log.Logger) *queryStatsShutdownPushReceiver {
	if cfg.ShutdownQueryStatsPushGatewayURL == "" {
		return nil
	}

	return &queryStatsShutdownPushReceiver{
		cfg:      cfg,
		gatherer: gatherer,
		logger:   logger,
	}
}

func (r *queryStatsShutdownPushReceiver) Stop() error {
	metricFamily, err := gatherMetricFamily(r.gatherer, queryStatsBytesProcessedMetricName)
	if err != nil {
		if errors.Is(err, errMetricFamilyNotFound) {
			level.Debug(r.logger).Log("msg", "skipping shutdown query-stats push because metric family is not present", "metric", queryStatsBytesProcessedMetricName)
			return nil
		}

		level.Warn(r.logger).Log("msg", "failed to gather shutdown query-stats metric", "metric", queryStatsBytesProcessedMetricName, "err", err)
		return nil
	}

	jobName := r.cfg.ShutdownQueryStatsPushJobName
	pusher := push.New(r.cfg.ShutdownQueryStatsPushGatewayURL, jobName).
		Gatherer(metricFamilyGatherer{metricFamily: metricFamily}).
		Grouping("component", "querier")

	querierID := r.cfg.QuerierID
	if querierID == "" {
		hostname, hostnameErr := os.Hostname()
		if hostnameErr == nil && hostname != "" {
			querierID = hostname
		}
	}
	if querierID != "" {
		pusher = pusher.Grouping("instance", querierID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.ShutdownQueryStatsPushTimeout)
	defer cancel()

	if err := pusher.PushContext(ctx); err != nil {
		level.Warn(r.logger).Log("msg", "failed to push shutdown query-stats metric", "metric", queryStatsBytesProcessedMetricName, "url", r.cfg.ShutdownQueryStatsPushGatewayURL, "err", err)
		return nil
	}

	level.Info(r.logger).Log("msg", "pushed shutdown query-stats metric", "metric", queryStatsBytesProcessedMetricName, "url", r.cfg.ShutdownQueryStatsPushGatewayURL)
	return nil
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
