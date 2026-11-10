package indexpointers

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	datasetmd_v2 "github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// IndexPointer is a pointer to an index object. It is used to lookup the index object
// by data object path within a time range.
//
// The path is the data object path, the start and end timestamps are the time range of data stored in the index object.
type IndexPointer struct {
	Path    string
	StartTs time.Time
	EndTs   time.Time

	// RunID is the run that holds the index object.
	RunID RunID

	// PostingKeys bounds the posting keys of the index object. The zero value
	// means the bounds are unknown, so the index object can hold any posting
	// key.
	PostingKeys postings.KeyRange

	// ShardBuckets is the shard layout of the tenant in the index object. It
	// must equal the layout that indexobj.TenantShardLayout reads from the
	// object, because readers trust it.
	ShardBuckets ShardBuckets
}

// RunID identifies a run: the index objects of one ToC that form one logical
// index. A RunID is unique within one ToC only, so two ToCs can use the same
// RunID for different runs.
//
// The zero RunID means no run, so the index object is a run of its own. Valid
// run IDs are positive.
type RunID int64

// ShardBuckets is the shard bucket count of the tenant's layout in one index
// object.
//
// The zero ShardBuckets means the layout is unknown, so a reader must read it
// from the index object. Every other value is a valid bucket count.
type ShardBuckets struct {
	count uint32
}

// NewShardBuckets returns the ShardBuckets of layout. It never returns the
// zero ShardBuckets, because every layout has at least one bucket.
func NewShardBuckets(layout streams.ShardLayout) ShardBuckets {
	return ShardBuckets{count: layout.Buckets()}
}

// Layout returns the shard layout of b. ok is false if b is the zero
// ShardBuckets.
func (b ShardBuckets) Layout() (layout streams.ShardLayout, ok bool) {
	if b.count == 0 {
		return streams.ShardLayout{}, false
	}
	return streams.MustShardLayoutFromBuckets(b.count), true
}

// TenantIndexPointer is meant to collectively hold IndexPointer with the ID of the tenant it belongs to.
type TenantIndexPointer struct {
	Tenant string
	IndexPointer
}

type Builder struct {
	metrics      *Metrics
	pageSize     int
	pageRowCount int
	tenant       string

	indexPointers []*IndexPointer

	// postingKeyBytes is the total length of the strings in the posting keys
	// of indexPointers. It keeps EstimatedSize constant time.
	postingKeyBytes int
}

func NewBuilder(metrics *Metrics, pageSize, pageRowCount int) *Builder {
	if metrics == nil {
		metrics = NewMetrics()
	}
	return &Builder{
		metrics:       metrics,
		pageSize:      pageSize,
		pageRowCount:  pageRowCount,
		indexPointers: make([]*IndexPointer, 0, 1024),
	}
}

func (b *Builder) SetTenant(tenant string) {
	b.tenant = tenant
}

func (b *Builder) Tenant() string { return b.tenant }

func (b *Builder) Type() dataobj.SectionType { return sectionType }

// Append adds a new index pointer to the builder.
func (b *Builder) Append(pointer IndexPointer) {
	pointer.StartTs = pointer.StartTs.UTC()
	pointer.EndTs = pointer.EndTs.UTC()
	b.indexPointers = append(b.indexPointers, &pointer)

	if keys := pointer.PostingKeys; !keys.IsZero() {
		b.postingKeyBytes += len(keys.Min().ColumnName) + len(keys.Min().LabelValue)
		b.postingKeyBytes += len(keys.Max().ColumnName) + len(keys.Max().LabelValue)
	}
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
	// 3. Count the posting keys by the length of their strings.
	// 4. Account for column metadata overhead.

	if len(b.indexPointers) == 0 {
		return 0
	}

	var (
		avgPathLength        = 85                                    // Average path length in bytes (based on real data)
		pathCompressionRatio = 3                                     // ZSTD compression ratio
		timestampDeltaSize   = streamio.VarintSize(int64(time.Hour)) // 1 hour delta
		runIDSize            = 1                                     // Delta of a mostly constant value
		shardBucketsSize     = 1                                     // Delta of a mostly constant value
		metadataOverhead     = 100                                   // Estimated metadata overhead per column
		columnCount          = 7
	)

	var sizeEstimate int

	// Path column (byte arrays with ZSTD compression)
	sizeEstimate += (len(b.indexPointers) * avgPathLength) / pathCompressionRatio

	// Start and end timestamp columns (int64 with delta encoding)
	sizeEstimate += 2 * len(b.indexPointers) * timestampDeltaSize

	sizeEstimate += b.postingKeyBytes
	sizeEstimate += len(b.indexPointers) * (runIDSize + shardBucketsSize)
	sizeEstimate += columnCount * metadataOverhead

	return sizeEstimate
}

