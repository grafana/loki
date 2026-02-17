package logs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	datasetmd_v2 "github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// A Record is an individual log record within the logs section.
type Record struct {
	StreamID  int64
	Timestamp time.Time
	Metadata  labels.Labels
	Line      []byte
}

type AppendStrategy int

const (
	AppendUnordered = iota
	AppendOrdered
)

type SortOrder int

const (
	SortStreamASC SortOrder = iota
	SortTimestampDESC
)

// BuilderOptions configures the behavior of the logs section.
type BuilderOptions struct {
	// PageSizeHint is the size of pages to use when encoding the logs section.
	PageSizeHint int

	// PageMaxRowCount is the maximum amount of rows of pages to use when encoding the logs section.
	PageMaxRowCount int

	// BufferSize is the size of the buffer to use when accumulating log records.
	BufferSize int

	// StripeMergeLimit is the maximum number of stripes to merge at once when
	// flushing stripes into a section. StripeMergeLimit must be larger than 1.
	//
	// Lower values of StripeMergeLimit reduce the memory overhead of merging but
	// increase time spent merging. Higher values of StripeMergeLimit increase
	// memory overhead but reduce time spent merging.
	StripeMergeLimit int

	// AppendStrategy is allowed to control how the builder creates the section.
	// When appending logs to the section in strict sort order, the [AppendOrdered] can be used to avoid
	// creating and sorting of stripes.
	AppendStrategy AppendStrategy

	// SortOrder defines the order in which the rows of the logs sections are sorted.
	// They can either be sorted by [streamID ASC, timestamp DESC] ([SortStreamASC]) or [timestamp DESC, streamID ASC] ([SortTimestampDESC]).
	SortOrder SortOrder
}

// Builder accumulate a set of [Record]s within a data object.
type Builder struct {
	metrics *Metrics
	opts    BuilderOptions

	// The optional tenant that owns the builder. If specified, the section
	// must only contain logs owned by the tenant, and no other tenants.
	tenant string

	// Sorting the entire set of logs is very expensive, so we need to break it
	// up into smaller pieces:
	//
	// 1. Records are accumulated in memory up to BufferSize; the current size is
	//    tracked by recordsSize.
	//
	// 2. Once the buffer is full, records are sorted and flushed to smaller
	//    [table]s called stripes.
	//
	// 3. Once the set of stripes reaches SectionSize, they are merged together
	//    into a final table that will be encoded as a single section.
	//
	// At the end of this process, there will be a set of sections that are
	// encoded separately.

	records     []Record // Buffered records to flush to a group.
	recordsSize int      // Byte size of all buffered records (uncompressed).

	stripes                 []*table // In-progress section; flushed with [mergeTables] into a single table.
	stripeBuffer            tableBuffer
	stripesUncompressedSize int // Estimated byte size of all elements in stripes (uncompressed).
	stripesCompressedSize   int // Estimated byte size of all elements in stripes (compressed).

	sectionBuffer tableBuffer
}

// Nwe creates a new logs section. The pageSize argument specifies how large
// pages should be.
func NewBuilder(metrics *Metrics, opts BuilderOptions) *Builder {
	if metrics == nil {
		metrics = NewMetrics()
	}

	return &Builder{
		metrics: metrics,
		opts:    opts,
	}
}

// Tenant returns the optional tenant that owns the builder.
func (b *Builder) Tenant() string { return b.tenant }

// SetTenant sets the tenant that owns the builder. A builder can be made
// multi-tenant by passing an empty string.
func (b *Builder) SetTenant(tenant string) { b.tenant = tenant }

// Type returns the [dataobj.SectionType] of the logs builder.
func (b *Builder) Type() dataobj.SectionType { return sectionType }

// Append adds a new entry to b.
func (b *Builder) Append(entry Record) {
	b.metrics.appendsTotal.Inc()
	b.metrics.recordCount.Inc()

	b.records = append(b.records, entry)
	b.recordsSize += recordSize(entry)

	// Shortcut for when logs are appending in strict sort order.
	// We skip building temporarily compressed stripes in favour of a speed
	// with a single pass compression of all records.
	if b.opts.AppendStrategy == AppendOrdered {
		return
	}

	if b.recordsSize >= b.opts.BufferSize {
		b.flushRecords(zstd.SpeedFastest)
	}
}

