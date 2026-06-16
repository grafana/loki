package metastore

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// postingsIndexSectionsReader resolves section pointers from a postings
// section. It resolves stream refs matching the stream matchers and time
// range, reads the pointer rows for those refs, then applies bloom filter
// predicates to further filter the results.
type postingsIndexSectionsReader struct {
	logger log.Logger
	obj    *dataobj.Object

	// Stream matching configuration
	matchers []*labels.Matcher
	start    time.Time
	end      time.Time

	// Bloom filter predicates (filtered to remove stream labels after streams are resolved)
	predicates []*labels.Matcher

	// Reader state
	initialized        bool
	hasData            bool // Whether stream resolution found matching streams
	resolvedStreams    bool
	matchingStreamRefs map[postings.StreamRef]struct{}
	streamLabelNames   map[string]struct{}

	postingsReaders []*postings.Reader
	currentReaderIdx int
	pointersRead     bool

	// Matched section keys for bloom filtering across all readers
	matchedKeys         map[SectionKey]struct{}
	matchedKeysComputed bool

	// Stats
	bloomRowsRead uint64

	// readSpan for recording observations, it is created once during the first Read.
	readSpan *xcap.Span
}

var (
	_ ArrowRecordBatchReader = (*postingsIndexSectionsReader)(nil)
	_ bloomStatsProvider     = (*postingsIndexSectionsReader)(nil)
)

func newPostingsIndexSectionsReader(
	logger log.Logger,
	obj *dataobj.Object,
	start, end time.Time,
	matchers []*labels.Matcher,
	predicates []*labels.Matcher,
) *postingsIndexSectionsReader {
	// Only keep equal predicates for bloom filtering
	var equalPredicates []*labels.Matcher
	for _, p := range predicates {
		if p.Type == labels.MatchEqual {
			equalPredicates = append(equalPredicates, p)
		}
	}

	return &postingsIndexSectionsReader{
		logger:             logger,
		obj:                obj,
		matchers:           matchers,
		predicates:         equalPredicates,
		start:              start,
		end:                end,
		matchingStreamRefs: make(map[postings.StreamRef]struct{}),
		streamLabelNames:   make(map[string]struct{}),
	}
}

func (r *postingsIndexSectionsReader) Open(ctx context.Context) error {
	if r.initialized {
		return nil
	}

	if err := r.init(ctx); err != nil {
		return err
	}

	r.initialized = true
	return nil
}

func (r *postingsIndexSectionsReader) init(ctx context.Context) error {
	if len(r.matchers) == 0 {
		return nil
	}

	ctx, sp := xcap.StartSpan(ctx, tracer, "metastore.postingsIndexSectionsReader.Open")
	defer sp.End()

	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	var unopenedSections []*dataobj.Section
	for _, section := range r.obj.Sections() {
		if section.Tenant != targetTenant {
			continue
		}
		if !postings.CheckSection(section) {
			continue
		}
		unopenedSections = append(unopenedSections, section)
	}

	if len(unopenedSections) == 0 {
		// No postings section for this tenant; Read returns io.EOF.
		return nil
	}

	for _, unopenedPostings := range unopenedSections {
		sec, err := postings.Open(ctx, unopenedPostings)
		if err != nil {
			// Close any previously opened readers on failure
			for _, reader := range r.postingsReaders {
				_ = reader.Close()
			}
			r.postingsReaders = nil
			return fmt.Errorf("opening postings section: %w", err)
		}

		reader := postings.NewReader(postings.ReaderOptions{
			Columns:   sec.Columns(),
			Allocator: memory.DefaultAllocator,
		})
		if err := reader.Open(ctx); err != nil {
			// Close any previously opened readers on failure
			for _, r := range r.postingsReaders {
				_ = r.Close()
			}
			r.postingsReaders = nil
			_ = reader.Close()
			return fmt.Errorf("opening postings reader: %w", err)
		}

		r.postingsReaders = append(r.postingsReaders, reader)
	}

	return nil
}

func (r *postingsIndexSectionsReader) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if !r.initialized {
		return nil, errIndexSectionsReaderNotOpen
	}

	if r.readSpan == nil {
		ctx, r.readSpan = xcap.StartSpan(ctx, tracer, "metastore.postingsIndexSectionsReader.Read")
	} else {
		ctx = xcap.ContextWithSpan(ctx, r.readSpan)
	}

	if err := r.lazyResolveStreams(ctx); err != nil {
		return nil, err
	} else if !r.hasData {
		return nil, io.EOF
	}

	if len(r.predicates) == 0 {
		return r.readPointers(ctx)
	}
	return r.readWithBloomFiltering(ctx)
}

