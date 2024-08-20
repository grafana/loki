package logql

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	logql_stats "github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

const (
	QueryTypeMetric  = "metric"
	QueryTypeFilter  = "filter"
	QueryTypeLimited = "limited"
	QueryTypeLabels  = "labels"
	QueryTypeSeries  = "series"
	QueryTypeStats   = "stats"
	QueryTypeShards  = "shards"
	QueryTypeVolume  = "volume"

	latencyTypeSlow = "slow"
	latencyTypeFast = "fast"

	slowQueryThresholdSecond = float64(10)
)

var (
	bytesPerSecond = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "logql_querystats_bytes_processed_per_seconds",
		Help:      "Distribution of bytes processed per second for LogQL queries.",
		// 50MB 100MB 200MB 400MB 600MB 800MB 1GB 2GB 3GB 4GB 5GB 6GB 7GB 8GB 9GB 10GB 15GB 20GB 30GB, 40GB 50GB 60GB
		Buckets: []float64{50 * 1e6, 100 * 1e6, 400 * 1e6, 600 * 1e6, 800 * 1e6, 1 * 1e9, 2 * 1e9, 3 * 1e9, 4 * 1e9, 5 * 1e9, 6 * 1e9, 7 * 1e9, 8 * 1e9, 9 * 1e9, 10 * 1e9, 15 * 1e9, 20 * 1e9, 30 * 1e9, 40 * 1e9, 50 * 1e9, 60 * 1e9},
	}, []string{"status_code", "type", "range", "latency_type", "sharded"})
	execLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "logql_querystats_latency_seconds",
		Help:      "Distribution of latency for LogQL queries.",
		// 0.25 0.5 1 2 4 8 16 32 64 128
		Buckets: prometheus.ExponentialBuckets(0.250, 2, 10),
	}, []string{"status_code", "type", "range"})
	chunkDownloadLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "logql_querystats_chunk_download_latency_seconds",
		Help:      "Distribution of chunk downloads latency for LogQL queries.",
		// 0.25 0.5 1 2 4 8 16 32 64 128
		Buckets: prometheus.ExponentialBuckets(0.250, 2, 10),
	}, []string{"status_code", "type", "range"})
	duplicatesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "logql_querystats_duplicates_total",
		Help:      "Total count of duplicates found while executing LogQL queries.",
	})
	chunkDownloadedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "logql_querystats_downloaded_chunk_total",
		Help:      "Total count of chunks downloaded found while executing LogQL queries.",
	}, []string{"status_code", "type", "range"})
	ingesterLineTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "logql_querystats_ingester_sent_lines_total",
		Help:      "Total count of lines sent from ingesters while executing LogQL queries.",
	})

	bytePerSecondMetricUsage = analytics.NewStatistics("query_metric_bytes_per_second")
	bytePerSecondLogUsage    = analytics.NewStatistics("query_log_bytes_per_second")
	linePerSecondMetricUsage = analytics.NewStatistics("query_metric_lines_per_second")
	linePerSecondLogUsage    = analytics.NewStatistics("query_log_lines_per_second")
)

