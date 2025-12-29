package engine_lab

/*
================================================================================
ARROW FUNDAMENTALS - Apache Arrow Columnar Data Format
================================================================================

This file covers Apache Arrow concepts that are fundamental to understanding
the Loki Query Engine V2. Arrow is the core data format used throughout the
execution layer for processing query results.

================================================================================
WHAT IS APACHE ARROW?
================================================================================

Apache Arrow is a cross-language development platform for in-memory data.
It specifies a standardized language-independent columnar memory format for
flat and hierarchical data.

Key Benefits:
  - COLUMNAR FORMAT: Data stored column-by-column, not row-by-row
  - ZERO-COPY: Data can be shared between processes without serialization
  - VECTORIZED: Enables SIMD operations for fast processing
  - CACHE-EFFICIENT: Sequential memory access patterns

Why Loki V2 Uses Arrow:
  - Enables efficient columnar processing of log data
  - Better CPU cache utilization for aggregations
  - Standard format for data exchange between components
  - Supports streaming data with RecordBatch sequences

================================================================================
COLUMNAR VS ROW-ORIENTED DATA
================================================================================

ROW-ORIENTED (Traditional):
  Row 0: [timestamp=1000, message="log1", level="error"]
  Row 1: [timestamp=2000, message="log2", level="info"]
  Row 2: [timestamp=3000, message="log3", level="error"]

Memory Layout: [ts, msg, lvl, ts, msg, lvl, ts, msg, lvl]

COLUMNAR (Arrow):
  timestamps: [1000, 2000, 3000]
  messages:   ["log1", "log2", "log3"]
  levels:     ["error", "info", "error"]

Memory Layout: [ts, ts, ts] [msg, msg, msg] [lvl, lvl, lvl]

Benefits of Columnar:
  1. Better compression (similar values together)
  2. Faster aggregations (only read needed columns)
  3. SIMD-friendly (process multiple values at once)
  4. Cache-efficient (sequential access patterns)

================================================================================
ARROW DATA STRUCTURES
================================================================================

SCHEMA:
  - Defines column names and types
  - Metadata for additional information
  - Immutable once created

ARRAY:
  - Contiguous memory buffer of values
  - Type-specific (Int64Array, StringArray, TimestampArray, etc.)
  - Supports null values via validity bitmap

RECORDBATCH:
  - Collection of Arrays with a shared Schema
  - Unit of data transfer
  - All arrays must have same length

================================================================================
LOKI SEMANTIC CONVENTIONS
================================================================================

Loki uses specific naming conventions for columns:

FQN Format: <datatype>.<columntype>.<name>

Column Types:
  - builtin.*   : Engine columns always present on log entries (timestamp, message)
  - generated.* : Columns created during query execution (value)
  - label.*     : Stream labels (app, namespace, cluster)
  - metadata.*  : Structured metadata (traceID, spanID)
  - parsed.*    : Fields extracted during query (from json/logfmt)
  - ambiguous.* : Type unknown until physical planning

Builtin Columns (always present on log entries):
  - builtin.timestamp : Log entry timestamp (nanoseconds)
  - builtin.message   : Log line text content

Generated Columns (created during metric query execution):
  - generated.value   : Numeric value from aggregations

================================================================================
MEMORY MANAGEMENT
================================================================================

Arrow uses reference counting for memory management:

  record.Retain()  - Increment reference count
  record.Release() - Decrement reference count, free when zero

IMPORTANT RULES:
  1. Always Release() records when done
  2. Call Retain() before passing to another goroutine
  3. Use defer record.Release() for exception safety
  4. Memory is freed when reference count reaches zero

Memory Allocators:
  - memory.NewGoAllocator(): Uses Go's memory allocator
  - memory.NewCheckedAllocator(): Debug allocator that tracks leaks

================================================================================
*/

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// ============================================================================
// BASIC ARROW CONCEPTS
// ============================================================================