// lazyResolveStreams resolves stream refs matching the configured matchers and
// collects stream label names so stream-label predicates can be dropped from
// the bloom checks. It unions per-matcher results across readers then ANDs them.
func (r *postingsIndexSectionsReader) lazyResolveStreams(ctx context.Context) error {
	if r.resolvedStreams {
		return nil
	}

	region := xcap.RegionFromContext(ctx)
	startTime := time.Now()
	defer func() {
		region.Record(xcap.StatMetastoreStreamsReadTime.Observe(time.Since(startTime).Seconds()))
	}()

	// Collect per-matcher stream refs from all readers
	allPerMatcher := make([]map[postings.StreamRef]struct{}, len(r.matchers))
	for i := range r.matchers {
		allPerMatcher[i] = make(map[postings.StreamRef]struct{})
	}

	for _, reader := range r.postingsReaders {
		perMatcher, err := reader.ResolvePerMatcherStreams(ctx, r.matchers)
		if err != nil {
			return fmt.Errorf("resolving labels via postings: %w", err)
		}

		// Union per-matcher results
		for i, refs := range perMatcher {
			for ref := range refs {
				allPerMatcher[i][ref] = struct{}{}
			}
		}

		streamLabelNames, err := reader.StreamLabelColumnNames(ctx)
		if err != nil {
			return fmt.Errorf("resolving stream label names via postings: %w", err)
		}
		for _, name := range streamLabelNames {
			r.streamLabelNames[name] = struct{}{}
		}
	}

	// AND across matchers to find streams matching all
	r.matchingStreamRefs = intersectMetastoreStreamRefSets(allPerMatcher)

	region.Record(xcap.StatMetastoreStreamsRead.Observe(int64(len(r.matchingStreamRefs))))

	r.filterBloomPredicates()
	r.resolvedStreams = true
	r.hasData = len(r.matchingStreamRefs) > 0
	return nil
}

// intersectMetastoreStreamRefSets returns the AND of the per-matcher stream-ref sets:
// a ref survives only if present in every matcher's set.
func intersectMetastoreStreamRefSets(perMatcher []map[postings.StreamRef]struct{}) map[postings.StreamRef]struct{} {
	if len(perMatcher) == 0 {
		return map[postings.StreamRef]struct{}{}
	}
	result := make(map[postings.StreamRef]struct{}, len(perMatcher[0]))
	for ref := range perMatcher[0] {
		result[ref] = struct{}{}
	}
	for i := 1; i < len(perMatcher) && len(result) > 0; i++ {
		for ref := range result {
			if _, ok := perMatcher[i][ref]; !ok {
				delete(result, ref)
			}
		}
	}
	return result
}

// filterBloomPredicates removes predicates on stream labels: those are already
// applied by stream resolution, and checking them against structured-metadata
// blooms would cause false negatives for columns with the same name.
func (r *postingsIndexSectionsReader) filterBloomPredicates() {
	filtered := make([]*labels.Matcher, 0, len(r.predicates))
	for _, predicate := range r.predicates {
		if _, isStreamLabel := r.streamLabelNames[predicate.Name]; !isStreamLabel {
			filtered = append(filtered, predicate)
		}
	}
	r.predicates = filtered
}

// readPointers returns pointer rows for the matching stream refs by reading
// sequentially from each reader; subsequent calls return io.EOF.
func (r *postingsIndexSectionsReader) readPointers(ctx context.Context) (arrow.RecordBatch, error) {
	if r.pointersRead {
		return nil, io.EOF
	}
	if len(r.postingsReaders) == 0 {
		r.pointersRead = true
		return nil, io.EOF
	}

	defer func(start time.Time) {
		r.readSpan.Record(xcap.StatMetastoreSectionPointersReadTime.Observe(time.Since(start).Seconds()))
	}(time.Now())

	// Read from the current reader; advance to next on empty result
	for r.currentReaderIdx < len(r.postingsReaders) {
		reader := r.postingsReaders[r.currentReaderIdx]
		rec, err := reader.ReadPointersForStreams(ctx, r.matchingStreamRefs, r.start, r.end)
		if err != nil {
			return nil, fmt.Errorf("reading postings pointers: %w", err)
		}

		if rec != nil && rec.NumRows() > 0 {
			r.readSpan.Record(xcap.StatMetastoreSectionPointersRead.Observe(rec.NumRows()))
			r.bloomRowsRead += uint64(rec.NumRows())
			r.currentReaderIdx++
			return rec, nil
		}

		// This reader yielded no rows, advance to next
		r.currentReaderIdx++
	}

	r.pointersRead = true
	return nil, io.EOF
}

