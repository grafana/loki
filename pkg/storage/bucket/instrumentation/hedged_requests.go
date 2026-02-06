package instrumentation

import (
	"github.com/cristalhq/hedgedhttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

var hedgedRequestsMetrics = promauto.NewCounter(
	prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "objstore_bucket_transport_hedged_requests_total",
		Help:      "Total number of hedged objstore requests.",
	},
)

// PublishHedgedMetrics flushes metrics from hedged requests every 10 seconds
func PublishHedgedMetrics(s *hedgedhttp.Stats) {
	publish(s, hedgedRequestsMetrics)
}
