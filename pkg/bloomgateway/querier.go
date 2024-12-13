package bloomgateway

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

type querierMetrics struct {
	chunksTotal    prometheus.Counter
	chunksFiltered prometheus.Counter
	chunksSkipped  prometheus.Counter
	seriesTotal    prometheus.Counter
	seriesFiltered prometheus.Counter
	seriesSkipped  prometheus.Counter
}

func newQuerierMetrics(registerer prometheus.Registerer, namespace, subsystem string) *querierMetrics {
	return &querierMetrics{
		chunksTotal: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "chunks_total",
			Help:      "Total amount of chunks pre filtering. Does not count chunks in failed requests.",
		}),
		chunksFiltered: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "chunks_filtered_total",
			Help:      "Total amount of chunks that have been filtered out. Does not count chunks in failed requests.",
		}),
		chunksSkipped: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "chunks_skipped_total",
			Help:      "Total amount of chunks that have been skipped and returned unfiltered, because no block matched the series.",
		}),
		seriesTotal: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "series_total",
			Help:      "Total amount of series pre filtering. Does not count series in failed requests.",
		}),
		seriesFiltered: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "series_filtered_total",
			Help:      "Total amount of series that have been filtered out. Does not count series in failed requests.",
		}),
		seriesSkipped: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "series_skipped_total",
			Help:      "Total amount of series that have been skipped and returned unfiltered, because no block matched the series.",
		}),
	}
}

type QuerierConfig struct {
	BuildInterval    time.Duration
	BuildTableOffset int
}

// BloomQuerier is a store-level abstraction on top of Client
// It is used by the index gateway to filter ChunkRefs based on given line fiter expression.
type BloomQuerier struct {
	c             Client
	cfg           QuerierConfig
	logger        log.Logger
	metrics       *querierMetrics
	limits        Limits
	blockResolver BlockResolver
}

func NewQuerier(c Client, cfg QuerierConfig, limits Limits, resolver BlockResolver, r prometheus.Registerer, logger log.Logger) *BloomQuerier {
	return &BloomQuerier{
		c:             c,
		cfg:           cfg,
		logger:        logger,
		metrics:       newQuerierMetrics(r, constants.Loki, querierMetricsSubsystem),
		limits:        limits,
		blockResolver: resolver,
	}
}

func convertToShortRef(ref *logproto.ChunkRef) *logproto.ShortRef {
	return &logproto.ShortRef{From: ref.From, Through: ref.Through, Checksum: ref.Checksum}
}

