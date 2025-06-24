package pointers

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/pointersmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
)

// A ObjPointer is an individual stream within a data object.
type ObjPointer struct {
	Path string

	// Stream index metadata
	StreamID         int64
	StreamIDRef      int64
	StartTs          time.Time
	EndTs            time.Time
	LineCount        int64
	UncompressedSize int64

	// Column index metadata
	// TODO: Write the blooms column by column somehow? Or at least sort them. We might need to do this to avoid downloading pages of blooms we don't need?
	Column            string
	Section           int64
	ValuesBloomFilter []byte
}

// Builder builds a streams section.
type Builder struct {
	metrics  *Metrics
	pageSize int
	// orderedStreams is used for consistently iterating over the list of
	// streams. It contains streamed added in append order.

	streamLookup     map[string]*ObjPointer
	streamObjectRefs map[string]map[int64]int64
	ordered          []*ObjPointer
}

// NewBuilder creates a new pointers section builder. The pageSize argument
// specifies how large pages should be.
func NewBuilder(metrics *Metrics, pageSize int) *Builder {
	if metrics == nil {
		metrics = NewMetrics()
	}
	return &Builder{
		metrics:  metrics,
		pageSize: pageSize,

		streamLookup:     make(map[string]*ObjPointer),
		streamObjectRefs: make(map[string]map[int64]int64),
		ordered:          make([]*ObjPointer, 0, 1024),
	}
}

// Type returns the [dataobj.SectionType] of the pointers builder.
func (b *Builder) Type() dataobj.SectionType { return sectionType }

// RecordStreamRef records a reference to a stream in a data object by storing the mapping from the Logs object's stream ID to the Index object's stream ID.
func (b *Builder) RecordStreamRef(path string, idInObject int64, idInIndex int64) {
	pathRef, ok := b.streamObjectRefs[path]
	if !ok {
		pathRef = make(map[int64]int64)
		b.streamObjectRefs[path] = pathRef
	}
	pathRef[idInObject] = idInIndex
}

// ObserveStream observes a stream in the index by recording the start & end timestamps, line count, and uncompressed size per-section.
func (b *Builder) ObserveStream(path string, section int64, idInObject int64, ts time.Time, uncompressedSize int64) {
	indexStreamID := b.streamObjectRefs[path][idInObject]
	key := fmt.Sprintf("%s:%d:%d", path, section, indexStreamID)
	pointer, ok := b.streamLookup[key]
	if ok {
		// Update the existing pointer
		if ts.Before(pointer.StartTs) {
			pointer.StartTs = ts
		}
		if ts.After(pointer.EndTs) {
			pointer.EndTs = ts
		}
		pointer.LineCount++
		pointer.UncompressedSize += uncompressedSize
		return
	}

	newPointer := &ObjPointer{
		Path:             path,
		Section:          section,
		StreamID:         indexStreamID,
		StreamIDRef:      idInObject,
		StartTs:          ts,
		EndTs:            ts,
		LineCount:        1,
		UncompressedSize: uncompressedSize,
	}
	b.ordered = append(b.ordered, newPointer)
	b.streamLookup[key] = newPointer
}

func (b *Builder) RecordColumnIndex(path string, section int64, column string, valuesBloomFilter []byte) {
	newPointer := &ObjPointer{
		Path:              path,
		Column:            column,
		Section:           section,
		ValuesBloomFilter: valuesBloomFilter,
	}
	b.ordered = append(b.ordered, newPointer)
}

// EstimatedSize returns the estimated size of the Pointers section in bytes.
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
	sizeEstimate += len(b.ordered) * 20                 // All labels (2x compression ratio)

	return sizeEstimate
}

// Flush flushes the streams section to the provided writer.
//
// After successful encoding, b is reset to a fresh state and can be reused.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	timer := prometheus.NewTimer(b.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	b.sortPointerObjects()

	var pointersEnc encoder
	defer pointersEnc.Reset()
	if err := b.encodeTo(&pointersEnc); err != nil {
		return 0, fmt.Errorf("building encoder: %w", err)
	}

	n, err = pointersEnc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}

