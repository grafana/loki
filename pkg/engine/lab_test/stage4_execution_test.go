package engine_lab

/*
================================================================================
STAGE 4: EXECUTION - Pipeline Execution and Result Building
================================================================================

This file covers Stage 4 of the Loki Query Engine V2 pipeline:
executing task fragments and converting Arrow RecordBatches to LogQL results.

================================================================================
STAGE OVERVIEW
================================================================================

Input:  workflow.Workflow (Task Graph) or physical.Plan (for basic execution)
Output: executor.Pipeline → arrow.RecordBatch → logqlmodel.Result

Execution is the stage where the planned query actually runs. The executor
walks the physical plan (or task fragment), creates pipelines for each node,
and produces Arrow RecordBatches that flow from leaves (scans) to the root.

================================================================================
PIPELINE INTERFACE
================================================================================

The Pipeline is the core execution abstraction:

    type Pipeline interface {
        // Read returns the next batch of data, or EOF when exhausted
        Read(context.Context) (arrow.RecordBatch, error)

        // Close releases resources
        Close()
    }

Pipelines form a tree matching the physical plan structure:

    Physical Plan:               Pipeline Tree:

    VectorAggregation           VectorAggPipeline.Read()
          │                           │
    RangeAggregation    ──>     RangeAggPipeline.Read()
          │                           │
       ScanSet                   MergePipeline.Read()
       /    \                      /        \
    Scan1  Scan2           ScanPipeline1  ScanPipeline2

Each Read() call pulls data up through the pipeline tree.

================================================================================
PIPELINE TYPES
================================================================================

The executor creates different pipeline types for different purposes:

1. GENERIC PIPELINE (GenericPipeline):
   - Base implementation with a read function
   - Used for most operators
   - Takes a function that produces RecordBatches

2. LAZY PIPELINE (lazyPipeline):
   - Defers construction until first Read() call
   - Useful for conditional execution
   - Avoids resource allocation for unused paths

3. PREFETCH WRAPPER (prefetchWrapper):
   - Wraps a pipeline with async prefetching
   - Reads ahead in a background goroutine
   - Reduces latency by overlapping I/O with processing

4. OBSERVED PIPELINE (observedPipeline):
   - Wraps a pipeline with statistics collection
   - Records timing, row counts, batch counts
   - Used for query profiling and metrics

5. BUFFERED PIPELINE (BufferedPipeline):
   - Holds pre-computed records in memory
   - Used for testing and caching
   - Returns EOF after all records are read

================================================================================
EXECUTION FLOW
================================================================================

1. executor.Run() is called with physical plan
2. The executor walks the plan from root to leaves
3. For each node, it creates the appropriate pipeline type
4. Pipelines are connected: child pipelines feed parent pipelines
5. The root pipeline is returned
6. Each Read() on root pipeline triggers reads from children
7. Data flows up as Arrow RecordBatches
8. EOF signals completion

================================================================================
RESULT BUILDING
================================================================================

After execution produces Arrow RecordBatches, ResultBuilder converts them to
LogQL result types that the API returns:

1. streamsResultBuilder → logqlmodel.Streams (log queries)
   - Groups log entries by stream labels
   - Extracts timestamp, message, structured metadata
   - Sorts by timestamp according to query direction

2. vectorResultBuilder → promql.Vector (instant metric queries)
   - Extracts timestamp, value, and labels
   - Returns single data point per series

3. matrixResultBuilder → promql.Matrix (range metric queries)
   - Groups samples by series labels
   - Returns time series with multiple data points

================================================================================
ARROW RECORDBATCH STRUCTURE
================================================================================

Arrow RecordBatches are columnar:

    RecordBatch
    ├── Schema (column definitions)
    │   ├── Field 0: "timestamp_ns.builtin.timestamp" (timestamp[ns])
    │   ├── Field 1: "utf8.builtin.message" (utf8)
    │   └── Field 2: "utf8.label.app" (utf8)
    └── Columns (data arrays)
        ├── Column 0: [1000000000, 1000000001, 1000000002]
        ├── Column 1: ["log line 1", "log line 2", "log line 3"]
        └── Column 2: ["test", "test", "test"]

Column names follow the FQN (Fully Qualified Name) format:
    <datatype>.<columntype>.<name>

Examples:
    - timestamp_ns.builtin.timestamp (nanosecond timestamp)
    - utf8.builtin.message (string message)
    - utf8.label.app (string label)
    - float64.builtin.value (numeric value for metrics)

================================================================================
MEMORY MANAGEMENT
================================================================================

Arrow uses reference counting for memory management:

    record := rows.Record(alloc, schema)  // ref count = 1
    record.Retain()                        // ref count = 2
    record.Release()                       // ref count = 1
    record.Release()                       // ref count = 0, memory freed

IMPORTANT: Always call Release() when done with a record to avoid memory leaks.
Use defer record.Release() immediately after creation when possible.

================================================================================
*/

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// ============================================================================
// PIPELINE EXECUTION TESTS
// ============================================================================

