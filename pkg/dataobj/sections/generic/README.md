# Generic Section Builder

The generic section builder provides a flexible way to create dataobj sections with dynamic schemas. Unlike other section builders that have fixed structures, the generic builder accepts a schema defined with Arrow fields and entities with corresponding values.

## Features

- **Dynamic Schema**: Define columns using Arrow field types
- **Type Support**: Supports INT64, UINT64, STRING, BINARY, and TIMESTAMP types
- **Sorting**: Optional sort information for columns
- **Compression**: Automatic compression based on data types
- **Statistics**: Range statistics for efficient querying

## Usage

### Basic Example

```go
import (
    "github.com/apache/arrow-go/v18/arrow"
    "github.com/grafana/loki/v3/pkg/dataobj"
    "github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
    "github.com/grafana/loki/v3/pkg/dataobj/sections/generic"
)

// Define a schema
schema := generic.NewSchema([]arrow.Field{
    {Name: "id", Type: arrow.PrimitiveTypes.Int64},
    {Name: "name", Type: arrow.BinaryTypes.String},
    {Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
})

// Create a builder with a custom kind
builder := generic.NewBuilder("user-events", schema, nil, 1024*1024, 10000)
builder.SetTenant("my-tenant")

// Append entities
entity := generic.NewEntity(schema, []dataset.Value{
    dataset.Int64Value(1),
    dataset.BinaryValue([]byte("Alice")),
    dataset.Int64Value(1234567890000000000),
})
err := builder.Append(entity)
if err != nil {
    // Handle error
}

// Check the section type
sectionType := builder.Type()
// sectionType.Namespace = "github.com/grafana/loki"
// sectionType.Kind = "user-events"

// Flush to a data object
objectBuilder := dataobj.NewBuilder(nil)
err = objectBuilder.Append(builder)
if err != nil {
    // Handle error
}

obj, closer, err := objectBuilder.Flush()
if err != nil {
    // Handle error
}
defer closer.Close()
```

### With Sorting

```go
// Define a schema with sort information
schema := generic.NewSchemaWithSort(
    []arrow.Field{
        {Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
        {Name: "id", Type: arrow.PrimitiveTypes.Int64},
    },
    []int{0},    // Sort by first column (timestamp)
    []int{1},    // Ascending order (use -1 for descending)
)

builder := generic.NewBuilder("sorted-events", schema, nil, 1024*1024, 10000)
// ... append entities and flush
```

### Reading Back

```go
import (
    "context"
    "github.com/grafana/loki/v3/pkg/dataobj/sections/generic"
)

// Open the section
section, err := generic.Open(context.Background(), obj.Sections()[0])
if err != nil {
    // Handle error
}

// Get columns
columns := section.Columns()
for _, col := range columns {
    fmt.Printf("Column: %s, Type: %s\n", col.Name, col.Type)
}

// Check sort order
colType, sortDir, err := section.PrimarySortOrder()
if err == nil {
    fmt.Printf("Sorted by: %s, Direction: %v\n", colType, sortDir)
}
```

## Supported Arrow Types

The generic builder supports the following Arrow types:

- **INT64**: 64-bit signed integers (uses delta encoding)
- **UINT64**: 64-bit unsigned integers (uses delta encoding)
- **STRING**: UTF-8 strings (uses ZSTD compression)
- **BINARY**: Binary data (uses ZSTD compression)
- **TIMESTAMP**: Nanosecond timestamps (uses delta encoding)

## Section Kind

The `kind` parameter in `NewBuilder()` defines the section type identifier. This allows you to create different types of generic sections:

```go
// Create different section types
logsBuilder := generic.NewBuilder("user-logs", schema, nil, 1024*1024, 10000)
metricsBuilder := generic.NewBuilder("custom-metrics", schema, nil, 1024*1024, 10000)
eventsBuilder := generic.NewBuilder("app-events", schema, nil, 1024*1024, 10000)

// Each builder has a unique section type
logsType := logsBuilder.Type()
// logsType.Namespace = "github.com/grafana/loki"
// logsType.Kind = "user-logs"
// logsType.Version = 1
```

The section type is used to identify and filter sections when reading from data objects:

```go
// Find sections by kind
for _, section := range obj.Sections() {
    if section.Type.Kind == "user-logs" {
        // Process user logs section
    }
}
```

## Architecture

The generic builder follows the same pattern as other section builders:

1. **Schema Definition**: Define the structure using Arrow fields
2. **Entity Creation**: Create entities with values matching the schema
3. **Append**: Add entities to the builder
4. **Flush**: Encode and write to a data object

Internally, the builder:
- Creates column builders for each field
- Populates columns with entity data
- Encodes columns using appropriate encoding/compression
- Writes to the columnar format

## Performance Considerations

- **Integer Columns**: Use delta encoding without compression for better performance
- **String/Binary Columns**: Use ZSTD compression for smaller size
- **Page Size**: Default page size is 1MB, adjust based on your use case
- **Page Row Count**: Default is 10,000 rows per page

### Size Estimation

The `EstimatedSize()` method provides an accurate estimate of the encoded section size by:

1. **Per-Column Calculation**: Tracks the raw size of each column separately
2. **Type-Aware Compression**: Applies different compression ratios based on field types:
   - **INT64/UINT64/TIMESTAMP** with delta encoding: ~35% of original size
   - **STRING/BINARY** with ZSTD compression: ~20% of original size
3. **Metadata Overhead**: Adds ~5% for column descriptors, page headers, etc.

