package aws

import (
	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type dynamoDBMetrics struct {
	dynamoRequestDuration  *instrument.HistogramCollector
	dynamoConsumedCapacity *prometheus.CounterVec
	dynamoThrottled        *prometheus.CounterVec
	dynamoFailures         *prometheus.CounterVec
	dynamoDroppedRequests  *prometheus.CounterVec
	dynamoQueryPagesCount  prometheus.Histogram
}

func newMetrics(r prometheus.Registerer) *dynamoDBMetrics {
	m := dynamoDBMetrics{}

	m.dynamoRequestDuration = instrument.NewHistogramCollector(promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "dynamo_request_duration_seconds",
		Help:      "Time spent doing DynamoDB requests.",

		// DynamoDB latency seems to range from a few ms to a several seconds and is
		// important.  So use 9 buckets from 1ms to just over 1 minute (65s).
		Buckets: prometheus.ExponentialBuckets(0.001, 4, 9),
	}, []string{"operation", "status_code"}))
	m.dynamoConsumedCapacity = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "dynamo_consumed_capacity_total",
		Help:      "The capacity units consumed by operation.",
	}, []string{"operation", tableNameLabel})
	m.dynamoThrottled = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "dynamo_throttled_total",
		Help:      "The total number of throttled events.",
	}, []string{"operation", tableNameLabel})
	m.dynamoFailures = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "dynamo_failures_total",
		Help:      "The total number of errors while storing chunks to the chunk store.",
	}, []string{tableNameLabel, errorReasonLabel, "operation"})
	m.dynamoDroppedRequests = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "dynamo_dropped_requests_total",
		Help:      "The total number of requests which were dropped due to errors encountered from dynamo.",
	}, []string{tableNameLabel, errorReasonLabel, "operation"})
	m.dynamoQueryPagesCount = promauto.With(r).NewHistogram(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "dynamo_query_pages_count",
		Help:      "Number of pages per query.",
		// Most queries will have one page, however this may increase with fuzzy
		// metric names.
		Buckets: prometheus.ExponentialBuckets(1, 4, 6),
	})

	return &m
}