// Flush flushes the streams section to the provided writer.
//
// After successful encoding, b is reset to a fresh state and can be reused.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	timer := prometheus.NewTimer(b.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	b.sortIndexPointers()

	var enc columnar.Encoder
	defer enc.Reset()
	if err := b.encodeTo(&enc); err != nil {
		return 0, fmt.Errorf("building encoder: %w", err)
	}

	enc.SetTenant(b.tenant)

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
	b.postingKeyBytes = 0

	b.metrics.minTimestamp.Set(0)
	b.metrics.maxTimestamp.Set(0)
}

func (b *Builder) encodeTo(enc *columnar.Encoder) error {
	pathBuilder, err := dataset.NewColumnBuilder("path", dataset.BuilderOptions{
		PageSizeHint:    b.pageSize,
		PageMaxRowCount: b.pageRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd_v2.PHYSICAL_TYPE_BINARY,
			Logical:  ColumnTypePath.String(),
		},
		Encoding:    datasetmd_v2.ENCODING_TYPE_PLAIN,
		Compression: datasetmd_v2.COMPRESSION_TYPE_ZSTD,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		return fmt.Errorf("creating path column: %w", err)
	}

	minTimestampBuilder, err := dataset.NewColumnBuilder("min_timestamp", dataset.BuilderOptions{
		PageSizeHint:    b.pageSize,
		PageMaxRowCount: b.pageRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd_v2.PHYSICAL_TYPE_INT64,
			Logical:  ColumnTypeMinTimestamp.String(),
		},
		Encoding:    datasetmd_v2.ENCODING_TYPE_DELTA,
		Compression: datasetmd_v2.COMPRESSION_TYPE_NONE,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		return fmt.Errorf("creating min timestamp column: %w", err)
	}

	maxTimestampBuilder, err := dataset.NewColumnBuilder("max_timestamp", dataset.BuilderOptions{
		PageSizeHint:    b.pageSize,
		PageMaxRowCount: b.pageRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd_v2.PHYSICAL_TYPE_INT64,
			Logical:  ColumnTypeMaxTimestamp.String(),
		},
		Encoding:    datasetmd_v2.ENCODING_TYPE_DELTA,
		Compression: datasetmd_v2.COMPRESSION_TYPE_NONE,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		return fmt.Errorf("creating max timestamp column: %w", err)
	}

	runIDBuilder, err := b.newColumnBuilder(ColumnTypeRunID, datasetmd_v2.PHYSICAL_TYPE_INT64, datasetmd_v2.ENCODING_TYPE_DELTA, datasetmd_v2.COMPRESSION_TYPE_NONE)
	if err != nil {
		return err
	}
	minPostingKeyBuilder, err := b.newColumnBuilder(ColumnTypeMinPostingKey, datasetmd_v2.PHYSICAL_TYPE_BINARY, datasetmd_v2.ENCODING_TYPE_PLAIN, datasetmd_v2.COMPRESSION_TYPE_ZSTD)
	if err != nil {
		return err
	}
	maxPostingKeyBuilder, err := b.newColumnBuilder(ColumnTypeMaxPostingKey, datasetmd_v2.PHYSICAL_TYPE_BINARY, datasetmd_v2.ENCODING_TYPE_PLAIN, datasetmd_v2.COMPRESSION_TYPE_ZSTD)
	if err != nil {
		return err
	}
	shardBucketsBuilder, err := b.newColumnBuilder(ColumnTypeShardBuckets, datasetmd_v2.PHYSICAL_TYPE_INT64, datasetmd_v2.ENCODING_TYPE_DELTA, datasetmd_v2.COMPRESSION_TYPE_NONE)
	if err != nil {
		return err
	}

	for i, pointer := range b.indexPointers {
		_ = pathBuilder.Append(i, dataset.BinaryValue([]byte(pointer.Path)))
		_ = minTimestampBuilder.Append(i, dataset.Int64Value(pointer.StartTs.UnixNano()))
		_ = maxTimestampBuilder.Append(i, dataset.Int64Value(pointer.EndTs.UnixNano()))

		// A missing optional value stays null. Backfill below fills the nulls
		// after the last value.
		if pointer.RunID != 0 {
			_ = runIDBuilder.Append(i, dataset.Int64Value(int64(pointer.RunID)))
		}
		if keys := pointer.PostingKeys; !keys.IsZero() {
			minEncoded, maxEncoded := keys.Encode()
			_ = minPostingKeyBuilder.Append(i, dataset.BinaryValue(minEncoded))
			_ = maxPostingKeyBuilder.Append(i, dataset.BinaryValue(maxEncoded))
		}
		if layout, ok := pointer.ShardBuckets.Layout(); ok {
			_ = shardBucketsBuilder.Append(i, dataset.Int64Value(int64(layout.Buckets())))
		}
	}
	for _, builder := range []*dataset.ColumnBuilder{runIDBuilder, minPostingKeyBuilder, maxPostingKeyBuilder, shardBucketsBuilder} {
		builder.Backfill(len(b.indexPointers))
	}

	// Encode our builders to sections. We ignore errors after enc.OpenStreams
	// (which may fail due to a caller) since we guarantee correct usage of the
	// encoding API.
	{
		var errs []error
		errs = append(errs, encodeColumn(enc, ColumnTypePath, pathBuilder))
		errs = append(errs, encodeColumn(enc, ColumnTypeMinTimestamp, minTimestampBuilder))
		errs = append(errs, encodeColumn(enc, ColumnTypeMaxTimestamp, maxTimestampBuilder))
		errs = append(errs, encodeColumn(enc, ColumnTypeRunID, runIDBuilder))
		errs = append(errs, encodeColumn(enc, ColumnTypeMinPostingKey, minPostingKeyBuilder))
		errs = append(errs, encodeColumn(enc, ColumnTypeMaxPostingKey, maxPostingKeyBuilder))
		errs = append(errs, encodeColumn(enc, ColumnTypeShardBuckets, shardBucketsBuilder))

		if err := errors.Join(errs...); err != nil {
			return fmt.Errorf("encoding columns: %w", err)
		}
	}

	return nil
}

