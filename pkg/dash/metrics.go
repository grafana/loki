package dash

import (
	"github.com/grafana/dskit/server"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// MetricLoader is an interface for how different metric categories used in dashboards are loaded.
// Ideally, the actual client_golang types are passed through here, usually via pkg-specific
// Metrics{} structs. This is _hopeful_, but as of now some types are private/otherwise harder to expose.
type MetricLoader interface {
	Server() *server.Metrics
	Index() *index.Metrics
}

// Not derived from a running loki instance, but created
type SimpleMetricLoader struct{}

func (s *SimpleMetricLoader) Server() *server.Metrics {
	return server.NewServerMetrics(server.Config{
		MetricsNamespace: constants.Loki,
		Registerer:       prometheus.NewRegistry(),
	})
}

func (s *SimpleMetricLoader) Index() *index.Metrics {
	return index.NewMetrics(prometheus.NewRegistry())
}