/*
TestPipelineExecution demonstrates pipeline execution with mock data.

These tests show:
1. How pipelines produce Arrow RecordBatches
2. How pipelines signal completion with EOF
3. How different pipeline types work
*/
func TestPipelineExecution(t *testing.T) {
	t.Run("mock pipeline produces arrow records", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Mock Pipeline Produces Arrow Records
		   ============================================================================

		   This test demonstrates the basic pipeline pattern:
		   1. Create a pipeline with predefined data
		   2. Read batches until EOF
		   3. Process the Arrow RecordBatches

		   The mockPipeline is defined in learning_test_utils.go and simulates
		   a data source that produces a fixed set of records.
		*/
		alloc := memory.NewGoAllocator()

		// Define columns using semantic conventions
		colTs := semconv.ColumnIdentTimestamp // builtin.timestamp
		colMsg := semconv.ColumnIdentMessage  // builtin.message
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String)

		// Create schema
		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false), // not nullable
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colEnv, false),
			},
			nil,
		)

		// Create test data using arrowtest helper
		rows := arrowtest.Rows{
			{
				colTs.FQN():  time.Unix(0, 1000000001).UTC(),
				colMsg.FQN(): "log line 1",
				colEnv.FQN(): "prod",
			},
			{
				colTs.FQN():  time.Unix(0, 1000000002).UTC(),
				colMsg.FQN(): "log line 2",
				colEnv.FQN(): "dev",
			},
			{
				colTs.FQN():  time.Unix(0, 1000000003).UTC(),
				colMsg.FQN(): "log line 3",
				colEnv.FQN(): "prod",
			},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// Create mock pipeline
		pipeline := newMockPipeline(record)
		defer pipeline.Close()

		// Read from pipeline
		ctx := context.Background()
		rec, err := pipeline.Read(ctx)
		require.NoError(t, err)
		require.NotNil(t, rec)

		// Verify record structure
		require.Equal(t, int64(3), rec.NumRows(), "should have 3 rows")
		require.Equal(t, int64(3), rec.NumCols(), "should have 3 columns")

		// Verify column names
		t.Logf("Column names:")
		for i := 0; i < int(rec.NumCols()); i++ {
			t.Logf("  %d: %s", i, rec.ColumnName(i))
		}

		// Read again - should get EOF
		_, err = pipeline.Read(ctx)
		require.ErrorIs(t, err, executor.EOF, "second read should return EOF")
	})

	t.Run("buffered pipeline collects records", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Buffered Pipeline Collects Records
		   ============================================================================

		   The BufferedPipeline holds pre-computed records in memory.
		   It's useful for:
		   - Testing (providing known data)
		   - Caching intermediate results
		   - Collecting results from multiple sources

		   Usage:
		       pipeline := executor.NewBufferedPipeline(record1, record2, ...)
		       for {
		           rec, err := pipeline.Read(ctx)
		           if errors.Is(err, executor.EOF) {
		               break
		           }
		           // process rec
		       }
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
			{colTs.FQN(): time.Unix(0, 1000000001).UTC(), colMsg.FQN(): "log 1"},
			{colTs.FQN(): time.Unix(0, 1000000002).UTC(), colMsg.FQN(): "log 2"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// Use the executor's buffered pipeline
		pipeline := executor.NewBufferedPipeline(record)
		defer pipeline.Close()

		ctx := context.Background()
		rec, err := pipeline.Read(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(2), rec.NumRows())

		t.Logf("Read %d rows from buffered pipeline", rec.NumRows())
	})

	t.Run("pipeline EOF handling", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Pipeline EOF Handling
		   ============================================================================

		   EOF (End Of File) signals that a pipeline has no more data.
		   This is NOT an error condition - it's the normal way to indicate
		   that all data has been read.

		   Pattern:
		       for {
		           rec, err := pipeline.Read(ctx)
		           if errors.Is(err, executor.EOF) {
		               break  // Normal completion
		           }
		           if err != nil {
		               return err  // Actual error
		           }
		           // process rec
		       }
		*/
		// Create an empty pipeline
		pipeline := newMockPipeline() // No records
		defer pipeline.Close()

		ctx := context.Background()

		// First read should return EOF immediately
		_, err := pipeline.Read(ctx)
		require.ErrorIs(t, err, executor.EOF, "empty pipeline should return EOF immediately")

		t.Log("Empty pipeline correctly returns EOF")
	})

	t.Run("createTestRecord helper", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: createTestRecord Helper
		   ============================================================================

		   The createTestRecord helper in learning_test_utils.go simplifies
		   creating test Arrow records with standard columns.

		   It creates records with:
		   - timestamp column (builtin.timestamp)
		   - message column (builtin.message)
		*/
		record := createTestRecord(t, 5)
		defer record.Release()

		require.Equal(t, int64(5), record.NumRows())
		require.Equal(t, int64(2), record.NumCols())

		t.Logf("Created test record with %d rows and %d columns",
			record.NumRows(), record.NumCols())
	})
}

