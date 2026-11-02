package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	lokimem "github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var (
	_ ArrowRecordBatchReader = (*postingsIndexSectionsReader)(nil)
	_ statsProvider          = (*postingsIndexSectionsReader)(nil)
)

// maxConcurrentSectionOpens caps how many postings sections are opened
// concurrently in Open. Hardcoded for now.
const maxConcurrentSectionOpens = 64

// internalLabelsFieldName is the name of the postingsResultSchema field that
// holds the ambiguous names of the row's section, joined by commas. Every row of
// one section carries the same names.
const internalLabelsFieldName = "__streamLabelNames__"

// Column positions in postingsResultSchema, in the order of its fields.
const (
	resultColPath = iota
	resultColSection
	resultColStreamID
	resultColStreamIDRef
	resultColMinTimestamp
	resultColMaxTimestamp
	resultColRowCount
	resultColUncompressedSize
	resultColAmbiguousNames
)

// postingsResultSchema is the Arrow schema of the batches that
// postingsIndexSectionsReader emits. CollectSections decodes them with
// resolvedRowsFromRecordBatch.
var postingsResultSchema = arrow.NewSchema([]arrow.Field{
	{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
	{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "stream_id.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "stream_id_ref.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "min_timestamp.timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	{Name: "max_timestamp.timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	{Name: "row_count.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "uncompressed_size.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: internalLabelsFieldName, Type: arrow.BinaryTypes.String, Nullable: true},
}, nil)

// resolvedRow is one row of postingsResultSchema: a single stream within a
// resolved section, expanded from that section's stream-ID bitmap.
type resolvedRow struct {
	objectPath   string
	sectionIndex int64

	// streamID is the stream ID within the data object. It maps to the
	// stream_id_ref column.
	streamID int64

	// minTimestamp and maxTimestamp are Unix nanoseconds.
	minTimestamp int64
	maxTimestamp int64

	// rowCount and uncompressedSize stay zero because the resolver reports
	// neither. Zero means "not reported".
	rowCount         int64
	uncompressedSize int64

	ambiguousNames []string
}

// postingsIndexSectionsReader resolves matching streams from an index object's
// postings sections via a streamSelector, expands each result's
// stream-ID bitmap into one resolvedRow per stream, and serializes those
// rows in batchSize chunks. Section lifetime is bound to the underlying
// *dataobj.Object, so the reader does not close sections itself.
type postingsIndexSectionsReader struct {
	logger log.Logger
	obj    *dataobj.Object

	matchers   []*labels.Matcher
	predicates []*labels.Matcher
	start, end time.Time
	batchSize  int
	hashRange  *streams.HashRange

	initialized bool
	resolved    bool
	selector    *streamSelector

	rows   []resolvedRow
	offset int

	resolvedRefs uint64

	readSpan *xcap.Span
}

func newPostingsIndexSectionsReader(
	logger log.Logger,
	obj *dataobj.Object,
	start, end time.Time,
	matchers []*labels.Matcher,
	predicates []*labels.Matcher,
	batchSize int,
	hashRange *streams.HashRange,
) *postingsIndexSectionsReader {
	if batchSize <= 0 {
		batchSize = 8192
	}
	return &postingsIndexSectionsReader{
		logger:     logger,
		obj:        obj,
		matchers:   matchers,
		predicates: predicates,
		start:      start,
		end:        end,
		batchSize:  batchSize,
		hashRange:  hashRange,
	}
}

func (r *postingsIndexSectionsReader) Open(ctx context.Context) error {
	if r.initialized {
		return nil
	}
	if len(r.matchers) == 0 {
		r.initialized = true
		r.resolved = true
		return nil
	}

	ctx, sp := xcap.StartSpan(ctx, tracer, "metastore.postingsIndexSectionsReader.Open")
	defer sp.End()

	tenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	var matching []*dataobj.Section
	for _, section := range r.obj.Sections() {
		if section.Tenant != tenant || !postings.CheckSection(section) {
			continue
		}
		matching = append(matching, section)
	}

	opened := make([]*postings.Section, len(matching))
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentSectionOpens)

	// Read the layout in parallel with the postings sections, so the two reads overlap. An index
	// with no postings section of the tenant has nothing to prune.
	var bucketRange *postings.ShardBucketRange
	if len(matching) > 0 {
		g.Go(func() error {
			var err error
			bucketRange, err = r.shardBucketRange(groupCtx, tenant)
			return err
		})
	}

	for i, section := range matching {
		g.Go(func() error {
			sec, err := postings.Open(groupCtx, section)
			if err != nil {
				return fmt.Errorf("opening postings section: %w", err)
			}
			opened[i] = sec
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Open readers of the sections, configured with the input predicates
	selector := newStreamSelector(r.matchers, r.predicates, r.start, r.end, bucketRange)
	if err := selector.open(ctx, opened, maxConcurrentSectionOpens); err != nil {
		return fmt.Errorf("opening postings readers: %w", err)
	}
	r.selector = selector

	r.initialized = true
	return nil
}

// shardBucketRange maps the hash range of the request to the shard buckets of tenant in this index
// object. It returns nil if the request has no hash range, or if the range covers every bucket of
// the layout, because such a range prunes nothing.
func (r *postingsIndexSectionsReader) shardBucketRange(ctx context.Context, tenant string) (*postings.ShardBucketRange, error) {
	if r.hashRange == nil {
		return nil, nil
	}

	layout, err := indexobj.TenantShardLayout(ctx, r.obj, tenant)
	if err != nil {
		return nil, fmt.Errorf("reading the shard layout of the index object: %w", err)
	}
	from, to := layout.BucketRange(*r.hashRange)
	if layout.CoversAllShardBuckets(from, to) {
		return nil, nil
	}
	return &postings.ShardBucketRange{From: from, To: to}, nil
}

var errIndexSectionsReaderNotOpen = errors.New("index sections reader not opened")

func (r *postingsIndexSectionsReader) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if !r.initialized {
		return nil, errIndexSectionsReaderNotOpen
	}
	if r.readSpan == nil {
		_, r.readSpan = xcap.StartSpan(ctx, tracer, "metastore.postingsIndexSectionsReader.Read")
	}

	// Lazily resolve the stream selector on first Read, caching the result on the reader
	err := r.resolveStreamsFromSelector(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving streams from selector: %w", err)
	}

	if r.offset >= len(r.rows) {
		return nil, io.EOF
	}

	start := r.offset
	end := min(start+r.batchSize, len(r.rows))
	batch := r.rows[start:end]
	r.offset = end

	return sectionResultsToRecordBatch(batch), nil
}

func (r *postingsIndexSectionsReader) resolveStreamsFromSelector(ctx context.Context) error {
	if r.resolved {
		return nil
	}

	results, err := r.selector.selectStreams(ctx)
	defer func() {
		_ = r.selector.close()
	}()
	if err != nil {
		return fmt.Errorf("resolving postings sections: %w", err)
	}

	r.rows = expandResults(results)
	r.resolvedRefs = uint64(len(r.rows))
	r.resolved = true

	return nil
}

// expandResults flattens each SectionStreams' stream-ID bitmap into one
// resolvedRow per set stream ID, carrying the section's timestamp envelope and
// ambiguous names.
func expandResults(results []SectionStreams) []resolvedRow {
	total := 0
	for _, result := range results {
		bmap := lokimem.BitmapFrom(result.StreamBitmap, len(result.StreamBitmap)*8, 0)
		total += bmap.SetCount()
	}

	rows := make([]resolvedRow, 0, total)
	for _, result := range results {
		bmap := lokimem.BitmapFrom(result.StreamBitmap, len(result.StreamBitmap)*8, 0)
		for id := range bmap.IterValues(true) {
			rows = append(rows, resolvedRow{
				objectPath:     result.Section.ObjectPath,
				sectionIndex:   result.Section.SectionIndex,
				streamID:       int64(id),
				minTimestamp:   result.MinTimestamp,
				maxTimestamp:   result.MaxTimestamp,
				ambiguousNames: result.AmbiguousNames,
			})
		}
	}
	return rows
}

func (r *postingsIndexSectionsReader) Close() {
	if r.readSpan != nil {
		r.readSpan.End()
	}
}

func (r *postingsIndexSectionsReader) totalReadRows() uint64 {
	return r.resolvedRefs
}

func (r *postingsIndexSectionsReader) stats() readerStats {
	return readerStats{
		Initialized: r.initialized,
		ReadRows:    r.totalReadRows(),
	}
}

// sectionResultsToRecordBatch serializes expanded per-stream rows into
// postingsResultSchema. stream_id is the index-internal stream ID,
// which the resolver does not carry and no consumer reads from this batch, so it
// is set to 0.
func sectionResultsToRecordBatch(rows []resolvedRow) arrow.RecordBatch {
	n := len(rows)
	pathB := array.NewStringBuilder(memory.DefaultAllocator)
	sectionB := array.NewInt64Builder(memory.DefaultAllocator)
	streamIDB := array.NewInt64Builder(memory.DefaultAllocator)
	streamIDRefB := array.NewInt64Builder(memory.DefaultAllocator)
	minTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	maxTsB := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	rowCountB := array.NewInt64Builder(memory.DefaultAllocator)
	sizeB := array.NewInt64Builder(memory.DefaultAllocator)
	labelsB := array.NewStringBuilder(memory.DefaultAllocator)

	pathB.Reserve(n)
	sectionB.Reserve(n)
	streamIDB.Reserve(n)
	streamIDRefB.Reserve(n)
	minTsB.Reserve(n)
	maxTsB.Reserve(n)
	rowCountB.Reserve(n)
	sizeB.Reserve(n)
	labelsB.Reserve(n)

	for _, row := range rows {
		pathB.Append(row.objectPath)
		sectionB.Append(row.sectionIndex)
		streamIDB.Append(0)
		streamIDRefB.Append(row.streamID)
		minTsB.Append(arrow.Timestamp(row.minTimestamp))
		maxTsB.Append(arrow.Timestamp(row.maxTimestamp))
		rowCountB.Append(row.rowCount)
		sizeB.Append(row.uncompressedSize)
		if len(row.ambiguousNames) > 0 {
			labelsB.Append(strings.Join(row.ambiguousNames, ","))
		} else {
			labelsB.AppendNull()
		}
	}

	return array.NewRecordBatch(postingsResultSchema, []arrow.Array{
		pathB.NewArray(),
		sectionB.NewArray(),
		streamIDB.NewArray(),
		streamIDRefB.NewArray(),
		minTsB.NewArray(),
		maxTsB.NewArray(),
		rowCountB.NewArray(),
		sizeB.NewArray(),
		labelsB.NewArray(),
	}, int64(n))
}

// resolvedRowsFromRecordBatch decodes rec into one resolvedRow per row. It
// returns an error when the fields of rec differ from postingsResultSchema in
// name, order, type, nullability, or metadata.
//
// A null ambiguous-names value decodes to nil. The decoder ignores the
// stream_id column.
func resolvedRowsFromRecordBatch(rec arrow.RecordBatch) ([]resolvedRow, error) {
	if !rec.Schema().Equal(postingsResultSchema) {
		return nil, errors.New("schema does not match postingsResultSchema")
	}

	var (
		paths          = rec.Column(resultColPath).(*array.String)
		sections       = rec.Column(resultColSection).(*array.Int64)
		streamIDRefs   = rec.Column(resultColStreamIDRef).(*array.Int64)
		minTimestamps  = rec.Column(resultColMinTimestamp).(*array.Timestamp)
		maxTimestamps  = rec.Column(resultColMaxTimestamp).(*array.Timestamp)
		rowCounts      = rec.Column(resultColRowCount).(*array.Int64)
		sizes          = rec.Column(resultColUncompressedSize).(*array.Int64)
		ambiguousNames = rec.Column(resultColAmbiguousNames).(*array.String)
	)

	rows := make([]resolvedRow, rec.NumRows())
	for i := range rows {
		rows[i] = resolvedRow{
			objectPath:       paths.Value(i),
			sectionIndex:     sections.Value(i),
			streamID:         streamIDRefs.Value(i),
			minTimestamp:     int64(minTimestamps.Value(i)),
			maxTimestamp:     int64(maxTimestamps.Value(i)),
			rowCount:         rowCounts.Value(i),
			uncompressedSize: sizes.Value(i),
		}
		if !ambiguousNames.IsNull(i) {
			rows[i].ambiguousNames = strings.Split(ambiguousNames.Value(i), ",")
		}
	}
	return rows, nil
}
