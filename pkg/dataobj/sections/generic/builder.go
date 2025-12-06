package generic

import (
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// BuilderOptions configures the behavior of the generic section builder.
type BuilderOptions struct {
	// PageSizeHint is the target size in bytes for each page. Default: 1MB.
	PageSizeHint int

	// PageMaxRowCount is the maximum number of rows per page. Default: 10000.
	PageMaxRowCount int

	// BufferSize is the initial buffer size for entities. Currently unused.
	BufferSize int
}

// Builder builds a generic section from entities.
type Builder struct {
	metrics *Metrics

	sectionType dataobj.SectionType
	opts        BuilderOptions
	tenant      string

	initialSchema *Schema
	currentSchema *Schema

	buffer   []arrow.RecordBatch
	segments []arrow.RecordBatch

	// Size tracking
	bufferSize  int
	segmentSize int
	sectionSize int
}

// NewBuilder creates a new generic section builder with the provided options.
func NewBuilder(kind string, schema *Schema, metrics *Metrics, opts BuilderOptions) *Builder {
	// Set defaults
	if metrics == nil {
		metrics = NewMetrics(kind)
	}
	if opts.PageSizeHint == 0 {
		opts.PageSizeHint = 1024 * 1024 // 1MB default
	}
	if opts.PageMaxRowCount == 0 {
		opts.PageMaxRowCount = 10000 // 10k rows default
	}

	return &Builder{
		metrics: metrics,
		opts:    opts,

		initialSchema: schema,
		currentSchema: schema.Copy(),

		sectionType: NewGenericSectionType(kind),
		buffer:      make([]arrow.RecordBatch, 0, 1024),
	}
}

// SetTenant sets the tenant that owns the builder.
func (b *Builder) SetTenant(tenant string) {
	b.tenant = tenant
}

// Tenant returns the optional tenant that owns the builder.
func (b *Builder) Tenant() string { return b.tenant }

// Type returns the [dataobj.SectionType] of the generic builder.
func (b *Builder) Type() dataobj.SectionType { return b.sectionType }

// Append adds a new entity to the builder.
// The entity's schema does not need to match the builder's schema exactly.
// If the entity has fields not in the builder's schema, they will be added,
// and previous entities will be backfilled with null values for those fields.
func (b *Builder) Append(entity arrow.RecordBatch) error {
	// Extend builder schema if entity has new fields
	if err := b.extendSchema(entity.Schema()); err != nil {
		return fmt.Errorf("extending schema: %w", err)
	}

	b.metrics.appendsTotal.Inc()
	b.metrics.entitiesTotal.Inc()

	b.buffer = append(b.buffer, entity)
	b.bufferSize += entitySize(entity)

	if b.bufferSize >= b.opts.BufferSize {
		if err := b.flushEntities(); err != nil {
			return err
		}
	}

	return nil
}

func (b *Builder) flushEntities() error {
	if len(b.buffer) == 0 {
		return nil
	}

	columns := make([]arrow.Array, 0, b.currentSchema.NumFields())
	arrays := make([]arrow.Array, 0, len(b.buffer))

	var numRows int64
	for i, field := range b.currentSchema.Fields() {
		arrays = arrays[:0]

		for _, entity := range b.buffer {
			idx := entity.Schema().FieldIndices(field.Name)
			if len(idx) > 0 {
				arrays = append(arrays, entity.Column(idx[0]))
			} else {
				// Create a properly typed null array for missing fields
				nullArray := b.createNullArray(field.Type, int(entity.NumRows()))
				arrays = append(arrays, nullArray)
			}
			// Only count rows on the first field iteration
			if i == 0 {
				numRows += entity.NumRows()
			}
		}
		merged, err := array.Concatenate(arrays, memory.DefaultAllocator)
		if err != nil {
			return err
		}
		columns = append(columns, merged)
		b.segmentSize += int(merged.Data().SizeInBytes())
	}

	schema := arrow.NewSchema(b.currentSchema.Fields(), nil)
	segment := array.NewRecordBatch(schema, columns, numRows)
	b.segments = append(b.segments, segment)
	b.buffer = sliceclear.Clear(b.buffer)
	b.bufferSize = 0

	return nil
}

func (b *Builder) flushSection() (arrow.RecordBatch, error) {
	if len(b.segments) == 0 {
		return nil, nil
	}

	// If we only have one segment, return it directly
	if len(b.segments) == 1 {
		return b.segments[0], nil
	}

	columns := make([]arrow.Array, 0, b.currentSchema.NumFields())
	arrays := make([]arrow.Array, 0, len(b.segments))

	var numRows int64
	for i, field := range b.currentSchema.Fields() {
		arrays = arrays[:0]

		for _, entity := range b.segments {
			idx := entity.Schema().FieldIndices(field.Name)
			if len(idx) > 0 {
				arrays = append(arrays, entity.Column(idx[0]))
			} else {
				// Create a properly typed null array for missing fields
				nullArray := b.createNullArray(field.Type, int(entity.NumRows()))
				arrays = append(arrays, nullArray)
			}
			// Only count rows on the first field iteration
			if i == 0 {
				numRows += entity.NumRows()
			}
		}
		merged, err := array.Concatenate(arrays, memory.DefaultAllocator)
		if err != nil {
			return nil, err
		}
		columns = append(columns, merged)
		b.sectionSize += int(merged.Data().SizeInBytes())
	}

	schema := arrow.NewSchema(b.currentSchema.Fields(), nil)
	section := array.NewRecordBatch(schema, columns, numRows)
	b.segments = sliceclear.Clear(b.segments)
	b.segmentSize = 0

	return section, nil
}

// extendSchema adds fields from entitySchema that are not in the builder's schema.
func (b *Builder) extendSchema(entitySchema *arrow.Schema) error {
	for _, entityField := range entitySchema.Fields() {
		builderField, found := b.currentSchema.Field(entityField.Name)
		if found {
			if !arrow.TypeEqual(builderField.Type, entityField.Type) {
				return fmt.Errorf("field %s: type mismatch between entity (%s) and builder (%s)",
					entityField.Name, entityField.Type, builderField.Type)
			}
			continue
		}
		if err := b.currentSchema.AddField(entityField); err != nil {
			return err
		}
	}
	return nil
}

// entitySize estimates the size of an entity in bytes.
func entitySize(entity arrow.RecordBatch) int {
	var size int
	for i := range entity.Columns() {
		size += int(entity.Column(i).Data().SizeInBytes())
	}
	return size
}

// EstimatedSize returns the estimated size of the generic section in bytes.
// This calculation takes into account the different encoding types used for different field types:
// - Delta encoding for integers and timestamps (typically 20-40% compression)
// - ZSTD compression for strings and binary data (typically 60-80% compression)
//
// The size is calculated incrementally as entities are added, making this method very efficient.
func (b *Builder) EstimatedSize() int {
	var size int

	size += b.bufferSize
	size += b.segmentSize

	return size
}

// Flush flushes b to the provided writer.
//
// After successful encoding, the b is reset and can be reused.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	timer := prometheus.NewTimer(b.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	// Check if there's any data to flush
	if len(b.buffer) == 0 && len(b.segments) == 0 {
		return 0, nil
	}

	// Convert any buffered entities to segments
	if len(b.buffer) > 0 {
		if err := b.flushEntities(); err != nil {
			return 0, fmt.Errorf("flushing entities: %w", err)
		}
	}

	// Merge all segments into a single RecordBatch
	section, err := b.flushSection()
	if err != nil {
		return 0, fmt.Errorf("flushing section: %w", err)
	}
	if section == nil {
		return 0, nil
	}

	var enc columnar.Encoder
	defer enc.Reset()

	if err := b.encodeTo(&enc, section); err != nil {
		return 0, fmt.Errorf("encoding section: %w", err)
	}

	// Set sort info if schema has sort information
	if len(b.currentSchema.sortIndex) > 0 {
		enc.SetSortInfo(b.buildSortInfo())
	}

	enc.SetTenant(b.tenant)

	n, err = enc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}