// ============================================================================
// ARROW RECORDBATCH STRUCTURE TESTS
// ============================================================================

/*
TestArrowRecordBatchStructure demonstrates the structure of Arrow RecordBatches.

These tests show:
1. How to create and inspect RecordBatches
2. How to access columns by index and name
3. How column data types work
*/
func TestArrowRecordBatchStructure(t *testing.T) {
	t.Run("record batch schema and columns", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: RecordBatch Schema and Columns
		   ============================================================================

		   A RecordBatch consists of:
		   - Schema: Defines column names and types
		   - Columns: Arrays of typed data (one per column)
		   - Row count: Number of rows (all columns have same length)

		   The schema describes the "shape" of the data, while the columns
		   contain the actual values.
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

		// Schema inspection
		t.Logf("Schema fields: %d", schema.NumFields())
		for i := 0; i < schema.NumFields(); i++ {
			field := schema.Field(i)
			t.Logf("  Field %d: name=%s, type=%s, nullable=%v",
				i, field.Name, field.Type, field.Nullable)
		}

		// Create record
		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1000000000).UTC(), colMsg.FQN(): "hello"},
			{colTs.FQN(): time.Unix(0, 2000000000).UTC(), colMsg.FQN(): "world"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// Record inspection
		require.Equal(t, int64(2), record.NumRows())
		require.Equal(t, int64(2), record.NumCols())

		// Access columns by index
		col0 := record.Column(0)
		col1 := record.Column(1)

		t.Logf("Column 0 length: %d", col0.Len())
		t.Logf("Column 1 length: %d", col1.Len())
	})

	t.Run("accessing typed column data", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Accessing Typed Column Data
		   ============================================================================

		   Columns are typed arrays. To access values, cast to the appropriate
		   Arrow array type:

		   - *array.Timestamp for timestamp columns
		   - *array.String for string columns
		   - *array.Float64 for float columns
		   - *array.Int64 for integer columns

		   Each array type has a Value(i) method to get the value at index i.
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
			{colTs.FQN(): time.Unix(0, 1000000000).UTC(), colMsg.FQN(): "first"},
			{colTs.FQN(): time.Unix(0, 2000000000).UTC(), colMsg.FQN(): "second"},
			{colTs.FQN(): time.Unix(0, 3000000000).UTC(), colMsg.FQN(): "third"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// Access timestamp column
		tsCol := record.Column(0).(*array.Timestamp)
		require.Equal(t, 3, tsCol.Len())

		// Access message column
		msgCol := record.Column(1).(*array.String)
		require.Equal(t, "first", msgCol.Value(0))
		require.Equal(t, "second", msgCol.Value(1))
		require.Equal(t, "third", msgCol.Value(2))

		t.Logf("Message values: %s, %s, %s",
			msgCol.Value(0), msgCol.Value(1), msgCol.Value(2))
	})

	t.Run("column with labels", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Column with Labels
		   ============================================================================

		   Label columns store stream labels like {app="test", env="prod"}.
		   They use the ColumnTypeLabel type and appear with the "label." prefix
		   in the FQN.

		   Example FQN: "utf8.label.app"
		*/
		alloc := memory.NewGoAllocator()

		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colApp := semconv.NewIdentifier("app", types.ColumnTypeLabel, types.Loki.String)
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colApp, false),
				semconv.FieldFromIdent(colEnv, false),
			},
			nil,
		)

		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1000000000).UTC(), colMsg.FQN(): "log 1", colApp.FQN(): "myapp", colEnv.FQN(): "prod"},
			{colTs.FQN(): time.Unix(0, 2000000000).UTC(), colMsg.FQN(): "log 2", colApp.FQN(): "myapp", colEnv.FQN(): "dev"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// Access label columns
		appCol := record.Column(2).(*array.String)
		envCol := record.Column(3).(*array.String)

		require.Equal(t, "myapp", appCol.Value(0))
		require.Equal(t, "prod", envCol.Value(0))
		require.Equal(t, "dev", envCol.Value(1))

		t.Logf("Label columns: app=%s, env values: %s, %s",
			appCol.Value(0), envCol.Value(0), envCol.Value(1))
	})
}

