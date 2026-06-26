package metastore

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
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
	initialized bool
	resolved    bool

	postingsReaders []*postings.Reader
	batchSize       int

	// refers to the actual references, not to be confused with pointer sections
	pointerRows   []postings.PointerRow
	pointerOffset int
	bloomFiltered bool

	// Stats
	bloomRowsRead uint64

	// metrics, when non-nil, receives per-object/per-flow observations at Close.
	// statsRecorded guards against double-recording if Close runs more than once.
	metrics       *ObjectMetastoreMetrics
	statsRecorded bool

	// readSpan for recording observations, it is created once during the first Read.
	readSpan *xcap.Span
}

func newPostingsIndexSectionsReader(
	logger log.Logger,
	obj *dataobj.Object,
	start, end time.Time,
	matchers []*labels.Matcher,
	predicates []*labels.Matcher,
	batchSize int,
	metrics *ObjectMetastoreMetrics,
) *postingsIndexSectionsReader {
	// Only keep equal predicates for bloom filtering
	var equalPredicates []*labels.Matcher
	for _, p := range predicates {
		if p.Type == labels.MatchEqual {
			equalPredicates = append(equalPredicates, p)
		}
	}

	if batchSize <= 0 {
		batchSize = 8192
	}

	return &postingsIndexSectionsReader{
		logger:     logger,
		obj:        obj,
		matchers:   matchers,
		predicates: equalPredicates,
		batchSize:  batchSize,
		start:      start,
		end:        end,
		metrics:    metrics,
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

	readers := make([]*postings.Reader, 0)
	for _, section := range r.obj.Sections() {
		if section.Tenant != targetTenant {
			continue
		}
		if !postings.CheckSection(section) {
			continue
		}

		sec, err := postings.Open(ctx, section)
		if err != nil {
			closeAll(readers)
			return fmt.Errorf("opening postings section: %w", err)
		}

		reader := postings.NewReader(postings.ReaderOptions{
			Columns:   sec.Columns(),
			Allocator: memory.DefaultAllocator,
		})
		if err := reader.Open(ctx); err != nil {
			closeAll(readers)
			return fmt.Errorf("opening postings reader: %w", err)
		}

		readers = append(readers, reader)
		sp.Record(StatMetastorePointerSectionsOpened.Observe(1))
	}
	r.postingsReaders = readers

	// If there are no postings sections for this tenant, Read returns io.EOF.
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
	}
	if len(r.pointerRows) == 0 {
		return nil, io.EOF
	}

	return r.readPointers(ctx)
}

// lazyResolveStreams resolves stream refs matching the configured matchers and
// collects stream label names so stream-label predicates can be dropped from
// the bloom checks.
func (r *postingsIndexSectionsReader) lazyResolveStreams(ctx context.Context) error {
	if r.resolved {
		return nil
	}

	region := xcap.RegionFromContext(ctx)
	startTime := time.Now()
	defer func() {
		region.Record(StatMetastoreStreamsReadTime.Observe(time.Since(startTime).Seconds()))
	}()

	var matchingStreamRefs map[postings.StreamRef]struct{}
	streamLabelNames := make(map[string]struct{})

	if len(r.postingsReaders) > 0 {
		pointerStart := time.Now()

		acc := postings.NewStreamScan(r.matchers, r.start, r.end)
		for _, reader := range r.postingsReaders {
			productive, err := reader.ScanLabelsInto(ctx, acc, r.batchSize)
			if err != nil {
				return fmt.Errorf("scanning postings labels: %w", err)
			}
			// Recorded at the metastore layer (rather than inside the postings
			// reader) so the postings package need not depend on metastore stats.
			if productive {
				region.Record(StatMetastorePointerSectionsProductive.Observe(1))
			}
		}
		res := acc.Finalize(ctx)

		if r.readSpan != nil {
			r.readSpan.Record(StatMetastoreSectionPointersReadTime.Observe(time.Since(pointerStart).Seconds()))
		}

		matchingStreamRefs = res.MatchingStreamRefs
		for _, name := range res.MatchingLabelColumnNames {
			streamLabelNames[name] = struct{}{}
		}
		// Pointer rows come from the same scan; stash them for readPointers.
		r.pointerRows = res.Pointers
	}

	region.Record(StatMetastoreStreamsRead.Observe(int64(len(matchingStreamRefs))))

	r.filterBloomPredicates(streamLabelNames)
	r.resolved = true
	return nil
}

