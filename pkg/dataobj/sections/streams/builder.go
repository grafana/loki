// Package streams defines types used for the data object streams section. The
// streams section holds a list of streams present in the data object.
package streams

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
)

// A Stream is an individual stream within a data object.
type Stream struct {
	// ID to uniquely represent a stream in a data object. Valid IDs start at 1.
	// IDs are used to track streams across multiple sections in the same data
	// object.
	ID int64

	// MinTime and MaxTime denote the range of timestamps across all entries in
	// the stream.
	MinTimestamp, MaxTimestamp time.Time // Minimum timestamp in the stream.

	// Uncompressed size of the log lines and structured metadata values in the stream.
	UncompressedSize int64

	// Labels of the stream.
	Labels labels.Labels //

	// Total number of log records in the stream.
	Rows int

	LbValueCaps []int // Capacities for each label value's byte array
}

// Reset zeroes all values in the stream struct so it can be reused.
func (s *Stream) Reset() {
	s.ID = 0
	s.Labels = nil
	s.MinTimestamp = time.Time{}
	s.MaxTimestamp = time.Time{}
	s.UncompressedSize = 0
	s.Rows = 0
}

var streamPool = sync.Pool{
	New: func() interface{} {
		return &Stream{}
	},
}

// Builder builds a streams section.
type Builder struct {
	metrics  *Metrics
	pageSize int
	lastID   atomic.Int64
	lookup   map[uint64][]*Stream

	// Size of all label values across all streams; used for
	// [Streams.EstimatedSize]. Resets on [Streams.Reset].
	currentLabelsSize int

	globalMinTimestamp time.Time // Minimum timestamp across all streams, used for metrics.
	globalMaxTimestamp time.Time // Maximum timestamp across all streams, used for metrics.

	// orderedStreams is used for consistently iterating over the list of
	// streams. It contains streamed added in append order.
	ordered []*Stream
}

// NewBuilder creates a new sterams section builder. The pageSize argument
// specifies how large pages should be.
func NewBuilder(metrics *Metrics, pageSize int) *Builder {
	if metrics == nil {
		metrics = NewMetrics()
	}
	return &Builder{
		metrics:  metrics,
		pageSize: pageSize,
		lookup:   make(map[uint64][]*Stream, 1024),
		ordered:  make([]*Stream, 0, 1024),
	}
}

// TimeRange returns the minimum and maximum timestamp across all streams.
func (b *Builder) TimeRange() (time.Time, time.Time) {
	return b.globalMinTimestamp, b.globalMaxTimestamp
}

// Record a stream record within the section. The provided timestamp is used to
// track the minimum and maximum timestamp of a stream. The number of calls to
// Record is used to track the number of rows for a stream. The recordSize is
// used to track the uncompressed size of the stream.
//
// The stream ID of the recorded stream is returned.
func (b *Builder) Record(streamLabels labels.Labels, ts time.Time, recordSize int64) int64 {
	ts = ts.UTC()
	b.observeRecord(ts)

	stream := b.getOrAddStream(streamLabels)
	if stream.MinTimestamp.IsZero() || ts.Before(stream.MinTimestamp) {
		stream.MinTimestamp = ts
	}
	if stream.MaxTimestamp.IsZero() || ts.After(stream.MaxTimestamp) {
		stream.MaxTimestamp = ts
	}
	stream.Rows++
	stream.UncompressedSize += recordSize

	return stream.ID
}

func (b *Builder) observeRecord(ts time.Time) {
	b.metrics.recordsTotal.Inc()

	if ts.Before(b.globalMinTimestamp) || b.globalMinTimestamp.IsZero() {
		b.globalMinTimestamp = ts
		b.metrics.minTimestamp.Set(float64(ts.Unix()))
	}
	if ts.After(b.globalMaxTimestamp) || b.globalMaxTimestamp.IsZero() {
		b.globalMaxTimestamp = ts
		b.metrics.maxTimestamp.Set(float64(ts.Unix()))
	}
}

