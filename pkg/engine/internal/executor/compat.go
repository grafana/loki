package executor

import (
	"cmp"
	"context"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/assertions"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func newColumnCompatibilityPipeline(compat *physical.ColumnCompat, input Pipeline) Pipeline {
	const extracted = semconv.ExtractedSuffix

	identCache := semconv.NewIdentifierCache()

	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.RecordBatch, error) {
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

					existMovedByIdx: -1,
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
		existingLowerPriorityDestCols := make(map[string][]int, len(duplicates))

		for oldIdx, field := range oldSchema.Fields() {
			ident, err := identCache.ParseFQN(field.Name)
			if err != nil {
				oldFieldToNewIdx[oldIdx] = len(newFields)
				newFields = append(newFields, field)
				continue
			}

			if destinationNames[ident.ShortName()] {
				if ident.ColumnType() == compat.Destination {
					// Skip existing _extracted columns that we're going to recreate
					// But save a reference to their data so we can preserve values
					existingDestCols[ident.ShortName()] = batch.Column(oldIdx).(*array.String)
					continue
				}

				// Existing _extracted columns of lower priority (parsed > metadata > label)
				// should be preserved and processed separately. Multiple columns of
				// different lower-priority types can share the same short name.
				existingLowerPriorityDestCols[ident.ShortName()] = append(existingLowerPriorityDestCols[ident.ShortName()], oldIdx)
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

			for _, lowerIdx := range existingLowerPriorityDestCols[sourceIdent.ShortName()+extracted] {
				// Store old and new indices for lower priority _extracted columns
				duplicates[i].lowerExtractedSourceIdxs = append(duplicates[i].lowerExtractedSourceIdxs, lowerIdx)
				duplicates[i].lowerExtractedDestinationIdxs = append(duplicates[i].lowerExtractedDestinationIdxs, oldFieldToNewIdx[lowerIdx])
			}

			// An existing destination column can itself be the source of another
			// duplicate (e.g. parsed.foo_extracted colliding with
			// label.foo_extracted while parsed.foo collides with label.foo). In
			// that case its value only remains in place on rows where the other
			// duplicate does not move it away.
			if existingDestCols[sourceIdent.ShortName()+extracted] != nil {
				duplicates[i].existMovedByIdx = slices.IndexFunc(duplicates, func(d duplicateColumn) bool {
					return d.name == sourceIdent.ShortName()+extracted
				})
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

		// Copy over all columns that are not modified by the collision handling:
		// everything except duplicate sources, their lower-priority _extracted
		// columns, and dropped existing _extracted columns (not in oldFieldToNewIdx).
		for oldIdx := range oldSchema.NumFields() {
			isHandledByDuplicate := slices.ContainsFunc(duplicates, func(d duplicateColumn) bool {
				return d.sourceIdx == oldIdx || slices.Contains(d.lowerExtractedSourceIdxs, oldIdx)
			})
			if isHandledByDuplicate {
				continue
			}
			if newIdx, ok := oldFieldToNewIdx[oldIdx]; ok {
				newSchemaColumns[newIdx] = batch.Column(oldIdx)
			}
		}

		// Resolve each duplicate: write the winning value into the destination
		// (_extracted) column, keep or clear the source column, and null out
		// lower-priority _extracted columns wherever the destination has a value
		// (mirroring v1, where setting a parsed label deletes same-named
		// stream/metadata labels).
		for di := range duplicates {
			duplicate := duplicates[di]

			sourceCol, ok := batch.Column(duplicate.sourceIdx).(*array.String)
			if !ok {
				panic("invalid source column type: only string columns can be checked for collisions")
			}

			collisionCols := make([]arrow.Array, len(duplicate.collisionIdxs))
			for i, collIdx := range duplicate.collisionIdxs {
				collisionCols[i] = batch.Column(collIdx)
			}

			destinationFieldBuilder := builder.Field(duplicate.destinationIdx).(*array.StringBuilder)

			// The source column is dropped from the new schema when it is itself
			// an existing destination recreated by another duplicate; that
			// duplicate then owns writing the column.
			var sourceFieldBuilder *array.StringBuilder
			sourceNewIdx, writesSource := oldFieldToNewIdx[duplicate.sourceIdx]
			if writesSource {
				sourceFieldBuilder = builder.Field(sourceNewIdx).(*array.StringBuilder)
			}

			// Existing destination column (_extracted) in the input batch. This
			// happens when multiple ColumnCompat nodes run sequentially (e.g.
			// `| json | logfmt`) or when the parser extracted a literal
			// `<name>_extracted` key. v1 semantics are first-writer-wins: an
			// existing value is preserved over the newly moved source value.
			existingDestCol := existingDestCols[duplicate.name+extracted]
			var existMovedByCollisionCols []arrow.Array
			if duplicate.existMovedByIdx != -1 {
				movedBy := duplicates[duplicate.existMovedByIdx]
				existMovedByCollisionCols = make([]arrow.Array, len(movedBy.collisionIdxs))
				for i, collIdx := range movedBy.collisionIdxs {
					existMovedByCollisionCols[i] = batch.Column(collIdx)
				}
			}

			lowerDestFieldBuilders := make([]*array.StringBuilder, len(duplicate.lowerExtractedDestinationIdxs))
			lowerDestCols := make([]*array.String, len(duplicate.lowerExtractedSourceIdxs))
			for i := range duplicate.lowerExtractedSourceIdxs {
				lowerDestFieldBuilders[i] = builder.Field(duplicate.lowerExtractedDestinationIdxs[i]).(*array.StringBuilder)
				lowerDestCols[i] = batch.Column(duplicate.lowerExtractedSourceIdxs[i]).(*array.String)
			}

			for i := range int(batch.NumRows()) {
				collides := !allColumnsNull(collisionCols, i)
				sourceValid := !sourceCol.IsNull(i) && sourceCol.IsValid(i)

				existValid := existingDestCol != nil && !existingDestCol.IsNull(i)
				if existValid && duplicate.existMovedByIdx != -1 && !allColumnsNull(existMovedByCollisionCols, i) {
					// The existing value is moved to its own _extracted column by
					// the other duplicate on this row, so it does not occupy the
					// destination here.
					existValid = false
				}

				destSet := true
				switch {
				case existValid:
					destinationFieldBuilder.Append(existingDestCol.Value(i))
				case collides && sourceValid:
					destinationFieldBuilder.Append(sourceCol.Value(i))
				default:
					destinationFieldBuilder.AppendNull()
					destSet = false
				}

				if writesSource {
					if !collides && sourceValid {
						sourceFieldBuilder.Append(sourceCol.Value(i))
					} else {
						sourceFieldBuilder.AppendNull()
					}
				}

				for k, lowerBuilder := range lowerDestFieldBuilders {
					if destSet {
						lowerBuilder.AppendNull()
					} else if !lowerDestCols[k].IsNull(i) && lowerDestCols[k].IsValid(i) {
						lowerBuilder.Append(lowerDestCols[k].Value(i))
					} else {
						lowerBuilder.AppendNull()
					}
				}
			}

			newSchemaColumns[duplicate.destinationIdx] = destinationFieldBuilder.NewArray()
			if writesSource {
				newSchemaColumns[sourceNewIdx] = sourceFieldBuilder.NewArray()
			}
			for i := range lowerDestFieldBuilders {
				newSchemaColumns[duplicate.lowerExtractedDestinationIdxs[i]] = lowerDestFieldBuilders[i].NewArray()
			}
		}

		rec := array.NewRecordBatch(newSchema, newSchemaColumns, batch.NumRows())

		assertions.CheckLabelValuesDuplicates(rec)

		return rec, nil
	}, input)
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
	// lowerExtractedDestinationIdxs are the new indices of _extracted columns
	// with lower priority (from earlier compats or literal stream/metadata keys).
	lowerExtractedDestinationIdxs []int
	// lowerExtractedSourceIdxs are the old indices of _extracted columns with
	// lower priority.
	lowerExtractedSourceIdxs []int
	// existMovedByIdx is the index (into the duplicates slice) of the duplicate
	// whose source column is this duplicate's existing destination column, or -1.
	existMovedByIdx int
}
