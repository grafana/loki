package executor

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var streamInjectorColumnName = "stream_id.int64"

// streamInjector injects stream labels into a logs Arrow record, replacing the
// streams ID column with columns for the labels composing those streams.
type streamInjector struct {
	alloc memory.Allocator
	view  *streamsView
}

// newStreamInjector creates a new streamInjector that uses the provided view
// for determining stream ID labels.
//
// streamInjector does not take ownership of the view, so the caller is
// responsible for closing the view when it is no longer needed.
func newStreamInjector(alloc memory.Allocator, view *streamsView) *streamInjector {
	return &streamInjector{alloc: alloc, view: view}
}

// Inject returns a new Arrow record where the stream ID column is replaced
// with a set of stream label columns.
//
// Inject fails if there is no stream ID column in the input record, or if the
// stream ID doesn't exist in the view given to [newStreamInjector].
//
// The returned record must be Release()d by the caller when no longer needed.
func (si *streamInjector) Inject(ctx context.Context, in arrow.Record) (arrow.Record, error) {
	streamIDIndex, err := si.findStreamIDColumn(in)
	if err != nil {
		return nil, fmt.Errorf("finding stream ID column: %w", err)
	}

	streamIDValues, ok := in.Column(streamIDIndex).(*array.Int64)
	if !ok {
		return nil, fmt.Errorf("stream ID column %q must be of type int64, got %s", streamInjectorColumnName, in.Schema().Field(streamIDIndex).Type)
	}

	type labelColumn struct {
		Field   arrow.Field
		Builder *array.StringBuilder
	}

	var (
		labels      = make([]*labelColumn, 0, si.view.NumLabels())
		labelLookup = make(map[string]*labelColumn, si.view.NumLabels())
	)
	defer func() {
		for _, col := range labels {
			col.Builder.Release()
		}
	}()

	getColumn := func(name string) *labelColumn {
		if col, ok := labelLookup[name]; ok {
			return col
		}

		col := &labelColumn{
			Field: arrow.Field{
				Name:     name,
				Type:     arrow.BinaryTypes.String,
				Nullable: true,
				Metadata: datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.Loki.String),
			},
			Builder: array.NewStringBuilder(si.alloc),
		}

		labels = append(labels, col)
		labelLookup[name] = col
		return col
	}

	for i := range streamIDValues.Len() {
		findID := streamIDValues.Value(i)

		// TODO(rfratto): this flips the processing of stream IDs into row-based
		// processing. It may be more efficient to vectorize this by building each
		// label column all at once.
		it, err := si.view.Labels(ctx, findID)
		if err != nil {
			return nil, fmt.Errorf("stream ID %d not found in section", findID)
		}

		for label := range it {
			col := getColumn(label.Name)

			// Backfill any missing NULLs in the column if needed.
			if missing := i - col.Builder.Len(); missing > 0 {
				col.Builder.AppendNulls(missing)
			}
			col.Builder.Append(label.Value)
		}
	}

	// Sort our labels by name for consistent ordering.
	slices.SortFunc(labels, func(a, b *labelColumn) int {
		return cmp.Compare(a.Field.Name, b.Field.Name)
	})

	var (
		// We preallocate enough space for all label fields + original columns
		// minus the stream ID column (which we drop from the final record).

		fields = make([]arrow.Field, 0, len(labels)+int(in.NumCols())-1)
		arrs   = make([]arrow.Array, 0, len(labels)+int(in.NumCols())-1)

		md = in.Schema().Metadata() // Use the same metadata as the input record.
	)

	// Prepare our arrays from the string builders.
	for _, col := range labels {
		// Backfill any final missing NULLs in the StringBuilder if needed.
		if missing := int(in.NumRows()) - col.Builder.Len(); missing > 0 {
			col.Builder.AppendNulls(missing)
		}

		arr := col.Builder.NewArray()
		defer arr.Release()

		fields = append(fields, col.Field)
		arrs = append(arrs, arr)
	}

	// Add all of the original columns except the stream ID column.
	for i := range int(in.NumCols()) {
		if i == streamIDIndex {
			continue
		}

		fields = append(fields, in.Schema().Field(i))
		arrs = append(arrs, in.Column(i))
	}

	schema := arrow.NewSchemaWithEndian(fields, &md, in.Schema().Endianness())
	return array.NewRecord(schema, arrs, in.NumRows()), nil
}

func (si *streamInjector) findStreamIDColumn(in arrow.Record) (int, error) {
	indices := in.Schema().FieldIndices(streamInjectorColumnName)
	if len(indices) == 0 {
		return -1, fmt.Errorf("stream ID column %q not found in input record", streamInjectorColumnName)
	} else if len(indices) > 1 {
		return -1, fmt.Errorf("multiple stream ID columns found in input record: %v", indices)
	}
	return indices[0], nil
}
