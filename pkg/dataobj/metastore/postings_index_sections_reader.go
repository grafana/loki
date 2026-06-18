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

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var (
	_ ArrowRecordBatchReader = (*postingsIndexSectionsReader)(nil)
	_ bloomStatsProvider     = (*postingsIndexSectionsReader)(nil)
)

// postingsResultSchema is the pointers-section Arrow schema the resolver's
// SectionRefs are serialized into, so the postings reader is a drop-in for the
// legacy reader on the shared ArrowRecordBatchReader seam. The query semantics
// live in the resolver; this schema is only the wire format consumers already
// decode via pointers.FromRecordBatch / pointers.InternalLabelsColumn.
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

// postingsIndexSectionsReader resolves SectionRefs from an index object's
// postings sections via a postings.StreamResolver, then serializes the resolved
// refs into the pointers-section Arrow schema in batchSize chunks. Section
// lifetime is bound to the underlying *dataobj.Object, so the reader does not
// close sections itself.
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
	refs         []postings.SectionRef
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

	var opened []*postings.Section
	for _, section := range r.obj.Sections() {
		if section.Tenant != tenant || !postings.CheckSection(section) {
			continue
		}
		sec, err := postings.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening postings section: %w", err)
		}
		opened = append(opened, sec)
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
	if r.offset >= len(r.refs) {
		return nil, io.EOF
	}

	start := r.offset
	end := min(start+r.batchSize, len(r.refs))
	batch := r.refs[start:end]
	r.offset = end

	r.readSpan.Record(xcap.StatMetastoreSectionPointersRead.Observe(int64(len(batch))))
	return sectionRefsToRecordBatch(batch), nil
}

func (r *postingsIndexSectionsReader) lazyResolve(ctx context.Context) error {
	if r.resolved {
		return nil
	}

	region := xcap.RegionFromContext(ctx)
	startTime := time.Now()
	defer func() {
		region.Record(xcap.StatMetastoreStreamsReadTime.Observe(time.Since(startTime).Seconds()))
		r.readSpan.Record(xcap.StatMetastoreSectionPointersReadTime.Observe(time.Since(startTime).Seconds()))
	}()

	resolver := postings.NewStreamResolver(r.matchers, r.predicates, r.start, r.end)
	refs, err := resolver.Resolve(ctx, r.openSections)
	if err != nil {
		return fmt.Errorf("resolving postings sections: %w", err)
	}
	r.refs = refs
	r.resolvedRefs = uint64(len(refs))
	r.resolved = true

	region.Record(xcap.StatMetastoreStreamsRead.Observe(int64(len(refs))))
	return nil
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

// sectionRefsToRecordBatch serializes resolved SectionRefs into the
// pointers-section Arrow schema. stream_id is the index-internal stream ID,
// which SectionRef does not carry and no consumer reads from this batch, so it
// is set to 0.
func sectionRefsToRecordBatch(refs []postings.SectionRef) arrow.RecordBatch {
	n := len(refs)
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

	for _, ref := range refs {
		pathB.Append(ref.ObjectPath)
		sectionB.Append(ref.SectionIndex)
		streamIDB.Append(0)
		streamIDRefB.Append(ref.StreamID)
		minTsB.Append(arrow.Timestamp(ref.MinTimestamp))
		maxTsB.Append(arrow.Timestamp(ref.MaxTimestamp))
		rowCountB.Append(ref.RowCount)
		sizeB.Append(ref.UncompressedSize)
		if len(ref.AmbiguousLabels) > 0 {
			labelsB.Append(strings.Join(ref.AmbiguousLabels, ","))
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
