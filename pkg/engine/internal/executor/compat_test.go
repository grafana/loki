package executor

import (
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestNewColumnCompatibilityPipeline(t *testing.T) {
	tests := []struct {
		name           string
		compat         *physical.ColumnCompat
		schema         *arrow.Schema
		inputRows      []arrowtest.Rows
		expectedSchema *arrow.Schema
		expectedRows   []arrowtest.Rows
		expectError    bool
		errorContains  string
	}{
		{
			name: "no column collisions - returns early",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", false),
				semconv.FieldFromFQN("utf8.label.service", true),
				semconv.FieldFromFQN("utf8.metadata.detected_level", true),
				semconv.FieldFromFQN("utf8.metadata.org_id", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.builtin.message": "test message", "timestamp_ns.builtin.timestamp": time.Unix(1000, 0).UTC(), "utf8.label.service": "api", "utf8.metadata.detected_level": "info", "utf8.metadata.org_id": "1"},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", false),
				semconv.FieldFromFQN("utf8.label.service", true),
				semconv.FieldFromFQN("utf8.metadata.detected_level", true),
				semconv.FieldFromFQN("utf8.metadata.org_id", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.builtin.message": "test message", "timestamp_ns.builtin.timestamp": time.Unix(1000, 0).UTC(), "utf8.label.service": "api", "utf8.metadata.detected_level": "info", "utf8.metadata.org_id": "1"},
				},
			},
		},
		{
			name: "single column collision - string type",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("utf8.label.env", true),
				semconv.FieldFromFQN("utf8.label.status", true),    // collision column
				semconv.FieldFromFQN("utf8.metadata.status", true), // source column with same name
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.builtin.message": "line 1", "utf8.label.env": "dev", "utf8.label.status": "success", "utf8.metadata.status": "200"},
					{"utf8.builtin.message": "line 2", "utf8.label.env": "dev", "utf8.label.status": "failure", "utf8.metadata.status": "500"},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("utf8.label.env", true),
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
				semconv.FieldFromFQN("utf8.metadata.status_extracted", true), // new extracted column
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.builtin.message": "line 1", "utf8.label.env": "dev", "utf8.label.status": "success", "utf8.metadata.status": nil, "utf8.metadata.status_extracted": "200"},
					{"utf8.builtin.message": "line 2", "utf8.label.env": "dev", "utf8.label.status": "failure", "utf8.metadata.status": nil, "utf8.metadata.status_extracted": "500"},
				},
			},
		},
		{
			name: "multiple column collisions",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("utf8.label.status", true),    // collision column 1
				semconv.FieldFromFQN("utf8.label.level", true),     // collision column 2
				semconv.FieldFromFQN("utf8.metadata.status", true), // source column 1
				semconv.FieldFromFQN("utf8.metadata.level", true),  // source column 2
				semconv.FieldFromFQN("utf8.metadata.unique", true), // non-colliding source column
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{
						"utf8.builtin.message": "test message",
						"utf8.label.status":    "active",
						"utf8.label.level":     "prod",
						"utf8.metadata.status": "200",
						"utf8.metadata.level":  "info",
						"utf8.metadata.unique": "value",
					},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.label.level", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
				semconv.FieldFromFQN("utf8.metadata.level", true),
				semconv.FieldFromFQN("utf8.metadata.unique", true),
				semconv.FieldFromFQN("utf8.metadata.level_extracted", true), // sorted by name: level comes before status
				semconv.FieldFromFQN("utf8.metadata.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{
						"utf8.builtin.message":           "test message",
						"utf8.label.status":              "active",
						"utf8.label.level":               "prod",
						"utf8.metadata.status":           nil,     // nullified due to collision
						"utf8.metadata.level":            nil,     // nullified due to collision
						"utf8.metadata.unique":           "value", // preserved as no collision
						"utf8.metadata.level_extracted":  "info",
						"utf8.metadata.status_extracted": "200",
					},
				},
			},
		},
		{
			name: "collision with null values in collision column",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.label.status": nil, "utf8.metadata.status": "200"},      // null collision column
					{"utf8.label.status": "active", "utf8.metadata.status": nil},   // null source column
					{"utf8.label.status": nil, "utf8.metadata.status": nil},        // both null
					{"utf8.label.status": "active", "utf8.metadata.status": "200"}, // both non-null
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
				semconv.FieldFromFQN("utf8.metadata.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.label.status": nil, "utf8.metadata.status": "200", "utf8.metadata.status_extracted": nil},      // null collision -> null extraction
					{"utf8.label.status": "active", "utf8.metadata.status": nil, "utf8.metadata.status_extracted": nil},   // null source -> null extraction
					{"utf8.label.status": nil, "utf8.metadata.status": nil, "utf8.metadata.status_extracted": nil},        // both null -> null extraction
					{"utf8.label.status": "active", "utf8.metadata.status": nil, "utf8.metadata.status_extracted": "200"}, // both non-null -> extract
				},
			},
		},
		{
			name: "collision with null values in source column",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.label.status": "active", "utf8.metadata.status": nil}, // null source column
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
				semconv.FieldFromFQN("utf8.metadata.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.label.status": "active", "utf8.metadata.status": nil, "utf8.metadata.status_extracted": nil},
				},
			},
		},
		{
			name: "multiple batches with collisions",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.label.status": "active", "utf8.metadata.status": "200"},
					{"utf8.label.status": "inactive", "utf8.metadata.status": "404"},
				},
				{
					{"utf8.label.status": "pending", "utf8.metadata.status": "202"},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
				semconv.FieldFromFQN("utf8.metadata.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.label.status": "active", "utf8.metadata.status": nil, "utf8.metadata.status_extracted": "200"},
					{"utf8.label.status": "inactive", "utf8.metadata.status": nil, "utf8.metadata.status_extracted": "404"},
				},
				{
					{"utf8.label.status": "pending", "utf8.metadata.status": nil, "utf8.metadata.status_extracted": "202"},
				},
			},
		},
		{
			name: "empty batch does not add _extracted column",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{}, // empty batch
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{}, // empty result
			},
		},
		{
			name: "non-string column types - should copy through unchanged",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", false),
				semconv.FieldFromFQN("float64.builtin.value", false),
				semconv.FieldFromFQN("int64.builtin.count", false),
				semconv.FieldFromFQN("utf8.label.status", true),    // collision column
				semconv.FieldFromFQN("utf8.metadata.status", true), // source column
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{
						"utf8.builtin.message":           "test message",
						"timestamp_ns.builtin.timestamp": time.Unix(1000000, 0).UTC(),
						"float64.builtin.value":          3.14,
						"int64.builtin.count":            int64(42),
						"utf8.label.status":              "active",
						"utf8.metadata.status":           "200",
					},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", false),
				semconv.FieldFromFQN("float64.builtin.value", false),
				semconv.FieldFromFQN("int64.builtin.count", false),
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
				semconv.FieldFromFQN("utf8.metadata.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{
						"utf8.builtin.message":           "test message",
						"timestamp_ns.builtin.timestamp": time.Unix(1000000, 0).UTC(),
						"float64.builtin.value":          3.14,
						"int64.builtin.count":            int64(42),
						"utf8.label.status":              "active",
						"utf8.metadata.status":           nil,
						"utf8.metadata.status_extracted": "200",
					},
				},
			},
		},
		{
			name: "multiple collision types - label and metadata",
			compat: &physical.ColumnCompat{
				Collisions:  []types.ColumnType{types.ColumnTypeLabel, types.ColumnTypeMetadata},
				Source:      types.ColumnTypeParsed,
				Destination: types.ColumnTypeParsed,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				// Collision columns from Label type
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.label.level", true),
				// Collision columns from Metadata type
				semconv.FieldFromFQN("utf8.metadata.status", true),
				semconv.FieldFromFQN("utf8.metadata.env", true),
				// Source columns (Parsed) that collide with both types
				semconv.FieldFromFQN("utf8.parsed.status", true), // collides with both label.status and metadata.status
				semconv.FieldFromFQN("utf8.parsed.level", true),  // collides with label.level
				semconv.FieldFromFQN("utf8.parsed.env", true),    // collides with metadata.env
				semconv.FieldFromFQN("utf8.parsed.unique", true), // no collision
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{
						"utf8.builtin.message": "test message",
						"utf8.label.status":    "active",
						"utf8.label.level":     "debug",
						"utf8.metadata.status": "200",
						"utf8.metadata.env":    "production",
						"utf8.parsed.status":   "ok",
						"utf8.parsed.level":    "info",
						"utf8.parsed.env":      "staging",
						"utf8.parsed.unique":   "value",
					},
					{
						"utf8.builtin.message": "test message 2",
						"utf8.label.status":    "inactive",
						"utf8.label.level":     "debug",
						"utf8.metadata.status": "404",
						"utf8.metadata.env":    "development",
						"utf8.parsed.status":   "error",
						"utf8.parsed.level":    "debug",
						"utf8.parsed.env":      "local",
						"utf8.parsed.unique":   "another",
					},
					// no duplicates as collision columns are null
					{
						"utf8.builtin.message": "test message 3",
						"utf8.label.status":    nil,
						"utf8.label.level":     nil,
						"utf8.metadata.status": nil,
						"utf8.metadata.env":    nil,
						"utf8.parsed.status":   "error",
						"utf8.parsed.level":    "debug",
						"utf8.parsed.env":      "local",
						"utf8.parsed.unique":   "another",
					},
					{
						"utf8.builtin.message": "test message 4",
						"utf8.label.status":    "inactive",
						"utf8.label.level":     "debug",
						"utf8.metadata.status": nil,
						"utf8.metadata.env":    nil,
						"utf8.parsed.status":   "error",
						"utf8.parsed.level":    "info",
						"utf8.parsed.env":      "local",
						"utf8.parsed.unique":   "another",
					},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("utf8.label.status", true),
				semconv.FieldFromFQN("utf8.label.level", true),
				semconv.FieldFromFQN("utf8.metadata.status", true),
				semconv.FieldFromFQN("utf8.metadata.env", true),
				semconv.FieldFromFQN("utf8.parsed.status", true),
				semconv.FieldFromFQN("utf8.parsed.level", true),
				semconv.FieldFromFQN("utf8.parsed.env", true),
				semconv.FieldFromFQN("utf8.parsed.unique", true),
				// Extracted columns (sorted by name)
				semconv.FieldFromFQN("utf8.parsed.env_extracted", true),
				semconv.FieldFromFQN("utf8.parsed.level_extracted", true),
				semconv.FieldFromFQN("utf8.parsed.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{
						"utf8.builtin.message":         "test message",
						"utf8.label.status":            "active",
						"utf8.label.level":             "debug",
						"utf8.metadata.status":         "200",
						"utf8.metadata.env":            "production",
						"utf8.parsed.status":           nil,
						"utf8.parsed.level":            nil,
						"utf8.parsed.env":              nil,
						"utf8.parsed.unique":           "value",
						"utf8.parsed.env_extracted":    "staging",
						"utf8.parsed.level_extracted":  "info",
						"utf8.parsed.status_extracted": "ok",
					},
					{
						"utf8.builtin.message":         "test message 2",
						"utf8.label.status":            "inactive",
						"utf8.label.level":             "debug",
						"utf8.metadata.status":         "404",
						"utf8.metadata.env":            "development",
						"utf8.parsed.status":           nil,
						"utf8.parsed.level":            nil,
						"utf8.parsed.env":              nil,
						"utf8.parsed.unique":           "another",
						"utf8.parsed.env_extracted":    "local",
						"utf8.parsed.level_extracted":  "debug",
						"utf8.parsed.status_extracted": "error",
					},
					{
						"utf8.builtin.message":         "test message 3",
						"utf8.label.status":            nil,
						"utf8.label.level":             nil,
						"utf8.metadata.status":         nil,
						"utf8.metadata.env":            nil,
						"utf8.parsed.status":           "error",
						"utf8.parsed.level":            "debug",
						"utf8.parsed.env":              "local",
						"utf8.parsed.unique":           "another",
						"utf8.parsed.env_extracted":    nil,
						"utf8.parsed.level_extracted":  nil,
						"utf8.parsed.status_extracted": nil,
					},
					{
						"utf8.builtin.message":         "test message 4",
						"utf8.label.status":            "inactive",
						"utf8.label.level":             "debug",
						"utf8.metadata.status":         nil,
						"utf8.metadata.env":            nil,
						"utf8.parsed.status":           nil,
						"utf8.parsed.level":            nil,
						"utf8.parsed.env":              "local",
						"utf8.parsed.unique":           "another",
						"utf8.parsed.env_extracted":    nil,
						"utf8.parsed.level_extracted":  "info",
						"utf8.parsed.status_extracted": "error",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create input pipeline
			var input Pipeline
			if len(tt.inputRows) == 0 {
				input = emptyPipeline()
			} else if len(tt.inputRows) == 1 {
				input = NewArrowtestPipeline(tt.schema, tt.inputRows[0])
			} else {
				// Multiple batches
				var records []arrow.RecordBatch
				for _, rows := range tt.inputRows {
					record := rows.Record(memory.DefaultAllocator, tt.schema)
					records = append(records, record)
				}
				input = NewBufferedPipeline(records...)
			}

			// Create compatibility pipeline
			pipeline := newColumnCompatibilityPipeline(tt.compat, input, nil)
			defer pipeline.Close()

			if tt.expectError {
				// Test error case
				_, err := pipeline.Read(t.Context())
				require.Error(t, err)
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains)
				}
				return
			}

			// Test successful cases
			var actualRows []arrowtest.Rows
			batchCount := 0

			for {
				record, err := pipeline.Read(t.Context())
				if err == EOF {
					break
				}
				require.NoError(t, err)

				// Verify schema matches expected (only check first batch)
				if batchCount == 0 && tt.expectedSchema != nil {
					require.True(t, tt.expectedSchema.Equal(record.Schema()),
						"Schema mismatch.\nExpected: %s\nActual: %s",
						tt.expectedSchema, record.Schema())
				}

				// Convert record to rows for comparison
				rows, err := arrowtest.RecordRows(record)
				require.NoError(t, err)
				actualRows = append(actualRows, rows)
				batchCount++
			}

			// Verify expected number of batches
			require.Len(t, actualRows, len(tt.expectedRows), "unexpected number of batches")

			// Verify row content
			for i, expectedBatchRows := range tt.expectedRows {
				require.Equal(t, expectedBatchRows, actualRows[i], "batch %d content mismatch", i)
			}
		})
	}
}

