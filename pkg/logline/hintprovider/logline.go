package hintprovider

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

const (
	defaultHintParallel = 64
)

// LoglineHintProvider resolves query hints from logline index files in store.
type LoglineHintProvider struct {
	store       *store.Store
	ngramLength int
	maxParallel int
	// queryMultipleObserver is optional and receives one callback per
	// QueryMultiple call with termination reason and term batches processed.
	queryMultipleObserver func(reason string, termBatchesProcessed int)
	logger                log.Logger
	cache                 *metadataCache
}

func NewLoglineHintProvider(
	indexStore *store.Store,
	ngramLength, maxParallel int,
	queryMultipleObserver func(reason string, termBatchesProcessed int),
	logger log.Logger,
	cacheReg prometheus.Registerer,
) (*LoglineHintProvider, error) {
	if indexStore == nil {
		return nil, fmt.Errorf("indexStore cannot be nil")
	}
	if ngramLength < 1 || ngramLength > 8 {
		return nil, fmt.Errorf("ngramLength must be between 1 and 8, got %d", ngramLength)
	}
	if maxParallel <= 0 {
		maxParallel = defaultHintParallel
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}

	p := &LoglineHintProvider{
		store:                 indexStore,
		ngramLength:           ngramLength,
		maxParallel:           maxParallel,
		queryMultipleObserver: queryMultipleObserver,
		logger:                logger,
	}
	// Only create a metadata cache on queriers
	if cacheReg != nil {
		p.cache = newMetadataCache(defaultMetadataCacheEntries, cacheReg)
		p.startCacheInvalidationLoop()
	}
	return p, nil
}

func (p *LoglineHintProvider) QueryHints(
	ctx context.Context,
	expr syntax.Expr,
	overlapping []logproto.HintIndex,
) (*logproto.HintResponse, error) {
	filters := SupportedQuery(expr, p.ngramLength)
	stats := NewQueryStats()
	shardRanges, err := p.executeQuery(ctx, filters, overlapping, stats)
	snap := stats.Snapshot()
	if err != nil {
		return &logproto.HintResponse{Stats: &snap}, err
	}
	return &logproto.HintResponse{TimeRanges: toProtoRanges(aggregateShardRanges(shardRanges)), Stats: &snap}, nil
}

type hintPlan struct {
	filters []string
	ranges  []HintTimeRange
	indexes []logproto.HintIndex
	stats   *QueryStats
}

func (p *LoglineHintProvider) getHintPlan(
	expr syntax.Expr,
	from, through model.Time,
	tenant string,
) (hintPlan, error) {
	_ = tenant // reserved for future tenant-aware hinting
	stats := NewQueryStats()

	filters := SupportedQuery(expr, p.ngramLength)
	if len(filters) == 0 {
		return hintPlan{filters, nil, nil, stats}, ErrUnsupported
	}

	start := from.Time().UTC()
	end := through.Time().UTC()
	if end.Before(start) {
		end = start
	}

	ranges := make([]HintTimeRange, 0, 1)
	minDate := p.MinDate()
	if start.Before(minDate) {
		ranges = append(ranges, HintTimeRange{
			Start:  time.Time{},
			End:    minDate,
			Source: HintSourcePreMinDate,
		})
		// Entire query window is pre-min-date: passthrough hint already fully covers it.
		if !end.After(minDate) {
			return hintPlan{filters, normalizeRanges(ranges), nil, stats}, nil
		}
	}

	overlapping := p.store.IndexesForRange(start, end)
	if len(overlapping) == 0 {
		return hintPlan{filters, normalizeRanges(ranges), nil, stats}, nil
	}

	indexes := make([]logproto.HintIndex, len(overlapping))
	for i, m := range overlapping {
		indexes[i] = logproto.HintIndex{
			ID:             m.ID(),
			Version:        m.Version,
			SizeBytes:      m.SizeBytes,
			MinLogTs:       m.MinLogTs,
			MaxLogTs:       m.MaxLogTs,
			ShardCount:     int64(m.ShardCount),
			ShardAlgorithm: m.ShardAlgorithm,
			ShardValue:     int64(m.ShardValue),
			IndexHeader:    m.IndexHeader,
		}
	}

	return hintPlan{filters, ranges, indexes, stats}, nil
}