// filterBloomPredicates removes predicates on stream labels
func (r *postingsIndexSectionsReader) filterBloomPredicates(streamLabelNames map[string]struct{}) {
	filtered := make([]*labels.Matcher, 0, len(r.predicates))
	for _, predicate := range r.predicates {
		if _, isStreamLabel := streamLabelNames[predicate.Name]; !isStreamLabel {
			filtered = append(filtered, predicate)
		}
	}
	r.predicates = filtered
}

func (r *postingsIndexSectionsReader) readPointers(ctx context.Context) (arrow.RecordBatch, error) {
	if err := r.lazyFilterPointersByBloom(ctx); err != nil {
		return nil, err
	}

	if r.pointerOffset >= len(r.pointerRows) {
		return nil, io.EOF
	}

	start := r.pointerOffset
	end := min(start+r.batchSize, len(r.pointerRows))
	batch := r.pointerRows[start:end]
	r.pointerOffset = end

	rec := buildPointersRecord(memory.DefaultAllocator, batch)
	r.readSpan.Record(StatMetastoreSectionPointersRead.Observe(rec.NumRows()))
	r.bloomRowsRead += uint64(rec.NumRows())
	return rec, nil
}

func (r *postingsIndexSectionsReader) lazyFilterPointersByBloom(ctx context.Context) error {
	if r.bloomFiltered {
		return nil
	}
	if len(r.predicates) == 0 {
		r.bloomFiltered = true
		return nil
	}

	matchedSectionKeys, err := r.readMatchedSectionKeys(ctx)
	if err != nil {
		return fmt.Errorf("reading matched section keys: %w", err)
	}
	r.pointerRows = filterRowsBySectionKeys(r.pointerRows, matchedSectionKeys)
	if len(r.pointerRows) == 0 {
		level.Debug(utillog.WithContext(ctx, r.logger)).Log("msg", "no sections resolved", "reason", "no matching predicates")
	}
	r.bloomFiltered = true
	return nil
}

// filterRowsBySectionKeys returns the pointer rows whose (object path, section)
// tuple is present in keys, preserving order.
func filterRowsBySectionKeys(rows []postings.PointerRow, keys map[SectionKey]struct{}) []postings.PointerRow {
	n := 0
	for _, row := range rows {
		sk := SectionKey{ObjectPath: row.ObjectPath, SectionIdx: row.SectionIndex}
		if _, ok := keys[sk]; ok {
			rows[n] = row
			n++
		}
	}
	return rows[:n]
}

// readMatchedSectionKeys reads bloom rows from the postings section and
// returns the section keys matching every bloom predicate.
func (r *postingsIndexSectionsReader) readMatchedSectionKeys(ctx context.Context) (map[SectionKey]struct{}, error) {
	predicateNames := make([]string, 0, len(r.predicates))
	for _, predicate := range r.predicates {
		predicateNames = append(predicateNames, predicate.Name)
	}

	// Each bloom row is self-contained, so reading the bloom rows from every
	// postings section and matching them together is equivalent to a single
	// section: the matched keys are simply the union across sections.
	var recs []arrow.RecordBatch
	for _, reader := range r.postingsReaders {
		rec, err := reader.ReadBloomRowsForColumns(ctx, predicateNames)
		if err != nil {
			return nil, fmt.Errorf("reading postings bloom rows: %w", err)
		}
		if rec == nil || rec.NumRows() == 0 {
			continue
		}
		recs = append(recs, rec)
	}
	defer func() {
		for _, rec := range recs {
			if rec != nil {
				rec.Release()
			}
		}
	}()
	if len(recs) == 0 {
		return map[SectionKey]struct{}{}, nil
	}

	postingsKeys, err := postings.MatchSections(ctx, recs, r.predicates)
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
	closeAll(r.postingsReaders)

	if r.initialized && r.metrics != nil && !r.statsRecorded {
		r.statsRecorded = true
		r.metrics.indexReadRowsPerObject.WithLabelValues(flowPostings).Observe(float64(r.totalReadRows()))
		r.metrics.resolvedSectionsPerObject.WithLabelValues(flowPostings).Observe(float64(r.resolvedSectionCount()))
	}

	if r.readSpan != nil {
		r.readSpan.End()
	}
}