// readWithBloomFiltering reads the pointer rows and keeps only those whose
// (object path, section) matched every bloom predicate. It loops over readPointers
// until a non-empty filtered batch or EOF.
func (r *postingsIndexSectionsReader) readWithBloomFiltering(ctx context.Context) (arrow.RecordBatch, error) {
	matchedSectionKeys, err := r.matchedSectionKeysOnce(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading matched section keys: %w", err)
	}
	if len(matchedSectionKeys) == 0 {
		level.Debug(utillog.WithContext(ctx, r.logger)).Log("msg", "no sections resolved", "reason", "no matching predicates")
		return nil, io.EOF
	}

	// Loop until we get a non-empty filtered batch or EOF
	for {
		rec, err := r.readPointers(ctx)
		if err != nil {
			// Includes io.EOF when all readers are drained
			return nil, err
		}

		mask, err := buildKeepBitmask(rec, matchedSectionKeys)
		if err != nil {
			return nil, fmt.Errorf("build keep bitmask: %w", err)
		}

		filtered, err := compute.FilterRecordBatch(ctx, rec, mask, compute.DefaultFilterOptions())
		if err != nil {
			return nil, err
		}
		if filtered.NumRows() > 0 {
			return filtered, nil
		}
		// This batch had no matching rows, continue to next reader
	}
}

// matchedSectionKeysOnce returns the matched section keys, computing them once
// and caching the result across all readers by unioning their bloom rows.
func (r *postingsIndexSectionsReader) matchedSectionKeysOnce(ctx context.Context) (map[SectionKey]struct{}, error) {
	if r.matchedKeysComputed {
		return r.matchedKeys, nil
	}

	// Collect bloom rows from all readers
	var bloomBatches []arrow.RecordBatch
	for _, reader := range r.postingsReaders {
		rec, err := reader.ReadBloomRows(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading postings bloom rows: %w", err)
		}
		if rec != nil && rec.NumRows() > 0 {
			bloomBatches = append(bloomBatches, rec)
		}
	}

	if len(bloomBatches) == 0 {
		r.matchedKeys = make(map[SectionKey]struct{})
		r.matchedKeysComputed = true
		return r.matchedKeys, nil
	}

	postingsKeys, err := postings.MatchSections(ctx, bloomBatches, r.predicates)
	if err != nil {
		return nil, fmt.Errorf("matching postings bloom rows: %w", err)
	}

	matched := make(map[SectionKey]struct{}, len(postingsKeys))
	for k := range postingsKeys {
		matched[SectionKey{ObjectPath: k.ObjectPath, SectionIdx: k.SectionIndex}] = struct{}{}
	}

	r.matchedKeys = matched
	r.matchedKeysComputed = true
	return r.matchedKeys, nil
}

// Close releases all postings readers held by this postingsIndexSectionsReader.
func (r *postingsIndexSectionsReader) Close() {
	for _, reader := range r.postingsReaders {
		closeIfNotNil(reader)
	}

	if r.readSpan != nil {
		r.readSpan.End()
	}
}

func (r *postingsIndexSectionsReader) totalReadRows() uint64 {
	return r.bloomRowsRead
}

// closeIfNotNil closes c unless it is the zero value (e.g. a nil pointer).
func closeIfNotNil[C closable](c C) {
	var zero C
	if c == zero {
		return
	}
	_ = c.Close()
}

// buildKeepBitmask returns a boolean mask selecting the rows of rec whose
// (object path, section) tuple is present in matchedSectionKeys.
func buildKeepBitmask(rec arrow.RecordBatch, matchedSectionKeys map[SectionKey]struct{}) (arrow.Array, error) {
	maskB := array.NewBooleanBuilder(memory.DefaultAllocator)
	maskB.Reserve(int(rec.NumRows()))

	buf := make([]pointers.SectionPointer, rec.NumRows())
	num, err := pointers.FromRecordBatch(rec, buf, pointers.PopulateSectionKey)
	if err != nil {
		return nil, err
	}

	for i := range num {
		sk := SectionKey{
			ObjectPath: buf[i].Path,
			SectionIdx: buf[i].Section,
		}
		_, keep := matchedSectionKeys[sk]
		maskB.Append(keep)
	}

	return maskB.NewBooleanArray(), nil
}