/*
TestArrowSchema demonstrates Arrow schema creation and inspection.

A Schema defines the structure of data - column names and their types.
Schemas are immutable and shared across RecordBatches.
*/
func TestArrowSchema(t *testing.T) {
	t.Run("create_schema_with_primitive_types", func(t *testing.T) {
		/*
		   ============================================================================
		   SCHEMA WITH PRIMITIVE TYPES
		   ============================================================================

		   Arrow supports many primitive types:
		   - Int8, Int16, Int32, Int64 (signed integers)
		   - Uint8, Uint16, Uint32, Uint64 (unsigned integers)
		   - Float32, Float64 (floating point)
		   - Boolean
		   - String, Binary (variable length)
		   - Timestamp (with timezone support)

		   Fields can be nullable or non-nullable.
		*/
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
				{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
				{Name: "score", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
				{Name: "active", Type: arrow.FixedWidthTypes.Boolean, Nullable: false},
			},
			nil, // metadata (optional)
		)

		require.Equal(t, 4, schema.NumFields())

		// Access fields by index
		idField := schema.Field(0)
		require.Equal(t, "id", idField.Name)
		require.Equal(t, arrow.PrimitiveTypes.Int64, idField.Type)
		require.False(t, idField.Nullable)

		// Access fields by name
		nameIdx := schema.FieldIndices("name")
		require.Len(t, nameIdx, 1, "should find exactly one 'name' field")
		require.Equal(t, 1, nameIdx[0]) // 'name' is the second field (index 1)

		t.Logf("Schema with %d fields:", schema.NumFields())
		for i := 0; i < schema.NumFields(); i++ {
			f := schema.Field(i)
			t.Logf("  %d: %s (%s, nullable=%v)", i, f.Name, f.Type, f.Nullable)
		}
	})

	t.Run("schema_with_timestamp", func(t *testing.T) {
		/*
		   ============================================================================
		   TIMESTAMP TYPES
		   ============================================================================

		   Arrow timestamps have precision and optional timezone:

		   Precision:
		   - Second
		   - Millisecond
		   - Microsecond
		   - Nanosecond (used by Loki)

		   Timezone:
		   - Empty string = naive timestamp (no timezone)
		   - "UTC" = UTC timezone
		   - "America/New_York" = specific timezone
		*/
		// Nanosecond precision with UTC timezone (Loki's timestamp format)
		tsType := arrow.FixedWidthTypes.Timestamp_ns

		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: "timestamp", Type: tsType, Nullable: false},
				{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
			},
			nil,
		)

		tsField := schema.Field(0)
		require.Equal(t, "timestamp", tsField.Name)

		// Type assertion to get timestamp details
		tsArrowType, ok := tsField.Type.(*arrow.TimestampType)
		require.True(t, ok)
		require.Equal(t, arrow.Nanosecond, tsArrowType.Unit)

		t.Logf("Timestamp type: unit=%v, timezone=%s", tsArrowType.Unit, tsArrowType.TimeZone)
	})

	t.Run("schema_with_metadata", func(t *testing.T) {
		/*
		   ============================================================================
		   SCHEMA METADATA
		   ============================================================================

		   Schemas can carry arbitrary key-value metadata.
		   Loki uses metadata to store additional information about columns.
		*/
		metadata := arrow.NewMetadata(
			[]string{"version", "source"},
			[]string{"1.0", "loki"},
		)

		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: "data", Type: arrow.BinaryTypes.String},
			},
			&metadata,
		)

		require.True(t, schema.HasMetadata())
		require.Equal(t, 2, schema.Metadata().Len())

		// Access metadata by key
		idx := schema.Metadata().FindKey("version")
		require.GreaterOrEqual(t, idx, 0)
		require.Equal(t, "1.0", schema.Metadata().Values()[idx])

		t.Logf("Schema metadata: %v", schema.Metadata())
	})
}