func RecordRangeAndInstantQueryMetrics(
	ctx context.Context,
	log log.Logger,
	p Params,
	status string,
	stats logql_stats.Result,
	result promql_parser.Value,
) {
	var (
		logger        = fixLogger(ctx, log)
		rangeType     = GetRangeType(p)
		rt            = string(rangeType)
		latencyType   = latencyTypeFast
		returnedLines = 0
		queryTags, _  = ctx.Value(httpreq.QueryTagsHTTPHeader).(string) // it's ok to be empty.
	)

	queryType, err := QueryType(p.GetExpression())
	if err != nil {
		level.Warn(logger).Log("msg", "error parsing query type", "err", err)
	}

	// datasample queries are executed with limited roundtripper
	if queryType == QueryTypeFilter && strings.Contains(queryTags, "datasample") {
		queryType = QueryTypeLimited
	}

	resultCache := stats.Caches.Result

	if queryType == QueryTypeMetric && rangeType == InstantType {
		resultCache = stats.Caches.InstantMetricResult
	}

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	if result != nil && result.Type() == logqlmodel.ValueTypeStreams {
		returnedLines = int(result.(logqlmodel.Streams).Lines())
	}

	var (
		query       = p.QueryString()
		hashedQuery = util.HashedQuery(query)
	)

	logValues := make([]interface{}, 0, 50)

	var bloomRatio float64 // what % are filtered
	if stats.Index.TotalChunks > 0 {
		bloomRatio = float64(stats.Index.TotalChunks-stats.Index.PostFilterChunks) / float64(stats.Index.TotalChunks)
	}

	logValues = append(logValues, []interface{}{
		"latency", latencyType, // this can be used to filter log lines.
		"query", query,
		"query_hash", hashedQuery,
		"query_type", queryType,
		"range_type", rt,
		"length", p.End().Sub(p.Start()),
		"start_delta", time.Since(p.Start()),
		"end_delta", time.Since(p.End()),
		"step", p.Step(),
		"duration", logql_stats.ConvertSecondsToNanoseconds(stats.Summary.ExecTime),
		"status", status,
		"limit", p.Limit(),
		"returned_lines", returnedLines,
		"throughput", humanizeBytes(uint64(stats.Summary.BytesProcessedPerSecond)),
		"total_bytes", humanizeBytes(uint64(stats.Summary.TotalBytesProcessed)),
		"total_bytes_structured_metadata", humanizeBytes(uint64(stats.Summary.TotalStructuredMetadataBytesProcessed)),
		"lines_per_second", stats.Summary.LinesProcessedPerSecond,
		"total_lines", stats.Summary.TotalLinesProcessed,
		"post_filter_lines", stats.Summary.TotalPostFilterLines,
		"total_entries", stats.Summary.TotalEntriesReturned,
		"store_chunks_download_time", stats.ChunksDownloadTime(),
		"queue_time", logql_stats.ConvertSecondsToNanoseconds(stats.Summary.QueueTime),
		"splits", stats.Summary.Splits,
		"shards", stats.Summary.Shards,
		"query_referenced_structured_metadata", stats.QueryReferencedStructuredMetadata(),
		"pipeline_wrapper_filtered_lines", stats.PipelineWrapperFilteredLines(),
		"chunk_refs_fetch_time", stats.ChunkRefsFetchTime(),
		"cache_chunk_req", stats.Caches.Chunk.EntriesRequested,
		"cache_chunk_hit", stats.Caches.Chunk.EntriesFound,
		"cache_chunk_bytes_stored", stats.Caches.Chunk.BytesSent,
		"cache_chunk_bytes_fetched", stats.Caches.Chunk.BytesReceived,
		"cache_chunk_download_time", stats.Caches.Chunk.CacheDownloadTime(),
		"cache_index_req", stats.Caches.Index.EntriesRequested,
		"cache_index_hit", stats.Caches.Index.EntriesFound,
		"cache_index_download_time", stats.Caches.Index.CacheDownloadTime(),
		"cache_stats_results_req", stats.Caches.StatsResult.EntriesRequested,
		"cache_stats_results_hit", stats.Caches.StatsResult.EntriesFound,
		"cache_stats_results_download_time", stats.Caches.StatsResult.CacheDownloadTime(),
		"cache_volume_results_req", stats.Caches.VolumeResult.EntriesRequested,
		"cache_volume_results_hit", stats.Caches.VolumeResult.EntriesFound,
		"cache_volume_results_download_time", stats.Caches.VolumeResult.CacheDownloadTime(),
		"cache_result_req", resultCache.EntriesRequested,
		"cache_result_hit", resultCache.EntriesFound,
		"cache_result_download_time", resultCache.CacheDownloadTime(),
		"cache_result_query_length_served", resultCache.CacheQueryLengthServed(),
		// The total of chunk reference fetched from index.
		"ingester_chunk_refs", stats.Ingester.Store.GetTotalChunksRef(),
		// Total number of chunks fetched.
		"ingester_chunk_downloaded", stats.Ingester.Store.GetTotalChunksDownloaded(),
		// Total of chunks matched by the query from ingesters.
		"ingester_chunk_matches", stats.Ingester.GetTotalChunksMatched(),
		// Total ingester reached for this query.
		"ingester_requests", stats.Ingester.GetTotalReached(),
		// Total bytes processed but was already in memory (found in the headchunk). Includes structured metadata bytes.
		"ingester_chunk_head_bytes", humanizeBytes(uint64(stats.Ingester.Store.Chunk.GetHeadChunkBytes())),
		// Total bytes of compressed chunks (blocks) processed.
		"ingester_chunk_compressed_bytes", humanizeBytes(uint64(stats.Ingester.Store.Chunk.GetCompressedBytes())),
		// Total bytes decompressed and processed from chunks. Includes structured metadata bytes.
		"ingester_chunk_decompressed_bytes", humanizeBytes(uint64(stats.Ingester.Store.Chunk.GetDecompressedBytes())),
		// Total lines post filtering.
		"ingester_post_filter_lines", stats.Ingester.Store.Chunk.GetPostFilterLines(),
		// Time spent being blocked on congestion control.
		"congestion_control_latency", stats.CongestionControlLatency(),
		"index_total_chunks", stats.Index.TotalChunks,
		"index_post_bloom_filter_chunks", stats.Index.PostFilterChunks,
		"index_bloom_filter_ratio", fmt.Sprintf("%.2f", bloomRatio),
		"index_shard_resolver_duration", time.Duration(stats.Index.ShardsDuration),
	}...)

	logValues = append(logValues, tagsToKeyValues(queryTags)...)

	if httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader) == "true" {
		logValues = append(logValues, "disable_pipeline_wrappers", "true")
	} else {
		logValues = append(logValues, "disable_pipeline_wrappers", "false")
	}

	level.Info(logger).Log(
		logValues...,
	)

	sharded := strconv.FormatBool(false)
	if stats.Summary.Shards > 1 {
		sharded = strconv.FormatBool(true)
	}

	bytesPerSecond.WithLabelValues(status, queryType, rt, latencyType, sharded).
		Observe(float64(stats.Summary.BytesProcessedPerSecond))
	execLatency.WithLabelValues(status, queryType, rt).
		Observe(stats.Summary.ExecTime)
	chunkDownloadLatency.WithLabelValues(status, queryType, rt).
		Observe(stats.ChunksDownloadTime().Seconds())
	duplicatesTotal.Add(float64(stats.TotalDuplicates()))
	chunkDownloadedTotal.WithLabelValues(status, queryType, rt).
		Add(float64(stats.TotalChunksDownloaded()))
	ingesterLineTotal.Add(float64(stats.Ingester.TotalLinesSent))

	recordUsageStats(queryType, stats)
}