// ============================================================================
// SEMANTIC CONVENTIONS TESTS
// ============================================================================

/*
TestSemanticConventions demonstrates the column naming conventions used in V2.

Column names follow the FQN (Fully Qualified Name) format:

	<datatype>.<columntype>.<name>

This allows identifying both the data type and semantic meaning of each column.
*/
func TestSemanticConventions(t *testing.T) {
	t.Run("builtin column identifiers", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Builtin and Generated Column Identifiers
		   ============================================================================

		   Special columns defined in the semconv package:

		   BUILTIN columns (always present on log entries):
		   - builtin.timestamp: Log entry timestamp (nanoseconds)
		   - builtin.message: The log line text

		   GENERATED columns (created during query execution):
		   - generated.value: Numeric value (created by metric aggregations)

		   Note: The "value" column is GENERATED, not BUILTIN, because it's
		   created during metric query aggregation, not present on raw log entries.
		*/
		// Timestamp column (builtin)
		tsIdent := semconv.ColumnIdentTimestamp
		require.Contains(t, tsIdent.FQN(), "builtin.timestamp")
		require.Equal(t, types.ColumnTypeBuiltin, tsIdent.ColumnType())
		require.Equal(t, "timestamp", tsIdent.ShortName())
		t.Logf("Timestamp FQN: %s", tsIdent.FQN())

		// Message column (builtin)
		msgIdent := semconv.ColumnIdentMessage
		require.Contains(t, msgIdent.FQN(), "builtin.message")
		require.Equal(t, types.ColumnTypeBuiltin, msgIdent.ColumnType())
		require.Equal(t, "message", msgIdent.ShortName())
		t.Logf("Message FQN: %s", msgIdent.FQN())

		// Value column (generated during aggregation, NOT builtin)
		valIdent := semconv.ColumnIdentValue
		require.Contains(t, valIdent.FQN(), "generated.value")
		require.Equal(t, types.ColumnTypeGenerated, valIdent.ColumnType())
		require.Equal(t, "value", valIdent.ShortName())
		t.Logf("Value FQN: %s", valIdent.FQN())
	})

	t.Run("custom column identifiers", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Custom Column Identifiers
		   ============================================================================

		   Custom columns are created for:
		   - Stream labels (label.*)
		   - Structured metadata (metadata.*)
		   - Parsed fields (parsed.*)
		   - Ambiguous fields (ambiguous.*)

		   Use semconv.NewIdentifier() to create custom identifiers.
		*/
		// Label column
		labelCol := semconv.NewIdentifier("app", types.ColumnTypeLabel, types.Loki.String)
		require.Contains(t, labelCol.FQN(), "label.app")
		require.Equal(t, types.ColumnTypeLabel, labelCol.ColumnType())
		require.Equal(t, "app", labelCol.ShortName())
		t.Logf("Label FQN: %s", labelCol.FQN())

		// Metadata column
		metaCol := semconv.NewIdentifier("traceID", types.ColumnTypeMetadata, types.Loki.String)
		require.Contains(t, metaCol.FQN(), "metadata.traceID")
		require.Equal(t, types.ColumnTypeMetadata, metaCol.ColumnType())
		t.Logf("Metadata FQN: %s", metaCol.FQN())

		// Parsed column (from JSON/logfmt parser)
		parsedCol := semconv.NewIdentifier("level", types.ColumnTypeParsed, types.Loki.String)
		require.Contains(t, parsedCol.FQN(), "parsed.level")
		require.Equal(t, types.ColumnTypeParsed, parsedCol.ColumnType())
		t.Logf("Parsed FQN: %s", parsedCol.FQN())

		// Ambiguous column (type unknown until physical planning)
		ambiguousCol := semconv.NewIdentifier("status", types.ColumnTypeAmbiguous, types.Loki.String)
		require.Contains(t, ambiguousCol.FQN(), "ambiguous.status")
		require.Equal(t, types.ColumnTypeAmbiguous, ambiguousCol.ColumnType())
		t.Logf("Ambiguous FQN: %s", ambiguousCol.FQN())
	})

	t.Run("column type summary", func(t *testing.T) {
		/*
		   ============================================================================
		   COLUMN TYPE SUMMARY
		   ============================================================================

		   | Type      | Prefix      | Description                           |
		   |-----------|-------------|---------------------------------------|
		   | Builtin   | builtin.*   | Engine-defined: timestamp, message    |
		   | Label     | label.*     | Stream labels set at ingestion        |
		   | Metadata  | metadata.*  | Structured metadata (not stream ID)   |
		   | Parsed    | parsed.*    | Fields extracted by query parsers     |
		   | Ambiguous | ambiguous.* | Type unknown until physical planning  |

		   FQN Format: <datatype>.<columntype>.<name>

		   Examples:
		   - timestamp_ns.builtin.timestamp
		   - utf8.builtin.message
		   - utf8.label.app
		   - utf8.metadata.traceID
		   - utf8.parsed.level
		   - utf8.ambiguous.status
		*/
		t.Log("Column Type Reference:")
		t.Log("  builtin.*   - Engine-defined columns (timestamp, message, value)")
		t.Log("  label.*     - Stream labels from ingestion")
		t.Log("  metadata.*  - Structured metadata")
		t.Log("  parsed.*    - Fields extracted by query parsers")
		t.Log("  ambiguous.* - Type resolved during physical planning")
	})
}