/*
TestArrowArrays demonstrates Arrow array creation and manipulation.

Arrays are the building blocks of RecordBatches. Each column in a
RecordBatch is an Array of a specific type.
*/
func TestArrowArrays(t *testing.T) {
	t.Run("build_int64_array", func(t *testing.T) {
		/*
		   ============================================================================
		   BUILDING ARRAYS WITH BUILDERS
		   ============================================================================

		   Arrays are built using type-specific builders:
		   1. Create builder with allocator
		   2. Append values (with optional null values)
		   3. Call NewArray() to finalize
		   4. Release the array when done

		   Builders support:
		   - Append(value) - Add a non-null value
		   - AppendNull() - Add a null value
		   - AppendValues(slice, valid) - Batch append
		   - Reserve(n) - Pre-allocate capacity
		*/
		alloc := memory.NewGoAllocator()
		builder := array.NewInt64Builder(alloc)
		defer builder.Release()

		// Append values
		builder.Append(100)
		builder.Append(200)
		builder.AppendNull() // Null value
		builder.Append(400)

		// Build the array
		arr := builder.NewArray()
		defer arr.Release()

		require.Equal(t, 4, arr.Len())
		require.Equal(t, 1, arr.NullN()) // 1 null value

		// Type assertion to access values
		int64Arr := arr.(*array.Int64)
		require.Equal(t, int64(100), int64Arr.Value(0))
		require.Equal(t, int64(200), int64Arr.Value(1))
		require.True(t, int64Arr.IsNull(2)) // Check for null
		require.Equal(t, int64(400), int64Arr.Value(3))

		t.Logf("Int64 array: len=%d, nulls=%d", arr.Len(), arr.NullN())
		for i := 0; i < arr.Len(); i++ {
			if int64Arr.IsNull(i) {
				t.Logf("  [%d]: NULL", i)
			} else {
				t.Logf("  [%d]: %d", i, int64Arr.Value(i))
			}
		}
	})

	t.Run("build_string_array", func(t *testing.T) {
		/*
		   ============================================================================
		   STRING ARRAYS
		   ============================================================================

		   String arrays use variable-length encoding:
		   - Offsets array: Start position of each string
		   - Values buffer: Concatenated string data

		   Memory layout for ["hello", "world", "!"]:
		   Offsets: [0, 5, 10, 11]
		   Values:  "helloworld!"
		*/
		alloc := memory.NewGoAllocator()
		builder := array.NewStringBuilder(alloc)
		defer builder.Release()

		builder.Append("hello")
		builder.Append("world")
		builder.AppendNull()
		builder.Append("loki")

		arr := builder.NewArray()
		defer arr.Release()

		strArr := arr.(*array.String)
		require.Equal(t, 4, strArr.Len())
		require.Equal(t, "hello", strArr.Value(0))
		require.Equal(t, "world", strArr.Value(1))
		require.True(t, strArr.IsNull(2))
		require.Equal(t, "loki", strArr.Value(3))

		t.Logf("String array: len=%d, nulls=%d", arr.Len(), arr.NullN())
	})

	t.Run("build_timestamp_array", func(t *testing.T) {
		/*
		   ============================================================================
		   TIMESTAMP ARRAYS
		   ============================================================================

		   Timestamp arrays store time as integers (nanoseconds since epoch).
		   Loki uses nanosecond precision for log timestamps.

		   Conversion:
		   - time.Time → int64 nanoseconds: t.UnixNano()
		   - int64 nanoseconds → time.Time: time.Unix(0, ns)
		*/
		alloc := memory.NewGoAllocator()
		tsType := arrow.FixedWidthTypes.Timestamp_ns
		builder := array.NewTimestampBuilder(alloc, tsType.(*arrow.TimestampType))
		defer builder.Release()

		// Append timestamps as nanoseconds
		t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		t2 := time.Date(2024, 1, 1, 12, 0, 1, 0, time.UTC)
		t3 := time.Date(2024, 1, 1, 12, 0, 2, 0, time.UTC)

		builder.Append(arrow.Timestamp(t1.UnixNano()))
		builder.Append(arrow.Timestamp(t2.UnixNano()))
		builder.Append(arrow.Timestamp(t3.UnixNano()))

		arr := builder.NewArray()
		defer arr.Release()

		tsArr := arr.(*array.Timestamp)
		require.Equal(t, 3, tsArr.Len())

		// Convert back to time.Time (must use UTC() for comparison)
		readT1 := time.Unix(0, int64(tsArr.Value(0))).UTC()
		require.Equal(t, t1, readT1)

		t.Logf("Timestamp array: len=%d", arr.Len())
		for i := 0; i < tsArr.Len(); i++ {
			ts := time.Unix(0, int64(tsArr.Value(i)))
			t.Logf("  [%d]: %v", i, ts)
		}
	})
}