func humanizeBytes(val uint64) string {
	return strings.Replace(humanize.Bytes(val), " ", "", 1)
}

func RecordLabelQueryMetrics(
	ctx context.Context,
	log log.Logger,
	start, end time.Time,
	label, query, status string,
	stats logql_stats.Result,
) {
	var (
		logger      = fixLogger(ctx, log)
		latencyType = latencyTypeFast
		queryType   = QueryTypeLabels
	)

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	level.Info(logger).Log(
		"latency", latencyType,
		"query_type", queryType,
		"splits", stats.Summary.Splits,
		"start", start.Format(time.RFC3339Nano),
		"end", end.Format(time.RFC3339Nano),
		"start_delta", time.Since(start),
		"end_delta", time.Since(end),
		"length", end.Sub(start),
		"duration", time.Duration(int64(stats.Summary.ExecTime*float64(time.Second))),
		"status", status,
		"label", label,
		"query", query,
		"query_hash", util.HashedQuery(query),
		"total_entries", stats.Summary.TotalEntriesReturned,
		"cache_label_results_req", stats.Caches.LabelResult.EntriesRequested,
		"cache_label_results_hit", stats.Caches.LabelResult.EntriesFound,
		"cache_label_results_stored", stats.Caches.LabelResult.EntriesStored,
		"cache_label_results_download_time", stats.Caches.LabelResult.CacheDownloadTime(),
		"cache_label_results_query_length_served", stats.Caches.LabelResult.CacheQueryLengthServed(),
	)

	execLatency.WithLabelValues(status, queryType, "").Observe(stats.Summary.ExecTime)
}

