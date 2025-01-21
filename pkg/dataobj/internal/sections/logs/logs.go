// Package logs defines types used for the data object logs section. The logs
// section holds a list of log records across multiple streams.
package logs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// A Record is an individual log record within the logs section.
type Record struct {
	StreamID  int64
	Timestamp time.Time
	Metadata  push.LabelsAdapter
	Line      string
}

// Options configures the behavior of the Logs section.
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
	builder *builder
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
		builder: newBuilder(opts),
	}
}

// Append adds a new entry to the set of Logs.
func (l *Logs) Append(entry Record) {
	l.metrics.appendsTotal.Inc()

	l.builder.Append(entry)
	l.metrics.recordCount.Inc()
}

// EstimatedSize returns the estimated size of the Logs section in bytes.
func (l *Logs) EstimatedSize() int {
	return l.builder.EstimatedSize()
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

	// Flush any remaining data in the builder.
	l.builder.Flush()

	// TODO(rfratto): handle individual sections having oversized metadata. Thsi
	// can happen when the number of columns is very wide, due to a lot of
	// metadata columns.
	//
	// As we're already splitting data into separate sections, the best solution
	// for this is to aggregate the lowest cardinality columns into a combined
	// column. This will reduce the number of columns in the section and thus the
	// metadata size.
	for _, section := range l.builder.sections {
		if err := l.encodeSection(enc, section); err != nil {
			return fmt.Errorf("encoding section: %w", err)
		}
	}

	return nil
}

func (l *Logs) encodeSection(enc *encoding.Encoder, stripe *stripe) error {
	logsEnc, err := enc.OpenLogs()
	if err != nil {
		return fmt.Errorf("opening logs section: %w", err)
	}
	defer func() {
		// Discard on defer for safety. This will return an error if we
		// successfully committed.
		_ = logsEnc.Discard()
	}()

	dset := dataset.FromMemory(stripe.Columns())

	dsetColumns, err := result.Collect(dset.ListColumns(context.Background())) // dset is in memory; "real" context not needed.
	if err != nil {
		return fmt.Errorf("listing columns: %w", err)
	}

	// Encode our columns. The slice order here *must* match the order in
	// [Logs.buildDataset]!
	{
		errs := make([]error, 0, len(dsetColumns))
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_STREAM_ID, dsetColumns[0]))
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_TIMESTAMP, dsetColumns[1]))
		for _, mdCol := range dsetColumns[2 : len(dsetColumns)-1] {
			errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_METADATA, mdCol))
		}
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_MESSAGE, dsetColumns[len(dsetColumns)-1]))
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
	l.builder.Reset()
}
