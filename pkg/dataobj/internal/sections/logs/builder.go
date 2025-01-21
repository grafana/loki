package logs

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// A builder is used to construct a set of logs sections.
//
// Sorting the logs table is very expensive, so we need to break it up into
// smaller pieces:
//
//  1. Records are accumulated in memory up to reaching a configured buffer
//     size.
//
//  2. Once the buffer is full, the records are sorted and flushed into a set
//     of pages in a [stripe].
//
//  3. Once the set of stripes reaches a configured size limit, the stripes are
//     merged together into a final stripe that will compose a section.
type builder struct {
	opts Options

	sections    []*stripe // Completed sections.
	stripes     []*stripe // In-progress section; stripes are merged with [mergeStripes] into a single section.
	stripesSize int

	records     []Record // Accumulated records.
	recordsSize int

	sectionBuffer stripeBuffer // Used to convert stripes into *section.
	stripeBuffer  stripeBuffer // Used to convert records into *stripe.
}

func newBuilder(opts Options) *builder {
	return &builder{opts: opts}
}

func (b *builder) Append(record Record) {
	b.records = append(b.records, record)
	b.recordsSize += recordSize(record)

	if b.recordsSize >= b.opts.BufferSize {
		b.flushBuffer()
	}
}

func (b *builder) EstimatedSize() int {
	var size int

	size += b.recordsSize
	size += b.stripesSize
	for _, section := range b.sections {
		size += section.EstimatedSize()
	}
	return size
}

func recordSize(record Record) int {
	var size int

	size += 1 // One byte per stream ID (for uvarint).
	size += 8 // Eight bytes for timestamp.
	for _, metadata := range record.Metadata {
		size += len(metadata.Value)
	}
	size += len(record.Line)

	return size
}

// Flush immediately flushes all buffered data into sections.
func (b *builder) Flush() {
	b.flushBuffer()
	b.flushStripes()
}

func (b *builder) flushBuffer() {
	if len(b.records) == 0 {
		return
	}

	stripe := buildStripe(b.opts, &b.stripeBuffer, b.records)
	b.stripes = append(b.stripes, stripe)
	b.records = b.records[:0]
	b.recordsSize = 0
	b.stripesSize += stripe.EstimatedSize()

	if b.stripesSize >= b.opts.SectionSize {
		b.flushStripes()
	}
}

func (b *builder) flushStripes() {
	if len(b.stripes) == 0 {
		return
	}

	section, err := mergeStripes(b.opts, &b.sectionBuffer, b.stripes)
	if err != nil {
		// We control the input to mergeStripes, so this should never happen.
		panic(fmt.Sprintf("merging stripes: %v", err))
	}
	b.sections = append(b.sections, section)
	b.stripes = b.stripes[:0]
	b.stripesSize = 0
}

func (b *builder) Reset() {
	b.sections = b.sections[:0]
	b.stripes = b.stripes[:0]
	b.stripesSize = 0

	b.records = b.records[:0]
	b.recordsSize = 0

	b.sectionBuffer.Reset()
	b.stripeBuffer.Reset()
}

// A stripe is a set of sorted pages. A set of stripes can be merged together
// using heap sort.
type stripe struct {
	streamIDs  *dataset.MemColumn
	timestamps *dataset.MemColumn
	metadatas  []*dataset.MemColumn
	messages   *dataset.MemColumn
}

func (s *stripe) Columns() []*dataset.MemColumn {
	cols := make([]*dataset.MemColumn, 0, 3+len(s.metadatas))
	cols = append(cols, s.streamIDs, s.timestamps)
	cols = append(cols, s.metadatas...)
	cols = append(cols, s.messages)
	return cols
}

func (s *stripe) Iter() result.Seq[dataset.Row] {
	return result.Iter(func(yield func(dataset.Row) bool) error {
		memColumns := make([]*dataset.MemColumn, 0, 3+len(s.metadatas))
		memColumns = append(memColumns, s.streamIDs, s.timestamps)
		memColumns = append(memColumns, s.metadatas...)
		memColumns = append(memColumns, s.messages)

		dset := dataset.FromMemory(memColumns)

		columns, err := result.Collect(dset.ListColumns(context.Background()))
		if err != nil {
			return err
		}

		for result := range dataset.Iter(context.Background(), columns) {
			row, err := result.Value()
			if err != nil || !yield(row) {
				return err
			}
		}

		return nil
	})
}

func (s *stripe) EstimatedSize() int {
	var size int

	size += s.streamIDs.ColumnInfo().CompressedSize
	size += s.timestamps.ColumnInfo().CompressedSize
	for _, metadata := range s.metadatas {
		size += metadata.ColumnInfo().CompressedSize
	}
	size += s.messages.ColumnInfo().CompressedSize

	return size
}

// A stripeBuffer holds column builders used for constructing stripes. The zero
// value is ready for use.
type stripeBuffer struct {
	streamIDs  *dataset.ColumnBuilder
	timestamps *dataset.ColumnBuilder

	metadatas      []*dataset.ColumnBuilder
	metadataLookup map[string]int // map of metadata key to index in metadatas

	messages *dataset.ColumnBuilder
}

func (b *stripeBuffer) StreamIDs(opts Options) *dataset.ColumnBuilder {
	if b.streamIDs != nil {
		return b.streamIDs
	}

	col, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: opts.PageSizeHint,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		// We control the Value/Encoding tuple so this can't fail; if it does,
		// we're left in an unrecoverable state where nothing can be encoded
		// properly so we panic.
		panic(fmt.Sprintf("creating stream ID column: %v", err))
	}

	b.streamIDs = col
	return col
}