/*
TestArrowRecordBatch demonstrates RecordBatch creation and manipulation.

A RecordBatch is a collection of equal-length Arrays with a shared Schema.
It's the primary unit of data in Arrow and the V2 engine.
*/
func TestArrowRecordBatch(t *testing.T) {
	t.Run("create_recordbatch_from_arrays", func(t *testing.T) {
		/*
		   ============================================================================
		   RECORDBATCH FROM ARRAYS
		   ============================================================================

		   To create a RecordBatch:
		   1. Create a Schema
		   2. Build arrays for each column
		   3. Use array.NewRecord() to combine them

		   All arrays must have the same length!
		*/
		alloc := memory.NewGoAllocator()

		// Define schema
		schema := arrow.NewSchema(
			[]arrow.Field{
				{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns, Nullable: false},
				{Name: "message", Type: arrow.BinaryTypes.String, Nullable: false},
				{Name: "level", Type: arrow.BinaryTypes.String, Nullable: true},
			},
			nil,
		)

		// Build timestamp array
		tsBuilder := array.NewTimestampBuilder(alloc, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
		tsBuilder.Append(arrow.Timestamp(1000000000))
		tsBuilder.Append(arrow.Timestamp(2000000000))
		tsBuilder.Append(arrow.Timestamp(3000000000))
		tsArr := tsBuilder.NewArray()
		defer tsArr.Release()
		tsBuilder.Release()

		// Build message array
		msgBuilder := array.NewStringBuilder(alloc)
		msgBuilder.Append("error occurred")
		msgBuilder.Append("request completed")
		msgBuilder.Append("connection lost")
		msgArr := msgBuilder.NewArray()
		defer msgArr.Release()
		msgBuilder.Release()

		// Build level array
		levelBuilder := array.NewStringBuilder(alloc)
		levelBuilder.Append("error")
		levelBuilder.Append("info")
		levelBuilder.Append("error")
		levelArr := levelBuilder.NewArray()
		defer levelArr.Release()
		levelBuilder.Release()

		// Create RecordBatch
		record := array.NewRecord(schema, []arrow.Array{tsArr, msgArr, levelArr}, 3)
		defer record.Release()

		require.Equal(t, int64(3), record.NumRows())
		require.Equal(t, int64(3), record.NumCols())

		// Access columns by index
		col0 := record.Column(0)
		require.Equal(t, 3, col0.Len())

		// Access columns by name
		msgCol := record.Column(1).(*array.String)
		require.Equal(t, "error occurred", msgCol.Value(0))

		t.Logf("RecordBatch: rows=%d, cols=%d", record.NumRows(), record.NumCols())
		t.Logf("Schema: %v", record.Schema())
	})

	t.Run("recordbatch_with_arrowtest_helper", func(t *testing.T) {
		/*
		   ============================================================================
		   USING ARROWTEST HELPER
		   ============================================================================

		   The arrowtest package provides a convenient way to create test RecordBatches
		   using a row-oriented syntax. The helper handles:
		   - Builder creation and cleanup
		   - Type inference from values
		   - Schema construction

		   This is the preferred method for test code.
		*/
		alloc := memory.NewGoAllocator()

		// Define columns using Loki's semantic conventions
		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colLevel := semconv.NewIdentifier("level", types.ColumnTypeLabel, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colLevel, false),
			},
			nil,
		)

		// Create rows using map syntax (much more readable!)
		rows := arrowtest.Rows{
			{
				colTs.FQN():    time.Unix(0, 1000000000).UTC(),
				colMsg.FQN():   "error: connection refused",
				colLevel.FQN(): "error",
			},
			{
				colTs.FQN():    time.Unix(0, 2000000000).UTC(),
				colMsg.FQN():   "request processed successfully",
				colLevel.FQN(): "info",
			},
			{
				colTs.FQN():    time.Unix(0, 3000000000).UTC(),
				colMsg.FQN():   "warning: high latency",
				colLevel.FQN(): "warn",
			},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		require.Equal(t, int64(3), record.NumRows())
		require.Equal(t, int64(3), record.NumCols())

		t.Logf("RecordBatch created with arrowtest helper:")
		t.Logf("  Rows: %d", record.NumRows())
		t.Logf("  Columns: %d", record.NumCols())
		for i := 0; i < int(record.NumCols()); i++ {
			t.Logf("  Column %d: %s", i, record.Schema().Field(i).Name)
		}
	})
}

// ============================================================================
// LOKI SEMANTIC CONVENTIONS
// ============================================================================