func TestNewColumnCompatibilityPipeline_ErrorCases(t *testing.T) {
	t.Run("invalid field name in schema", func(t *testing.T) {
		compat := &physical.ColumnCompat{
			Collisions:  []types.ColumnType{types.ColumnTypeLabel},
			Source:      types.ColumnTypeMetadata,
			Destination: types.ColumnTypeMetadata,
		}

		// Create schema with invalid field name that cannot be parsed by semconv.ParseFQN
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "invalid-field-name", Type: types.Arrow.String, Nullable: true},
		}, nil)

		input := NewArrowtestPipeline(schema, arrowtest.Rows{
			{"invalid-field-name": "test"},
		})

		pipeline := newColumnCompatibilityPipeline(compat, input, nil)
		defer pipeline.Close()

		_, err := pipeline.Read(t.Context())
		require.Error(t, err)
		// Should contain parsing error from semconv.ParseFQN
	})

	t.Run("input pipeline error", func(t *testing.T) {
		compat := &physical.ColumnCompat{
			Collisions:  []types.ColumnType{types.ColumnTypeLabel},
			Source:      types.ColumnTypeMetadata,
			Destination: types.ColumnTypeMetadata,
		}

		// Create a pipeline that will return an expected error
		expectedErr := errors.New("test error")
		input := errorPipeline(t.Context(), expectedErr)

		pipeline := newColumnCompatibilityPipeline(compat, input, nil)
		defer pipeline.Close()

		_, err := pipeline.Read(t.Context())
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("non-string collision column should panic", func(t *testing.T) {
		compat := &physical.ColumnCompat{
			Collisions:  []types.ColumnType{types.ColumnTypeLabel},
			Source:      types.ColumnTypeMetadata,
			Destination: types.ColumnTypeMetadata,
		}

		// Create schema where source column is not string type
		// This should trigger the panic in the switch statement for non-string columns
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.label.status", false),    // collision column - string
			semconv.FieldFromFQN("int64.metadata.status", true), // source column - not string
		}, nil)

		input := NewArrowtestPipeline(schema, arrowtest.Rows{
			{"utf8.label.status": "200", "int64.metadata.status": int64(200)},
		})

		pipeline := newColumnCompatibilityPipeline(compat, input, nil)
		defer pipeline.Close()

		// This should panic with "invalid column type: only string columns can be checked for collisions"
		require.Panics(t, func() {
			_, _ = pipeline.Read(t.Context())
		})
	})
}