// fixLogger forces the given logger to include a caller=metrics.go kv pair.
// The given logger might be a spanlogger instance, in which case it only logs caller=spanlogger.go:<line>.
// We use `caller=metrics.go` when querying our logs for performance issues, and some logs were missing.
func fixLogger(ctx context.Context, logger log.Logger) log.Logger {
	nl := util_log.WithContext(ctx, logger)
	if _, ok := logger.(*spanlogger.SpanLogger); ok {
		return log.With(nl, "caller", "metrics.go")
	}
	return nl
}

func PrintMatches(matches []string) string {
	// not using comma (,) as separator as matcher may already have comma (e.g: `{a="b", c="d"}`)
	return strings.Join(matches, ":")
}

func RecordSeriesQueryMetrics(ctx context.Context, log log.Logger, start, end time.Time, match []string, status string, shards []string, stats logql_stats.Result) {
	var (
		logger      = fixLogger(ctx, log)
		latencyType = latencyTypeFast
		queryType   = QueryTypeSeries
	)

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	shard := extractShard(shards)

	logValues := make([]interface{}, 0, 15)
	logValues = append(logValues,
		"latency", latencyType,
		"query_type", queryType,
		"splits", stats.Summary.Splits,
		"start", start.Format(time.RFC3339Nano),
		"end", end.Format(time.RFC3339Nano),
		"start_delta", time.Since(start),
		"end_delta", time.Since(end),
		"length", end.Sub(start),
		"duration", time.Duration(int64(stats.Summary.ExecTime*float64(time.Second))),
		"status", status,
		"match", PrintMatches(match),
		"query_hash", util.HashedQuery(PrintMatches(match)),
		"total_entries", stats.Summary.TotalEntriesReturned,
		"cache_series_results_req", stats.Caches.SeriesResult.EntriesRequested,
		"cache_series_results_hit", stats.Caches.SeriesResult.EntriesFound,
		"cache_series_results_stored", stats.Caches.SeriesResult.EntriesStored,
		"cache_series_results_download_time", stats.Caches.SeriesResult.CacheDownloadTime(),
		"cache_series_results_query_length_served", stats.Caches.SeriesResult.CacheQueryLengthServed(),
	)

	if shard != nil {
		logValues = append(logValues,
			"shard_num", shard.Shard,
			"shard_count", shard.Of,
		)
	}

	level.Info(logger).Log(logValues...)

	execLatency.WithLabelValues(status, queryType, "").Observe(stats.Summary.ExecTime)
}

func RecordStatsQueryMetrics(ctx context.Context, log log.Logger, start, end time.Time, query string, status string, stats logql_stats.Result) {
	var (
		logger      = fixLogger(ctx, log)
		latencyType = latencyTypeFast
		queryType   = QueryTypeStats
	)

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	logValues := make([]interface{}, 0, 15)
	logValues = append(logValues,
		"latency", latencyType,
		"query_type", queryType,
		"start", start.Format(time.RFC3339Nano),
		"end", end.Format(time.RFC3339Nano),
		"start_delta", time.Since(start),
		"end_delta", time.Since(end),
		"length", end.Sub(start),
		"duration", time.Duration(int64(stats.Summary.ExecTime*float64(time.Second))),
		"status", status,
		"query", query,
		"query_hash", util.HashedQuery(query),
		"total_entries", stats.Summary.TotalEntriesReturned)
	level.Info(logger).Log(logValues...)

	execLatency.WithLabelValues(status, queryType, "").Observe(stats.Summary.ExecTime)
}

