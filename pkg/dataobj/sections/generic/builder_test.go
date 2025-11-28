package generic

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

// testBuilderOptions provides common builder options for tests.
// Individual tests should copy and customize these options as needed.
var testBuilderOptions = BuilderOptions{
	PageSizeHint:    1024 * 1024,
	PageMaxRowCount: 10000,
}

func TestBuilder(t *testing.T) {
	// Define a schema with multiple columns
	schema := NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	})

	// Create a builder
	builder := NewBuilder("test-data", schema, nil, testBuilderOptions)
	builder.SetTenant("test-tenant")

	// Append some entities
	entities := []*Entity{
		NewEntity(schema, []dataset.Value{
			dataset.Int64Value(1),
			dataset.BinaryValue([]byte("Alice")),
			dataset.Int64Value(1234567890000000000),
		}),
		NewEntity(schema, []dataset.Value{
			dataset.Int64Value(2),
			dataset.BinaryValue([]byte("Bob")),
			dataset.Int64Value(1234567891000000000),
		}),
		NewEntity(schema, []dataset.Value{
			dataset.Int64Value(3),
			dataset.BinaryValue([]byte("Charlie")),
			dataset.Int64Value(1234567892000000000),
		}),
	}

	for _, entity := range entities {
		err := builder.Append(entity)
		require.NoError(t, err)
	}

	// Check estimated size
	size := builder.EstimatedSize()
	require.Greater(t, size, 0)

	// Verify section type
	sectionType := builder.Type()
	require.Equal(t, "github.com/grafana/loki", sectionType.Namespace)
	require.Equal(t, "test-data", sectionType.Kind)

	// Flush to a data object
	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(builder))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Verify the object has sections
	require.Len(t, obj.Sections(), 1)
}

func TestBuilderWithSort(t *testing.T) {
	// Define a schema with sort information
	schema := NewSchemaWithSort(
		[]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
			{Name: "name", Type: arrow.BinaryTypes.String},
		},
		[]int{1, 2},  // Sort by first column (timestamp)
		[]int{1, -1}, // Ascending order
	)

	// Create a builder
	kind := "generic"
	builder := NewBuilder(kind, schema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})

	// Append some entities
	entities := []*Entity{
		NewEntity(schema, []dataset.Value{
			dataset.Int64Value(1),
			dataset.Int64Value(1234567890000000000),
			dataset.BinaryValue([]byte("Alice")),
		}),
		NewEntity(schema, []dataset.Value{
			dataset.Int64Value(2),
			dataset.Int64Value(1234567891000000000),
			dataset.BinaryValue([]byte("Alice")),
		}),
		NewEntity(schema, []dataset.Value{
			dataset.Int64Value(2),
			dataset.Int64Value(1234567891000000000),
			dataset.BinaryValue([]byte("Ben")),
		}),
	}

	for _, entity := range entities {
		err := builder.Append(entity)
		require.NoError(t, err)
	}

	// Flush to a data object
	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(builder))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Verify the object has sections
	require.Len(t, obj.Sections(), 1)

	// Open the section and check sort info
	section, err := Open(context.Background(), obj.Sections()[0], kind)
	require.NoError(t, err)

	colType, sortDir, err := section.PrimarySortOrder()
	require.NoError(t, err)
	require.Equal(t, ColumnTypeTimestamp, colType)
	require.Equal(t, SortDirectionAscending, sortDir)
}

func TestBuilderValidation(t *testing.T) {
	// Define a schema
	schema := NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	})

	// Create a builder
	builder := NewBuilder("generic", schema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})

	t.Run("wrong type for field", func(t *testing.T) {
		// Try to append an entity with wrong type for second field
		wrongSchema := NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "name", Type: arrow.PrimitiveTypes.Int64}, // Wrong type!
		})
		entity := NewEntity(wrongSchema, []dataset.Value{
			dataset.Int64Value(1),
			dataset.Int64Value(123), // Should be binary/string, not int64
		})

		err := builder.Append(entity)
		require.Error(t, err)
		require.Contains(t, err.Error(), "field name: type mismatch")
	})

	t.Run("wrong type for first field", func(t *testing.T) {
		// Try to append an entity with wrong type for first field
		wrongSchema := NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.BinaryTypes.String}, // Wrong type!
			{Name: "name", Type: arrow.BinaryTypes.String},
		})
		entity := NewEntity(wrongSchema, []dataset.Value{
			dataset.BinaryValue([]byte("not an int")), // Should be int64, not binary
			dataset.BinaryValue([]byte("Alice")),
		})

		err := builder.Append(entity)
		require.Error(t, err)
		require.Contains(t, err.Error(), "field id: type mismatch")
	})

	t.Run("valid entity", func(t *testing.T) {
		// Append a valid entity
		entity := NewEntity(schema, []dataset.Value{
			dataset.Int64Value(1),
			dataset.BinaryValue([]byte("Alice")),
		})

		err := builder.Append(entity)
		require.NoError(t, err)
	})

	t.Run("nil values are allowed", func(t *testing.T) {
		// Append an entity with nil values
		entity := NewEntity(schema, []dataset.Value{
			dataset.Int64Value(2),
			dataset.Value{}, // Nil value
		})

		err := builder.Append(entity)
		require.NoError(t, err)
	})

	t.Run("entity with subset of fields", func(t *testing.T) {
		// Append an entity with only one field (subset of builder schema)
		partialSchema := NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		})
		entity := NewEntity(partialSchema, []dataset.Value{
			dataset.Int64Value(3),
		})

		err := builder.Append(entity)
		require.NoError(t, err)
	})

	t.Run("entity with additional fields", func(t *testing.T) {
		// Append an entity with extra fields (extends builder schema)
		extendedSchema := NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "name", Type: arrow.BinaryTypes.String},
			{Name: "age", Type: arrow.PrimitiveTypes.Int64}, // New field
		})
		entity := NewEntity(extendedSchema, []dataset.Value{
			dataset.Int64Value(4),
			dataset.BinaryValue([]byte("Bob")),
			dataset.Int64Value(25),
		})

		err := builder.Append(entity)
		require.NoError(t, err)

		// Verify the builder schema was extended
		require.Equal(t, 3, builder.currentSchema.NumFields())
	})
}