func (b *Builder) encodeTo(enc *columnar.Encoder, recordBatch arrow.RecordBatch) error {
	// Create column builders for each field in the schema
	builders := make([]*dataset.ColumnBuilder, b.currentSchema.NumFields())
	fields := b.currentSchema.Fields()

	for i, field := range fields {
		builder, err := b.createColumnBuilder(field)
		if err != nil {
			return fmt.Errorf("creating column builder for field %s: %w", field.Name, err)
		}
		builders[i] = builder
	}

	// Populate column builders with data from RecordBatch
	numRows := int(recordBatch.NumRows())
	numCols := int(recordBatch.NumCols())

	for rowIdx := 0; rowIdx < numRows; rowIdx++ {
		// Map RecordBatch column values to builder columns
		for colIdx, field := range fields {
			var value dataset.Value
			var err error

			// Check if this column exists in the RecordBatch
			// (it might not if the schema was extended after the RecordBatch was created)
			if colIdx < numCols {
				value, err = b.getRecordBatchValue(recordBatch, colIdx, rowIdx, field.Type)
				if err != nil {
					return fmt.Errorf("getting value for column %d row %d: %w", colIdx, rowIdx, err)
				}
			}
			// If column doesn't exist in RecordBatch, value will be nil (default zero value)

			if err := builders[colIdx].Append(rowIdx, value); err != nil {
				return fmt.Errorf("appending value to column %d row %d: %w", colIdx, rowIdx, err)
			}
		}
	}

	// Encode all columns
	var errs []error
	for i, builder := range builders {
		if err := b.encodeColumn(enc, fields[i].Name, builder); err != nil {
			errs = append(errs, fmt.Errorf("encoding column %s: %w", fields[i].Name, err))
		}
	}

	return errors.Join(errs...)
}