// TestMultipleColumnCompatPreservesValues tests that when multiple ColumnCompat nodes run sequentially
// (as happens with multiple parsers like `| json | logfmt |`), the second ColumnCompat preserves
// values created by the first ColumnCompat instead of clobbering them.
func TestMultipleColumnCompatPreservesValues(t *testing.T) {
	// Simulate a batch with mixed JSON and logfmt lines
	// After JSON parser + ColumnCompat₁:
	// - Row 0: level="info" from JSON, level_extracted="info" created
	// - Row 1: level=NULL (logfmt line, JSON parsing failed), level_extracted=NULL
	//
	// After logfmt parser + ColumnCompat₂:
	// - Row 0: level=NULL (JSON line, logfmt parsing failed), level_extracted should STAY "info"
	// - Row 1: level="warn" from logfmt, level_extracted should be "warn"

	compat := &physical.ColumnCompat{
		Source:      types.ColumnTypeParsed,
		Destination: types.ColumnTypeParsed,
		Collisions:  []types.ColumnType{types.ColumnTypeMetadata},
	}

	// Step 1: Simulate state after JSON parser + ColumnCompat₁
	// Row 0: JSON line successfully parsed level="info", moved to level_extracted
	// Row 1: logfmt line, JSON parsing failed, level=NULL, level_extracted=NULL
	schemaAfterFirstCompat := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN("utf8.builtin.message", true),
		semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", false),
		semconv.FieldFromFQN("utf8.metadata.level", true),         // metadata level
		semconv.FieldFromFQN("utf8.parsed.level", true),           // parsed level (NULL for row 0, set by first ColumnCompat)
		semconv.FieldFromFQN("utf8.parsed.level_extracted", true), // created by first ColumnCompat
	}, nil)

	inputAfterFirstCompat := NewArrowtestPipeline(schemaAfterFirstCompat, arrowtest.Rows{
		{
			"utf8.builtin.message":           `{"level":"info","msg":"test"}`,
			"timestamp_ns.builtin.timestamp": time.Unix(1000, 0).UTC(),
			"utf8.metadata.level":            "debug", // metadata level different from parsed
			"utf8.parsed.level":              nil,     // NULL - was moved to level_extracted by first ColumnCompat
			"utf8.parsed.level_extracted":    "info",  // created by first ColumnCompat
		},
		{
			"utf8.builtin.message":           `level=warn msg="test"`,
			"timestamp_ns.builtin.timestamp": time.Unix(1001, 0).UTC(),
			"utf8.metadata.level":            "debug",
			"utf8.parsed.level":              "warn", // NOW set by logfmt parser (simulated)
			"utf8.parsed.level_extracted":    nil,    // Was NULL from first ColumnCompat
		},
	})

	// Step 2: Run second ColumnCompat (simulating logfmt parser's ColumnCompat)
	pipeline := newColumnCompatibilityPipeline(compat, inputAfterFirstCompat, nil)
	defer pipeline.Close()

	batch, err := pipeline.Read(t.Context())
	require.NoError(t, err)
	require.NotNil(t, batch)

	levelExtractedIdx := -1
	for i := range batch.Schema().NumFields() {
		if batch.Schema().Field(i).Name == "utf8.parsed.level_extracted" {
			levelExtractedIdx = i
			break
		}
	}
	require.NotEqual(t, -1, levelExtractedIdx, "level_extracted column should exist")

	levelExtractedCol := batch.Column(levelExtractedIdx).(*array.String)

	// Row 0: Should preserve "info" from first ColumnCompat, NOT overwrite with NULL
	require.False(t, levelExtractedCol.IsNull(0), "Row 0 level_extracted should not be NULL")
	require.Equal(t, "info", levelExtractedCol.Value(0),
		"Row 0 level_extracted should preserve 'info' from first ColumnCompat")

	// Row 1: Should have "warn" from second ColumnCompat
	require.False(t, levelExtractedCol.IsNull(1), "Row 1 level_extracted should not be NULL")
	require.Equal(t, "warn", levelExtractedCol.Value(1),
		"Row 1 level_extracted should be 'warn' from second ColumnCompat")
}
