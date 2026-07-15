package metastore

import (
	"context"
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
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	lokimem "github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var (
	_ ArrowRecordBatchReader = (*postingsIndexSectionsReader)(nil)
	_ statsProvider          = (*postingsIndexSectionsReader)(nil)
)

// maxConcurrentSectionOpens caps how many postings sections are opened
// concurrently in Open. Hardcoded for now.
const maxConcurrentSectionOpens = 16

// postingsResultSchema is the pointers-section Arrow schema the resolver's
// results are serialized into, so the postings reader is a drop-in for the
// legacy reader on the shared ArrowRecordBatchReader seam. This schema is only
// the wire format consumers already decode via pointers.FromRecordBatch /
// pointers.InternalLabelsColumn.
var postingsResultSchema = arrow.NewSchema([]arrow.Field{
	{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
	{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "stream_id.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "stream_id_ref.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "min_timestamp.timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	{Name: "max_timestamp.timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	{Name: "row_count.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: "uncompressed_size.int64", Type: arrow.PrimitiveTypes.Int64},
	{Name: pointers.InternalLabelsFieldName, Type: arrow.BinaryTypes.String, Nullable: true},
}, nil)

// resolvedRow is one pointers-schema row: a single stream within a resolved
// section, expanded from that section's stream-ID bitmap.
type resolvedRow struct {
	objectPath     string
	sectionIndex   int64
	streamID       int64
	minTimestamp   int64
	maxTimestamp   int64
	ambiguousNames []string
}

// postingsIndexSectionsReader resolves matching streams from an index object's
// postings sections via a streamSelector, expands each result's
// stream-ID bitmap into one pointers-schema row per stream, and serializes those
// rows in batchSize chunks. Section lifetime is bound to the underlying
// *dataobj.Object, so the reader does not close sections itself.
type postingsIndexSectionsReader struct {
	logger log.Logger
	obj    *dataobj.Object

	matchers   []*labels.Matcher
	predicates []*labels.Matcher
	start, end time.Time
	batchSize  int

	initialized bool
	resolved    bool

	openSections []*postings.Section
	rows         []resolvedRow
	offset       int

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
	}
}

func (r *postingsIndexSectionsReader) Open(ctx context.Context) error {
	if r.initialized {
		return nil
	}
	if len(r.matchers) == 0 {
		r.initialized = true
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
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentSectionOpens)
	for i, section := range matching {
		g.Go(func() error {
			sec, err := postings.Open(ctx, section)
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

	r.openSections = opened
	r.initialized = true
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

	if err := r.lazyResolve(ctx); err != nil {
		return nil, err
	}
	if r.offset >= len(r.rows) {
		return nil, io.EOF
	}

	start := r.offset
	end := min(start+r.batchSize, len(r.rows))
	batch := r.rows[start:end]
	r.offset = end

	r.readSpan.Record(StatMetastoreSectionPointersRead.Observe(int64(len(batch))))
	return sectionResultsToRecordBatch(batch), nil
}

func (r *postingsIndexSectionsReader) lazyResolve(ctx context.Context) error {
	if r.resolved {
		return nil
	}

	region := xcap.RegionFromContext(ctx)
	startTime := time.Now()
	defer func() {
		region.Record(StatMetastoreStreamsReadTime.Observe(time.Since(startTime).Seconds()))
		r.readSpan.Record(StatMetastoreSectionPointersReadTime.Observe(time.Since(startTime).Seconds()))
	}()

	selector := newStreamSelector(r.matchers, r.predicates, r.start, r.end)
	results, err := selector.selectStreams(ctx, r.openSections)
	if err != nil {
		return fmt.Errorf("resolving postings sections: %w", err)
	}
	r.rows = expandResults(results)
	r.resolvedRefs = uint64(len(r.rows))
	r.resolved = true

	region.Record(StatMetastoreStreamsRead.Observe(int64(len(r.rows))))
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
	r.openSections = nil
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

// sectionResultsToRecordBatch serializes expanded per-stream rows into the
// pointers-section Arrow schema. stream_id is the index-internal stream ID,
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
		rowCountB.Append(0)
		sizeB.Append(0)
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