/*
TestLokiSemanticConventions demonstrates Loki's column naming conventions.

Loki uses a specific naming scheme for columns that encodes:
- Data type (utf8, timestamp_ns, float64, etc.)
- Column type (builtin, label, metadata, parsed, ambiguous)
- Column name
*/
func TestLokiSemanticConventions(t *testing.T) {
	t.Run("builtin_column_identifiers", func(t *testing.T) {
		/*
		   ============================================================================
		   BUILTIN AND GENERATED COLUMNS
		   ============================================================================

		   Engine-defined columns fall into two categories:

		   BUILTIN COLUMNS (exist on all log entries):

		   1. timestamp (builtin.timestamp)
		      - Log entry timestamp
		      - Nanosecond precision
		      - Used for time-based filtering and ordering

		   2. message (builtin.message)
		      - Log line content
		      - UTF-8 string
		      - Input for parsers (json, logfmt)

		   GENERATED COLUMNS (created during query execution):

		   3. value (generated.value)
		      - Numeric value for metric queries
		      - Float64
		      - Result of aggregations (count_over_time, rate, etc.)
		      - Does not exist on raw log entries, only on metric results
		*/
		// Predefined column identifiers
		tsIdent := semconv.ColumnIdentTimestamp
		msgIdent := semconv.ColumnIdentMessage
		valIdent := semconv.ColumnIdentValue

		// Timestamp and Message are builtin columns (always present on log entries)
		require.Equal(t, types.ColumnTypeBuiltin, tsIdent.ColumnType())
		require.Equal(t, types.ColumnTypeBuiltin, msgIdent.ColumnType())

		// Value is a GENERATED column (created during metric query execution)
		require.Equal(t, types.ColumnTypeGenerated, valIdent.ColumnType())

		// Short names are the column names without prefix
		require.Equal(t, "timestamp", tsIdent.ShortName())
		require.Equal(t, "message", msgIdent.ShortName())
		require.Equal(t, "value", valIdent.ShortName())

		// FQN includes the full identifier path
		require.Contains(t, tsIdent.FQN(), "builtin.timestamp")
		require.Contains(t, msgIdent.FQN(), "builtin.message")
		require.Contains(t, valIdent.FQN(), "generated.value")

		t.Logf("Builtin columns (always present on log entries):")
		t.Logf("  Timestamp: %s", tsIdent.FQN())
		t.Logf("  Message: %s", msgIdent.FQN())
		t.Logf("Generated columns (created during query execution):")
		t.Logf("  Value: %s", valIdent.FQN())
	})

	t.Run("label_column_identifiers", func(t *testing.T) {
		/*
		   ============================================================================
		   LABEL COLUMNS
		   ============================================================================

		   Labels are set at ingestion time and define the stream identity:
		   - {app="loki", namespace="prod"} defines a unique stream
		   - Labels are indexed for fast filtering
		   - Changing labels creates a new stream

		   FQN format: <datatype>.label.<name>
		   Example: utf8.label.app
		*/
		appLabel := semconv.NewIdentifier("app", types.ColumnTypeLabel, types.Loki.String)
		nsLabel := semconv.NewIdentifier("namespace", types.ColumnTypeLabel, types.Loki.String)

		require.Equal(t, types.ColumnTypeLabel, appLabel.ColumnType())
		require.Equal(t, "app", appLabel.ShortName())
		require.Contains(t, appLabel.FQN(), "label.app")

		require.Equal(t, types.ColumnTypeLabel, nsLabel.ColumnType())
		require.Contains(t, nsLabel.FQN(), "label.namespace")

		t.Logf("Label columns:")
		t.Logf("  App: %s", appLabel.FQN())
		t.Logf("  Namespace: %s", nsLabel.FQN())
	})

	t.Run("metadata_column_identifiers", func(t *testing.T) {
		/*
		   ============================================================================
		   METADATA COLUMNS
		   ============================================================================

		   Structured metadata is per-line data that doesn't affect stream identity:
		   - traceID, spanID for distributed tracing
		   - pod, container for Kubernetes context
		   - Indexed for fast filtering
		   - Different lines in same stream can have different metadata

		   FQN format: <datatype>.metadata.<name>
		   Example: utf8.metadata.traceID
		*/
		traceID := semconv.NewIdentifier("traceID", types.ColumnTypeMetadata, types.Loki.String)
		spanID := semconv.NewIdentifier("spanID", types.ColumnTypeMetadata, types.Loki.String)

		require.Equal(t, types.ColumnTypeMetadata, traceID.ColumnType())
		require.Contains(t, traceID.FQN(), "metadata.traceID")

		require.Equal(t, types.ColumnTypeMetadata, spanID.ColumnType())
		require.Contains(t, spanID.FQN(), "metadata.spanID")

		t.Logf("Metadata columns:")
		t.Logf("  TraceID: %s", traceID.FQN())
		t.Logf("  SpanID: %s", spanID.FQN())
	})

	t.Run("parsed_column_identifiers", func(t *testing.T) {
		/*
		   ============================================================================
		   PARSED COLUMNS
		   ============================================================================

		   Parsed columns are extracted during query execution:
		   - Created by parsers: | json, | logfmt, | regexp, | pattern
		   - Only exist for the duration of the query
		   - Not indexed (computed at query time)

		   FQN format: <datatype>.parsed.<name>
		   Example: utf8.parsed.level

		   Example query:
		     {app="test"} | json | level="error"
		   Creates:
		     parsed.level, parsed.msg, etc. from JSON fields
		*/
		levelParsed := semconv.NewIdentifier("level", types.ColumnTypeParsed, types.Loki.String)
		msgParsed := semconv.NewIdentifier("msg", types.ColumnTypeParsed, types.Loki.String)

		require.Equal(t, types.ColumnTypeParsed, levelParsed.ColumnType())
		require.Contains(t, levelParsed.FQN(), "parsed.level")

		require.Equal(t, types.ColumnTypeParsed, msgParsed.ColumnType())
		require.Contains(t, msgParsed.FQN(), "parsed.msg")

		t.Logf("Parsed columns:")
		t.Logf("  Level: %s", levelParsed.FQN())
		t.Logf("  Msg: %s", msgParsed.FQN())
	})

	t.Run("ambiguous_column_identifiers", func(t *testing.T) {
		/*
		   ============================================================================
		   AMBIGUOUS COLUMNS
		   ============================================================================

		   During logical planning, the planner may not know if a field is:
		   - A stream label (label.*)
		   - Structured metadata (metadata.*)
		   - A parsed field (parsed.*)

		   The "ambiguous" type defers resolution to physical planning
		   when schema information is available.

		   FQN format: <datatype>.ambiguous.<name>
		   Example: utf8.ambiguous.level

		   Example query:
		     {app="test"} | json | level="error"
		   At logical planning:
		     ambiguous.level (could be label or parsed)
		   At physical planning:
		     parsed.level (resolved from json parser context)
		*/
		levelAmbig := semconv.NewIdentifier("level", types.ColumnTypeAmbiguous, types.Loki.String)

		require.Equal(t, types.ColumnTypeAmbiguous, levelAmbig.ColumnType())
		require.Contains(t, levelAmbig.FQN(), "ambiguous.level")

		t.Logf("Ambiguous column: %s", levelAmbig.FQN())
		t.Logf("  Will be resolved to label.*, metadata.*, or parsed.* during physical planning")
	})

	t.Run("create_schema_with_semantic_conventions", func(t *testing.T) {
		/*
		   ============================================================================
		   USING SEMCONV FOR SCHEMAS
		   ============================================================================

		   The semconv.FieldFromIdent helper creates Arrow fields from identifiers.
		   This ensures consistent naming and type mapping across the engine.
		*/
		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colApp := semconv.NewIdentifier("app", types.ColumnTypeLabel, types.Loki.String)
		colLevel := semconv.NewIdentifier("level", types.ColumnTypeParsed, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),   // not nullable
				semconv.FieldFromIdent(colMsg, false),  // not nullable
				semconv.FieldFromIdent(colApp, true),   // nullable
				semconv.FieldFromIdent(colLevel, true), // nullable
			},
			nil,
		)

		require.Equal(t, 4, schema.NumFields())

		t.Logf("Schema with semantic conventions:")
		for i := 0; i < schema.NumFields(); i++ {
			f := schema.Field(i)
			t.Logf("  %d: %s (type=%s, nullable=%v)", i, f.Name, f.Type, f.Nullable)
		}
	})
}