func (p *LoglineHintProvider) provideHintsRemote(
	ctx context.Context,
	tenant string,
	expr syntax.Expr,
	from, through model.Time,
	next queryrangebase.Handler,
) (*Hints, *QueryStats, error) {
	plan, err := p.getHintPlan(expr, from, through, tenant)
	if err != nil || len(plan.indexes) == 0 {
		return &Hints{TimeRanges: plan.ranges}, plan.stats, err
	}

	resp, err := next.Do(ctx, &logproto.HintRequest{
		From:    from,
		Through: through,
		Expr:    expr.String(),
		Indexes: plan.indexes,
	})
	if err != nil {
		return nil, plan.stats, err
	}

	hr, ok := resp.(*queryrange.HintResponse)
	if !ok || hr == nil || hr.Response == nil {
		return nil, plan.stats, fmt.Errorf("unexpected hint response type %T", resp)
	}

	ranges := append(plan.ranges, fromProtoRanges(hr.Response.TimeRanges)...)
	return &Hints{TimeRanges: normalizeRanges(ranges)}, QueryStatsFromProto(hr.Response.Stats), nil
}

func (p *LoglineHintProvider) ProvideHints(
	ctx context.Context,
	tenant string,
	expr syntax.Expr,
	from, through model.Time,
	next queryrangebase.Handler,
) (*Hints, *QueryStats, error) {
	if next != nil {
		return p.provideHintsRemote(ctx, tenant, expr, from, through, next)
	}
	plan, err := p.getHintPlan(expr, from, through, tenant)
	if err != nil || len(plan.indexes) == 0 {
		return &Hints{TimeRanges: plan.ranges}, plan.stats, err
	}

	shardRanges, err := p.executeQuery(ctx, plan.filters, plan.indexes, plan.stats)
	if err != nil {
		return nil, plan.stats, err
	}

	ranges := append(plan.ranges, aggregateShardRanges(shardRanges)...)
	return &Hints{TimeRanges: normalizeRanges(ranges)}, plan.stats, nil
}

// MinDate returns the configured minimum trusted date boundary used by the
// underlying store. A zero value means no boundary is available.
func (p *LoglineHintProvider) MinDate() time.Time {
	if p == nil || p.store == nil {
		return time.Time{}
	}
	return p.store.MinDate()
}

func (p *LoglineHintProvider) openIndexReader(
	ctx context.Context,
	idx logproto.HintIndex,
	stats *QueryStats,
) (logline.Reader, error) {
	if idx.IndexHeader == nil {
		return nil, fmt.Errorf("index %s is missing required index_header", idx.ID)
	}
	storeReader := p.store.GetIndexReaderAt(ctx, idx.IndexPath())

	if p.cache != nil {
		if cached, ok := p.cache.get(idx.ID); ok {
			trackedReader := newTrackingReaderAt(storeReader, stats)
			reader, err := logline.OpenReaderCached(idx.Version, trackedReader, 0, idx.SizeBytes, cached.state)
			if err == nil {
				trackedReader.SetClassifier(reader)
				return reader, nil
			}
			// Cache entry may be stale/corrupt; evict it before uncached reopen.
			p.cache.delete(idx.ID)
		}
		stats.ObserveMetadataCacheMiss()
	}

	trackedReader := newTrackingReaderAt(storeReader, stats)
	reader, cachedState, err := logline.OpenReader(idx.Version, trackedReader, 0, idx.SizeBytes, *idx.IndexHeader)
	if err != nil {
		return nil, fmt.Errorf("open reader: %w", err)
	}
	trackedReader.SetClassifier(reader)
	if p.cache != nil && cachedState != nil {
		p.cache.put(idx.ID, cachedMetadata{
			headerInfo: *idx.IndexHeader,
			state:      cachedState,
		})
	}
	return reader, nil
}

func (p *LoglineHintProvider) startCacheInvalidationLoop() {
	ch := p.store.PollNotify()
	if ch == nil {
		return
	}
	go func() {
		for snap := range ch {
			p.cache.evictStale(snap)
		}
	}()
}

