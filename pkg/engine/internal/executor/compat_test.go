package executor

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestFindDuplicates(t *testing.T) {
	tests := []struct {
		name     string
		slice1   []string
		slice2   []string
		expected []duplicate
	}{
		{
			name:     "empty slices",
			slice1:   []string{},
			slice2:   []string{},
			expected: nil,
		},
		{
			name:     "first slice empty",
			slice1:   []string{},
			slice2:   []string{"a", "b", "c"},
			expected: nil,
		},
		{
			name:     "second slice empty",
			slice1:   []string{"a", "b", "c"},
			slice2:   []string{},
			expected: nil,
		},
		{
			name:     "no duplicates",
			slice1:   []string{"a", "b", "c"},
			slice2:   []string{"d", "e", "f"},
			expected: nil,
		},
		{
			name:   "single duplicate",
			slice1: []string{"a", "b", "c"},
			slice2: []string{"c", "d", "e"},
			expected: []duplicate{
				{
					value: "c",
					s1Idx: 2,
					s2Idx: 0,
				},
			},
		},
		{
			name:   "multiple duplicates",
			slice1: []string{"a", "b", "c", "d"},
			slice2: []string{"c", "d", "e", "f"},
			expected: []duplicate{
				{
					value: "c",
					s1Idx: 2,
					s2Idx: 0,
				},
				{
					value: "d",
					s1Idx: 3,
					s2Idx: 1,
				},
			},
		},
		{
			name:   "duplicate with different positions",
			slice1: []string{"x", "y", "z"},
			slice2: []string{"z", "y", "x"},
			expected: []duplicate{
				{
					value: "x",
					s1Idx: 0,
					s2Idx: 2,
				},
				{
					value: "y",
					s1Idx: 1,
					s2Idx: 1,
				},
				{
					value: "z",
					s1Idx: 2,
					s2Idx: 0,
				},
			},
		},
		{
			name:   "identical slices",
			slice1: []string{"a", "b", "c"},
			slice2: []string{"a", "b", "c"},
			expected: []duplicate{
				{
					value: "a",
					s1Idx: 0,
					s2Idx: 0,
				},
				{
					value: "b",
					s1Idx: 1,
					s2Idx: 1,
				},
				{
					value: "c",
					s1Idx: 2,
					s2Idx: 2,
				},
			},
		},
		{
			name:   "duplicate values in first slice - function assumes unique elements",
			slice1: []string{"a", "b", "a"}, // Note: function assumes unique elements
			slice2: []string{"a", "c"},
			expected: []duplicate{
				{
					value: "a",
					s1Idx: 2,
					s2Idx: 0, // Will use the last occurrence index due to map overwrite
				},
			},
		},
		{
			name:   "duplicate values in second slice - function assumes unique elements",
			slice1: []string{"a", "b"},
			slice2: []string{"a", "c", "a"}, // Note: function assumes unique elements
			expected: []duplicate{
				{
					value: "a",
					s1Idx: 0, // Will use the last occurrence index due to map overwrite
					s2Idx: 2,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findDuplicates(tt.slice1, tt.slice2)

			// Sort both expected and result slices by value for consistent comparison
			// since the order of duplicates in the result is not guaranteed
			sortDuplicatesByValue(result)
			sortDuplicatesByValue(tt.expected)

			require.Equal(t, tt.expected, result)
		})
	}
}

// sortDuplicatesByValue sorts a slice of duplicate structs by their value field
func sortDuplicatesByValue(duplicates []duplicate) {
	if len(duplicates) <= 1 {
		return
	}
	slices.SortStableFunc(duplicates, func(a, b duplicate) int { return strings.Compare(a.value, b.value) })
}

func TestNewColumnCompatibilityPipeline(t *testing.T) {
	tests := []struct {
		name           string
		compat         *physicalpb.ColumnCompat
		schema         *arrow.Schema
		inputRows      []arrowtest.Rows
		expectedSchema *arrow.Schema
		expectedRows   []arrowtest.Rows
		expectError    bool
		errorContains  string
	}{
		{
			name: "no column collisions - returns early",
			compat: &physicalpb.ColumnCompat{
				Collision:   physicalpb.COLUMN_TYPE_LABEL,
				Source:      physicalpb.COLUMN_TYPE_METADATA,
				Destination: physicalpb.COLUMN_TYPE_METADATA,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp", false),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.service", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.detected_level", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.org_id", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_BUILTIN.message": "test message", "timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp": time.Unix(1000, 0).UTC(), "utf8.COLUMN_TYPE_LABEL.service": "api", "utf8.COLUMN_TYPE_METADATA.detected_level": "info", "utf8.COLUMN_TYPE_METADATA.org_id": "1"},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp", false),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.service", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.detected_level", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.org_id", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_BUILTIN.message": "test message", "timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp": time.Unix(1000, 0).UTC(), "utf8.COLUMN_TYPE_LABEL.service": "api", "utf8.COLUMN_TYPE_METADATA.detected_level": "info", "utf8.COLUMN_TYPE_METADATA.org_id": "1"},
				},
			},
		},
		{
			name: "single column collision - string type",
			compat: &physicalpb.ColumnCompat{
				Collision:   physicalpb.COLUMN_TYPE_LABEL,
				Source:      physicalpb.COLUMN_TYPE_METADATA,
				Destination: physicalpb.COLUMN_TYPE_METADATA,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.env", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),    // collision column
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true), // source column with same name
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_BUILTIN.message": "line 1", "utf8.COLUMN_TYPE_LABEL.env": "dev", "utf8.COLUMN_TYPE_LABEL.status": "success", "utf8.COLUMN_TYPE_METADATA.status": "200"},
					{"utf8.COLUMN_TYPE_BUILTIN.message": "line 2", "utf8.COLUMN_TYPE_LABEL.env": "dev", "utf8.COLUMN_TYPE_LABEL.status": "failure", "utf8.COLUMN_TYPE_METADATA.status": "500"},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.env", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status_extracted", true), // new extracted column
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_BUILTIN.message": "line 1", "utf8.COLUMN_TYPE_LABEL.env": "dev", "utf8.COLUMN_TYPE_LABEL.status": "success", "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": "200"},
					{"utf8.COLUMN_TYPE_BUILTIN.message": "line 2", "utf8.COLUMN_TYPE_LABEL.env": "dev", "utf8.COLUMN_TYPE_LABEL.status": "failure", "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": "500"},
				},
			},
		},
		{
			name: "multiple column collisions",
			compat: &physicalpb.ColumnCompat{
				Collision:   physicalpb.COLUMN_TYPE_LABEL,
				Source:      physicalpb.COLUMN_TYPE_METADATA,
				Destination: physicalpb.COLUMN_TYPE_METADATA,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),    // collision column 1
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.level", true),     // collision column 2
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true), // source column 1
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.level", true),  // source column 2
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.unique", true), // non-colliding source column
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{
						"utf8.COLUMN_TYPE_BUILTIN.message": "test message",
						"utf8.COLUMN_TYPE_LABEL.status":    "active",
						"utf8.COLUMN_TYPE_LABEL.level":     "prod",
						"utf8.COLUMN_TYPE_METADATA.status": "200",
						"utf8.COLUMN_TYPE_METADATA.level":  "info",
						"utf8.COLUMN_TYPE_METADATA.unique": "value",
					},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.level", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.level", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.unique", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.level_extracted", true), // sorted by name: level comes before status
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{
						"utf8.COLUMN_TYPE_BUILTIN.message":           "test message",
						"utf8.COLUMN_TYPE_LABEL.status":              "active",
						"utf8.COLUMN_TYPE_LABEL.level":               "prod",
						"utf8.COLUMN_TYPE_METADATA.status":           nil,     // nullified due to collision
						"utf8.COLUMN_TYPE_METADATA.level":            nil,     // nullified due to collision
						"utf8.COLUMN_TYPE_METADATA.unique":           "value", // preserved as no collision
						"utf8.COLUMN_TYPE_METADATA.level_extracted":  "info",
						"utf8.COLUMN_TYPE_METADATA.status_extracted": "200",
					},
				},
			},
		},
		{
			name: "collision with null values in collision column",
			compat: &physicalpb.ColumnCompat{
				Collision:   physicalpb.COLUMN_TYPE_LABEL,
				Source:      physicalpb.COLUMN_TYPE_METADATA,
				Destination: physicalpb.COLUMN_TYPE_METADATA,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_LABEL.status": nil, "utf8.COLUMN_TYPE_METADATA.status": "200"},      // null collision column
					{"utf8.COLUMN_TYPE_LABEL.status": "active", "utf8.COLUMN_TYPE_METADATA.status": nil},   // null source column
					{"utf8.COLUMN_TYPE_LABEL.status": nil, "utf8.COLUMN_TYPE_METADATA.status": nil},        // both null
					{"utf8.COLUMN_TYPE_LABEL.status": "active", "utf8.COLUMN_TYPE_METADATA.status": "200"}, // both non-null
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_LABEL.status": nil, "utf8.COLUMN_TYPE_METADATA.status": "200", "utf8.COLUMN_TYPE_METADATA.status_extracted": nil},      // null collision -> null extraction
					{"utf8.COLUMN_TYPE_LABEL.status": "active", "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": nil},   // null source -> null extraction
					{"utf8.COLUMN_TYPE_LABEL.status": nil, "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": nil},        // both null -> null extraction
					{"utf8.COLUMN_TYPE_LABEL.status": "active", "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": "200"}, // both non-null -> extract
				},
			},
		},
		{
			name: "collision with null values in source column",
			compat: &physicalpb.ColumnCompat{
				Collision:   physicalpb.COLUMN_TYPE_LABEL,
				Source:      physicalpb.COLUMN_TYPE_METADATA,
				Destination: physicalpb.COLUMN_TYPE_METADATA,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_LABEL.status": "active", "utf8.COLUMN_TYPE_METADATA.status": nil}, // null source column
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_LABEL.status": "active", "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": nil},
				},
			},
		},
		{
			name: "multiple batches with collisions",
			compat: &physicalpb.ColumnCompat{
				Collision:   physicalpb.COLUMN_TYPE_LABEL,
				Source:      physicalpb.COLUMN_TYPE_METADATA,
				Destination: physicalpb.COLUMN_TYPE_METADATA,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_LABEL.status": "active", "utf8.COLUMN_TYPE_METADATA.status": "200"},
					{"utf8.COLUMN_TYPE_LABEL.status": "inactive", "utf8.COLUMN_TYPE_METADATA.status": "404"},
				},
				{
					{"utf8.COLUMN_TYPE_LABEL.status": "pending", "utf8.COLUMN_TYPE_METADATA.status": "202"},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{"utf8.COLUMN_TYPE_LABEL.status": "active", "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": "200"},
					{"utf8.COLUMN_TYPE_LABEL.status": "inactive", "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": "404"},
				},
				{
					{"utf8.COLUMN_TYPE_LABEL.status": "pending", "utf8.COLUMN_TYPE_METADATA.status": nil, "utf8.COLUMN_TYPE_METADATA.status_extracted": "202"},
				},
			},
		},
		{
			name: "empty batch does not add _extracted column",
			compat: &physicalpb.ColumnCompat{
				Collision:   physicalpb.COLUMN_TYPE_LABEL,
				Source:      physicalpb.COLUMN_TYPE_METADATA,
				Destination: physicalpb.COLUMN_TYPE_METADATA,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
			}, nil),
			inputRows: []arrowtest.Rows{
				{}, // empty batch
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{}, // empty result
			},
		},
		{
			name: "non-string column types - should copy through unchanged",
			compat: &physicalpb.ColumnCompat{
				Collision:   physicalpb.COLUMN_TYPE_LABEL,
				Source:      physicalpb.COLUMN_TYPE_METADATA,
				Destination: physicalpb.COLUMN_TYPE_METADATA,
			},
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp", false),
				semconv.FieldFromFQN("float64.COLUMN_TYPE_BUILTIN.value", false),
				semconv.FieldFromFQN("int64.COLUMN_TYPE_BUILTIN.count", false),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),    // collision column
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true), // source column
			}, nil),
			inputRows: []arrowtest.Rows{
				{
					{
						"utf8.COLUMN_TYPE_BUILTIN.message":           "test message",
						"timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp": time.Unix(1000000, 0).UTC(),
						"float64.COLUMN_TYPE_BUILTIN.value":          3.14,
						"int64.COLUMN_TYPE_BUILTIN.count":            int64(42),
						"utf8.COLUMN_TYPE_LABEL.status":              "active",
						"utf8.COLUMN_TYPE_METADATA.status":           "200",
					},
				},
			},
			expectedSchema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_BUILTIN.message", true),
				semconv.FieldFromFQN("timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp", false),
				semconv.FieldFromFQN("float64.COLUMN_TYPE_BUILTIN.value", false),
				semconv.FieldFromFQN("int64.COLUMN_TYPE_BUILTIN.count", false),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status", true),
				semconv.FieldFromFQN("utf8.COLUMN_TYPE_METADATA.status_extracted", true),
			}, nil),
			expectedRows: []arrowtest.Rows{
				{
					{
						"utf8.COLUMN_TYPE_BUILTIN.message":           "test message",
						"timestamp_ns.COLUMN_TYPE_BUILTIN.timestamp": time.Unix(1000000, 0).UTC(),
						"float64.COLUMN_TYPE_BUILTIN.value":          3.14,
						"int64.COLUMN_TYPE_BUILTIN.count":            int64(42),
						"utf8.COLUMN_TYPE_LABEL.status":              "active",
						"utf8.COLUMN_TYPE_METADATA.status":           nil,
						"utf8.COLUMN_TYPE_METADATA.status_extracted": "200",
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
				var records []arrow.Record
				for _, rows := range tt.inputRows {
					record := rows.Record(memory.DefaultAllocator, tt.schema)
					records = append(records, record)
				}
				input = NewBufferedPipeline(records...)
			}

			// Create compatibility pipeline
			pipeline := newColumnCompatibilityPipeline(tt.compat, input)
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
		compat := &physicalpb.ColumnCompat{
			Collision:   physicalpb.COLUMN_TYPE_LABEL,
			Source:      physicalpb.COLUMN_TYPE_METADATA,
			Destination: physicalpb.COLUMN_TYPE_METADATA,
		}

		// Create schema with invalid field name that cannot be parsed by semconv.ParseFQN
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "invalid-field-name", Type: types.Arrow.String, Nullable: true},
		}, nil)

		input := NewArrowtestPipeline(schema, arrowtest.Rows{
			{"invalid-field-name": "test"},
		})

		pipeline := newColumnCompatibilityPipeline(compat, input)
		defer pipeline.Close()

		_, err := pipeline.Read(t.Context())
		require.Error(t, err)
		// Should contain parsing error from semconv.ParseFQN
	})

	t.Run("input pipeline error", func(t *testing.T) {
		compat := &physicalpb.ColumnCompat{
			Collision:   physicalpb.COLUMN_TYPE_LABEL,
			Source:      physicalpb.COLUMN_TYPE_METADATA,
			Destination: physicalpb.COLUMN_TYPE_METADATA,
		}

		// Create a pipeline that will return an expected error
		expectedErr := errors.New("test error")
		input := errorPipeline(t.Context(), expectedErr)

		pipeline := newColumnCompatibilityPipeline(compat, input)
		defer pipeline.Close()

		_, err := pipeline.Read(t.Context())
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("non-string collision column should panic", func(t *testing.T) {
		compat := &physicalpb.ColumnCompat{
			Collision:   physicalpb.COLUMN_TYPE_LABEL,
			Source:      physicalpb.COLUMN_TYPE_METADATA,
			Destination: physicalpb.COLUMN_TYPE_METADATA,
		}

		// Create schema where source column is not string type
		// This should trigger the panic in the switch statement for non-string columns
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("utf8.COLUMN_TYPE_LABEL.status", false),    // collision column - string
			semconv.FieldFromFQN("int64.COLUMN_TYPE_METADATA.status", true), // source column - not string
		}, nil)

		input := NewArrowtestPipeline(schema, arrowtest.Rows{
			{"utf8.COLUMN_TYPE_LABEL.status": "200", "int64.COLUMN_TYPE_METADATA.status": int64(200)},
		})

		pipeline := newColumnCompatibilityPipeline(compat, input)
		defer pipeline.Close()

		// This should panic with "invalid column type: only string columns can be checked for collisions"
		require.Panics(t, func() {
			_, _ = pipeline.Read(t.Context())
		})
	})
}