// EstimatedSize returns the estimated size of the Streams section in bytes.
func (b *Builder) EstimatedSize() int {
	// Since columns are only built when encoding, we can't use
	// [dataset.ColumnBuilder.EstimatedSize] here.
	//
	// Instead, we use a basic heuristic, estimating delta encoding and
	// compression:
	//
	// 1. Assume an ID delta of 1.
	// 2. Assume a timestamp delta of 1s.
	// 3. Assume a row count delta of 500.
	// 4. Assume (conservative) 2x compression ratio of all label values.

	var (
		idDeltaSize        = streamio.VarintSize(1)
		timestampDeltaSize = streamio.VarintSize(int64(time.Second))
		rowDeltaSize       = streamio.VarintSize(500)
	)

	var sizeEstimate int

	sizeEstimate += len(b.ordered) * idDeltaSize        // ID
	sizeEstimate += len(b.ordered) * timestampDeltaSize // Min timestamp
	sizeEstimate += len(b.ordered) * timestampDeltaSize // Max timestamp
	sizeEstimate += len(b.ordered) * rowDeltaSize       // Rows
	sizeEstimate += b.currentLabelsSize / 2             // All labels (2x compression ratio)

	return sizeEstimate
}

func (b *Builder) getOrAddStream(streamLabels labels.Labels) *Stream {
	hash := streamLabels.Hash()
	matches, ok := b.lookup[hash]
	if !ok {
		return b.addStream(hash, streamLabels)
	}

	for _, stream := range matches {
		if labels.Equal(stream.Labels, streamLabels) {
			return stream
		}
	}

	return b.addStream(hash, streamLabels)
}

func (b *Builder) addStream(hash uint64, streamLabels labels.Labels) *Stream {
	// Ensure streamLabels are sorted prior to adding to ensure consistent column
	// ordering.
	sort.Sort(streamLabels)

	for _, lbl := range streamLabels {
		b.currentLabelsSize += len(lbl.Value)
	}

	newStream := streamPool.Get().(*Stream)
	newStream.Reset()
	newStream.ID = b.lastID.Add(1)
	newStream.Labels = streamLabels

	b.lookup[hash] = append(b.lookup[hash], newStream)
	b.ordered = append(b.ordered, newStream)
	b.metrics.streamCount.Inc()
	return newStream
}

// StreamID returns the stream ID for the provided streamLabels. If the stream
// has not been recorded, StreamID returns 0.
func (b *Builder) StreamID(streamLabels labels.Labels) int64 {
	hash := streamLabels.Hash()
	matches, ok := b.lookup[hash]
	if !ok {
		return 0
	}

	for _, stream := range matches {
		if labels.Equal(stream.Labels, streamLabels) {
			return stream.ID
		}
	}

	return 0
}

