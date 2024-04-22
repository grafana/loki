package bloomgateway

import (
	"context"
	"sort"

	"github.com/go-kit/log"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

type querierMetrics struct {
	chunksTotal    prometheus.Counter
	chunksFiltered prometheus.Counter
	seriesTotal    prometheus.Counter
	seriesFiltered prometheus.Counter
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

	result := make([]*logproto.ChunkRef, 0, len(chunkRefs))
	seriesSeen := make(map[uint64]struct{}, len(grouped))

	parted := partitionSeriesByDay(from, through, grouped)
	sp.LogKV("series", len(grouped), "chunks", len(chunkRefs), "days", len(parted))

	// We can perform requests sequentially, because most of the time the request
	// only covers a single day, and if not, it's at most two days.
	for _, s := range parted {
		blocks, err := bq.blockResolver.Resolve(ctx, tenant, s.interval, s.series)
		if err != nil {
			return nil, err
		}
		sp.LogKV(
			"day", s.day.Time.Time(),
			"from", s.interval.Start.Time(),
			"through", s.interval.End.Time(),
			"series", len(s.series),
			"blocks", len(blocks),
		)

		refs, err := bq.c.FilterChunks(ctx, tenant, s.interval, blocks, queryPlan)
		if err != nil {
			return nil, err
		}

		for i := range refs {
			seriesSeen[refs[i].Fingerprint] = struct{}{}
			for _, ref := range refs[i].Refs {
				result = append(result, &logproto.ChunkRef{
					Fingerprint: refs[i].Fingerprint,
					UserID:      tenant,
					From:        ref.From,
					Through:     ref.Through,
					Checksum:    ref.Checksum,
				})
			}
		}
	}

	postFilterChunks := len(result)
	postFilterSeries := len(seriesSeen)

	bq.metrics.chunksTotal.Add(float64(preFilterChunks))
	bq.metrics.chunksFiltered.Add(float64(preFilterChunks - postFilterChunks))
	bq.metrics.seriesTotal.Add(float64(preFilterSeries))
	bq.metrics.seriesFiltered.Add(float64(preFilterSeries - postFilterSeries))

	return result, nil
}

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

	sort.Slice(grouped, func(i, j int) bool {
		return grouped[i].Fingerprint < grouped[j].Fingerprint
	})

	return grouped
}
