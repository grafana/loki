package bloomgateway

import (
	"context"
	"sort"

	"github.com/go-kit/log"
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
	c       Client
	logger  log.Logger
	metrics *querierMetrics
}

func NewQuerier(c Client, r prometheus.Registerer, logger log.Logger) *BloomQuerier {
	return &BloomQuerier{
		c:       c,
		metrics: newQuerierMetrics(r, constants.Loki, querierMetricsSubsystem),
		logger:  logger,
	}
}

func convertToShortRef(ref *logproto.ChunkRef) *logproto.ShortRef {
	return &logproto.ShortRef{From: ref.From, Through: ref.Through, Checksum: ref.Checksum}
}

func (bq *BloomQuerier) FilterChunkRefs(ctx context.Context, tenant string, from, through model.Time, chunkRefs []*logproto.ChunkRef, queryPlan plan.QueryPlan) ([]*logproto.ChunkRef, error) {
	// Shortcut that does not require any filtering
	if len(chunkRefs) == 0 || len(v1.ExtractTestableLineFilters(queryPlan.AST)) == 0 {
		return chunkRefs, nil
	}

	// The indexes of the chunks slice correspond to the indexes of the fingerprint slice.
	grouped := groupedChunksRefPool.Get(len(chunkRefs))
	defer groupedChunksRefPool.Put(grouped)
	grouped = groupChunkRefs(chunkRefs, grouped)

	preFilterChunks := len(chunkRefs)
	preFilterSeries := len(grouped)

	refs, err := bq.c.FilterChunks(ctx, tenant, from, through, grouped, queryPlan)
	if err != nil {
		return nil, err
	}

	// Flatten response from client and return
	result := make([]*logproto.ChunkRef, 0, len(chunkRefs))
	for i := range refs {
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

	postFilterChunks := len(result)
	postFilterSeries := len(refs)

	bq.metrics.chunksTotal.Add(float64(preFilterChunks))
	bq.metrics.chunksFiltered.Add(float64(preFilterChunks - postFilterChunks))
	bq.metrics.seriesTotal.Add(float64(preFilterSeries))
	bq.metrics.seriesFiltered.Add(float64(preFilterSeries - postFilterSeries))

	return result, nil
}

func groupChunkRefs(chunkRefs []*logproto.ChunkRef, grouped []*logproto.GroupedChunkRefs) []*logproto.GroupedChunkRefs {
	// Sort the chunkRefs by their stream fingerprint
	// so we can easily append them to the target slice by iterating over them.
	sort.Slice(chunkRefs, func(i, j int) bool {
		return chunkRefs[i].Fingerprint < chunkRefs[j].Fingerprint
	})

	for _, chunkRef := range chunkRefs {
		idx := len(grouped) - 1
		if idx == -1 || grouped[idx].Fingerprint < chunkRef.Fingerprint {
			grouped = append(grouped, &logproto.GroupedChunkRefs{
				Fingerprint: chunkRef.Fingerprint,
				Tenant:      chunkRef.UserID,
				Refs:        []*logproto.ShortRef{convertToShortRef(chunkRef)},
			})
			continue
		}
		grouped[idx].Refs = append(grouped[idx].Refs, convertToShortRef(chunkRef))
	}
	return grouped
}