// sortPointerObjects sorts the pointers so all the column indexes are together and all the stream indexes are ordered by StreamID then Timestamp.
func (b *Builder) sortPointerObjects() {
	sort.Slice(b.ordered, func(i, j int) bool {
		if b.ordered[i].StreamID == 0 && b.ordered[j].StreamID == 0 {
			// A column index
			if b.ordered[i].Column == b.ordered[j].Column {
				return b.ordered[i].Section < b.ordered[j].Section
			}
			return b.ordered[i].Column < b.ordered[j].Column
		} else if b.ordered[i].StreamID != 0 && b.ordered[j].StreamID != 0 {
			// A stream
			if b.ordered[i].StartTs.Equal(b.ordered[j].StartTs) {
				return b.ordered[i].EndTs.Before(b.ordered[j].EndTs)
			}
			return b.ordered[i].StartTs.Before(b.ordered[j].StartTs)
		}
		// They're different, just make sure all the streams are separate from the columns
		return b.ordered[i].StreamID < b.ordered[j].StreamID
	})
}

func (b *Builder) encodeTo(enc *encoder) error {
	// TODO(rfratto): handle one section becoming too large. This can happen when
	// the number of columns is very wide. There are two approaches to handle
	// this:
	//
	// 1. Split streams into multiple sections.
	// 2. Move some columns into an aggregated column which holds multiple label
	//    keys and values.

	pathBuilder, err := dataset.NewColumnBuilder("path", dataset.BuilderOptions{
		PageSizeHint: b.pageSize,
		Value:        datasetmd.VALUE_TYPE_BYTE_ARRAY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		return fmt.Errorf("creating path column: %w", err)
	}
	sectionBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating section column: %w", err)
	}

	// Stream info
	idBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating ID column: %w", err)
	}
	streamIDRefBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating stream ID in object column: %w", err)
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

	// Column index info
	columnNameBuilder, err := dataset.NewColumnBuilder("column_name", dataset.BuilderOptions{
		PageSizeHint: b.pageSize,
		Value:        datasetmd.VALUE_TYPE_BYTE_ARRAY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
	})
	if err != nil {
		return fmt.Errorf("creating column name column: %w", err)
	}

	valuesBloomFilterBuilder, err := dataset.NewColumnBuilder("values_bloom_filter", dataset.BuilderOptions{
		PageSizeHint: b.pageSize,
		Value:        datasetmd.VALUE_TYPE_BYTE_ARRAY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE, // TODO: Is there any point compression a bloom?
	})
	if err != nil {
		return fmt.Errorf("creating values bloom filter column: %w", err)
	}

	// Populate our column builders.
	for i, pointer := range b.ordered {
		_ = pathBuilder.Append(i, dataset.ByteArrayValue([]byte(pointer.Path)))
		_ = sectionBuilder.Append(i, dataset.Int64Value(pointer.Section))

		if pointer.StreamID != 0 {
			// Append only fails if the rows are out-of-order, which can't happen here.
			_ = idBuilder.Append(i, dataset.Int64Value(pointer.StreamID))
			_ = streamIDRefBuilder.Append(i, dataset.Int64Value(pointer.StreamIDRef))
			_ = minTimestampBuilder.Append(i, dataset.Int64Value(pointer.StartTs.UnixNano()))
			_ = maxTimestampBuilder.Append(i, dataset.Int64Value(pointer.EndTs.UnixNano()))
			_ = rowsCountBuilder.Append(i, dataset.Int64Value(pointer.LineCount))
			_ = uncompressedSizeBuilder.Append(i, dataset.Int64Value(pointer.UncompressedSize))
		}

		if pointer.Column != "" {
			_ = columnNameBuilder.Append(i, dataset.ByteArrayValue([]byte(pointer.Column)))
			_ = valuesBloomFilterBuilder.Append(i, dataset.ByteArrayValue(pointer.ValuesBloomFilter))
		}
	}

	// Encode our builders to sections. We ignore errors after enc.OpenStreams
	// (which may fail due to a caller) since we guarantee correct usage of the
	// encoding API.
	{
		var errs []error
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_PATH, pathBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_SECTION, sectionBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_STREAM_ID, idBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_STREAM_ID_REF, streamIDRefBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_MIN_TIMESTAMP, minTimestampBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_MAX_TIMESTAMP, maxTimestampBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_ROWS, rowsCountBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_UNCOMPRESSED_SIZE, uncompressedSizeBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_COLUMN_NAME, columnNameBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_VALUES_BLOOM_FILTER, valuesBloomFilterBuilder))

		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("encoding columns: %w", err)
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

func encodeColumn(enc *encoder, columnType pointersmd.ColumnType, builder *dataset.ColumnBuilder) error {
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
	b.ordered = sliceclear.Clear(b.ordered)
	clear(b.streamLookup)
	clear(b.streamObjectRefs)

	b.metrics.minTimestamp.Set(0)
	b.metrics.maxTimestamp.Set(0)
}
