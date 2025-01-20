// Package logs defines types used for the data object logs section. The logs
// section holds a list of log records across multiple streams.
package logs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
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
	pageSize int

	// To put less work on dataset.Sort, we internally accumulate records across
	// streams separately. Once we're building the final dataset, we combine
	// individual streams in order to our final set of columns.

	streams map[int64]*stream
}

// Nwe creates a new Logs section. The pageSize argument specifies how large
// pages should be.
func New(metrics *Metrics, pageSize int) *Logs {
	if metrics == nil {
		metrics = NewMetrics()
	}

	return &Logs{
		metrics:  metrics,
		pageSize: pageSize,

		streams: make(map[int64]*stream),
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

	stream := l.getStream(entry.StreamID)
	stream.Append(entry)

	l.metrics.recordCount.Inc()
}

func (l *Logs) getStream(streamID int64) *stream {
	if stream, ok := l.streams[streamID]; ok {
		return stream
	}

	stream := newStream(streamID, l.pageSize)
	l.streams[streamID] = stream
	return stream
}

// EstimatedSize returns the estimated size of the Logs section in bytes.
func (l *Logs) EstimatedSize() int {
	var size int
	for _, s := range l.streams {
		size += s.EstimatedSize()
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

	// TODO(rfratto): handle one section becoming too large. This can happen when
	// the number of columns is very wide, due to a lot of metadata columns.
	// There are two approaches to handle this:
	//
	// 1. Split logs across multiple sections.
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
	streamIDBuilder, release, err := getColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: l.pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		return nil, fmt.Errorf("creating stream ID column: %w", err)
	}
	defer release()

	timestampBuilder, release, err := getColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: l.pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		return nil, fmt.Errorf("creating timestamp column: %w", err)
	}
	defer release()

	var (
		metadataReleasers = []func(){}
		metadataBuilders  = make(map[string]*dataset.ColumnBuilder)
	)
	getMetadataBuilder := func(key string) (*dataset.ColumnBuilder, error) {
		if col, ok := metadataBuilders[key]; ok {
			return col, nil
		}

		col, release, err := getColumnBuilder(key, dataset.BuilderOptions{
			PageSizeHint: l.pageSize,
			Value:        datasetmd.VALUE_TYPE_STRING,
			Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
			Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		})
		if err != nil {
			return nil, fmt.Errorf("creating metadata column: %w", err)
		}
		metadataReleasers = append(metadataReleasers, release)

		metadataBuilders[key] = col
		return col, nil
	}
	defer func() {
		for _, release := range metadataReleasers {
			release()
		}
	}()

	messageBuilder, release, err := getColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: l.pageSize,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
	})
	if err != nil {
		return nil, fmt.Errorf("creating messages column: %w", err)
	}
	defer release()

	var rows int

	for stream := range l.orderedStreams() {
		streamDataset, err := stream.Build()
		if err != nil {
			return nil, fmt.Errorf("building stream %d dataset: %w", stream.id, err)
		}

		columns, err := result.Collect(streamDataset.ListColumns(context.Background()))
		if err != nil {
			return nil, fmt.Errorf("listing stream %d columns: %w", stream.id, err)
		} else if len(columns) < 2 {
			// There should be at least a timestamp and message column.
			return nil, fmt.Errorf("stream %d has %d columns, expected at least 2", stream.id, len(columns))
		}

		// Do basic validation of the values in a column; the first column should
		// be VALUE_TYPE_INT64 (timestamp), and every other column should be
		// VALUE_TYPE_STRING (metadata or message).
		if expect, actual := datasetmd.VALUE_TYPE_INT64, columns[0].ColumnInfo().Type; expect != actual {
			return nil, fmt.Errorf("stream %d column 0 should be %s, got %s", stream.id, expect, actual)
		}
		for i, col := range columns[1:] {
			if expect, actual := datasetmd.VALUE_TYPE_STRING, col.ColumnInfo().Type; expect != actual {
				return nil, fmt.Errorf("stream %d column %d should be %s, got %s", stream.id, i+1, expect, actual)
			}
		}

		// Column order in the stream is determined by [stream.build].
		for result := range dataset.Iter(context.Background(), columns) {
			row, err := result.Value()
			if err != nil {
				return nil, fmt.Errorf("getting stream %d column: %w", stream.id, err)
			} else if len(row.Values) != len(columns) {
				return nil, fmt.Errorf("stream %d column has %d values, expected %d", stream.id, len(row.Values), len(columns))
			}

			_ = streamIDBuilder.Append(rows, dataset.Int64Value(stream.id))
			_ = timestampBuilder.Append(rows, row.Values[0])

			for i, md := range row.Values[1 : len(row.Values)-1] {
				col, err := getMetadataBuilder(columns[i+1].ColumnInfo().Name)
				if err != nil {
					return nil, fmt.Errorf("getting metadata column: %w", err)
				}
				_ = col.Append(rows, md)
			}

			_ = messageBuilder.Append(rows, row.Values[len(row.Values)-1])
			rows++
		}
	}

	// Our columns in the final dataset are ordered as follows:
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
	columns := make([]*dataset.MemColumn, 0, 3+len(metadataBuilders))

	// Flush never returns an error so we ignore it here to keep the code simple.
	//
	// TODO(rfratto): remove error return from Flush to clean up code.
	streamIDs, _ := streamIDBuilder.Flush()
	timestamps, _ := timestampBuilder.Flush()
	columns = append(columns, streamIDs, timestamps)

	for metadataBuilder := range l.orderedBuilders(metadataBuilders) {
		metadataBuilder.Backfill(rows)

		mdColumn, _ := metadataBuilder.Flush()
		columns = append(columns, mdColumn)
	}

	messages, _ := messageBuilder.Flush()
	columns = append(columns, messages)

	// Our final dataset is already sorted; [stream.build] sorts each stream by
	// timestamp, and then we appended each stream in sorted order.
	//
	// This saves a significant amount of CPU time compared to appending logs
	// into a massive table and then sorting that massive table.
	return dataset.FromMemory(columns), nil
}

func (l *Logs) orderedStreams() iter.Seq[*stream] {
	ids := slices.Collect(maps.Keys(l.streams))
	slices.Sort(ids)

	return func(yield func(*stream) bool) {
		for _, streamID := range ids {
			if !yield(l.streams[streamID]) {
				return
			}
		}
	}
}

func (l *Logs) orderedBuilders(builders map[string]*dataset.ColumnBuilder) iter.Seq[*dataset.ColumnBuilder] {
	keys := slices.Collect(maps.Keys(builders))
	slices.Sort(keys)

	return func(yield func(*dataset.ColumnBuilder) bool) {
		for _, key := range keys {
			if !yield(builders[key]) {
				return
			}
		}
	}
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

	for _, stream := range l.streams {
		stream.Close()
	}
	clear(l.streams)
}
