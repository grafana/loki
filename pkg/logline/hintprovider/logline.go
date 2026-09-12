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
	"github.com/grafana/loki/v3/pkg/logql/syntax"
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
	reg prometheus.Registerer,
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
		cache:                 newMetadataCache(defaultMetadataCacheEntries, reg),
	}
	p.startCacheInvalidationLoop()
	return p, nil
}

func (p *LoglineHintProvider) ProvideHints(
	ctx context.Context,
	tenant string,
	expr syntax.Expr,
	from, through model.Time,
) (*Hints, *QueryStats, error) {
	_ = tenant // reserved for future tenant-aware hinting
	stats := NewQueryStats()

	filters := SupportedQuery(expr, p.ngramLength)
	if len(filters) == 0 {
		return nil, stats, ErrUnsupported
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
			return &Hints{TimeRanges: normalizeRanges(ranges)}, stats, nil
		}
	}

	overlapping := p.store.IndexesForRange(start, end)
	if len(overlapping) == 0 {
		return &Hints{TimeRanges: normalizeRanges(ranges)}, stats, nil
	}

	shardRanges, err := p.executeQuery(ctx, filters, overlapping, stats)
	if err != nil {
		return nil, stats, err
	}

	ranges = append(ranges, aggregateShardRanges(shardRanges)...)
	return &Hints{TimeRanges: normalizeRanges(ranges)}, stats, nil
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
	meta store.Meta,
	stats *QueryStats,
) (logline.Reader, error) {
	indexID := meta.ID()

	storeReader := p.store.GetIndexReaderAt(ctx, meta)

	if cached, ok := p.cache.get(indexID); ok {
		trackedReader := newTrackingReaderAt(storeReader, stats)
		reader, err := logline.OpenReaderCached(meta.Version, trackedReader, 0, meta.SizeBytes, cached.state)
		if err == nil {
			trackedReader.SetClassifier(reader)
			return reader, nil
		}
		// Cache entry may be stale/corrupt; evict it before uncached reopen.
		p.cache.delete(indexID)
	}

	stats.ObserveMetadataCacheMiss()
	if meta.IndexHeader == nil {
		return nil, fmt.Errorf("index %s is missing required meta.index_header", indexID)
	}

	trackedReader := newTrackingReaderAt(storeReader, stats)
	reader, cachedState, err := logline.OpenReader(meta.Version, trackedReader, 0, meta.SizeBytes, *meta.IndexHeader)
	if err != nil {
		return nil, fmt.Errorf("open reader: %w", err)
	}
	trackedReader.SetClassifier(reader)

	if cachedState != nil {
		p.cache.put(indexID, cachedMetadata{
			headerInfo: *meta.IndexHeader,
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