func RecordShardsQueryMetrics(
	ctx context.Context,
	log log.Logger,
	start,
	end time.Time,
	query string,
	targetBytesPerShard uint64,
	status string,
	shards int,
	stats logql_stats.Result,
) {
	var (
		logger      = fixLogger(ctx, log)
		latencyType = latencyTypeFast
		queryType   = QueryTypeShards
	)

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	var bloomRatio float64 // what % are filtered
	if stats.Index.TotalChunks > 0 {
		bloomRatio = float64(stats.Index.TotalChunks-stats.Index.PostFilterChunks) / float64(stats.Index.TotalChunks)
	}
	logValues := make([]interface{}, 0, 15)
	logValues = append(logValues,
		"latency", latencyType,
		"query_type", queryType,
		"start", start.Format(time.RFC3339Nano),
		"end", end.Format(time.RFC3339Nano),
		"start_delta", time.Since(start),
		"end_delta", time.Since(end),
		"length", end.Sub(start),
		"duration", time.Duration(int64(stats.Summary.ExecTime*float64(time.Second))),
		"status", status,
		"query", query,
		"query_hash", util.HashedQuery(query),
		"target_bytes_per_shard", datasize.ByteSize(targetBytesPerShard).HumanReadable(),
		"shards", shards,
		"index_total_chunks", stats.Index.TotalChunks,
		"index_post_bloom_filter_chunks", stats.Index.PostFilterChunks,
		"index_bloom_filter_ratio", fmt.Sprintf("%.2f", bloomRatio),
	)

	level.Info(logger).Log(logValues...)

	execLatency.WithLabelValues(status, queryType, "").Observe(stats.Summary.ExecTime)
}

func RecordVolumeQueryMetrics(ctx context.Context, log log.Logger, start, end time.Time, query string, limit uint32, step time.Duration, status string, stats logql_stats.Result) {
	var (
		logger      = fixLogger(ctx, log)
		latencyType = latencyTypeFast
		queryType   = QueryTypeVolume
	)

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	rangeType := "range"
	if step == 0 {
		rangeType = "instant"
	}

	level.Info(logger).Log(
		"latency", latencyType,
		"query_type", queryType,
		"query", query,
		"query_hash", util.HashedQuery(query),
		"start", start.Format(time.RFC3339Nano),
		"end", end.Format(time.RFC3339Nano),
		"start_delta", time.Since(start),
		"end_delta", time.Since(end),
		"range_type", rangeType,
		"step", step,
		"limit", limit,
		"length", end.Sub(start),
		"duration", time.Duration(int64(stats.Summary.ExecTime*float64(time.Second))),
		"status", status,
		"splits", stats.Summary.Splits,
		"total_entries", stats.Summary.TotalEntriesReturned,
		// cache is accumulated by middleware used by the frontend only; logs from the queriers will not show cache stats
		"cache_volume_results_req", stats.Caches.VolumeResult.EntriesRequested,
		"cache_volume_results_hit", stats.Caches.VolumeResult.EntriesFound,
		"cache_volume_results_stored", stats.Caches.VolumeResult.EntriesStored,
		"cache_volume_results_download_time", stats.Caches.VolumeResult.CacheDownloadTime(),
		"cache_volume_results_query_length_served", stats.Caches.VolumeResult.CacheQueryLengthServed(),
	)

	execLatency.WithLabelValues(status, queryType, "").Observe(stats.Summary.ExecTime)
}

func RecordDetectedFieldsQueryMetrics(ctx context.Context, log log.Logger, start, end time.Time, query string, status string, stats logql_stats.Result) {
	var (
		logger      = fixLogger(ctx, log)
		latencyType = latencyTypeFast
		queryType   = QueryTypeVolume
	)

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	level.Info(logger).Log(
		"latency", latencyType,
		"query_type", queryType,
		"query", query,
		"query_hash", util.HashedQuery(query),
		"start", start.Format(time.RFC3339Nano),
		"end", end.Format(time.RFC3339Nano),
		"start_delta", time.Since(start),
		"end_delta", time.Since(end),
		"length", end.Sub(start),
		"status", status,
		// "duration", time.Duration(int64(stats.Summary.ExecTime*float64(time.Second))),
	)
	//TODO(twhitney): add stats and exec time
	// execLatency.WithLabelValues(status, queryType, "").Observe(stats.Summary.ExecTime)
}