// aggregateShardRanges combines per-shard results with the correct semantics:
//   - Within each shard value (same algorithm/count/value): UNION time ranges
//   - Across shard values in the same group (same algorithm/count): INTERSECT
//   - Across shard groups (different algorithm or count): UNION
//   - Unsharded indexes: UNION with the final result
func aggregateShardRanges(byKey map[shardKey][]HintTimeRange) []HintTimeRange {
	if len(byKey) == 0 {
		return nil
	}

	// Partition keys into unsharded and per-group buckets.
	var allRanges []HintTimeRange
	groups := make(map[shardGroup]map[int][]HintTimeRange) // group → value → ranges

	for key, ranges := range byKey {
		if !key.isSharded() {
			allRanges = append(allRanges, ranges...)
			continue
		}
		byValue, ok := groups[key.shardGroup]
		if !ok {
			byValue = make(map[int][]HintTimeRange)
			groups[key.shardGroup] = byValue
		}
		byValue[key.ShardValue] = append(byValue[key.ShardValue], ranges...)
	}

	// For each shard group: union within each value, intersect across values.
	for _, byValue := range groups {
		var groupResult []HintTimeRange
		for _, ranges := range byValue {
			normalized := normalizeRanges(ranges)
			if groupResult == nil {
				groupResult = normalized
			} else {
				groupResult = intersectRanges(groupResult, normalized)
			}
			// no future intersections can add ranges, so let's just bail now
			if len(groupResult) == 0 {
				break
			}
		}
		allRanges = append(allRanges, groupResult...)
	}

	return normalizeRanges(allRanges)
}

// normalizeRanges sorts, drops empty [start, end) windows, and merges
// overlapping or abutting ranges. Abutting ranges (a.End == b.Start) merge
// because they form a contiguous half-open cover.
func normalizeRanges(ranges []HintTimeRange) []HintTimeRange {
	if len(ranges) == 0 {
		return nil
	}

	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start.Equal(ranges[j].Start) {
			return ranges[i].End.Before(ranges[j].End)
		}
		return ranges[i].Start.Before(ranges[j].Start)
	})

	out := make([]HintTimeRange, 0, len(ranges))
	for _, current := range ranges {
		if !current.Start.Before(current.End) {
			continue // empty under [start, end)
		}
		if len(out) == 0 {
			out = append(out, current)
			continue
		}
		last := &out[len(out)-1]
		if !current.Start.After(last.End) {
			if current.End.After(last.End) {
				last.End = current.End
			}
			last.Source = mergeSources(last.Source, current.Source)
			continue
		}
		out = append(out, current)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// maxMergedSourceLen caps merged source strings. Source is diagnostic-only
// provenance; during normalizeRanges and intersectRanges across many indexes
// the string would grow quadratically without a cap. 32 KiB keeps enough
// detail for false-negative diagnosis while staying well under the multi-MiB
// sizes that caused OOM in production.
const (
	maxMergedSourceLen    = 32 * 1024
	truncatedSourceMarker = ";...(truncated)"
)

func mergeSources(left, right string) string {
	if left == "" {
		return boundSource(right)
	}
	if right == "" || left == right {
		return boundSource(left)
	}
	// Already truncated: freeze left so later merges do not allocate or grow.
	if strings.HasSuffix(left, truncatedSourceMarker) {
		return boundSource(left)
	}
	// Check before concatenating so the merge itself cannot exceed the cap.
	if len(left)+1+len(right) <= maxMergedSourceLen {
		return left + ";" + right
	}
	return appendTruncationMarker(left)
}

func boundSource(s string) string {
	if len(s) <= maxMergedSourceLen {
		return s
	}
	if strings.HasSuffix(s, truncatedSourceMarker) {
		return s[:maxMergedSourceLen]
	}
	return appendTruncationMarker(s)
}

func appendTruncationMarker(s string) string {
	keep := min(max(maxMergedSourceLen-len(truncatedSourceMarker), 0), len(s))
	return s[:keep] + truncatedSourceMarker
}

var _ QueryHintProvider = (*LoglineHintProvider)(nil)