func (r *postingsIndexSectionsReader) totalReadRows() uint64 {
	return r.bloomRowsRead
}

// resolvedSectionCount returns the number of distinct sections resolved for this
// object, i.e. the distinct (object path, section) tuples across the resolved
// pointer rows after stream matching and bloom filtering.
func (r *postingsIndexSectionsReader) resolvedSectionCount() int {
	seen := make(map[SectionKey]struct{}, len(r.pointerRows))
	for _, row := range r.pointerRows {
		seen[SectionKey{ObjectPath: row.ObjectPath, SectionIdx: row.SectionIndex}] = struct{}{}
	}
	return len(seen)
}

const (
	streamLabelNamesField        = "__streamLabelNames__"
	pointerKindStreamIndex int64 = 1
)

var postingsPointersSchema = newPointersRecordSchema()

func pointersRecordSchema() *arrow.Schema {
	return postingsPointersSchema
}

func newPointersRecordSchema() *arrow.Schema {
	mkField := func(label, typeName string, dty arrow.DataType) arrow.Field {
		name := typeName + "." + dty.Name()
		if label != "" {
			name = label + "." + name
		}
		return arrow.Field{Name: name, Type: dty, Nullable: true}
	}
	fields := []arrow.Field{
		// The path column carries Tag="path"; all others have an empty Tag.
		mkField("path", "path", arrow.BinaryTypes.String),
		mkField("", "section", arrow.PrimitiveTypes.Int64),
		mkField("", "pointer_kind", arrow.PrimitiveTypes.Int64),
		mkField("", "stream_id", arrow.PrimitiveTypes.Int64),
		mkField("", "stream_id_ref", arrow.PrimitiveTypes.Int64),
		mkField("", "min_timestamp", arrow.FixedWidthTypes.Timestamp_ns),
		mkField("", "max_timestamp", arrow.FixedWidthTypes.Timestamp_ns),
		mkField("", "row_count", arrow.PrimitiveTypes.Int64),
		mkField("", "uncompressed_size", arrow.PrimitiveTypes.Int64),
		// Internal label-names column; null here, populated by upstream label
		// resolution. Kept for schema parity with the streams+pointers path.
		{Name: streamLabelNamesField, Type: arrow.BinaryTypes.String, Nullable: true},
	}
	return arrow.NewSchema(fields, nil)
}

// buildPointersRecord builds a batch in [pointersRecordSchema] field order from
// the pointer rows resolved out of the postings section.
func buildPointersRecord(alloc memory.Allocator, rows []postings.PointerRow) arrow.RecordBatch {
	rb := array.NewRecordBuilder(alloc, pointersRecordSchema())

	for _, row := range rows {
		rb.Field(0).(*array.StringBuilder).Append(row.ObjectPath)
		rb.Field(1).(*array.Int64Builder).Append(row.SectionIndex)
		rb.Field(2).(*array.Int64Builder).Append(pointerKindStreamIndex)
		rb.Field(3).(*array.Int64Builder).Append(row.StreamID)
		rb.Field(4).(*array.Int64Builder).Append(row.StreamID)
		rb.Field(5).(*array.TimestampBuilder).Append(arrow.Timestamp(row.MinTimestamp))
		rb.Field(6).(*array.TimestampBuilder).Append(arrow.Timestamp(row.MaxTimestamp))
		rb.Field(7).(*array.Int64Builder).Append(0)
		rb.Field(8).(*array.Int64Builder).Append(0)
		// __streamLabelNames__: null here, populated by upstream label resolution.
		rb.Field(9).(*array.StringBuilder).AppendNull()
	}

	return rb.NewRecordBatch()
}
