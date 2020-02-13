package logql

import (
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logql/stats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	typeMetric  = "metric"
	typeFilter  = "filter"
	typeLimited = "limited"
)

var (
	bytesPerSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "logql_querystats_bytes_processed_per_seconds",
		Help:      "Distribution of bytes processed per seconds for LogQL queries.",
		// 0 MB 40 MB 80 MB 160 MB 320 MB 640 MB 1.3 GB 2.6 GB 5.1 GB 10 GB
		Buckets: prometheus.ExponentialBuckets(20*1e6, 2, 10),
	}, []string{"status", "type", "range"})
	execLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "logql_querystats_latency_seconds",
		Help:      "Distribution of latency for LogQL queries.",
		// 0.25 0.5 1 2 4 8 16 32 64 128
		Buckets: prometheus.ExponentialBuckets(0.250, 2, 10),
	}, []string{"status", "type", "range"})
	chunkDownloadLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "logql_querystats_chunk_download_latency_seconds",
		Help:      "Distribution of chunk downloads latency for LogQL queries.",
		// 0.125 0.25 0.5 1 2 4 8 16 32 64
		Buckets: prometheus.ExponentialBuckets(0.125, 2, 10),
	}, []string{"status", "type", "range"})
	duplicatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "logql_querystats_duplicates_total",
		Help:      "Total count of duplicates found while executing LogQL queries.",
	})
	chunkDownloadedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "logql_querystats_downloaded_chunk_total",
		Help:      "Total count of chunks downloaded found while executing LogQL queries.",
	}, []string{"status", "type", "range"})
	ingesterLineTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "logql_querystats_ingester_sent_lines_total",
		Help:      "Total count of lines sent from ingesters while executing LogQL queries.",
	})
)

func RecordMetrics(status, query string, rangeType QueryRangeType, stats stats.Result) {
	queryType := queryType(query)
	rt := string(rangeType)
	bytesPerSeconds.WithLabelValues(status, queryType, rt).
		Observe(float64(stats.Summary.BytesProcessedPerSeconds))
	execLatency.WithLabelValues(status, queryType, rt).
		Observe(stats.Summary.ExecTime)
	chunkDownloadLatency.WithLabelValues(status, queryType, rt).
		Observe(stats.Store.ChunksDownloadTime)
	duplicatesTotal.Add(float64(stats.Store.TotalDuplicates))
	chunkDownloadedTotal.WithLabelValues(status, queryType, rt).
		Add(float64(stats.Store.TotalChunksDownloaded))
	ingesterLineTotal.Add(float64(stats.Ingester.TotalLinesSent))
}

func queryType(query string) string {
	expr, err := ParseExpr(query)
	if err != nil {
		level.Warn(util.Logger).Log("msg", "error parsing query type", "err", err)
		return ""
	}
	switch expr.(type) {
	case SampleExpr:
		return typeMetric
	case *matchersExpr:
		return typeLimited
	case *filterExpr:
		return typeFilter
	default:
		return ""
	}
}
