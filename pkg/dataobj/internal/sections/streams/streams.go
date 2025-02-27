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

	Labels           labels.Labels // Stream labels.
	MinTimestamp     time.Time     // Minimum timestamp in the stream.
	MaxTimestamp     time.Time     // Maximum timestamp in the stream.
	UncompressedSize int64         // Uncompressed size of the log lines and structured metadata values in the stream.
	Rows             int           // Number of rows in the stream.
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

// Streams tracks information about streams in a data object.
type Streams struct {
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

// New creates a new Streams section. The pageSize argument specifies how large
// pages should be.
func New(metrics *Metrics, pageSize int) *Streams {
	if metrics == nil {
		metrics = NewMetrics()
	}
	return &Streams{
		metrics:  metrics,
		pageSize: pageSize,
		lookup:   make(map[uint64][]*Stream, 1024),
		ordered:  make([]*Stream, 0, 1024),
	}
}

// TimeRange returns the minimum and maximum timestamp across all streams.
func (s *Streams) TimeRange() (time.Time, time.Time) {
	return s.globalMinTimestamp, s.globalMaxTimestamp
}

// Record a stream record within the Streams section. The provided timestamp is
// used to track the minimum and maximum timestamp of a stream. The number of
// calls to Record is used to track the number of rows for a stream.
// The recordSize is used to track the uncompressed size of the stream.
//
// The stream ID of the recorded stream is returned.
func (s *Streams) Record(streamLabels labels.Labels, ts time.Time, recordSize int64) int64 {
	ts = ts.UTC()
	s.observeRecord(ts)

	stream := s.getOrAddStream(streamLabels)
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

func (s *Streams) observeRecord(ts time.Time) {
	s.metrics.recordsTotal.Inc()

	if ts.Before(s.globalMinTimestamp) || s.globalMinTimestamp.IsZero() {
		s.globalMinTimestamp = ts
		s.metrics.minTimestamp.Set(float64(ts.Unix()))
	}
	if ts.After(s.globalMaxTimestamp) || s.globalMaxTimestamp.IsZero() {
		s.globalMaxTimestamp = ts
		s.metrics.maxTimestamp.Set(float64(ts.Unix()))
	}
}

// EstimatedSize returns the estimated size of the Streams section in bytes.
func (s *Streams) EstimatedSize() int {
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

	sizeEstimate += len(s.ordered) * idDeltaSize        // ID
	sizeEstimate += len(s.ordered) * timestampDeltaSize // Min timestamp
	sizeEstimate += len(s.ordered) * timestampDeltaSize // Max timestamp
	sizeEstimate += len(s.ordered) * rowDeltaSize       // Rows
	sizeEstimate += s.currentLabelsSize / 2             // All labels (2x compression ratio)

	return sizeEstimate
}

func (s *Streams) getOrAddStream(streamLabels labels.Labels) *Stream {
	hash := streamLabels.Hash()
	matches, ok := s.lookup[hash]
	if !ok {
		return s.addStream(hash, streamLabels)
	}

	for _, stream := range matches {
		if labels.Equal(stream.Labels, streamLabels) {
			return stream
		}
	}

	return s.addStream(hash, streamLabels)
}

func (s *Streams) addStream(hash uint64, streamLabels labels.Labels) *Stream {
	// Ensure streamLabels are sorted prior to adding to ensure consistent column
	// ordering.
	sort.Sort(streamLabels)

	for _, lbl := range streamLabels {
		s.currentLabelsSize += len(lbl.Value)
	}

	newStream := streamPool.Get().(*Stream)
	newStream.Reset()
	newStream.ID = s.lastID.Add(1)
	newStream.Labels = streamLabels

	s.lookup[hash] = append(s.lookup[hash], newStream)
	s.ordered = append(s.ordered, newStream)
	s.metrics.streamCount.Inc()
	return newStream
}

// StreamID returns the stream ID for the provided streamLabels. If the stream
// has not been recorded, StreamID returns 0.
func (s *Streams) StreamID(streamLabels labels.Labels) int64 {
	hash := streamLabels.Hash()
	matches, ok := s.lookup[hash]
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

// EncodeTo encodes the list of recorded streams to the provided encoder.
//
// EncodeTo may generate multiple sections if the list of streams is too big to
// fit into a single section.
//
// [Streams.Reset] is invoked after encoding, even if encoding fails.
func (s *Streams) EncodeTo(enc *encoding.Encoder) error {
	timer := prometheus.NewTimer(s.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	// TODO(rfratto): handle one section becoming too large. This can happen when
	// the number of columns is very wide. There are two approaches to handle
	// this:
	//
	// 1. Split streams into multiple sections.
	// 2. Move some columns into an aggregated column which holds multiple label
	//    keys and values.

	idBuilder, err := numberColumnBuilder(s.pageSize)
	if err != nil {
		return fmt.Errorf("creating ID column: %w", err)
	}
	minTimestampBuilder, err := numberColumnBuilder(s.pageSize)
	if err != nil {
		return fmt.Errorf("creating minimum timestamp column: %w", err)
	}
	maxTimestampBuilder, err := numberColumnBuilder(s.pageSize)
	if err != nil {
		return fmt.Errorf("creating maximum timestamp column: %w", err)
	}
	rowsCountBuilder, err := numberColumnBuilder(s.pageSize)
	if err != nil {
		return fmt.Errorf("creating rows column: %w", err)
	}
	uncompressedSizeBuilder, err := numberColumnBuilder(s.pageSize)
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
			PageSizeHint: s.pageSize,
			Value:        datasetmd.VALUE_TYPE_STRING,
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
	for i, stream := range s.ordered {
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
			_ = builder.Append(i, dataset.StringValue(label.Value))
		}
	}

	// Encode our builders to sections. We ignore errors after enc.OpenStreams
	// (which may fail due to a caller) since we guarantee correct usage of the
	// encoding API.
	streamsEnc, err := enc.OpenStreams()
	if err != nil {
		return fmt.Errorf("opening streams section: %w", err)
	}
	defer func() {
		// Discard on defer for safety. This will return an error if we
		// successfully committed.
		_ = streamsEnc.Discard()
	}()

	{
		var errs []error
		errs = append(errs, encodeColumn(streamsEnc, streamsmd.COLUMN_TYPE_STREAM_ID, idBuilder))
		errs = append(errs, encodeColumn(streamsEnc, streamsmd.COLUMN_TYPE_MIN_TIMESTAMP, minTimestampBuilder))
		errs = append(errs, encodeColumn(streamsEnc, streamsmd.COLUMN_TYPE_MAX_TIMESTAMP, maxTimestampBuilder))
		errs = append(errs, encodeColumn(streamsEnc, streamsmd.COLUMN_TYPE_ROWS, rowsCountBuilder))
		errs = append(errs, encodeColumn(streamsEnc, streamsmd.COLUMN_TYPE_UNCOMPRESSED_SIZE, uncompressedSizeBuilder))
		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("encoding columns: %w", err)
		}
	}

	for _, labelBuilder := range labelBuilders {
		// For consistency we'll make sure each label builder has the same number
		// of rows as the other columns (which is the number of streams).
		labelBuilder.Backfill(len(s.ordered))

		err := encodeColumn(streamsEnc, streamsmd.COLUMN_TYPE_LABEL, labelBuilder)
		if err != nil {
			return fmt.Errorf("encoding label column: %w", err)
		}
	}

	return streamsEnc.Commit()
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

func encodeColumn(enc *encoding.StreamsEncoder, columnType streamsmd.ColumnType, builder *dataset.ColumnBuilder) error {
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
func (s *Streams) Reset() {
	s.lastID.Store(0)
	for _, stream := range s.ordered {
		streamPool.Put(stream)
	}
	clear(s.lookup)
	s.ordered = sliceclear.Clear(s.ordered)
	s.currentLabelsSize = 0
	s.globalMinTimestamp = time.Time{}
	s.globalMaxTimestamp = time.Time{}

	s.metrics.streamCount.Set(0)
	s.metrics.minTimestamp.Set(0)
	s.metrics.maxTimestamp.Set(0)
}