func recordUsageStats(queryType string, stats logql_stats.Result) {
	if queryType == QueryTypeMetric {
		bytePerSecondMetricUsage.Record(float64(stats.Summary.BytesProcessedPerSecond))
		linePerSecondMetricUsage.Record(float64(stats.Summary.LinesProcessedPerSecond))
	} else {
		bytePerSecondLogUsage.Record(float64(stats.Summary.BytesProcessedPerSecond))
		linePerSecondLogUsage.Record(float64(stats.Summary.LinesProcessedPerSecond))
	}
}

func QueryType(expr syntax.Expr) (string, error) {
	switch e := expr.(type) {
	case syntax.SampleExpr:
		return QueryTypeMetric, nil
	case syntax.LogSelectorExpr:
		if e.HasFilter() {
			return QueryTypeFilter, nil
		}
		return QueryTypeLimited, nil
	default:
		return "", nil
	}
}

// tagsToKeyValues converts QueryTags to form that is easy to log.
// e.g: `Source=foo,Feature=beta` -> []interface{}{"source", "foo", "feature", "beta"}
// so that we could log nicely!
// If queryTags is not in canonical form then its completely ignored (e.g: `key1=value1,key2=value`)
func tagsToKeyValues(queryTags string) []interface{} {
	toks := strings.FieldsFunc(queryTags, func(r rune) bool {
		return r == ','
	})

	vals := make([]string, 0)

	for _, tok := range toks {
		val := strings.FieldsFunc(tok, func(r rune) bool {
			return r == '='
		})

		if len(val) != 2 {
			continue
		}
		vals = append(vals, strings.ToLower(val[0]), val[1])
	}

	res := make([]interface{}, 0, len(vals))

	for _, val := range vals {
		res = append(res, val)
	}

	return res
}

func extractShard(shards []string) *astmapper.ShardAnnotation {
	if len(shards) == 0 {
		return nil
	}

	var shard astmapper.ShardAnnotation
	shard, err := astmapper.ParseShard(shards[0])
	if err != nil {
		return nil
	}

	return &shard
}

func RecordDetectedLabelsQueryMetrics(ctx context.Context, log log.Logger, start time.Time, end time.Time, query string, status string, stats logql_stats.Result) {
	var (
		logger      = fixLogger(ctx, log)
		latencyType = latencyTypeFast
		queryType   = QueryTypeVolume
	)

	// Tag throughput metric by latency type based on a threshold.
	// Latency below the threshold is fast, above is slow.
	if stats.Summary.ExecTime > slowQueryThresholdSecond {
		latencyType = latencyTypeSlow
	}

	rangeType := "range"

	level.Info(logger).Log(
		"api", "detected_labels",
		"latency", latencyType,
		"query_type", queryType,
		"query", query,
		"query_hash", util.HashedQuery(query),
		"start", start.Format(time.RFC3339Nano),
		"end", end.Format(time.RFC3339Nano),
		"start_delta", time.Since(start),
		"end_delta", time.Since(end),
		"range_type", rangeType,
		"length", end.Sub(start),
		"duration", time.Duration(int64(stats.Summary.ExecTime*float64(time.Second))),
		"status", status,
		"splits", stats.Summary.Splits,
		"total_entries", stats.Summary.TotalEntriesReturned,
		// cache is accumulated by middleware used by the frontend only; logs from the queriers will not show cache stats
		//"cache_volume_results_req", stats.Caches.VolumeResult.EntriesRequested,
		//"cache_volume_results_hit", stats.Caches.VolumeResult.EntriesFound,
		//"cache_volume_results_stored", stats.Caches.VolumeResult.EntriesStored,
		//"cache_volume_results_download_time", stats.Caches.VolumeResult.CacheDownloadTime(),
		//"cache_volume_results_query_length_served", stats.Caches.VolumeResult.CacheQueryLengthServed(),
	)

	execLatency.WithLabelValues(status, queryType, "").Observe(stats.Summary.ExecTime)
}