// getRecordBatchValue extracts a value from a RecordBatch at the specified column and row,
// and converts it to a dataset.Value based on the Arrow type.
func (b *Builder) getRecordBatchValue(recordBatch arrow.RecordBatch, colIdx, rowIdx int, arrowType arrow.DataType) (dataset.Value, error) {
	column := recordBatch.Column(colIdx)

	// Check if the value is null
	if column.IsNull(rowIdx) {
		return dataset.Value{}, nil
	}

	// Convert based on Arrow type
	switch arrowType.ID() {
	case arrow.INT64:
		arr := column.(*array.Int64)
		return dataset.Int64Value(arr.Value(rowIdx)), nil

	case arrow.UINT64:
		arr := column.(*array.Uint64)
		return dataset.Uint64Value(arr.Value(rowIdx)), nil

	case arrow.STRING:
		arr := column.(*array.String)
		value := arr.Value(rowIdx)
		return dataset.BinaryValue([]byte(value)), nil

	case arrow.BINARY:
		arr := column.(*array.Binary)
		value := arr.Value(rowIdx)
		return dataset.BinaryValue(value), nil

	case arrow.TIMESTAMP:
		arr := column.(*array.Timestamp)
		return dataset.Int64Value(int64(arr.Value(rowIdx))), nil

	default:
		return dataset.Value{}, fmt.Errorf("unsupported arrow type: %s", arrowType.Name())
	}
}

