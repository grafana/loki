// Package logs defines types used for the data object logs section. The logs
// section holds a list of log records across multiple streams.
package logs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
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

// Logs accumulate a set of [Record]s within a data object.
type Logs struct {
	metrics  *Metrics
	rows     int
	pageSize int

	streamIDs  *dataset.ColumnBuilder
	timestamps *dataset.ColumnBuilder

	metadatas      []*dataset.ColumnBuilder
	metadataLookup map[string]int // map of metadata key to index in metadatas

	messages *dataset.ColumnBuilder
}

// Nwe creates a new Logs section. The pageSize argument specifies how large
// pages should be.
func New(metrics *Metrics, pageSize int) *Logs {
	if metrics == nil {
		metrics = NewMetrics()
	}

	// We control the Value/Encoding tuple so creating column builders can't
	// fail; if it does, we're left in an unrecoverable state where nothing can
	// be encoded properly so we panic.
	streamIDs, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		panic(fmt.Sprintf("creating stream ID column: %v", err))
	}

	timestamps, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		panic(fmt.Sprintf("creating timestamp column: %v", err))
	}

	messages, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: pageSize,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
	})
	if err != nil {
		panic(fmt.Sprintf("creating message column: %v", err))
	}

	return &Logs{
		metrics:  metrics,
		pageSize: pageSize,

		streamIDs:  streamIDs,
		timestamps: timestamps,

		metadataLookup: make(map[string]int),

		messages: messages,
	}
}

// Append adds a new entry to the set of Logs.
func (l *Logs) Append(entry Record) {
	l.metrics.appendsTotal.Inc()

	// Sort metadata to ensure consistent encoding. Metadata is sorted by key.
	// While keys must be unique, we sort by value if two keys match; this
	// ensures that the same value always gets encoded for duplicate keys.
	slices.SortFunc(entry.Metadata, func(a, b push.LabelAdapter) int {
		if res := cmp.Compare(a.Name, b.Name); res != 0 {
			return res
		}
		return cmp.Compare(a.Value, b.Value)
	})

	// We ignore the errors below; they only fail if given out-of-order data
	// (where the row number is less than the previous row number), which can't
	// ever happen here.

	_ = l.streamIDs.Append(l.rows, dataset.Int64Value(entry.StreamID))
	_ = l.timestamps.Append(l.rows, dataset.Int64Value(entry.Timestamp.UnixNano()))
	_ = l.messages.Append(l.rows, dataset.StringValue(entry.Line))

	for _, m := range entry.Metadata {
		col := l.getMetadataColumn(m.Name)
		_ = col.Append(l.rows, dataset.StringValue(m.Value))
	}

	l.rows++
	l.metrics.recordCount.Inc()
}

// EstimatedSize returns the estimated size of the Logs section in bytes.
func (l *Logs) EstimatedSize() int {
	var size int

	size += l.streamIDs.EstimatedSize()
	size += l.timestamps.EstimatedSize()
	size += l.messages.EstimatedSize()

	for _, md := range l.metadatas {
		size += md.EstimatedSize()
	}

	return size
}

func (l *Logs) getMetadataColumn(key string) *dataset.ColumnBuilder {
	idx, ok := l.metadataLookup[key]
	if !ok {
		col, err := dataset.NewColumnBuilder(key, dataset.BuilderOptions{
			PageSizeHint: l.pageSize,
			Value:        datasetmd.VALUE_TYPE_STRING,
			Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
			Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		})
		if err != nil {
			// We control the Value/Encoding tuple so this can't fail; if it does,
			// we're left in an unrecoverable state where nothing can be encoded
			// properly so we panic.
			panic(fmt.Sprintf("creating metadata column: %v", err))
		}

		l.metadatas = append(l.metadatas, col)
		l.metadataLookup[key] = len(l.metadatas) - 1
		return col
	}
	return l.metadatas[idx]
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

	// TODO(rfratto): handle one section becoming too large. This can happen when
	// the number of columns is very wide, due to a lot of metadata columns.
	// There are two approaches to handle this:
	//
	// 1. Split streams into multiple sections.
	// 2. Move some columns into an aggregated column which holds multiple label
	//    keys and values.

	dset, err := l.buildDataset()
	if err != nil {
		return fmt.Errorf("building dataset: %w", err)
	}
	cols, err := result.Collect(dset.ListColumns(context.Background())) // dset is in memory; "real" context not needed.
	if err != nil {
		return fmt.Errorf("listing columns: %w", err)
	}

	logsEnc, err := enc.OpenLogs()
	if err != nil {
		return fmt.Errorf("opening logs section: %w", err)
	}
	defer func() {
		// Discard on defer for safety. This will return an error if we
		// successfully committed.
		_ = logsEnc.Discard()
	}()

	// Encode our columns. The slice order here *must* match the order in
	// [Logs.buildDataset]!
	{
		errs := make([]error, 0, len(cols))
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_STREAM_ID, cols[0]))
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_TIMESTAMP, cols[1]))
		for _, mdCol := range cols[2 : len(cols)-1] {
			errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_METADATA, mdCol))
		}
		errs = append(errs, encodeColumn(logsEnc, logsmd.COLUMN_TYPE_MESSAGE, cols[len(cols)-1]))
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("encoding columns: %w", err)
		}
	}

	return logsEnc.Commit()
}

func (l *Logs) buildDataset() (dataset.Dataset, error) {
	// Our columns are ordered as follows:
	//
	// 1. StreamID
	// 2. Timestamp
	// 3. Metadata columns
	// 4. Message
	//
	// Do *not* change this order without updating [Logs.EncodeTo]!
	//
	// TODO(rfratto): find a clean way to decorate columns with additional
	// metadata so we don't have to rely on order.
	columns := make([]*dataset.MemColumn, 0, 3+len(l.metadatas))

	// Flush never returns an error so we ignore it here to keep the code simple.
	//
	// TODO(rfratto): remove error return from Flush to clean up code.
	streamID, _ := l.streamIDs.Flush()
	timestamp, _ := l.timestamps.Flush()
	columns = append(columns, streamID, timestamp)

	for _, mdBuilder := range l.metadatas {
		mdBuilder.Backfill(l.rows)

		mdColumn, _ := mdBuilder.Flush()
		columns = append(columns, mdColumn)
	}

	messages, _ := l.messages.Flush()
	columns = append(columns, messages)

	// TODO(rfratto): We need to be able to sort the columns first by StreamID
	// and then by timestamp, but as it is now this is way too slow; sorting a
	// 20MB dataset took several minutes due to the number of page loads
	// happening across streams.
	//
	// Sorting can be made more efficient by:
	//
	// 1. Separating streams into separate datasets while appending
	// 2. Sorting each stream separately
	// 3. Combining sorted streams into a single dataset, which will already be
	//    sorted.
	return dataset.FromMemory(columns), nil
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
	l.rows = 0
	l.metrics.recordCount.Set(0)

	l.streamIDs.Reset()
	l.timestamps.Reset()
	l.metadatas = l.metadatas[:0]
	clear(l.metadataLookup)
	l.messages.Reset()
}
