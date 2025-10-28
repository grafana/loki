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
)

func newColumnCompatibilityPipeline(compat *physical.ColumnCompat, input Pipeline) Pipeline {
	const extracted = "_extracted"

	return newGenericPipeline(func(ctx context.Context, inputs []Pipeline) (arrow.Record, error) {
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
		// based on the collision column type and the source column type.
		var (
			collisionFieldIndices []int
			collisionFieldNames   []string
			sourceFieldIndices    []int
			sourceFieldNames      []string
		)

		schema := batch.Schema()

		for idx := range schema.NumFields() {
			ident, err := semconv.ParseFQN(schema.Field(idx).Name)
			if err != nil {
				return nil, err
			}
			switch ident.ColumnType() {
			case compat.Collision:
				collisionFieldIndices = append(collisionFieldIndices, idx)
				collisionFieldNames = append(collisionFieldNames, ident.ShortName())
			case compat.Source:
				sourceFieldIndices = append(sourceFieldIndices, idx)
				sourceFieldNames = append(sourceFieldNames, ident.ShortName())
			}
		}

		duplicates := findDuplicates(collisionFieldNames, sourceFieldNames)

		// Return early if there are no colliding column names.
		if len(duplicates) == 0 {
			return batch, nil
		}

		// Next, update the schema with the new columns that have the _extracted suffix.
		newSchema := batch.Schema()
		duplicateCols := make([]duplicateColumn, 0, len(duplicates))
		r := int(batch.NumCols())
		for i, duplicate := range duplicates {
			collisionFieldIdx := collisionFieldIndices[duplicate.s1Idx]
			sourceFieldIdx := sourceFieldIndices[duplicate.s2Idx]

			sourceField := newSchema.Field(sourceFieldIdx)
			sourceIdent, err := semconv.ParseFQN(sourceField.Name)
			if err != nil {
				return nil, err
			}

			destinationIdent := semconv.NewIdentifier(sourceIdent.ShortName()+extracted, compat.Destination, sourceIdent.DataType())
			newSchema, err = newSchema.AddField(len(newSchema.Fields()), semconv.FieldFromIdent(destinationIdent, true))
			if err != nil {
				return nil, err
			}

			duplicateCols = append(duplicateCols, duplicateColumn{
				name:           duplicate.value,
				collisionIdx:   collisionFieldIdx,
				sourceIdx:      sourceFieldIdx,
				destinationIdx: r + i,
			})
		}

		// Create a new builder with the updated schema.
		// The per-field builders are only used for columns where row values are modified,
		// otherwise the full column from the input record is copied into the new record.
		builder := array.NewRecordBuilder(memory.DefaultAllocator, newSchema)
		builder.Reserve(int(batch.NumRows()))

		newSchemaColumns := make([]arrow.Array, newSchema.NumFields())

		// Now, go through all fields of the old schema and append the rows to the new builder.
		for idx := range schema.NumFields() {
			col := batch.Column(idx)

			duplicateIdx := slices.IndexFunc(duplicateCols, func(d duplicateColumn) bool { return d.sourceIdx == idx })

			// If not a colliding column, just copy over the column data of the original record.
			if duplicateIdx < 0 {
				newSchemaColumns[idx] = col
				continue
			}

			// If the currently processed column is the source field for a colliding column,
			// then write non-null values from source column into destination column.
			// Also, "clear" the original column value by writing a NULL instead of the original value.
			duplicate := duplicateCols[duplicateIdx]
			collisionCol := batch.Column(duplicate.collisionIdx)

			switch sourceFieldBuilder := builder.Field(idx).(type) {
			case *array.StringBuilder:
				destinationFieldBuilder := builder.Field(duplicate.destinationIdx).(*array.StringBuilder)
				for i := range int(batch.NumRows()) {
					if col.IsNull(i) || !col.IsValid(i) {
						sourceFieldBuilder.AppendNull()      // append NULL to original column
						destinationFieldBuilder.AppendNull() // append NULL to _extracted column
					} else if collisionCol.IsNull(i) || !collisionCol.IsValid(i) {
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
				newSchemaColumns[duplicate.sourceIdx] = sourceCol

				destinationCol := destinationFieldBuilder.NewArray()
				newSchemaColumns[duplicate.destinationIdx] = destinationCol
			default:
				panic("invalid source column type: only string columns can be checked for collisions")
			}
		}

		return array.NewRecord(newSchema, newSchemaColumns, batch.NumRows()), nil
	}, input)
}

// duplicate holds indexes to a duplicate values in two slices
type duplicate struct {
	value        string
	s1Idx, s2Idx int
}

// findDuplicates finds strings that appear in both slices and returns
// their indexes in each slice.
// The function assumes that elements in a slices are unique.
func findDuplicates(s1, s2 []string) []duplicate {
	if len(s1) == 0 || len(s2) == 0 {
		return nil
	}

	set1 := make(map[string]int)
	for i, v := range s1 {
		set1[v] = i
	}

	set2 := make(map[string]int)
	for i, v := range s2 {
		set2[v] = i
	}

	// Find duplicates that exist in both slices
	var duplicates []duplicate
	for value, s1Idx := range set1 {
		if s2Idx, exists := set2[value]; exists {
			duplicates = append(duplicates, duplicate{
				value: value,
				s1Idx: s1Idx,
				s2Idx: s2Idx,
			})
		}
	}

	slices.SortStableFunc(duplicates, func(a, b duplicate) int { return cmp.Compare(a.value, b.value) })
	return duplicates
}

// duplicateColumn holds indexes to fields/columns in an [*arrow.Schema].
type duplicateColumn struct {
	// name is the duplicate column name
	name string
	// collisionIdx is the index of the collision column
	collisionIdx int
	// sourceIdx is the index of the source column
	sourceIdx int
	// destinationIdx is the index of the destination column
	destinationIdx int
}