func TestBuilderReset(t *testing.T) {
	// Define a schema
	schema := NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	})

	// Create a builder
	builder := NewBuilder("generic", schema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})
	builder.SetTenant("test-tenant")

	// Append an entity
	entity := NewEntity(schema, []dataset.Value{
		dataset.Int64Value(1),
	})
	err := builder.Append(entity)
	require.NoError(t, err)

	// Reset the builder
	builder.Reset()

	// Verify state is cleared
	require.Equal(t, "", builder.Tenant())
	require.Equal(t, 0, builder.EstimatedSize())
}

func TestBuilderRoundTrip(t *testing.T) {
	// Define a schema
	schema := NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	})

	// Create a builder
	kind := "generic"
	builder := NewBuilder(kind, schema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})
	builder.SetTenant("test-tenant")

	// Append entities
	entities := []*Entity{
		NewEntity(schema, []dataset.Value{
			dataset.Int64Value(1),
			dataset.BinaryValue([]byte("Alice")),
			dataset.Int64Value(1234567890000000000),
		}),
		NewEntity(schema, []dataset.Value{
			dataset.Int64Value(2),
			dataset.BinaryValue([]byte("Bob")),
			dataset.Int64Value(1234567891000000000),
		}),
	}

	for _, entity := range entities {
		err := builder.Append(entity)
		require.NoError(t, err)
	}

	// Flush to a data object
	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(builder))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Verify the object has sections
	require.Len(t, obj.Sections(), 1)

	// Open the section
	section, err := Open(context.Background(), obj.Sections()[0], kind)
	require.NoError(t, err)
	require.NotNil(t, section)

	// Verify columns
	columns := section.Columns()
	t.Logf("Number of columns: %d", len(columns))
	for i, col := range columns {
		t.Logf("Column %d: %s", i, col.Name)
	}
	require.Len(t, columns, 3)
	require.Equal(t, "id", columns[0].Name)
	require.Equal(t, "name", columns[1].Name)
	require.Equal(t, "timestamp", columns[2].Name)
}

func TestSchemaExtensionAndBackfilling(t *testing.T) {
	// Start with a basic schema
	schema := NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
	})

	kind := "generic"
	builder := NewBuilder(kind, schema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})

	// Append first entity with initial schema
	entity1 := NewEntity(schema, []dataset.Value{
		dataset.Int64Value(1),
		dataset.BinaryValue([]byte("Alice")),
	})
	err := builder.Append(entity1)
	require.NoError(t, err)
	require.Equal(t, 2, builder.currentSchema.NumFields())

	// Append second entity with an additional field
	extendedSchema := NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "age", Type: arrow.PrimitiveTypes.Int64},
	})
	entity2 := NewEntity(extendedSchema, []dataset.Value{
		dataset.Int64Value(2),
		dataset.BinaryValue([]byte("Bob")),
		dataset.Int64Value(30),
	})
	err = builder.Append(entity2)
	require.NoError(t, err)
	// Builder schema should now have 3 fields
	require.Equal(t, 3, builder.currentSchema.NumFields())

	// Append third entity with yet another additional field
	furtherExtendedSchema := NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "city", Type: arrow.BinaryTypes.String},
		{Name: "age", Type: arrow.PrimitiveTypes.Int64},
	})
	entity3 := NewEntity(furtherExtendedSchema, []dataset.Value{
		dataset.BinaryValue([]byte("Charlie")),
		dataset.Int64Value(3),
		dataset.BinaryValue([]byte("NYC")),
		dataset.Int64Value(25),
	})
	err = builder.Append(entity3)
	require.NoError(t, err)
	// Builder schema should now have 4 fields
	require.Equal(t, 4, builder.currentSchema.NumFields())

	// Append fourth entity with only partial fields (subset)
	partialSchema := NewSchema([]arrow.Field{
		{Name: "city", Type: arrow.BinaryTypes.String},
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
	})
	entity4 := NewEntity(partialSchema, []dataset.Value{
		dataset.BinaryValue([]byte("LA")),
		dataset.Int64Value(4),
	})
	err = builder.Append(entity4)
	require.NoError(t, err)
	// Builder schema should still have 4 fields
	require.Equal(t, 4, builder.currentSchema.NumFields())

	// Flush to a data object
	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(builder))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Verify the object has sections
	require.Len(t, obj.Sections(), 1)

	// Open the section
	section, err := Open(context.Background(), obj.Sections()[0], kind)
	require.NoError(t, err)
	require.NotNil(t, section)

	// Verify all 4 columns are present
	columns := section.Columns()
	require.Len(t, columns, 4)
	require.Equal(t, "id", columns[0].Name)
	require.Equal(t, "name", columns[1].Name)
	require.Equal(t, "age", columns[2].Name)
	require.Equal(t, "city", columns[3].Name)

	// Note: The actual backfilling with null values happens during encoding.
	// Entity 1 will have null values for "age" and "city"
	// Entity 2 will have null value for "city"
	// Entity 3 will have all values
	// Entity 4 will have null values for "name" and "age"
}