func recordSize(record Record) int {
	var size int

	size++    // One byte per stream ID (for uvarint).
	size += 8 // Eight bytes for timestamp.
	record.Metadata.Range(func(metadata labels.Label) {
		size += len(metadata.Value)
	})
	size += len(record.Line)

	return size
}

func (b *Builder) flushRecords(encLevel zstd.EncoderLevel) {
	if len(b.records) == 0 {
		return
	}

	// We can panic in case flushRecords is called multiple times before flushing a section
	// when using the [AppendOrdered] strategy, because that should not happen and is
	// considered a programming error.
	if b.opts.AppendStrategy == AppendOrdered && len(b.stripes) > 0 {
		panic("must not call flushRecords multiple times for a single section when using AppendOrdered strategy")
	}

	// Our stripes are intermediate tables that don't need to have the best
	// compression. To maintain high throughput on appends, we use the fastest
	// compression for a stripe. Better compression is then used for sections.
	compressionOpts := &dataset.CompressionOptions{
		Zstd: []zstd.EOption{zstd.WithEncoderLevel(encLevel)},
	}

	stripe := buildTable(&b.stripeBuffer, b.opts.PageSizeHint, b.opts.PageMaxRowCount, compressionOpts, b.records, b.opts.SortOrder)
	b.stripes = append(b.stripes, stripe)
	b.stripesUncompressedSize += stripe.UncompressedSize()
	b.stripesCompressedSize += stripe.CompressedSize()

	b.records = sliceclear.Clear(b.records)
	b.recordsSize = 0
}

func (b *Builder) flushSection() *table {
	if len(b.stripes) == 0 {
		return nil
	}

	compressionOpts := &dataset.CompressionOptions{
		Zstd: []zstd.EOption{zstd.WithEncoderLevel(zstd.SpeedDefault)},
	}

	section, err := mergeTablesIncremental(&b.sectionBuffer, b.opts.PageSizeHint, b.opts.PageMaxRowCount, compressionOpts, b.stripes, b.opts.StripeMergeLimit, b.opts.SortOrder)
	if err != nil {
		// We control the input to mergeTables, so this should never happen.
		panic(fmt.Sprintf("merging tables: %v", err))
	}

	b.stripes = sliceclear.Clear(b.stripes)
	b.stripesCompressedSize = 0
	b.stripesUncompressedSize = 0
	return section
}

func (b *Builder) flushSectionOrdered() *table {
	b.flushRecords(zstd.SpeedDefault)

	if len(b.stripes) == 0 {
		return nil
	}

	section := b.stripes[0]
	b.stripes = sliceclear.Clear(b.stripes)
	b.stripesCompressedSize = 0
	b.stripesUncompressedSize = 0
	return section
}

// UncompressedSize returns the current uncompressed size of the logs section
// in bytes.
func (b *Builder) UncompressedSize() int {
	var size int

	size += b.recordsSize
	size += b.stripesUncompressedSize

	return size
}

// EstimatedSize returns the estimated size of the Logs section in bytes.
func (b *Builder) EstimatedSize() int {
	var size int

	size += b.recordsSize
	size += b.stripesCompressedSize

	return size
}

