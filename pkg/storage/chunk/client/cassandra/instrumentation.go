package cassandra

import (
	"context"
	"strings"

	"github.com/gocql/gocql"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

var requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: constants.Loki,
	Name:      "cassandra_request_duration_seconds",
	Help:      "Time spent doing Cassandra requests.",
	Buckets:   prometheus.ExponentialBuckets(0.001, 4, 9),
}, []string{"operation", "status_code"})

func init() {
	prometheus.MustRegister(requestDuration)
}

type observer struct{}

func err(err error) string {
	if err != nil {
		return "500"
	}
	return "200"
}

func (observer) ObserveBatch(_ context.Context, b gocql.ObservedBatch) {
	requestDuration.WithLabelValues("BATCH", err(b.Err)).Observe(b.End.Sub(b.Start).Seconds())
}

func (observer) ObserveQuery(_ context.Context, q gocql.ObservedQuery) {
	parts := strings.SplitN(q.Statement, " ", 2)
	requestDuration.WithLabelValues(parts[0], err(q.Err)).Observe(q.End.Sub(q.Start).Seconds())
}