// ============================================================================
// RESULT BUILDING TESTS
// ============================================================================

/*
TestResultBuilding demonstrates how Arrow RecordBatches are converted to LogQL results.

NOTE: The actual result builders (streamsResultBuilder, vectorResultBuilder) are
internal to the engine package. These tests demonstrate the concepts using
the available test utilities.
*/
func TestResultBuilding(t *testing.T) {
	t.Run("log query result structure", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Log Query Result Structure
		   ============================================================================

		   Log queries return logqlmodel.Streams, which contains:
		   - Multiple Stream objects, each with:
		     - Labels: The stream's label set (e.g., {app="test", env="prod"})
		     - Entries: List of log entries, each with:
		       - Timestamp: Entry time
		       - Line: Log message
		       - StructuredMetadata: Optional metadata
		       - Parsed: Optional parsed fields

		   The streamsResultBuilder groups entries by their label set.
		*/
		alloc := memory.NewGoAllocator()

		colTs := semconv.ColumnIdentTimestamp
		colMsg := semconv.ColumnIdentMessage
		colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colEnv, false),
			},
			nil,
		)

		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1000).UTC(), colMsg.FQN(): "prod log 1", colEnv.FQN(): "prod"},
			{colTs.FQN(): time.Unix(0, 2000).UTC(), colMsg.FQN(): "dev log 1", colEnv.FQN(): "dev"},
			{colTs.FQN(): time.Unix(0, 3000).UTC(), colMsg.FQN(): "prod log 2", colEnv.FQN(): "prod"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// In actual usage, streamsResultBuilder would group these by labels:
		// - Stream {env="prod"}: 2 entries (prod log 1, prod log 2)
		// - Stream {env="dev"}: 1 entry (dev log 1)

		t.Logf("Log entries that would be grouped by labels:")
		t.Logf("  {env=prod}: 2 entries")
		t.Logf("  {env=dev}: 1 entry")
	})

	t.Run("metric query result structure", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Metric Query Result Structure
		   ============================================================================

		   Metric queries return either:
		   - promql.Vector (instant queries): Single value per series
		   - promql.Matrix (range queries): Multiple values per series

		   Vector Sample:
		     - Timestamp: Evaluation time
		     - Value: Numeric result
		     - Labels: Series labels

		   Matrix Series:
		     - Labels: Series labels
		     - Points: List of (timestamp, value) pairs
		*/
		alloc := memory.NewGoAllocator()

		colTs := semconv.ColumnIdentTimestamp
		colVal := semconv.ColumnIdentValue
		colLevel := semconv.NewIdentifier("level", types.ColumnTypeLabel, types.Loki.String)

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colTs, false),
				semconv.FieldFromIdent(colVal, false),
				semconv.FieldFromIdent(colLevel, false),
			},
			nil,
		)

		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1000000000).UTC(), colVal.FQN(): float64(42.0), colLevel.FQN(): "error"},
			{colTs.FQN(): time.Unix(0, 1000000000).UTC(), colVal.FQN(): float64(17.0), colLevel.FQN(): "info"},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// In actual usage, vectorResultBuilder would create:
		// Vector:
		//   - Sample{Labels: {level="error"}, Value: 42.0, Timestamp: ...}
		//   - Sample{Labels: {level="info"}, Value: 17.0, Timestamp: ...}

		t.Logf("Vector samples that would be created:")
		t.Logf("  {level=error}: value=42.0")
		t.Logf("  {level=info}: value=17.0")
	})
}