Example:
```go
builder := NewBuilder("data", schema, nil, 1024*1024, 10000)

// Append 100 entities
for i := 0; i < 100; i++ {
    entity := NewEntity(schema, []dataset.Value{
        dataset.Int64Value(int64(i)),           // 8 bytes each
        dataset.BinaryValue([]byte("name")),    // 4 bytes each
    })
    builder.Append(entity)
}

// Estimated size calculation:
// - id column: 100 * 8 = 800 bytes * 0.35 = 280 bytes
// - name column: 100 * 4 = 400 bytes * 0.20 = 80 bytes
// - overhead: (280 + 80) * 0.05 = 18 bytes
// Total: ~378 bytes (vs 1200 bytes raw)

estimatedSize := builder.EstimatedSize() // Returns ~378
```

This estimation helps with:
- **Memory Management**: Decide when to flush based on size limits
- **Resource Planning**: Estimate storage requirements
- **Performance Optimization**: Balance between compression and speed

## Validation

The builder validates entities on `Append()`:
- **Value Count**: Entity must have the same number of values as its schema has fields
- **Type Checking**: Each value must match the expected type from the schema
  - INT64 fields require `dataset.Int64Value()`
  - UINT64 fields require `dataset.Uint64Value()`
  - STRING/BINARY fields require `dataset.BinaryValue()`
  - TIMESTAMP fields require `dataset.Int64Value()` (nanoseconds)
- **Nil Values**: Nil values are allowed for any field type

Invalid entities will return a descriptive error on `Append()`:
```go
wrongSchema := NewSchema([]arrow.Field{
    {Name: "id", Type: arrow.PrimitiveTypes.Int64},
    {Name: "name", Type: arrow.PrimitiveTypes.Int64}, // Wrong type!
})
entity := NewEntity(wrongSchema, []dataset.Value{
    dataset.Int64Value(1),
    dataset.Int64Value(123), // Should be BinaryValue for string field
})

err := builder.Append(entity)
// Error: field name: type mismatch between entity (int64) and builder (utf8)
```

## Schema Extension and Backfilling

The generic builder supports dynamic schema evolution. Entities can have different schemas, and the builder will automatically extend its schema to accommodate new fields:

### How It Works

1. **Schema Extension**: When an entity with new fields is appended, those fields are automatically added to the builder's schema
2. **Type Safety**: If an entity has a field with the same name as an existing field, the types must match
3. **Backfilling**: Previous entities that don't have the new fields will be backfilled with null values during encoding
4. **Subset Schemas**: Entities can have a subset of the builder's fields - missing fields are filled with nulls

### Example

```go
// Start with a basic schema
initialSchema := NewSchema([]arrow.Field{
    {Name: "id", Type: arrow.PrimitiveTypes.Int64},
    {Name: "name", Type: arrow.BinaryTypes.String},
})

builder := NewBuilder("evolving-data", initialSchema, nil, 1024*1024, 10000)

// Append first entity with initial schema
entity1 := NewEntity(initialSchema, []dataset.Value{
    dataset.Int64Value(1),
    dataset.BinaryValue([]byte("Alice")),
})
builder.Append(entity1)
// Builder schema: [id, name]

// Append second entity with an additional field
extendedSchema := NewSchema([]arrow.Field{
    {Name: "id", Type: arrow.PrimitiveTypes.Int64},
    {Name: "name", Type: arrow.BinaryTypes.String},
    {Name: "age", Type: arrow.PrimitiveTypes.Int64}, // New field!
})
entity2 := NewEntity(extendedSchema, []dataset.Value{
    dataset.Int64Value(2),
    dataset.BinaryValue([]byte("Bob")),
    dataset.Int64Value(30),
})
builder.Append(entity2)
// Builder schema: [id, name, age]
// entity1 will be backfilled with null for "age"

// Append third entity with only some fields
partialSchema := NewSchema([]arrow.Field{
    {Name: "id", Type: arrow.PrimitiveTypes.Int64},
    {Name: "age", Type: arrow.PrimitiveTypes.Int64},
})
entity3 := NewEntity(partialSchema, []dataset.Value{
    dataset.Int64Value(3),
    dataset.Int64Value(25),
})
builder.Append(entity3)
// Builder schema: [id, name, age]
// entity3 will be backfilled with null for "name"

// When flushed, all entities will have the same columns:
// entity1: id=1, name="Alice", age=null
// entity2: id=2, name="Bob", age=30
// entity3: id=3, name=null, age=25
```

### Use Cases

Schema extension is useful for:

- **Log Aggregation**: Different log sources may have different fields
- **Event Streaming**: Events may evolve over time with new attributes
- **Multi-Tenant Data**: Different tenants may have custom fields
- **Flexible Data Models**: When the schema isn't known upfront

### Performance Considerations

- Schema extension is efficient - it only allocates new memory when fields are added
- Backfilling happens during encoding and doesn't require re-processing previous entities
- Each entity keeps its original schema, avoiding unnecessary memory overhead
- The builder's schema grows monotonically (fields are only added, never removed)

### Type Conflicts

If you try to append an entity with a field that has a different type than the builder's schema, you'll get an error:

```go
// Builder has "age" as INT64
firstSchema := NewSchema([]arrow.Field{
    {Name: "age", Type: arrow.PrimitiveTypes.Int64},
})
builder := NewBuilder("data", firstSchema, nil, 1024*1024, 10000)

// Try to append entity with "age" as STRING
conflictSchema := NewSchema([]arrow.Field{
    {Name: "age", Type: arrow.BinaryTypes.String}, // Type mismatch!
})
entity := NewEntity(conflictSchema, []dataset.Value{
    dataset.BinaryValue([]byte("30")),
})

err := builder.Append(entity)
// Error: field age: type mismatch between entity (utf8) and builder (int64)
```

## Thread Safety

The builder is **not** thread-safe. Use separate builders for concurrent operations or add external synchronization.

