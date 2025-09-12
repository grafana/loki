package executor

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func NewParsePipeline(parse *physical.ParseNode, input Pipeline, allocator memory.Allocator) *GenericPipeline {
	return newGenericPipeline(Local, func(ctx context.Context, inputs []Pipeline) state {
		// Pull the next item from the input pipeline
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return failureState(err)
		}

		// Batch needs to be released here since it won't be passed to the caller and won't be reused after
		// this call to newGenericPipeline.
		defer batch.Release()

		// Find the message column
		messageColIdx := -1
		schema := batch.Schema()
		for i := 0; i < schema.NumFields(); i++ {
			if schema.Field(i).Name == types.ColumnNameBuiltinMessage {
				messageColIdx = i
				break
			}
		}
		if messageColIdx == -1 {
			return failureState(fmt.Errorf("message column not found"))
		}

		messageCol := batch.Column(messageColIdx)
		stringCol, ok := messageCol.(*array.String)
		if !ok {
			return failureState(fmt.Errorf("message column is not a string column"))
		}

		// Parse logfmt based on the parser kind
		var headers []string
		var parsedColumns []arrow.Array
		switch parse.Kind {
		case physical.ParserLogfmt:
			headers, parsedColumns = BuildLogfmtColumns(stringCol, parse.RequestedKeys, allocator)
		default:
			return failureState(fmt.Errorf("unsupported parser kind: %v", parse.Kind))
		}

		// Build new schema with original fields plus parsed fields
		newFields := make([]arrow.Field, 0, schema.NumFields()+len(headers))
		for i := 0; i < schema.NumFields(); i++ {
			newFields = append(newFields, schema.Field(i))
		}
		for _, header := range headers {
			// Add metadata to mark these as parsed columns
			metadata := datatype.ColumnMetadata(
				types.ColumnTypeParsed,
				datatype.Loki.String,
			)

			newFields = append(newFields, arrow.Field{
				Name:     header,
				Type:     arrow.BinaryTypes.String,
				Metadata: metadata,
				Nullable: true,
			})
		}
		newSchema := arrow.NewSchema(newFields, nil)

		// Build new record with all columns
		numOriginalCols := int(batch.NumCols())
		numParsedCols := len(parsedColumns)
		allColumns := make([]arrow.Array, numOriginalCols+numParsedCols)

		// Copy original columns
		for i := range numOriginalCols {
			col := batch.Column(i)
			col.Retain() // Retain since we're releasing the batch
			allColumns[i] = col
		}

		// Add parsed columns
		for i, col := range parsedColumns {
			// Defenisve check added for clarity and safety, but BuildLogfmtColumns should already guarantee this
			if col.Len() != stringCol.Len() {
				return failureState(fmt.Errorf("parsed column %d (%s) has %d rows but expected %d",
					i, headers[i], col.Len(), stringCol.Len()))
			}
			allColumns[numOriginalCols+i] = col
		}

		// Create the new record
		newRecord := array.NewRecord(newSchema, allColumns, int64(stringCol.Len()))

		// Release the columns we retained/created
		for _, col := range allColumns {
			col.Release()
		}

		return successState(newRecord)
	}, input)
}