// ============================================================================
// MEMORY MANAGEMENT
// ============================================================================

/*
TestArrowMemoryManagement demonstrates Arrow's reference counting model.

Proper memory management is critical for avoiding leaks and use-after-free bugs.
*/
func TestArrowMemoryManagement(t *testing.T) {
	t.Run("reference_counting_basics", func(t *testing.T) {
		/*
		   ============================================================================
		   REFERENCE COUNTING
		   ============================================================================

		   Arrow objects are reference counted:
		   - New objects start with refcount = 1
		   - Retain() increments refcount
		   - Release() decrements refcount
		   - Memory freed when refcount reaches 0

		   RULES:
		   1. Always Release() objects you create or Retain()
		   2. Use defer to ensure Release() is called
		   3. Retain() before passing to another owner
		*/
		alloc := memory.NewGoAllocator()
		builder := array.NewInt64Builder(alloc)
		builder.Append(42)

		arr := builder.NewArray()
		// arr has refcount = 1

		// Create another reference
		arr.Retain()
		// arr has refcount = 2

		// Release one reference
		arr.Release()
		// arr has refcount = 1

		// Release final reference
		arr.Release()
		// arr has refcount = 0, memory freed

		builder.Release()
		// Don't access arr after this!

		t.Logf("Reference counting demonstration complete")
	})

	t.Run("defer_pattern_for_cleanup", func(t *testing.T) {
		/*
		   ============================================================================
		   DEFER PATTERN
		   ============================================================================

		   Use defer immediately after creation for exception safety:

		     record := createRecord()
		     defer record.Release()  // Always called, even on panic

		   This ensures cleanup even if the function returns early or panics.
		*/
		alloc := memory.NewGoAllocator()

		// Pattern: Create, defer release, use
		func() {
			builder := array.NewInt64Builder(alloc)
			defer builder.Release() // Cleanup builder

			builder.Append(1)
			builder.Append(2)

			arr := builder.NewArray()
			defer arr.Release() // Cleanup array

			require.Equal(t, 2, arr.Len())
		}()

		t.Logf("Defer pattern ensures cleanup")
	})

	t.Run("recordbatch_lifecycle", func(t *testing.T) {
		/*
		   ============================================================================
		   RECORDBATCH LIFECYCLE
		   ============================================================================

		   RecordBatches hold references to their column arrays.
		   When you Release() a RecordBatch, it also releases its arrays.

		   Lifecycle:
		   1. Create arrays (refcount = 1 each)
		   2. Create RecordBatch (arrays refcount incremented)
		   3. Release arrays (safe, RecordBatch still holds reference)
		   4. Use RecordBatch
		   5. Release RecordBatch (arrays refcount decremented, may free)
		*/
		alloc := memory.NewGoAllocator()

		schema := arrow.NewSchema(
			[]arrow.Field{{Name: "val", Type: arrow.PrimitiveTypes.Int64}},
			nil,
		)

		builder := array.NewInt64Builder(alloc)
		builder.Append(100)
		arr := builder.NewArray()
		// arr refcount = 1

		record := array.NewRecord(schema, []arrow.Array{arr}, 1)
		// arr refcount = 2 (record holds reference)

		arr.Release()
		// arr refcount = 1 (record still holds reference)
		// Safe to continue using record!

		require.Equal(t, int64(1), record.NumRows())

		builder.Release()
		record.Release()
		// arr refcount = 0, memory freed

		t.Logf("RecordBatch lifecycle demonstration complete")
	})

	t.Run("checked_allocator_for_debugging", func(t *testing.T) {
		/*
		   ============================================================================
		   CHECKED ALLOCATOR (DEBUGGING)
		   ============================================================================

		   The CheckedAllocator tracks allocations and can detect leaks.
		   Use it in tests to verify proper cleanup.

		   Note: This adds overhead, only use for debugging/testing.
		*/
		// Create checked allocator
		checked := memory.NewCheckedAllocator(memory.NewGoAllocator())

		// Create and properly release
		builder := array.NewInt64Builder(checked)
		builder.Append(42)
		arr := builder.NewArray()

		require.Equal(t, 1, arr.Len())

		arr.Release()
		builder.Release()

		// Assert no leaks
		checked.AssertSize(t, 0)

		t.Logf("No memory leaks detected")
	})
}

