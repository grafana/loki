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

	// Requested
	matchers   []*labels.Matcher
	predicates []*labels.Matcher
	start      time.Time
	end        time.Time
	batchSize  int

	// Reader state
	initialized     bool
	postingsReaders []*postings.Reader
	pointersCursor  *pointersCursor

	// Stats
	bloomRowsRead uint64

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
) *postingsIndexSectionsReader {
	if batchSize <= 0 {
		batchSize = 8192
	}

	return &postingsIndexSectionsReader{
		logger:     logger,
		obj:        obj,
		matchers:   matchers,
		predicates: predicates,
		batchSize:  batchSize,
		start:      start,
		end:        end,
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

	if r.pointersCursor == nil {
		pointers, err := r.findPointers(ctx, r.postingsReaders, r.matchers, r.start, r.end, r.batchSize)
		if err != nil {
			return nil, fmt.Errorf("find pointers: %w", err)
		}

		pointers, err = r.filterBlooms(ctx, r.predicates, pointers)
		if err != nil {
			return nil, fmt.Errorf("filter blooms: %w", err)
		}

		r.pointersCursor = pointers.Cursor(r.batchSize)
	}

	batch := r.pointersCursor.Next()
	if len(batch) == 0 {
		return nil, io.EOF
	}

	rec := buildPointersRecord(memory.DefaultAllocator, batch)
	r.readSpan.Record(xcap.StatMetastoreSectionPointersRead.Observe(rec.NumRows()))
	r.bloomRowsRead += uint64(rec.NumRows())
	return rec, nil
}

func (r *postingsIndexSectionsReader) findPointers(
	ctx context.Context,
	readers []*postings.Reader,
	matchers []*labels.Matcher,
	start, end time.Time,
	batchSize int,
) (resolvedPointers, error) {
	region := xcap.RegionFromContext(ctx)
	startTime := time.Now()
	defer func() {
		region.Record(xcap.StatMetastoreStreamsReadTime.Observe(time.Since(startTime).Seconds()))
	}()

	if len(readers) == 0 {
		return resolvedPointers{}, nil
	}

	streamLabelNames := make(map[string]struct{})

	pointerStart := time.Now()

	acc := postings.NewStreamScan(matchers, start, end)
	for _, reader := range readers {
		if err := reader.ScanLabelsInto(ctx, acc, batchSize); err != nil {
			return resolvedPointers{}, fmt.Errorf("scanning postings labels: %w", err)
		}
	}
	res := acc.Finalize(ctx)

	if r.readSpan != nil {
		r.readSpan.Record(xcap.StatMetastoreSectionPointersReadTime.Observe(time.Since(pointerStart).Seconds()))
	}

	for _, name := range res.MatchingLabelColumnNames {
		streamLabelNames[name] = struct{}{}
	}

	region.Record(xcap.StatMetastoreStreamsRead.Observe(int64(len(res.MatchingStreamRefs))))

	rp := resolvedPointers{
		seenLabelNames: streamLabelNames,
		pointers:       res.Pointers,
	}

	return rp, nil
}

func (r *postingsIndexSectionsReader) filterBlooms(ctx context.Context, predicates []*labels.Matcher, pointers resolvedPointers) (resolvedPointers, error) {
	var finalPredicates []*labels.Matcher
	for _, p := range predicates {
		if p.Type != labels.MatchEqual {
			continue
		}
		if _, couldBeLabel := pointers.seenLabelNames[p.Name]; couldBeLabel {
			continue
		}
		finalPredicates = append(finalPredicates, p)
	}

	if len(finalPredicates) == 0 {
		return pointers, nil
	}

	matchedSectionKeys, err := r.readMatchedSectionKeys(ctx, finalPredicates)
	if err != nil {
		return resolvedPointers{}, fmt.Errorf("reading matched section keys: %w", err)
	}

	filteredPointers := filterRowsBySectionKeys(pointers.pointers, matchedSectionKeys)
	if len(filteredPointers) == 0 {
		level.Debug(utillog.WithContext(ctx, r.logger)).Log("msg", "no sections resolved", "reason", "no matching predicates")
	}

	return resolvedPointers{
		pointers:       filteredPointers,
		seenLabelNames: pointers.seenLabelNames,
	}, nil
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
func (r *postingsIndexSectionsReader) readMatchedSectionKeys(ctx context.Context, predicates []*labels.Matcher) (map[SectionKey]struct{}, error) {
	predicateNames := make([]string, 0, len(predicates))
	for _, predicate := range predicates {
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

	postingsKeys, err := postings.MatchSections(ctx, recs, predicates)
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

	if r.readSpan != nil {
		r.readSpan.End()
	}
}

func (r *postingsIndexSectionsReader) totalReadRows() uint64 {
	return r.bloomRowsRead
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

type resolvedPointers struct {
	seenLabelNames map[string]struct{}
	pointers       []postings.PointerRow
}

func (p resolvedPointers) Cursor(batchSize int) *pointersCursor {
	return &pointersCursor{
		batchSize:   batchSize,
		lastReadIdx: -1,
		pointers:    p.pointers,
	}
}

type pointersCursor struct {
	batchSize   int
	lastReadIdx int
	pointers    []postings.PointerRow
}

func (c *pointersCursor) Next() []postings.PointerRow {
	start := c.lastReadIdx + 1
	end := min(start+c.batchSize, len(c.pointers))
	batch := c.pointers[start:end]
	c.lastReadIdx = end - 1
	return batch
}