// ============================================================================
// MEMORY MANAGEMENT TESTS
// ============================================================================

/*
TestMemoryManagement demonstrates Arrow's reference counting for memory.
*/
func TestMemoryManagement(t *testing.T) {
	t.Run("reference counting basics", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Reference Counting Basics
		   ============================================================================

		   Arrow objects use reference counting:
		   - Initial ref count is 1 after creation
		   - Retain() increments ref count
		   - Release() decrements ref count
		   - When ref count reaches 0, memory is freed

		   IMPORTANT: Always call Release() when done with Arrow objects.
		   Failure to release leads to memory leaks.
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
			{colTs.FQN(): time.Unix(0, 1000000000).UTC(), colMsg.FQN(): "test"},
		}

		record := rows.Record(alloc, schema)

		// Use defer to ensure Release is called
		defer record.Release()

		// Record is valid for use
		require.Equal(t, int64(1), record.NumRows())

		t.Log("Reference counting pattern: use defer record.Release()")
	})

	t.Run("retain for sharing", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Retain for Sharing
		   ============================================================================

		   When passing a record to another function that will keep a reference,
		   the receiver should call Retain() and be responsible for calling
		   Release() when done.

		   Pattern:
		       func process(record arrow.Record) {
		           record.Retain()  // Take ownership
		           defer record.Release()
		           // ... use record ...
		       }
		*/
		alloc := memory.NewGoAllocator()

		colTs := semconv.ColumnIdentTimestamp
		schema := arrow.NewSchema(
			[]arrow.Field{semconv.FieldFromIdent(colTs, false)},
			nil,
		)

		rows := arrowtest.Rows{
			{colTs.FQN(): time.Unix(0, 1000000000).UTC()},
		}

		record := rows.Record(alloc, schema)
		defer record.Release()

		// Simulate passing to another owner
		record.Retain() // New owner takes reference

		// Original and new owner both have valid references
		require.Equal(t, int64(1), record.NumRows())

		// New owner releases when done
		record.Release()

		// Original still has valid reference
		require.Equal(t, int64(1), record.NumRows())

		t.Log("Use Retain() when sharing records between owners")
	})
}

// ============================================================================
// HELPER FUNCTION TESTS
// ============================================================================

/*
TestExecutionHelpers demonstrates the helper functions for execution testing.
*/
func TestExecutionHelpers(t *testing.T) {
	t.Run("createTestRecord creates valid records", func(t *testing.T) {
		/*
		   createTestRecord is a convenience function that creates Arrow records
		   with timestamp and message columns for testing.
		*/
		record := createTestRecord(t, 10)
		defer record.Release()

		require.Equal(t, int64(10), record.NumRows())
		require.Equal(t, int64(2), record.NumCols())

		// Verify columns are accessible
		tsCol := record.Column(0).(*array.Timestamp)
		msgCol := record.Column(1).(*array.String)

		require.Equal(t, 10, tsCol.Len())
		require.Equal(t, 10, msgCol.Len())

		t.Logf("Created test record with %d rows", record.NumRows())
	})

	t.Run("newMockPipeline creates pipelines", func(t *testing.T) {
		/*
		   newMockPipeline creates a simple pipeline for testing.
		   It yields the provided records in order, then returns EOF.
		*/
		record := createTestRecord(t, 5)
		defer record.Release()

		pipeline := newMockPipeline(record)
		defer pipeline.Close()

		ctx := context.Background()

		// First read returns the record
		rec, err := pipeline.Read(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(5), rec.NumRows())

		// Second read returns EOF
		_, err = pipeline.Read(ctx)
		require.ErrorIs(t, err, executor.EOF)

		t.Log("Mock pipeline correctly produces records and EOF")
	})
}