func (bq *BloomQuerier) FilterChunkRefs(ctx context.Context, tenant string, from, through model.Time, series map[uint64]labels.Labels, chunkRefs []*logproto.ChunkRef, queryPlan plan.QueryPlan) ([]*logproto.ChunkRef, bool, error) {
	// Shortcut that does not require any filtering
	if !bq.limits.BloomGatewayEnabled(tenant) || len(chunkRefs) == 0 || len(v1.ExtractTestableLabelMatchers(queryPlan.AST)) == 0 {
		return chunkRefs, false, nil
	}

	logger, ctx := spanlogger.NewWithLogger(ctx, bq.logger, "bloomquerier.FilterChunkRefs")
	defer logger.Finish()

	grouped := groupedChunksRefPool.Get(len(chunkRefs))
	defer groupedChunksRefPool.Put(grouped)
	grouped = groupChunkRefs(series, chunkRefs, grouped)

	preFilterChunks := len(chunkRefs)
	preFilterSeries := len(grouped)

	// Do not attempt to filter chunks for which there are no blooms
	minAge := model.Now().Add(-1 * (config.ObjectStorageIndexRequiredPeriod*time.Duration(bq.cfg.BuildTableOffset) + 2*bq.cfg.BuildInterval))
	if through.After(minAge) {
		level.Info(logger).Log(
			"msg", "skip too recent chunks",
			"tenant", tenant,
			"from", from.Time(),
			"through", through.Time(),
			"responses", 0,
			"preFilterChunks", preFilterChunks,
			"postFilterChunks", preFilterChunks,
			"filteredChunks", 0,
			"preFilterSeries", preFilterSeries,
			"postFilterSeries", preFilterSeries,
			"filteredSeries", 0,
		)

		bq.metrics.chunksTotal.Add(float64(preFilterChunks))
		bq.metrics.chunksFiltered.Add(0)
		bq.metrics.seriesTotal.Add(float64(preFilterSeries))
		bq.metrics.seriesFiltered.Add(0)

		return chunkRefs, false, nil
	}

	var skippedGrps [][]*logproto.GroupedChunkRefs
	responses := make([][]*logproto.GroupedChunkRefs, 0, 2)
	// We can perform requests sequentially, because most of the time the request
	// only covers a single day, and if not, it's at most two days.
	for _, s := range partitionSeriesByDay(from, through, grouped) {
		day := bloomshipper.NewInterval(s.day.Time, s.day.Add(Day))
		blocks, skipped, err := bq.blockResolver.Resolve(ctx, tenant, day, s.series)
		if err != nil {
			return nil, false, err
		}

		refs, err := bq.c.FilterChunks(ctx, tenant, s.interval, blocks, queryPlan)
		if err != nil {
			return nil, false, err
		}

		skippedGrps = append(skippedGrps, skipped)
		responses = append(responses, refs, skipped)
	}

	// add chunk refs from series that were not mapped to any blocks
	skippedDeduped, err := mergeSeries(skippedGrps, nil)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to dedupe skipped series")
	}

	var chunksSkipped int
	for _, skippedSeries := range skippedDeduped {
		chunksSkipped += len(skippedSeries.Refs)
	}

	deduped, err := mergeSeries(responses, nil)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to dedupe results")
	}

	result := make([]*logproto.ChunkRef, 0, len(chunkRefs))
	for i := range deduped {
		for _, ref := range deduped[i].Refs {
			result = append(result, &logproto.ChunkRef{
				Fingerprint: deduped[i].Fingerprint,
				UserID:      tenant,
				From:        ref.From,
				Through:     ref.Through,
				Checksum:    ref.Checksum,
			})
		}
	}

	postFilterChunks := len(result)
	postFilterSeries := len(deduped)

	level.Debug(logger).Log(
		"tenant", tenant,
		"from", from.Time(),
		"through", through.Time(),
		"responses", len(responses),
		"preFilterChunks", preFilterChunks,
		"postFilterChunks", postFilterChunks,
		"skippedChunks", chunksSkipped,
		"filteredChunks", preFilterChunks-postFilterChunks,
		"preFilterSeries", preFilterSeries,
		"postFilterSeries", postFilterSeries,
		"skippedSeries", len(skippedDeduped),
		"filteredSeries", preFilterSeries-postFilterSeries,
	)

	bq.metrics.chunksTotal.Add(float64(preFilterChunks))
	bq.metrics.chunksSkipped.Add(float64(chunksSkipped))
	bq.metrics.chunksFiltered.Add(float64(preFilterChunks - postFilterChunks))
	bq.metrics.seriesTotal.Add(float64(preFilterSeries))
	bq.metrics.seriesSkipped.Add(float64(len(skippedDeduped)))
	bq.metrics.seriesFiltered.Add(float64(preFilterSeries - postFilterSeries))

	return result, true, nil
}

// groupChunkRefs takes a slice of chunk refs sorted by their fingerprint and
// groups them by fingerprint.
// The second argument `grouped` can be used to pass a buffer to avoid allocations.
// If it's nil, the returned slice will be allocated.
func groupChunkRefs(series map[uint64]labels.Labels, chunkRefs []*logproto.ChunkRef, grouped []*logproto.GroupedChunkRefs) []*logproto.GroupedChunkRefs {
	seen := make(map[uint64]int, len(grouped))
	for _, chunkRef := range chunkRefs {
		if idx, found := seen[chunkRef.Fingerprint]; found {
			grouped[idx].Refs = append(grouped[idx].Refs, convertToShortRef(chunkRef))
		} else {
			seen[chunkRef.Fingerprint] = len(grouped)
			grouped = append(grouped, &logproto.GroupedChunkRefs{
				Fingerprint: chunkRef.Fingerprint,
				Labels: &logproto.IndexSeries{
					Labels: logproto.FromLabelsToLabelAdapters(series[chunkRef.Fingerprint]),
				},
				Tenant: chunkRef.UserID,
				Refs:   []*logproto.ShortRef{convertToShortRef(chunkRef)},
			})
		}
	}

	return grouped
}