func (b *stripeBuffer) Timestamps(opts Options) *dataset.ColumnBuilder {
	if b.timestamps != nil {
		return b.timestamps
	}

	col, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: opts.PageSizeHint,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		// We control the Value/Encoding tuple so this can't fail; if it does,
		// we're left in an unrecoverable state where nothing can be encoded
		// properly so we panic.
		panic(fmt.Sprintf("creating timestamp column: %v", err))
	}

	b.timestamps = col
	return col
}

func (b *stripeBuffer) AllMetadatas() []*dataset.ColumnBuilder { return b.metadatas }

func (b *stripeBuffer) Metadatas(key string, opts Options, compression datasetmd.CompressionType) *dataset.ColumnBuilder {
	index, ok := b.metadataLookup[key]
	if ok {
		return b.metadatas[index]
	}

	col, err := dataset.NewColumnBuilder(key, dataset.BuilderOptions{
		PageSizeHint: opts.PageSizeHint,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  compression,
	})
	if err != nil {
		// We control the Value/Encoding tuple so this can't fail; if it does,
		// we're left in an unrecoverable state where nothing can be encoded
		// properly so we panic.
		panic(fmt.Sprintf("creating metadata column: %v", err))
	}

	b.metadatas = append(b.metadatas, col)

	if b.metadataLookup == nil {
		b.metadataLookup = make(map[string]int)
	}
	b.metadataLookup[key] = len(b.metadatas) - 1
	return col
}

// CleanupMetadatas removes metadata columns that are not found in keys.
func (b *stripeBuffer) CleanupMetadatas(keys iter.Seq[string]) {
	// TODO(rfratto): this is a small number of allocations each flush; do we
	// need to find a way to reuse these?
	var (
		newColumns = make([]*dataset.ColumnBuilder, 0, len(b.metadatas))
		newLookup  = make(map[string]int, len(b.metadatas))
	)

	for key := range keys {
		index, ok := b.metadataLookup[key]
		if !ok {
			continue
		}

		if _, ok := newLookup[key]; ok {
			// Already in the new columns.
			continue
		}

		newColumns = append(newColumns, b.metadatas[index])
		newLookup[key] = len(newColumns) - 1
	}

	b.metadatas = newColumns
	b.metadataLookup = newLookup
}

// Reset resets the buffer to its initial state.
func (b *stripeBuffer) Reset() {
	if b.streamIDs != nil {
		b.streamIDs.Reset()
	}
	if b.timestamps != nil {
		b.timestamps.Reset()
	}
	if b.messages != nil {
		b.messages.Reset()
	}
	for _, md := range b.metadatas {
		md.Reset()
	}
}

func (b *stripeBuffer) Messages(opts Options, compression datasetmd.CompressionType) *dataset.ColumnBuilder {
	if b.messages != nil {
		return b.messages
	}

	col, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: opts.PageSizeHint,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  compression,
	})
	if err != nil {
		// We control the Value/Encoding tuple so this can't fail; if it does,
		// we're left in an unrecoverable state where nothing can be encoded
		// properly so we panic.
		panic(fmt.Sprintf("creating messages column: %v", err))
	}

	b.messages = col
	return col
}

func buildStripe(opts Options, buf *stripeBuffer, records []Record) *stripe {
	slices.SortFunc(records, func(a, b Record) int {
		// Records are sorted by stream ID then timestamp.
		if res := cmp.Compare(a.StreamID, b.StreamID); res != 0 {
			return res
		}
		return a.Timestamp.Compare(b.Timestamp)
	})

	// Ensure the buffer doesn't have any data in it before we append.
	buf.Reset()

	var (
		streamIDsBuilder  = buf.StreamIDs(opts)
		timestampsBuilder = buf.Timestamps(opts)
		messagesBuilder   = buf.Messages(opts, datasetmd.COMPRESSION_TYPE_SNAPPY)
	)

	for i, record := range records {
		// We ignore the errors below; they only fail if given out-of-order data
		// (where the row number is less than the previous row number), which can't
		// ever happen here.

		_ = streamIDsBuilder.Append(i, dataset.Int64Value(record.StreamID))
		_ = timestampsBuilder.Append(i, dataset.Int64Value(record.Timestamp.UnixNano()))
		_ = messagesBuilder.Append(i, dataset.StringValue(record.Line))

		for _, metadata := range record.Metadata {
			metadataBuilder := buf.Metadatas(metadata.Name, opts, datasetmd.COMPRESSION_TYPE_SNAPPY)
			_ = metadataBuilder.Append(i, dataset.StringValue(metadata.Value))
		}
	}

	// Remove unused metadata columns.
	buf.CleanupMetadatas(func(yield func(string) bool) {
		for _, record := range records {
			for _, metadata := range record.Metadata {
				if !yield(metadata.Name) {
					return
				}
			}
		}
	})

	// Flush never returns an error so we ignore it here to keep the code simple.
	//
	// TODO(rfratto): remove error return from Flush to clean up code.
	streamIDs, _ := streamIDsBuilder.Flush()
	timestamps, _ := timestampsBuilder.Flush()
	messages, _ := messagesBuilder.Flush()

	var metadatas []*dataset.MemColumn

	for _, metadataBuilder := range buf.AllMetadatas() {
		// Each metadata column may have a different number of rows compared to
		// other columns. Since adding NULLs isn't free, we don't call Backfill
		// here.

		metadata, _ := metadataBuilder.Flush()
		metadatas = append(metadatas, metadata)
	}

	// Sort metadata columns by name.
	slices.SortFunc(metadatas, func(a, b *dataset.MemColumn) int {
		return cmp.Compare(a.ColumnInfo().Name, b.ColumnInfo().Name)
	})

	return &stripe{
		streamIDs:  streamIDs,
		timestamps: timestamps,
		metadatas:  metadatas,
		messages:   messages,
	}
}