// newColumnBuilder returns a builder for a column of columnType that stores no
// range statistics.
func (b *Builder) newColumnBuilder(columnType ColumnType, physical datasetmd_v2.PhysicalType, encoding datasetmd_v2.EncodingType, compression datasetmd_v2.CompressionType) (*dataset.ColumnBuilder, error) {
	builder, err := dataset.NewColumnBuilder(columnType.String(), dataset.BuilderOptions{
		PageSizeHint:    b.pageSize,
		PageMaxRowCount: b.pageRowCount,
		Type: dataset.ColumnType{
			Physical: physical,
			Logical:  columnType.String(),
		},
		Encoding:    encoding,
		Compression: compression,
	})
	if err != nil {
		return nil, fmt.Errorf("creating %s column: %w", columnType, err)
	}
	return builder, nil
}

func encodeColumn(enc *columnar.Encoder, columnType ColumnType, builder *dataset.ColumnBuilder) error {
	column, err := builder.Flush()
	if err != nil {
		return fmt.Errorf("flushing %s column: %w", columnType, err)
	}

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

	for _, page := range column.Pages {
		err := columnEnc.AppendPage(page)
		if err != nil {
			return fmt.Errorf("appending %s page: %w", columnType, err)
		}
	}

	if columnType == ColumnTypeMinTimestamp {
		enc.SetSortInfo(&datasetmd_v2.SortInfo{
			ColumnSorts: []*datasetmd_v2.SortInfo_ColumnSort{{
				// NumColumns increases after calling Commit, so we can use the
				// current value as the index.
				ColumnIndex: uint32(enc.NumColumns()),
				Direction:   datasetmd_v2.SORT_DIRECTION_ASCENDING,
			}},
		})
	}

	return columnEnc.Commit()
}