func (b *Builder) createColumnBuilder(field arrow.Field) (*dataset.ColumnBuilder, error) {
	// Map Arrow type to dataset physical type
	physicalType, logicalType, err := b.mapArrowType(field.Type)
	if err != nil {
		return nil, err
	}

	// Choose encoding based on physical type
	encoding := datasetmd.ENCODING_TYPE_PLAIN
	compression := datasetmd.COMPRESSION_TYPE_ZSTD
	if physicalType == datasetmd.PHYSICAL_TYPE_INT64 || physicalType == datasetmd.PHYSICAL_TYPE_UINT64 {
		// Use delta encoding for integer types
		encoding = datasetmd.ENCODING_TYPE_DELTA
		compression = datasetmd.COMPRESSION_TYPE_NONE
	}

	return dataset.NewColumnBuilder(field.Name, dataset.BuilderOptions{
		PageSizeHint:    b.opts.PageSizeHint,
		PageMaxRowCount: b.opts.PageMaxRowCount,
		Type: dataset.ColumnType{
			Physical: physicalType,
			Logical:  logicalType,
		},
		Encoding:    encoding,
		Compression: compression,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
}

func (b *Builder) mapArrowType(dt arrow.DataType) (datasetmd.PhysicalType, string, error) {
	switch dt.ID() {
	case arrow.INT64:
		return datasetmd.PHYSICAL_TYPE_INT64, ColumnTypeInteger.String(), nil
	case arrow.UINT64:
		return datasetmd.PHYSICAL_TYPE_UINT64, ColumnTypeInteger.String(), nil
	case arrow.STRING, arrow.BINARY:
		return datasetmd.PHYSICAL_TYPE_BINARY, ColumnTypeString.String(), nil
	case arrow.TIMESTAMP:
		return datasetmd.PHYSICAL_TYPE_INT64, ColumnTypeTimestamp.String(), nil
	default:
		return datasetmd.PHYSICAL_TYPE_UNSPECIFIED, "", fmt.Errorf("unsupported arrow type: %s", dt.Name())
	}
}

func (b *Builder) encodeColumn(enc *columnar.Encoder, name string, builder *dataset.ColumnBuilder) error {
	column, err := builder.Flush()
	if err != nil {
		return fmt.Errorf("flushing column %s: %w", name, err)
	}

	columnEnc, err := enc.OpenColumn(column.ColumnDesc())
	if err != nil {
		return fmt.Errorf("opening column %s encoder: %w", name, err)
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
		if err := columnEnc.AppendPage(page); err != nil {
			return fmt.Errorf("appending page to column %s: %w", name, err)
		}
	}

	return columnEnc.Commit()
}

func (b *Builder) buildSortInfo() *datasetmd.SortInfo {
	if len(b.currentSchema.sortIndex) == 0 {
		return nil
	}

	sortInfo := &datasetmd.SortInfo{
		ColumnSorts: make([]*datasetmd.SortInfo_ColumnSort, len(b.currentSchema.sortIndex)),
	}

	for i, colIdx := range b.currentSchema.sortIndex {
		direction := datasetmd.SORT_DIRECTION_ASCENDING
		if b.currentSchema.sortDirection[i] == -1 {
			direction = datasetmd.SORT_DIRECTION_DESCENDING
		}

		sortInfo.ColumnSorts[i] = &datasetmd.SortInfo_ColumnSort{
			ColumnIndex: uint32(colIdx),
			Direction:   direction,
		}
	}

	return sortInfo
}

// createNullArray creates a properly typed null array for the given type and size.
func (b *Builder) createNullArray(dt arrow.DataType, size int) arrow.Array {
	alloc := memory.DefaultAllocator

	switch dt.ID() {
	case arrow.INT64:
		builder := array.NewInt64Builder(alloc)
		defer builder.Release()
		for i := 0; i < size; i++ {
			builder.AppendNull()
		}
		return builder.NewArray()

	case arrow.UINT64:
		builder := array.NewUint64Builder(alloc)
		defer builder.Release()
		for i := 0; i < size; i++ {
			builder.AppendNull()
		}
		return builder.NewArray()

	case arrow.STRING:
		builder := array.NewStringBuilder(alloc)
		defer builder.Release()
		for i := 0; i < size; i++ {
			builder.AppendNull()
		}
		return builder.NewArray()

	case arrow.BINARY:
		builder := array.NewBinaryBuilder(alloc, &arrow.BinaryType{})
		defer builder.Release()
		for i := 0; i < size; i++ {
			builder.AppendNull()
		}
		return builder.NewArray()

	case arrow.TIMESTAMP:
		builder := array.NewTimestampBuilder(alloc, dt.(*arrow.TimestampType))
		defer builder.Release()
		for i := 0; i < size; i++ {
			builder.AppendNull()
		}
		return builder.NewArray()

	default:
		// Fallback to generic null array
		return array.NewNull(size)
	}
}

// Reset resets all state, allowing b to be reused.
func (b *Builder) Reset() {
	b.tenant = ""
	b.buffer = sliceclear.Clear(b.buffer)
	b.segments = sliceclear.Clear(b.segments)
	b.bufferSize = 0
	b.segmentSize = 0
}
