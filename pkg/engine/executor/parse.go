package executor

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func NewParsePipeline(parse *physical.ParseNode, input Pipeline, allocator memory.Allocator) *GenericPipeline {
	return newGenericPipeline(Local, func(ctx context.Context, inputs []Pipeline) state {
		// Pull the next item from the input pipeline
		input := inputs[0]
		err := input.Read(ctx)
		if err != nil {
			return failureState(err)
		}

		batch, err := input.Value()
		if err != nil {
			return failureState(err)
		}
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

		// Extract messages into string slice for parsing
		messages := make([]string, 0, stringCol.Len())
		for i := 0; i < stringCol.Len(); i++ {
			if stringCol.IsValid(i) {
				messages = append(messages, stringCol.Value(i))
			}
		}

		// Parse logfmt based on the parser kind
		var headers []string
		var parsedColumns []arrow.Array
		switch parse.Kind {
		case logical.ParserLogfmt:
			headers, parsedColumns, err = BuildLogfmtColumns(messages, parse.RequestedKeys, allocator)
			if err != nil {
				return failureState(fmt.Errorf("failed to parse logfmt: %w", err))
			}
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
			metadata := arrow.NewMetadata([]string{
				types.MetadataKeyColumnType,
				types.MetadataKeyColumnDataType,
			}, []string{
				types.ColumnTypeParsed.String(),
				datatype.Loki.String.String(),
			})
			newFields = append(newFields, arrow.Field{
				Name:     header,
				Type:     arrow.BinaryTypes.String,
				Metadata: metadata,
			})
		}
		newSchema := arrow.NewSchema(newFields, nil)

		// Build new record with all columns
		allColumns := make([]arrow.Array, 0, len(newFields))
		// Copy original columns
		for i := int64(0); i < batch.NumCols(); i++ {
			col := batch.Column(int(i))
			col.Retain() // Retain since we're releasing the batch
			allColumns = append(allColumns, col)
		}
		// Add parsed columns
		allColumns = append(allColumns, parsedColumns...)

		// Create the new record
		newRecord := array.NewRecord(newSchema, allColumns, int64(stringCol.Len()))

		// Release the columns we retained/created
		for _, col := range allColumns {
			col.Release()
		}

		return successState(newRecord)
	}, input)
}