// Flush flushes b to the provided writer.
//
// After successful encoding, the b is reset and can be reused.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	timer := prometheus.NewTimer(b.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	var section *table
	if b.opts.AppendStrategy == AppendOrdered {
		// Flush buffered data all at once
		section = b.flushSectionOrdered()
	} else {
		// Flush any remaining buffered data.
		b.flushRecords(zstd.SpeedFastest)
		section = b.flushSection()
	}

	if section == nil {
		return 0, nil
	}

	// TODO(rfratto): handle an individual section having oversized metadata.
	// This can happen when the number of columns is very wide, due to a lot of
	// metadata columns.
	//
	// As the caller likely creates many smaller logs sections, the best solution
	// for this is to aggregate the lowest cardinality columns into a combined
	// column. This will reduce the number of columns in the section and thus the
	// metadata size.

	var logsEnc columnar.Encoder
	if err := b.encodeSection(&logsEnc, section); err != nil {
		return 0, fmt.Errorf("encoding section: %w", err)
	}

	// The first two columns of each row are *always* stream ID and timestamp.
	// TODO(ashwanth): Find a safer way to do this. Same as [CompareRows]
	logsEnc.SetSortInfo(sortInfo(b.opts.SortOrder))
	logsEnc.SetTenant(b.tenant)

	n, err = logsEnc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}

func (b *Builder) encodeSection(enc *columnar.Encoder, section *table) error {
	{
		errs := make([]error, 0, len(section.Metadatas)+3)
		errs = append(errs, encodeColumn(enc, ColumnTypeStreamID, section.StreamID))
		errs = append(errs, encodeColumn(enc, ColumnTypeTimestamp, section.Timestamp))
		for _, md := range section.Metadatas {
			errs = append(errs, encodeColumn(enc, ColumnTypeMetadata, md))
		}
		errs = append(errs, encodeColumn(enc, ColumnTypeMessage, section.Message))
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("encoding columns: %w", err)
		}
	}

	return nil
}

func sortInfo(sort SortOrder) *datasetmd_v2.SortInfo {
	switch sort {
	case SortStreamASC:
		return &datasetmd_v2.SortInfo{
			ColumnSorts: []*datasetmd_v2.SortInfo_ColumnSort{
				{ColumnIndex: 0, Direction: datasetmd_v2.SORT_DIRECTION_ASCENDING},  // StreamID ASC
				{ColumnIndex: 1, Direction: datasetmd_v2.SORT_DIRECTION_DESCENDING}, // Timestamp DESC
			},
		}
	case SortTimestampDESC:
		return &datasetmd_v2.SortInfo{
			ColumnSorts: []*datasetmd_v2.SortInfo_ColumnSort{
				{ColumnIndex: 1, Direction: datasetmd_v2.SORT_DIRECTION_DESCENDING}, // Timestamp DESC
				{ColumnIndex: 0, Direction: datasetmd_v2.SORT_DIRECTION_ASCENDING},  // StreamID ASC
			},
		}
	default:
		panic("invalid sort order")
	}
}

func encodeColumn(enc *columnar.Encoder, columnType ColumnType, column *tableColumn) error {
	columnEnc, err := enc.OpenColumn(column.ColumnDesc())
	if err != nil {
		return fmt.Errorf("opening %s column encoder: %w", columnType, err)
	}
	defer func() {
		// Discard on defer for safety. This will return an error if we
		// successfully committed.
		_ = columnEnc.Discard()
	}()
	if len(column.Pages) == 0 {
		// Column has no data; discard.
		return nil
	}

	// Our column is in memory, so we don't need a "real" context in the calls
	// below.
	for result := range column.ListPages(context.Background()) {
		page, err := result.Value()
		if err != nil {
			return fmt.Errorf("getting %s page: %w", columnType, err)
		}

		data, err := page.ReadPage(context.Background())
		if err != nil {
			return fmt.Errorf("reading %s page: %w", columnType, err)
		}

		memPage := &dataset.MemPage{
			Desc: *page.PageDesc(),
			Data: data,
		}
		if err := columnEnc.AppendPage(memPage); err != nil {
			return fmt.Errorf("appending %s page: %w", columnType, err)
		}
	}

	return columnEnc.Commit()
}

// Reset resets all state, allowing b to be reused.
func (b *Builder) Reset() {
	b.metrics.recordCount.Set(0)

	b.tenant = ""

	b.records = sliceclear.Clear(b.records)
	b.recordsSize = 0

	b.stripes = sliceclear.Clear(b.stripes)
	b.stripeBuffer.Reset()
	b.stripesCompressedSize = 0
	b.stripesUncompressedSize = 0

	b.sectionBuffer.Reset()
}
