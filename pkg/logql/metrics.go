package logql

import (
	"context"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/dslog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	QueryTypeMetric  = "metric"
	QueryTypeFilter  = "filter"
	QueryTypeLimited = "limited"

	latencyTypeSlow = "slow"
	latencyTypeFast = "fast"

	slowQueryThresholdSecond = float64(10)
)

var (
	bytesPerSecond = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "logql_querystats_bytes_processed_per_seconds",
		Help:      "Distribution of bytes processed per second for LogQL queries.",
		// 50MB 100MB 200MB 400MB 600MB 800MB 1GB 2GB 3GB 4GB 5GB 6GB 7GB 8GB 9GB 10GB 15GB 20GB 30GB, 40GB 50GB 60GB
		Buckets: []float64{50 * 1e6, 100 * 1e6, 400 * 1e6, 600 * 1e6, 800 * 1e6, 1 * 1e9, 2 * 1e9, 3 * 1e9, 4 * 1e9, 5 * 1e9, 6 * 1e9, 7 * 1e9, 8 * 1e9, 9 * 1e9, 10 * 1e9, 15 * 1e9, 20 * 1e9, 30 * 1e9, 40 * 1e9, 50 * 1e9, 60 * 1e9},
	}, []string{"status_code", "type", "range", "latency_type"})
	execLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "logql_querystats_latency_seconds",
		Help:      "Distribution of latency for LogQL queries.",
		// 0.25 0.5 1 2 4 8 16 32 64 128
		Buckets: prometheus.ExponentialBuckets(0.250, 2, 10),
	}, []string{"status_code", "type", "range"})
	chunkDownloadLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "logql_querystats_chunk_download_latency_seconds",
		Help:      "Distribution of chunk downloads latency for LogQL queries.",
		// 0.25 0.5 1 2 4 8 16 32 64 128
		Buckets: prometheus.ExponentialBuckets(0.250, 2, 10),
	}, []string{"status_code", "type", "range"})
	duplicatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "logql_querystats_duplicates_total",
		Help:      "Total count of duplicates found while executing LogQL queries.",
	})
	chunkDownloadedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "logql_querystats_downloaded_chunk_total",
		Help:      "Total count of chunks downloaded found while executing LogQL queries.",
	}, []string{"status_code", "type", "range"})
	ingesterLineTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "logql_querystats_ingester_sent_lines_total",
		Help:      "Total count of lines sent from ingesters while executing LogQL queries.",
	})
)

func RecordMetrics(ctx context.Context, p Params, status string, stats stats.Result, result promql_parser.Value) {
	var (
		logger        = dslog.WithContext(ctx, util_log.Logger)
		rt            = string(GetRangeType(p))
		latencyType   = latencyTypeFast
		returnedLines = 0
	)
	queryType, err := QueryType(p.Query())
	if err != nil {
		level.Warn(logger).Log("msg", "error parsing query type", "err", err)
	}

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	if result != nil && result.Type() == logqlmodel.ValueTypeStreams {
		returnedLines = int(result.(logqlmodel.Streams).Lines())
	}

	// we also log queries, useful for troubleshooting slow queries.
	level.Info(logger).Log(
		"latency", latencyType, // this can be used to filter log lines.
		"query", p.Query(),
		"query_type", queryType,
		"range_type", rt,
		"length", p.End().Sub(p.Start()),
		"step", p.Step(),
		"duration", time.Duration(int64(stats.Summary.ExecTime*float64(time.Second))),
		"status", status,
		"limit", p.Limit(),
		"returned_lines", returnedLines,
		"throughput", strings.Replace(humanize.Bytes(uint64(stats.Summary.BytesProcessedPerSecond)), " ", "", 1),
		"total_bytes", strings.Replace(humanize.Bytes(uint64(stats.Summary.TotalBytesProcessed)), " ", "", 1),
	)

	bytesPerSecond.WithLabelValues(status, queryType, rt, latencyType).
		Observe(float64(stats.Summary.BytesProcessedPerSecond))
	execLatency.WithLabelValues(status, queryType, rt).
		Observe(stats.Summary.ExecTime)
	chunkDownloadLatency.WithLabelValues(status, queryType, rt).
		Observe(stats.Store.ChunksDownloadTime)
	duplicatesTotal.Add(float64(stats.Store.TotalDuplicates))
	chunkDownloadedTotal.WithLabelValues(status, queryType, rt).
		Add(float64(stats.Store.TotalChunksDownloaded))
	ingesterLineTotal.Add(float64(stats.Ingester.TotalLinesSent))
}

func QueryType(query string) (string, error) {
	expr, err := ParseExpr(query)
	if err != nil {
		return "", err
	}
	switch e := expr.(type) {
	case SampleExpr:
		return QueryTypeMetric, nil
	case LogSelectorExpr:
		if e.HasFilter() {
			return QueryTypeFilter, nil
		}
		return QueryTypeLimited, nil
	default:
		return "", nil
	}
}
