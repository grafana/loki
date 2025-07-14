package indexpointers

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/indexpointersmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
)

// IndexPointer is a pointer to an index object. It is used to lookup the index object
// by data object path within a time range.
//
// The path is the data object path, the start and end timestamps are the time range of data stored in the index object.
type IndexPointer struct {
	Path    string
	StartTs time.Time
	EndTs   time.Time
}

type Builder struct {
	metrics  *Metrics
	pageSize int

	indexPointers []*IndexPointer
}

func NewBuilder(metrics *Metrics, pageSize int) *Builder {
	if metrics == nil {
		metrics = NewMetrics()
	}
	return &Builder{
		metrics:       metrics,
		pageSize:      pageSize,
		indexPointers: make([]*IndexPointer, 0, 1024),
	}
}

func (b *Builder) Type() dataobj.SectionType { return sectionType }

// Append adds a new index pointer to the builder.
func (b *Builder) Append(path string, startTs time.Time, endTs time.Time) {
	b.indexPointers = append(b.indexPointers, &IndexPointer{
		Path:    path,
		StartTs: startTs,
		EndTs:   endTs,
	})
}

// EstimatedSize returns the estimated size of the Pointers section in bytes.
func (b *Builder) EstimatedSize() int {
	// Since columns are only built when encoding, we can't use
	// [dataset.ColumnBuilder.EstimatedSize] here.
	//
	// Instead, we use a basic heuristic, estimating delta encoding and
	// compression:
	//
	// 1. Assume an average path length of 50 bytes with 3x compression ratio.
	// 2. Assume a timestamp delta of 1 hour (3600 seconds).
	// 3. Account for column metadata overhead.

	if len(b.indexPointers) == 0 {
		return 0
	}

	var (
		avgPathLength        = 85                                    // Average path length in bytes (based on real data)
		pathCompressionRatio = 3                                     // ZSTD compression ratio
		timestampDeltaSize   = streamio.VarintSize(int64(time.Hour)) // 1 hour delta
		metadataOverhead     = 100                                   // Estimated metadata overhead per column
	)

	var sizeEstimate int

	// Path column (byte arrays with ZSTD compression)
	sizeEstimate += (len(b.indexPointers) * avgPathLength) / pathCompressionRatio

	// Start timestamp column (int64 with delta encoding)
	sizeEstimate += len(b.indexPointers) * timestampDeltaSize

	// End timestamp column (int64 with delta encoding)
	sizeEstimate += len(b.indexPointers) * timestampDeltaSize

	// Column metadata overhead (3 columns)
	sizeEstimate += 3 * metadataOverhead

	return sizeEstimate
}

// Flush flushes the streams section to the provided writer.
//
// After successful encoding, b is reset to a fresh state and can be reused.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	timer := prometheus.NewTimer(b.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	b.sortIndexPointers()

	var enc encoder
	defer enc.Reset()
	if err := b.encodeTo(&enc); err != nil {
		return 0, fmt.Errorf("building encoder: %w", err)
	}

	n, err = enc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}

func (b *Builder) sortIndexPointers() {
	sort.Slice(b.indexPointers, func(i, j int) bool {
		return b.indexPointers[i].StartTs.Before(b.indexPointers[j].StartTs) && b.indexPointers[i].EndTs.Before(b.indexPointers[j].StartTs)
	})
}

// Reset resets all state, allowing IndexPointers to be reused.
func (b *Builder) Reset() {
	b.indexPointers = sliceclear.Clear(b.indexPointers)

	b.metrics.minTimestamp.Set(0)
	b.metrics.maxTimestamp.Set(0)
}

func (b *Builder) encodeTo(enc *encoder) error {
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

	minTimestampBuilder, err := dataset.NewColumnBuilder("min_timestamp", dataset.BuilderOptions{
		PageSizeHint: b.pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		return fmt.Errorf("creating min timestamp column: %w", err)
	}

	maxTimestampBuilder, err := dataset.NewColumnBuilder("max_timestamp", dataset.BuilderOptions{
		PageSizeHint: b.pageSize,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		return fmt.Errorf("creating max timestamp column: %w", err)
	}

	for i, pointer := range b.indexPointers {
		_ = pathBuilder.Append(i, dataset.ByteArrayValue([]byte(pointer.Path)))
		_ = minTimestampBuilder.Append(i, dataset.Int64Value(pointer.StartTs.UnixNano()))
		_ = maxTimestampBuilder.Append(i, dataset.Int64Value(pointer.EndTs.UnixNano()))
	}

	// Encode our builders to sections. We ignore errors after enc.OpenStreams
	// (which may fail due to a caller) since we guarantee correct usage of the
	// encoding API.
	{
		var errs []error
		errs = append(errs, encodeColumn(enc, indexpointersmd.COLUMN_TYPE_PATH, pathBuilder))
		errs = append(errs, encodeColumn(enc, indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP, minTimestampBuilder))
		errs = append(errs, encodeColumn(enc, indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP, maxTimestampBuilder))

		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("encoding columns: %w", err)
		}
	}

	return nil
}

func encodeColumn(enc *encoder, columnType indexpointersmd.ColumnType, builder *dataset.ColumnBuilder) error {
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
