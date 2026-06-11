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

	postingsReader *postings.Reader
	pointersRead   bool

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

	var unopenedPostings *dataobj.Section
	for _, section := range r.obj.Sections() {
		if section.Tenant != targetTenant {
			continue
		}
		if !postings.CheckSection(section) {
			continue
		}
		if unopenedPostings != nil {
			return fmt.Errorf("multiple postings sections found for tenant %s", targetTenant)
		}
		unopenedPostings = section
	}

	if unopenedPostings == nil {
		// No postings section for this tenant; Read returns io.EOF.
		return nil
	}

	sec, err := postings.Open(ctx, unopenedPostings)
	if err != nil {
		return fmt.Errorf("opening postings section: %w", err)
	}

	reader := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	if err := reader.Open(ctx); err != nil {
		return fmt.Errorf("opening postings reader: %w", err)
	}

	r.postingsReader = reader
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
// the bloom checks.
func (r *postingsIndexSectionsReader) lazyResolveStreams(ctx context.Context) error {
	if r.resolvedStreams {
		return nil
	}

	region := xcap.RegionFromContext(ctx)
	startTime := time.Now()
	defer func() {
		region.Record(xcap.StatMetastoreStreamsReadTime.Observe(time.Since(startTime).Seconds()))
	}()

	if r.postingsReader != nil {
		streamRefs, labelNamesByRef, err := r.postingsReader.ResolveMatchingStreamRefs(ctx, r.matchers)
		if err != nil {
			return fmt.Errorf("resolving labels via postings: %w", err)
		}

		if streamRefs != nil {
			r.matchingStreamRefs = streamRefs
		}
		for _, names := range labelNamesByRef {
			for _, name := range names {
				r.streamLabelNames[name] = struct{}{}
			}
		}

		streamLabelNames, err := r.postingsReader.StreamLabelColumnNames(ctx)
		if err != nil {
			return fmt.Errorf("resolving stream label names via postings: %w", err)
		}
		for _, name := range streamLabelNames {
			r.streamLabelNames[name] = struct{}{}
		}
	}

	region.Record(xcap.StatMetastoreStreamsRead.Observe(int64(len(r.matchingStreamRefs))))

	r.filterBloomPredicates()
	r.resolvedStreams = true
	r.hasData = len(r.matchingStreamRefs) > 0
	return nil
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

// readPointers returns all pointer rows for the matching stream refs in a
// single batch; subsequent calls return io.EOF.
func (r *postingsIndexSectionsReader) readPointers(ctx context.Context) (arrow.RecordBatch, error) {
	if r.pointersRead {
		return nil, io.EOF
	}
	if r.postingsReader == nil {
		r.pointersRead = true
		return nil, io.EOF
	}

	defer func(start time.Time) {
		r.readSpan.Record(xcap.StatMetastoreSectionPointersReadTime.Observe(time.Since(start).Seconds()))
	}(time.Now())

	rec, err := r.postingsReader.ReadPointersForStreams(ctx, r.matchingStreamRefs, r.start, r.end)
	if err != nil {
		// Leave pointersRead false so a transient error surfaces on every
		// call (not just the first); the one-shot behaviour is enforced only
		// on a successful read below.
		return nil, fmt.Errorf("reading postings pointers: %w", err)
	}
	r.pointersRead = true

	if rec == nil || rec.NumRows() == 0 {
		return nil, io.EOF
	}

	r.readSpan.Record(xcap.StatMetastoreSectionPointersRead.Observe(rec.NumRows()))
	r.bloomRowsRead += uint64(rec.NumRows())
	return rec, nil
}

// readWithBloomFiltering reads the pointer rows and keeps only those whose
// (object path, section) matched every bloom predicate.
func (r *postingsIndexSectionsReader) readWithBloomFiltering(ctx context.Context) (arrow.RecordBatch, error) {
	rec, err := r.readPointers(ctx)
	if err != nil {
		// Includes io.EOF on subsequent calls or when no pointer rows matched.
		return nil, err
	}

	matchedSectionKeys, err := r.readMatchedSectionKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading matched section keys: %w", err)
	}
	if len(matchedSectionKeys) == 0 {
		level.Debug(utillog.WithContext(ctx, r.logger)).Log("msg", "no sections resolved", "reason", "no matching predicates")
		return nil, io.EOF
	}

	mask, err := buildKeepBitmask(rec, matchedSectionKeys)
	if err != nil {
		return nil, fmt.Errorf("build keep bitmask: %w", err)
	}

	filtered, err := compute.FilterRecordBatch(ctx, rec, mask, compute.DefaultFilterOptions())
	if err != nil {
		return nil, err
	}
	if filtered.NumRows() == 0 {
		return nil, io.EOF
	}
	return filtered, nil
}

// readMatchedSectionKeys reads bloom rows from the postings section and
// returns the section keys matching every bloom predicate.
func (r *postingsIndexSectionsReader) readMatchedSectionKeys(ctx context.Context) (map[SectionKey]struct{}, error) {
	rec, err := r.postingsReader.ReadBloomRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading postings bloom rows: %w", err)
	}
	if rec == nil || rec.NumRows() == 0 {
		return map[SectionKey]struct{}{}, nil
	}

	postingsKeys, err := postings.MatchSections(ctx, []arrow.RecordBatch{rec}, r.predicates)
	if err != nil {
		return nil, fmt.Errorf("matching postings bloom rows: %w", err)
	}

	matched := make(map[SectionKey]struct{}, len(postingsKeys))
	for k := range postingsKeys {
		matched[SectionKey{ObjectPath: k.ObjectPath, SectionIdx: k.SectionIndex}] = struct{}{}
	}
	return matched, nil
}

// Close releases the postings reader held by this postingsIndexSectionsReader.
func (r *postingsIndexSectionsReader) Close() {
	closeIfNotNil(r.postingsReader)

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
