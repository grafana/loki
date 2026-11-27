package postings

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/arrowconv"
	"github.com/grafana/loki/v3/pkg/memory"
)

// readArrow reads the next batch of r and converts it to Arrow, named per
// [columnsSchema]. Tests use it to check the null values that a section
// stores, which a [Row] cannot show.
func readArrow(ctx context.Context, r *Reader, batchSize int) (arrow.RecordBatch, error) {
	rb, err := r.Read(ctx, memory.NewAllocator(nil), batchSize)
	if err != nil {
		return nil, err
	}
	rec, err := arrowconv.ToRecordBatch(rb, columnsSchema(r.Columns()))
	if err != nil {
		return nil, fmt.Errorf("converting to arrow: %w", err)
	}
	return rec, nil
}

// readTable drains r into an Arrow table. It returns io.EOF if r has no row.
func readTable(ctx context.Context, r *Reader) (arrow.Table, error) {
	var recs []arrow.RecordBatch
	for {
		rec, err := readArrow(ctx, r, 128)
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		recs = append(recs, rec)
	}
	if len(recs) == 0 {
		return nil, io.EOF
	}
	return array.NewTableFromRecords(recs[0].Schema(), recs), nil
}

// columnsSchema builds the arrow schema for the given projected columns.
func columnsSchema(cols []*Column) *arrow.Schema {
	fields := make([]arrow.Field, 0, len(cols))
	for _, col := range cols {
		fields = append(fields, columnToField(col))
	}
	return arrow.NewSchema(fields, nil)
}

var columnDatatypes = map[ColumnType]arrow.DataType{
	ColumnTypeInvalid:          arrow.Null,
	ColumnTypeKind:             arrow.PrimitiveTypes.Int64,
	ColumnTypeObjectPath:       arrow.BinaryTypes.String,
	ColumnTypeShardBuckets:     arrow.PrimitiveTypes.Int64,
	ColumnTypeSectionIndex:     arrow.PrimitiveTypes.Int64,
	ColumnTypeColumnName:       arrow.BinaryTypes.String,
	ColumnTypeLabelValue:       arrow.BinaryTypes.String,
	ColumnTypeBloomFilter:      arrow.BinaryTypes.Binary,
	ColumnTypeStreamIDBitmap:   arrow.BinaryTypes.Binary,
	ColumnTypeUncompressedSize: arrow.PrimitiveTypes.Int64,
	ColumnTypeMinTimestamp:     arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeMaxTimestamp:     arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeMinShardBucket:   arrow.PrimitiveTypes.Int64,
	ColumnTypeMaxShardBucket:   arrow.PrimitiveTypes.Int64,
}

func columnToField(col *Column) arrow.Field {
	dtype, ok := columnDatatypes[col.Type]
	if !ok {
		dtype = arrow.Null
	}
	return arrow.Field{
		Name:     makeColumnName(col.Name, col.Type.String(), dtype),
		Type:     dtype,
		Nullable: true,
	}
}

// makeColumnName produces a unique field name "<name>.<type>.<dtype>" or
// "<type>.<dtype>" when the column has no name. Mirrors streams/reader.go.
func makeColumnName(label, name string, dty arrow.DataType) string {
	switch {
	case label == "" && name == "":
		return dty.Name()
	case label == "" && name != "":
		return name + "." + dty.Name()
	default:
		if name == "" {
			name = "<invalid>"
		}
		return label + "." + name + "." + dty.Name()
	}
}