// ============================================================================
// ADVANCED PATTERNS
// ============================================================================

/*
TestArrowAdvancedPatterns demonstrates advanced Arrow usage patterns.
*/
func TestArrowAdvancedPatterns(t *testing.T) {
	t.Run("batch_append_with_validity", func(t *testing.T) {
		/*
		   ============================================================================
		   BATCH APPEND WITH VALIDITY BITMAP
		   ============================================================================

		   For high performance, use batch append with a validity bitmap:
		   - AppendValues(values, valid) appends multiple values at once
		   - valid[i] = true means values[i] is valid (not null)
		   - valid[i] = false means values[i] is null (value ignored)
		*/
		alloc := memory.NewGoAllocator()
		builder := array.NewInt64Builder(alloc)
		defer builder.Release()

		values := []int64{100, 200, 300, 400, 500}
		valid := []bool{true, true, false, true, false} // 300 and 500 are null

		builder.AppendValues(values, valid)

		arr := builder.NewArray()
		defer arr.Release()

		int64Arr := arr.(*array.Int64)
		require.Equal(t, 5, int64Arr.Len())
		require.Equal(t, 2, int64Arr.NullN())
		require.False(t, int64Arr.IsNull(0))
		require.False(t, int64Arr.IsNull(1))
		require.True(t, int64Arr.IsNull(2)) // null
		require.False(t, int64Arr.IsNull(3))
		require.True(t, int64Arr.IsNull(4)) // null

		t.Logf("Batch append: %d values, %d nulls", arr.Len(), arr.NullN())
	})

	t.Run("slice_recordbatch", func(t *testing.T) {
		/*
		   ============================================================================
		   SLICING RECORDBATCHES
		   ============================================================================

		   RecordBatches can be sliced to create views without copying data:
		   - NewSlice(start, end) creates a view of rows [start, end)
		   - The slice shares memory with the original
		   - Modifying one affects the other (both reference same buffers)
		*/
		alloc := memory.NewGoAllocator()

		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
			},
			nil,
		)

		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1000).UTC(), colMsg.FQN(): "row 0"},
			{colTs.FQN(): time.Unix(0, 2000).UTC(), colMsg.FQN(): "row 1"},
			{colTs.FQN(): time.Unix(0, 3000).UTC(), colMsg.FQN(): "row 2"},
			{colTs.FQN(): time.Unix(0, 4000).UTC(), colMsg.FQN(): "row 3"},
			{colTs.FQN(): time.Unix(0, 5000).UTC(), colMsg.FQN(): "row 4"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// Slice rows 1-3 (exclusive end)
		sliced := record.NewSlice(1, 4)
		defer sliced.Release()

		require.Equal(t, int64(5), record.NumRows())
		require.Equal(t, int64(3), sliced.NumRows())

		// Verify slice contents
		msgCol := sliced.Column(1).(*array.String)
		require.Equal(t, "row 1", msgCol.Value(0))
		require.Equal(t, "row 2", msgCol.Value(1))
		require.Equal(t, "row 3", msgCol.Value(2))

		t.Logf("Sliced RecordBatch: %d rows (from %d)", sliced.NumRows(), record.NumRows())
	})

	t.Run("iterate_over_columns", func(t *testing.T) {
		/*
		   ============================================================================
		   ITERATING OVER RECORDBATCH
		   ============================================================================

		   Common patterns for processing RecordBatch data:
		   - Iterate by column for aggregations
		   - Iterate by row for filtering/transformation
		   - Use type assertions for type-specific operations
		*/
		alloc := memory.NewGoAllocator()

		colTs := semconv.ColumnIdentTimestamp
		colVal := semconv.ColumnIdentValue

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colVal, false),
			},
			nil,
		)

		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1000).UTC(), colVal.FQN(): float64(10.0)},
			{colTs.FQN(): time.Unix(0, 2000).UTC(), colVal.FQN(): float64(20.0)},
			{colTs.FQN(): time.Unix(0, 3000).UTC(), colVal.FQN(): float64(30.0)},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// Column-oriented: Sum all values
		valCol := record.Column(1).(*array.Float64)
		sum := 0.0
		for i := 0; i < valCol.Len(); i++ {
			if !valCol.IsNull(i) {
				sum += valCol.Value(i)
			}
		}
		require.Equal(t, 60.0, sum)

		// Row-oriented: Process each row
		tsCol := record.Column(0).(*array.Timestamp)
		for i := 0; i < int(record.NumRows()); i++ {
			ts := time.Unix(0, int64(tsCol.Value(i)))
			val := valCol.Value(i)
			t.Logf("Row %d: ts=%v, val=%.1f", i, ts, val)
		}

		t.Logf("Sum of values: %.1f", sum)
	})
}
