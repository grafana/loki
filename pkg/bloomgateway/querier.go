package bloomgateway

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

type querierMetrics struct {
	chunksTotal    prometheus.Counter
	chunksFiltered prometheus.Counter
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

// BloomQuerier is a store-level abstraction on top of Client
// It is used by the index gateway to filter ChunkRefs based on given line fiter expression.
type BloomQuerier struct {
	c             Client
	logger        log.Logger
	metrics       *querierMetrics
	limits        Limits
	blockResolver BlockResolver
}

func NewQuerier(c Client, limits Limits, resolver BlockResolver, r prometheus.Registerer, logger log.Logger) *BloomQuerier {
	return &BloomQuerier{
		c:             c,
		logger:        logger,
		metrics:       newQuerierMetrics(r, constants.Loki, querierMetricsSubsystem),
		limits:        limits,
		blockResolver: resolver,
	}
}

func convertToShortRef(ref *logproto.ChunkRef) *logproto.ShortRef {
	return &logproto.ShortRef{From: ref.From, Through: ref.Through, Checksum: ref.Checksum}
}

func (bq *BloomQuerier) FilterChunkRefs(ctx context.Context, tenant string, from, through model.Time, chunkRefs []*logproto.ChunkRef, queryPlan plan.QueryPlan) ([]*logproto.ChunkRef, error) {
	// Shortcut that does not require any filtering
	if !bq.limits.BloomGatewayEnabled(tenant) || len(chunkRefs) == 0 || len(v1.ExtractTestableLineFilters(queryPlan.AST)) == 0 {
		return chunkRefs, nil
	}
	sp, ctx := opentracing.StartSpanFromContext(ctx, "bloomquerier.FilterChunkRefs")
	defer sp.Finish()

	grouped := groupedChunksRefPool.Get(len(chunkRefs))
	defer groupedChunksRefPool.Put(grouped)
	grouped = groupChunkRefs(chunkRefs, grouped)

	preFilterChunks := len(chunkRefs)
	preFilterSeries := len(grouped)

	responses := make([][]*logproto.GroupedChunkRefs, 0, 4)
	// We can perform requests sequentially, because most of the time the request
	// only covers a single day, and if not, it's at most two days.
	for _, s := range partitionSeriesByDay(from, through, grouped) {
		day := bloomshipper.NewInterval(s.day.Time, s.day.Time.Add(Day))
		blocks, skipped, err := bq.blockResolver.Resolve(ctx, tenant, day, s.series)
		if err != nil {
			return nil, err
		}

		refs, err := bq.c.FilterChunks(ctx, tenant, s.interval, blocks, queryPlan)
		if err != nil {
			return nil, err
		}

		// add chunk refs from series that were not mapped to any blocks
		responses = append(responses, refs, skipped)
		bq.metrics.seriesSkipped.Add(float64(len(skipped)))
	}

	deduped, err := mergeSeries(responses, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to dedupe results")
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

	level.Debug(bq.logger).Log(
		"operation", "bloomquerier.FilterChunkRefs",
		"tenant", tenant,
		"from", from.Time(),
		"through", through.Time(),
		"responses", len(responses),
		"preFilterChunks", preFilterChunks,
		"postFilterChunks", postFilterChunks,
		"filteredChunks", preFilterChunks-postFilterChunks,
		"preFilterSeries", preFilterSeries,
		"postFilterSeries", postFilterSeries,
		"filteredSeries", preFilterSeries-postFilterSeries,
	)

	bq.metrics.chunksTotal.Add(float64(preFilterChunks))
	bq.metrics.chunksFiltered.Add(float64(preFilterChunks - postFilterChunks))
	bq.metrics.seriesTotal.Add(float64(preFilterSeries))
	bq.metrics.seriesFiltered.Add(float64(preFilterSeries - postFilterSeries))

	return result, nil
}

// groupChunkRefs takes a slice of chunk refs sorted by their fingerprint and
// groups them by fingerprint.
// The second argument `grouped` can be used to pass a buffer to avoid allocations.
// If it's nil, the returned slice will be allocated.
func groupChunkRefs(chunkRefs []*logproto.ChunkRef, grouped []*logproto.GroupedChunkRefs) []*logproto.GroupedChunkRefs {
	seen := make(map[uint64]int, len(grouped))
	for _, chunkRef := range chunkRefs {
		if idx, found := seen[chunkRef.Fingerprint]; found {
			grouped[idx].Refs = append(grouped[idx].Refs, convertToShortRef(chunkRef))
		} else {
			seen[chunkRef.Fingerprint] = len(grouped)
			grouped = append(grouped, &logproto.GroupedChunkRefs{
				Fingerprint: chunkRef.Fingerprint,
				Tenant:      chunkRef.UserID,
				Refs:        []*logproto.ShortRef{convertToShortRef(chunkRef)},
			})
		}
	}
	return grouped
}
