// Package logs defines types used for the data object logs section. The logs
// section holds a list of log records across multiple streams.
package logs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
)

// A Record is an individual log record within the logs section.
type Record struct {
	StreamID  int64
	Timestamp time.Time
	Metadata  labels.Labels
	Line      []byte
}

// Options configures the behavior of the logs section.
type Options struct {
	// PageSizeHint is the size of pages to use when encoding the logs section.
	PageSizeHint int

	// BufferSize is the size of the buffer to use when accumulating log records.
	BufferSize int

	// SectionSizeHint is the size of the section to use when encoding the logs
	// section. If the section size is exceeded, multiple sections will be
	// created.
	SectionSize int
}

// Logs accumulate a set of [Record]s within a data object.
type Logs struct {
	metrics *Metrics
	opts    Options

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
	recordsSize int

	stripes      []*table // In-progress section; flushed with [mergeTables] into a single table.
	stripeBuffer tableBuffer
	stripesSize  int // Estimated byte size of all elements in stripes.

	sections      []*table // Completed sections.
	sectionBuffer tableBuffer
}

// Nwe creates a new Logs section. The pageSize argument specifies how large
// pages should be.
func New(metrics *Metrics, opts Options) *Logs {
	if metrics == nil {
		metrics = NewMetrics()
	}

	return &Logs{
		metrics: metrics,
		opts:    opts,
	}
}

// Append adds a new entry to the set of Logs.
func (l *Logs) Append(entry Record) {
	l.metrics.appendsTotal.Inc()

	l.records = append(l.records, entry)
	l.recordsSize += recordSize(entry)

	if l.recordsSize >= l.opts.BufferSize {
		l.flushRecords()
	}

	l.metrics.recordCount.Inc()
}

func recordSize(record Record) int {
	var size int

	size++    // One byte per stream ID (for uvarint).
	size += 8 // Eight bytes for timestamp.
	for _, metadata := range record.Metadata {
		size += len(metadata.Value)
	}
	size += len(record.Line)

	return size
}

func (l *Logs) flushRecords() {
	if len(l.records) == 0 {
		return
	}

	// Our stripes are intermediate tables that don't need to have the best
	// compression. To maintain high throughput on appends, we use the fastest
	// compression for a stripe. Better compression is then used for sections.
	compressionOpts := dataset.CompressionOptions{
		Zstd: []zstd.EOption{zstd.WithEncoderLevel(zstd.SpeedFastest)},
	}

	stripe := buildTable(&l.stripeBuffer, l.opts.PageSizeHint, compressionOpts, l.records)
	l.stripes = append(l.stripes, stripe)
	l.stripesSize += stripe.Size()

	l.records = sliceclear.Clear(l.records)
	l.recordsSize = 0

	if l.stripesSize >= l.opts.SectionSize {
		l.flushSection()
	}
}

func (l *Logs) flushSection() {
	if len(l.stripes) == 0 {
		return
	}

	compressionOpts := dataset.CompressionOptions{
		Zstd: []zstd.EOption{zstd.WithEncoderLevel(zstd.SpeedDefault)},
	}

	section, err := mergeTables(&l.sectionBuffer, l.opts.PageSizeHint, compressionOpts, l.stripes)
	if err != nil {
		// We control the input to mergeTables, so this should never happen.
		panic(fmt.Sprintf("merging tables: %v", err))
	}

	l.sections = append(l.sections, section)

	l.stripes = sliceclear.Clear(l.stripes)
	l.stripesSize = 0
}

// EstimatedSize returns the estimated size of the Logs section in bytes.
func (l *Logs) EstimatedSize() int {
	var size int

	size += l.recordsSize
	size += l.stripesSize
	for _, section := range l.sections {
		size += section.Size()
	}

	return size
}

// EncodeTo encodes the set of logs to the provided encoder. Before encoding,
// log records are sorted by StreamID and Timestamp.
//
// EncodeTo may generate multiple sections if the list of log records is too
// big to fit into a single section.
//
// [Logs.Reset] is invoked after encoding, even if encoding fails.
func (l *Logs) EncodeTo(enc *encoding.Encoder) error {
	timer := prometheus.NewTimer(l.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	defer l.Reset()

	// Flush any remaining buffered data.
	l.flushRecords()
	l.flushSection()

	// TODO(rfratto): handle individual sections having oversized metadata. This
	// can happen when the number of columns is very wide, due to a lot of
	// metadata columns.
	//
	// As we're already splitting data into separate sections, the best solution
	// for this is to aggregate the lowest cardinality columns into a combined
	// column. This will reduce the number of columns in the section and thus the
	// metadata size.
	for _, section := range l.sections {
		if err := l.encodeSection(enc, section); err != nil {
			return fmt.Errorf("encoding section: %w", err)
		}
	}

	return nil
}

func (l *Logs) encodeSection(enc *encoding.Encoder, section *table) error {
	logsEnc, err := enc.OpenLogs()
	if err != nil {
		return fmt.Errorf("opening logs section: %w", err)
	}
	defer func() {
		// Discard on defer for safety. This will return an error if we
		// successfully committed.
		_ = logsEnc.Discard()
	}()

	{
		errs := make([]error, 0, len(section.Metadatas)+3)
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_STREAM_ID, section.StreamID))
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_TIMESTAMP, section.Timestamp))
		for _, md := range section.Metadatas {
			errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_METADATA, md))
		}
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_MESSAGE, section.Message))
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("encoding columns: %w", err)
		}
	}

	return logsEnc.Commit()
}

func encodeColumn(enc *encoding.LogsEncoder, columnType logsmd.ColumnType, column dataset.Column) error {
	columnEnc, err := enc.OpenColumn(columnType, column.ColumnInfo())
	if err != nil {
		return fmt.Errorf("opening %s column encoder: %w", columnType, err)
	}
	defer func() {
		// Discard on defer for safety. This will return an error if we
		// successfully committed.
		_ = columnEnc.Discard()
	}()

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
			Info: *page.PageInfo(),
			Data: data,
		}
		if err := columnEnc.AppendPage(memPage); err != nil {
			return fmt.Errorf("appending %s page: %w", columnType, err)
		}
	}

	return columnEnc.Commit()
}

// Reset resets all state, allowing Logs to be reused.
func (l *Logs) Reset() {
	l.metrics.recordCount.Set(0)

	l.records = sliceclear.Clear(l.records)
	l.recordsSize = 0

	l.stripes = sliceclear.Clear(l.stripes)
	l.stripeBuffer.Reset()
	l.stripesSize = 0

	l.sections = sliceclear.Clear(l.sections)
	l.sectionBuffer.Reset()
}