// Flush flushes the streams section to the provided writer.
//
// After successful encoding, b is reset to a fresh state and can be reused.
func (b *Builder) Flush(w encoding.SectionWriter) (n int64, err error) {
	timer := prometheus.NewTimer(b.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	var streamsEnc encoder
	defer streamsEnc.Reset()
	if err := b.encodeTo(&streamsEnc); err != nil {
		return 0, fmt.Errorf("building encoder: %w", err)
	}

	n, err = streamsEnc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}

// EncodeTo encodes the list of recorded streams to the provided encoder.
//
// EncodeTo may generate multiple sections if the list of streams is too big to
// fit into a single section.
func (b *Builder) EncodeTo(enc *encoding.Encoder) error {
	timer := prometheus.NewTimer(b.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	var streamsEnc encoder
	defer streamsEnc.Reset()
	if err := b.encodeTo(&streamsEnc); err != nil {
		return fmt.Errorf("building encoder: %w", err)
	}

	_, err := streamsEnc.EncodeTo(enc)
	return err
}

func (b *Builder) encodeTo(enc *encoder) error {
	// TODO(rfratto): handle one section becoming too large. This can happen when
	// the number of columns is very wide. There are two approaches to handle
	// this:
	//
	// 1. Split streams into multiple sections.
	// 2. Move some columns into an aggregated column which holds multiple label
	//    keys and values.

	idBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating ID column: %w", err)
	}
	minTimestampBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating minimum timestamp column: %w", err)
	}
	maxTimestampBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating maximum timestamp column: %w", err)
	}
	rowsCountBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating rows column: %w", err)
	}
	uncompressedSizeBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating uncompressed size column: %w", err)
	}

	var (
		labelBuilders      []*dataset.ColumnBuilder
		labelBuilderlookup = map[string]int{} // Name to index
	)

	getLabelColumn := func(name string) (*dataset.ColumnBuilder, error) {
		idx, ok := labelBuilderlookup[name]
		if ok {
			return labelBuilders[idx], nil
		}

		builder, err := dataset.NewColumnBuilder(name, dataset.BuilderOptions{
			PageSizeHint: b.pageSize,
			Value:        datasetmd.VALUE_TYPE_BYTE_ARRAY,
			Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
			Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
			Statistics: dataset.StatisticsOptions{
				StoreRangeStats: true,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("creating label column: %w", err)
		}

		labelBuilders = append(labelBuilders, builder)
		labelBuilderlookup[name] = len(labelBuilders) - 1
		return builder, nil
	}

	// Populate our column builders.
	for i, stream := range b.ordered {
		// Append only fails if the rows are out-of-order, which can't happen here.
		_ = idBuilder.Append(i, dataset.Int64Value(stream.ID))
		_ = minTimestampBuilder.Append(i, dataset.Int64Value(stream.MinTimestamp.UnixNano()))
		_ = maxTimestampBuilder.Append(i, dataset.Int64Value(stream.MaxTimestamp.UnixNano()))
		_ = rowsCountBuilder.Append(i, dataset.Int64Value(int64(stream.Rows)))
		_ = uncompressedSizeBuilder.Append(i, dataset.Int64Value(stream.UncompressedSize))

		for _, label := range stream.Labels {
			builder, err := getLabelColumn(label.Name)
			if err != nil {
				return fmt.Errorf("getting label column: %w", err)
			}
			_ = builder.Append(i, dataset.ByteArrayValue([]byte(label.Value)))
		}
	}

	// Encode our builders to sections. We ignore errors after enc.OpenStreams
	// (which may fail due to a caller) since we guarantee correct usage of the
	// encoding API.
	{
		var errs []error
		errs = append(errs, encodeColumn(enc, streamsmd.COLUMN_TYPE_STREAM_ID, idBuilder))
		errs = append(errs, encodeColumn(enc, streamsmd.COLUMN_TYPE_MIN_TIMESTAMP, minTimestampBuilder))
		errs = append(errs, encodeColumn(enc, streamsmd.COLUMN_TYPE_MAX_TIMESTAMP, maxTimestampBuilder))
		errs = append(errs, encodeColumn(enc, streamsmd.COLUMN_TYPE_ROWS, rowsCountBuilder))
		errs = append(errs, encodeColumn(enc, streamsmd.COLUMN_TYPE_UNCOMPRESSED_SIZE, uncompressedSizeBuilder))
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("encoding columns: %w", err)
		}
	}

	for _, labelBuilder := range labelBuilders {
		// For consistency we'll make sure each label builder has the same number
		// of rows as the other columns (which is the number of streams).
		labelBuilder.Backfill(len(b.ordered))

		err := encodeColumn(enc, streamsmd.COLUMN_TYPE_LABEL, labelBuilder)
		if err != nil {
			return fmt.Errorf("encoding label column: %w", err)
		}
	}

	return nil
}

func numberColumnBuilder(pageSize int) (*dataset.ColumnBuilder, error) {
	return dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint: pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
}

func encodeColumn(enc *encoder, columnType streamsmd.ColumnType, builder *dataset.ColumnBuilder) error {
	column, err := builder.Flush()
	if err != nil {
		return fmt.Errorf("flushing %s column: %w", columnType, err)
	}

	columnEnc, err := enc.OpenColumn(columnType, &column.Info)
	if err != nil {
		return fmt.Errorf("opening %s column encoder: %w", columnType, err)
	}
	defer func() {
		// Discard on defer for safety. This will return an error if we
		// successfully committed.
		_ = columnEnc.Discard()
	}()

	for _, page := range column.Pages {
		err := columnEnc.AppendPage(page)
		if err != nil {
			return fmt.Errorf("appending %s page: %w", columnType, err)
		}
	}

	return columnEnc.Commit()
}

// Reset resets all state, allowing Streams to be reused.
func (b *Builder) Reset() {
	b.lastID.Store(0)
	for _, stream := range b.ordered {
		streamPool.Put(stream)
	}
	clear(b.lookup)
	b.ordered = sliceclear.Clear(b.ordered)
	b.currentLabelsSize = 0
	b.globalMinTimestamp = time.Time{}
	b.globalMaxTimestamp = time.Time{}

	b.metrics.streamCount.Set(0)
	b.metrics.minTimestamp.Set(0)
	b.metrics.maxTimestamp.Set(0)
}
