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

// A SectionPointer is a pointer to an section within another object.
// It is a wide object containing two types of index information:
//
// 1. Stream indexing metadata
// 2. Column indexing metadata
//
// The stream indexing metadata is used to lookup which stream is in the referenced section, and their ID within the object.
// The column indexing metadata is used to lookup which column values are present in the referenced section.
// Path & Section are mandatory fields, and are used to uniquely identify the section within the referenced object.
type SectionPointer struct {
	Path        string
	Section     int64
	PointerKind PointerKind

	// Stream indexing metadata
	StreamID         int64
	StreamIDRef      int64
	StartTs          time.Time
	EndTs            time.Time
	LineCount        int64
	UncompressedSize int64

	// Column indexing metadata
	ColumnIndex       int64
	ColumnName        string
	ValuesBloomFilter []byte
}

type PointerKind int

const (
	PointerKindInvalid     PointerKind = iota // PointerKindInvalid is an invalid pointer kind.
	PointerKindStreamIndex                    // PointerKindStreamIndex is a pointer for a stream index.
	PointerKindColumnIndex                    // PointerKindColumnIndex is a pointer for a column index.
)

// Builder builds a pointers section.
type Builder struct {
	metrics  *Metrics
	pageSize int

	// streamLookup is a map of the stream ID in this index object to the pointer.
	streamLookup map[string]*SectionPointer
	// streamObjectRefs is a map of the stream ID in the referenced logs object to the stream ID in this index object.
	streamObjectRefs map[string]map[int64]int64
	// pointers is the list of pointers to encode.
	pointers []*SectionPointer
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

		streamLookup:     make(map[string]*SectionPointer),
		streamObjectRefs: make(map[string]map[int64]int64),
		pointers:         make([]*SectionPointer, 0, 1024),
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

	newPointer := &SectionPointer{
		Path:             path,
		Section:          section,
		PointerKind:      PointerKindStreamIndex,
		StreamID:         indexStreamID,
		StreamIDRef:      idInObject,
		StartTs:          ts,
		EndTs:            ts,
		LineCount:        1,
		UncompressedSize: uncompressedSize,
	}
	b.pointers = append(b.pointers, newPointer)
	b.streamLookup[key] = newPointer
}

