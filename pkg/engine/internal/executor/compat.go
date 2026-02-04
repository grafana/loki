package executor

import (
	"cmp"
	"context"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func newColumnCompatibilityPipeline(compat *physical.ColumnCompat, input Pipeline, region *xcap.Region) Pipeline {
	const extracted = "_extracted"

	identCache := semconv.NewIdentifierCache()

	return newGenericPipelineWithRegion(func(ctx context.Context, inputs []Pipeline) (arrow.RecordBatch, error) {
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return nil, err
		}

		// Return early if the batch has zero rows, even if column names would collide.
		if batch.NumRows() == 0 {
			return batch, nil
		}

		// First, find all fields in the schema that have colliding names,
		// based on the collision column types and the source column type.
		var (
			sourceFieldIndices  []int
			sourceFieldNames    []string
			collisionIdxsByName = make(map[string][]int)
		)

		schema := batch.Schema()
		collisionTypes := make(map[types.ColumnType]bool, len(compat.Collisions))
		for _, ct := range compat.Collisions {
			collisionTypes[ct] = true
		}

		for idx := range schema.NumFields() {
			ident, err := identCache.ParseFQN(schema.Field(idx).Name)
			if err != nil {
				return nil, err
			}
			colType := ident.ColumnType()
			if collisionTypes[colType] {
				shortName := ident.ShortName()
				collisionIdxsByName[shortName] = append(collisionIdxsByName[shortName], idx)
			} else if colType == compat.Source {
				sourceFieldIndices = append(sourceFieldIndices, idx)
				sourceFieldNames = append(sourceFieldNames, ident.ShortName())
			}
		}

		// Find source columns that have collisions
		var duplicates []duplicateColumn
		for i, sourceName := range sourceFieldNames {
			if collisionIdxs, exists := collisionIdxsByName[sourceName]; exists {
				duplicates = append(duplicates, duplicateColumn{
					name:          sourceName,
					collisionIdxs: collisionIdxs,
					sourceIdx:     sourceFieldIndices[i],
				})
			}
		}

		// Return early if there are no colliding column names.
		if len(duplicates) == 0 {
			return batch, nil
		}

		// Sort by name for deterministic ordering of _extracted columns
		slices.SortStableFunc(duplicates, func(a, b duplicateColumn) int {
			return cmp.Compare(a.name, b.name)
		})

		region.Record(xcap.StatCompatCollisionFound.Observe(true))

		// Next, update the schema with the new columns that have the _extracted suffix.
		oldSchema := batch.Schema()

		destinationNames := make(map[string]bool, len(duplicates))
		for i := range duplicates {
			destinationNames[duplicates[i].name+extracted] = true
		}
		// Copy old fields, but skip any existing _extracted columns that we're about to recreate
		newFields := make([]arrow.Field, 0, oldSchema.NumFields()+len(duplicates))
		oldFieldToNewIdx := make(map[int]int, oldSchema.NumFields())
		existingDestCols := make(map[string]*array.String, len(duplicates))

		for oldIdx, field := range oldSchema.Fields() {
			ident, err := identCache.ParseFQN(field.Name)
			if err != nil {
				oldFieldToNewIdx[oldIdx] = len(newFields)
				newFields = append(newFields, field)
				continue
			}

			// Skip existing _extracted columns that we're going to recreate
			// But save a reference to their data so we can preserve values
			if ident.ColumnType() == compat.Destination && destinationNames[ident.ShortName()] {
				existingDestCols[ident.ShortName()] = batch.Column(oldIdx).(*array.String)
				continue
			}

			oldFieldToNewIdx[oldIdx] = len(newFields)
			newFields = append(newFields, field)
		}

		// Now add the new _extracted columns
		for i := range duplicates {
			sourceFieldIdx := duplicates[i].sourceIdx
			sourceField := oldSchema.Field(sourceFieldIdx)
			sourceIdent, err := identCache.ParseFQN(sourceField.Name)
			if err != nil {
				return nil, err
			}

			destinationIdent := semconv.NewIdentifier(sourceIdent.ShortName()+extracted, compat.Destination, sourceIdent.DataType())
			newFields = append(newFields, semconv.FieldFromIdent(destinationIdent, true))
			duplicates[i].destinationIdx = len(newFields) - 1
		}

		// Create a new builder with the updated schema.
		// The per-field builders are only used for columns where row values are modified,
		// otherwise the full column from the input record is copied into the new record.
		md := oldSchema.Metadata()
		newSchema := arrow.NewSchema(newFields, &md)
		builder := array.NewRecordBuilder(memory.DefaultAllocator, newSchema)
		builder.Reserve(int(batch.NumRows()))

		newSchemaColumns := make([]arrow.Array, newSchema.NumFields())

		// Now, go through all fields of the old schema and append the rows to the new builder.
		for oldIdx := range oldSchema.NumFields() {
			col := batch.Column(oldIdx)

			duplicateIdx := slices.IndexFunc(duplicates, func(d duplicateColumn) bool { return d.sourceIdx == oldIdx })

			// If not a colliding column, just copy over the column data of the original record.
			if duplicateIdx < 0 {
				// Check if this column should be copied (not a skipped _extracted column)
				if newIdx, ok := oldFieldToNewIdx[oldIdx]; ok {
					newSchemaColumns[newIdx] = col
				}
				// If not in the map, it was skipped (existing _extracted column)
				continue
			}

			// If the currently processed column is the source field for a colliding column,
			// then write non-null values from source column into destination column.
			// Also, "clear" the original column value by writing a NULL instead of the original value.
			duplicate := duplicates[duplicateIdx]
			collisionCols := make([]arrow.Array, len(duplicate.collisionIdxs))
			for i, collIdx := range duplicate.collisionIdxs {
				collisionCols[i] = batch.Column(collIdx)
			}

			// Get the new index for this source field
			sourceNewIdx, ok := oldFieldToNewIdx[oldIdx]
			if !ok {
				// This shouldn't happen, but handle it gracefully
				continue
			}

			switch sourceFieldBuilder := builder.Field(sourceNewIdx).(type) {
			case *array.StringBuilder:
				destinationFieldBuilder := builder.Field(duplicate.destinationIdx).(*array.StringBuilder)

				// Check if there's an existing destination column (_extracted) in the input batch
				// This happens when multiple ColumnCompat nodes run sequentially (e.g., | json | logfmt |)
				existingDestCol := existingDestCols[duplicate.name+extracted]

				for i := range int(batch.NumRows()) {
					// Preserve existing values over adding null
					if existingDestCol != nil && !existingDestCol.IsNull(i) && existingDestCol.Value(i) != "" {
						if col.IsNull(i) || !col.IsValid(i) {
							sourceFieldBuilder.AppendNull() // append NULL to original column
						} else {
							sourceFieldBuilder.Append(col.(*array.String).Value(i)) // append value to original column
						}

						existingVal := existingDestCol.Value(i)
						destinationFieldBuilder.Append(existingVal) // append value to _extracted column
					} else if col.IsNull(i) || !col.IsValid(i) {
						sourceFieldBuilder.AppendNull()      // append NULL to original column
						destinationFieldBuilder.AppendNull() // append NULL to _extracted column
					} else if allColumnsNull(collisionCols, i) {
						// All collision columns are null for this row, keep value in source column
						v := col.(*array.String).Value(i)
						sourceFieldBuilder.Append(v)         // append value to original column
						destinationFieldBuilder.AppendNull() // append NULL to _extracted column
					} else {
						sourceFieldBuilder.AppendNull() // append NULL to original column
						v := col.(*array.String).Value(i)
						destinationFieldBuilder.Append(v) // append value to _extracted column
					}
				}

				sourceCol := sourceFieldBuilder.NewArray()
				newSchemaColumns[sourceNewIdx] = sourceCol

				destinationCol := destinationFieldBuilder.NewArray()
				newSchemaColumns[duplicate.destinationIdx] = destinationCol
			default:
				panic("invalid source column type: only string columns can be checked for collisions")
			}
		}

		return array.NewRecordBatch(newSchema, newSchemaColumns, batch.NumRows()), nil
	}, region, input)
}

// allColumnsNull returns true if all columns are null or invalid at the given row index.
func allColumnsNull(collisionCols []arrow.Array, rowIdx int) bool {
	for _, col := range collisionCols {
		if !col.IsNull(rowIdx) && col.IsValid(rowIdx) {
			return false
		}
	}
	return true
}

// duplicateColumn holds indexes to fields/columns in an [*arrow.Schema].
type duplicateColumn struct {
	// name is the duplicate column name
	name string
	// collisionIdxs holds the indices of ALL collision columns with this name.
	// Multiple collision types (e.g., label and metadata) can have columns with the same short name.
	collisionIdxs []int
	// sourceIdx is the index of the source column
	sourceIdx int
	// destinationIdx is the index of the destination column
	destinationIdx int
}
