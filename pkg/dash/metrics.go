package dash

import (
	"github.com/grafana/dskit/server"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

var (
	gcsHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "gcs_request_duration_seconds",
		Help:      "Time spent doing GCS requests.",
	}, []string{"operation", "status_code"})

	awsHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "s3_request_duration_seconds",
		Help:      "Time spent doing AWS requests.",
	}, []string{"operation", "status_code"})
)

// MetricLoader is an interface for how different metric categories used in dashboards are loaded.
// Ideally, the actual client_golang types are passed through here, usually via pkg-specific
// Metrics{} structs. This is _hopeful_, but as of now some types are private/otherwise harder to expose.
// TODO: pass in actual metrics structs
type MetricLoader interface {
	Server() *server.Metrics
	Index() *index.Metrics
	ObjectStorage() *ObjectStorageMetrics

	// utils needed for determining which metrics to use, e.g. S3 vs GCS depending on the current live backend.
	SchemaConfig() config.SchemaConfig
}

var DummyLoader = NewSimpleMetricLoader(config.SchemaConfig{
	Configs: []config.PeriodConfig{
		{
			ObjectType: "gcs",
		},
	},
})

// Not derived from a running loki instance, but created
type SimpleMetricLoader struct {
	schemaConfig config.SchemaConfig
}

func NewSimpleMetricLoader(schemaConfig config.SchemaConfig) *SimpleMetricLoader {
	return &SimpleMetricLoader{
		schemaConfig: schemaConfig,
	}
}

func (s *SimpleMetricLoader) SchemaConfig() config.SchemaConfig {
	return s.schemaConfig
}

func (s *SimpleMetricLoader) Server() *server.Metrics {
	return server.NewServerMetrics(server.Config{
		MetricsNamespace: constants.Loki,
		Registerer:       prometheus.NewRegistry(),
	})
}

func (s *SimpleMetricLoader) Index() *index.Metrics {
	return index.NewMetrics(prometheus.NewRegistry())
}

// use the last period's type to determine which storage backend to select on;
// defaulting to gcs if there is no match or there are no period configs
func (s *SimpleMetricLoader) ObjectStorage() *ObjectStorageMetrics {
	periods := s.schemaConfig.Configs
	if len(periods) == 0 {
		return &ObjectStorageMetrics{
			Provider:        "GCS",
			Backend:         "GCS",
			RequestDuration: gcsHistogram,
		}
	}

	lastPeriod := periods[len(periods)-1]
	switch lastPeriod.ObjectType {
	case "s3", "aws":
		return &ObjectStorageMetrics{
			Provider:        "AWS",
			Backend:         "S3",
			RequestDuration: awsHistogram,
		}
	case "gcs":
		return &ObjectStorageMetrics{
			Provider:        "GCP",
			Backend:         "GCS",
			RequestDuration: gcsHistogram,
		}
	default:
		return &ObjectStorageMetrics{
			Provider:        "GCP",
			Backend:         "GCS",
			RequestDuration: gcsHistogram,
		}
	}
}

type ObjectStorageMetrics struct {
	Provider        string // e.g. "AWS"
	Backend         string // e.g. "S3"
	RequestDuration *prometheus.HistogramVec
}