func (b *Builder) RecordColumnIndex(path string, section int64, columnName string, columnIndex int64, valuesBloomFilter []byte) {
	newPointer := &SectionPointer{
		Path:              path,
		Section:           section,
		PointerKind:       PointerKindColumnIndex,
		ColumnName:        columnName,
		ColumnIndex:       columnIndex,
		ValuesBloomFilter: valuesBloomFilter,
	}
	b.pointers = append(b.pointers, newPointer)
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
	// 2. Assume a timestamp delta of 10m.
	// 3. Assume a row count delta of 500.
	// 4. Assume a 10kb uncompressed size delta.
	// 5. Assume 5 bytes per column name with a 2x compression ratio.
	// 6. Assume 50 bytes per column bloom.
	// 7. Assume 10% of the pointers are column indexes.

	var (
		idDeltaSize        = streamio.VarintSize(1)
		timestampDeltaSize = streamio.VarintSize(10 * int64(time.Minute))
		rowDeltaSize       = streamio.VarintSize(500)
		bytesDeltaSize     = streamio.VarintSize(10000)
		streamIndexCount   = int(float64(len(b.streamLookup)) * 0.9)
		columnIndexCount   = int(float64(len(b.pointers)) * 0.1)
	)

	var sizeEstimate int

	sizeEstimate += len(b.pointers) * idDeltaSize * 2     // ID + StreamIDRef
	sizeEstimate += streamIndexCount * timestampDeltaSize // Min timestamp
	sizeEstimate += streamIndexCount * timestampDeltaSize // Max timestamp
	sizeEstimate += streamIndexCount * rowDeltaSize       // Rows
	sizeEstimate += streamIndexCount * bytesDeltaSize     // Uncompressed size
	sizeEstimate += columnIndexCount * 5                  // Column name (2x compression ratio)
	sizeEstimate += columnIndexCount * 50                 // Column bloom

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
	sort.Slice(b.pointers, func(i, j int) bool {
		if b.pointers[i].StreamID == 0 && b.pointers[j].StreamID == 0 {
			// A column index
			if b.pointers[i].ColumnIndex == b.pointers[j].ColumnIndex {
				return b.pointers[i].Section < b.pointers[j].Section
			}
			return b.pointers[i].ColumnIndex < b.pointers[j].ColumnIndex
		} else if b.pointers[i].StreamID != 0 && b.pointers[j].StreamID != 0 {
			// A stream
			if b.pointers[i].StartTs.Equal(b.pointers[j].StartTs) {
				return b.pointers[i].EndTs.Before(b.pointers[j].EndTs)
			}
			return b.pointers[i].StartTs.Before(b.pointers[j].StartTs)
		}
		// They're different, just make sure all the streams are separate from the columns
		return b.pointers[i].StreamID < b.pointers[j].StreamID
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
	pointerKindBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating pointer kind column: %w", err)
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
	rowCountBuilder, err := numberColumnBuilder(b.pageSize)
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
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		return fmt.Errorf("creating column name column: %w", err)
	}

	columnIndexBuilder, err := numberColumnBuilder(b.pageSize)
	if err != nil {
		return fmt.Errorf("creating column index column: %w", err)
	}

	valuesBloomFilterBuilder, err := dataset.NewColumnBuilder("values_bloom_filter", dataset.BuilderOptions{
		PageSizeHint: b.pageSize,
		Value:        datasetmd.VALUE_TYPE_BYTE_ARRAY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE, // TODO: is there a sensible compression algorithm for bloom filters?
	})
	if err != nil {
		return fmt.Errorf("creating values bloom filter column: %w", err)
	}

	// Populate our column builders.
	for i, pointer := range b.pointers {
		_ = pathBuilder.Append(i, dataset.ByteArrayValue([]byte(pointer.Path)))
		_ = sectionBuilder.Append(i, dataset.Int64Value(pointer.Section))
		_ = pointerKindBuilder.Append(i, dataset.Int64Value(int64(pointer.PointerKind)))

		if pointer.PointerKind == PointerKindStreamIndex {
			// Append only fails if the rows are out-of-order, which can't happen here.
			_ = idBuilder.Append(i, dataset.Int64Value(pointer.StreamID))
			_ = streamIDRefBuilder.Append(i, dataset.Int64Value(pointer.StreamIDRef))
			_ = minTimestampBuilder.Append(i, dataset.Int64Value(pointer.StartTs.UnixNano()))
			_ = maxTimestampBuilder.Append(i, dataset.Int64Value(pointer.EndTs.UnixNano()))
			_ = rowCountBuilder.Append(i, dataset.Int64Value(pointer.LineCount))
			_ = uncompressedSizeBuilder.Append(i, dataset.Int64Value(pointer.UncompressedSize))
		}

		if pointer.PointerKind == PointerKindColumnIndex {
			_ = columnNameBuilder.Append(i, dataset.ByteArrayValue([]byte(pointer.ColumnName)))
			_ = columnIndexBuilder.Append(i, dataset.Int64Value(pointer.ColumnIndex))
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
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_POINTER_KIND, pointerKindBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_STREAM_ID, idBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_STREAM_ID_REF, streamIDRefBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_MIN_TIMESTAMP, minTimestampBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_MAX_TIMESTAMP, maxTimestampBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_ROW_COUNT, rowCountBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_UNCOMPRESSED_SIZE, uncompressedSizeBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_COLUMN_NAME, columnNameBuilder))
		errs = append(errs, encodeColumn(enc, pointersmd.COLUMN_TYPE_COLUMN_INDEX, columnIndexBuilder))
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
	b.pointers = sliceclear.Clear(b.pointers)
	clear(b.streamLookup)
	clear(b.streamObjectRefs)

	b.metrics.minTimestamp.Set(0)
	b.metrics.maxTimestamp.Set(0)
}
