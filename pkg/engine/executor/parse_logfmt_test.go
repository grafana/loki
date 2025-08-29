package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestBuildLogfmtColumns(t *testing.T) {
	tests := []struct {
		name            string
		input           []string
		requestedKeys   []string
		expectedHeaders []string
		expected        []struct {
			values []string
			nulls  []bool // true means NULL at that position
		}
	}{
		{
			name: "Build Single String Column",
			input: []string{
				"level=error",
				"level=info",
				"level=debug",
			},
			requestedKeys:   []string{"level"},
			expectedHeaders: []string{"level"},
			expected: []struct {
				values []string
				nulls  []bool
			}{
				{
					values: []string{"error", "info", "debug"},
					nulls:  []bool{false, false, false},
				},
			},
		},
		{
			name: "Handle Missing Keys with NULL",
			input: []string{
				"level=error",
				"status=200",
				"level=info",
			},
			requestedKeys:   []string{"level"},
			expectedHeaders: []string{"level"},
			expected: []struct {
				values []string
				nulls  []bool
			}{
				{
					values: []string{"error", "", "info"},
					nulls:  []bool{false, true, false}, // Middle one should be NULL
				},
			},
		},
		{
			name: "Build Multiple Columns",
			input: []string{
				"level=error status=500",
				"level=info",
			},
			requestedKeys:   []string{"level", "status"},
			expectedHeaders: []string{"level", "status"},
			expected: []struct {
				values []string
				nulls  []bool
			}{
				{
					values: []string{"error", "info"},
					nulls:  []bool{false, false},
				},
				{
					values: []string{"500", ""},
					nulls:  []bool{false, true}, // Second row missing status
				},
			},
		},
		{
			name: "Handle Errors with Error Columns",
			input: []string{
				"level=info status=200",       // No errors
				"status==value level=error",   // Double equals error on requested key
				"level=\"unclosed status=500", // Unclosed quote error
			},
			requestedKeys:   []string{"level", "status"},
			expectedHeaders: []string{"level", "status", "__error__", "__error_details__"},
			expected: []struct {
				values []string
				nulls  []bool
			}{
				// Regular columns
				{
					// When parser hits error, it stops parsing the rest of the line
					values: []string{"info", "", ""},
					nulls:  []bool{false, true, true}, // NULL for both error lines
				},
				{
					values: []string{"200", "", ""},
					nulls:  []bool{false, true, true}, // status is NULL for both error lines
				},
				// Error columns (appended when errors occur)
				{
					values: []string{"", "LogfmtParserErr", "LogfmtParserErr"},
					nulls:  []bool{true, false, false},
				},
				{
					values: []string{"", "logfmt syntax error at pos 8 : unexpected '='", "logfmt syntax error at pos 27 : unterminated quoted value"},
					nulls:  []bool{true, false, false},
				},
			},
		},
		{
			name: "No Error Columns When No Errors",
			input: []string{
				"level=info status=200",
				"level=warn status=304",
				"level=debug status=201",
			},
			requestedKeys:   []string{"level", "status"},
			expectedHeaders: []string{"level", "status"},
			expected: []struct {
				values []string
				nulls  []bool
			}{
				// Only regular columns, no error columns
				{
					values: []string{"info", "warn", "debug"},
					nulls:  []bool{false, false, false},
				},
				{
					values: []string{"200", "304", "201"},
					nulls:  []bool{false, false, false},
				},
			},
		},
		{
			name: "Extract All Keys When None Requested",
			input: []string{
				"level=info status=200 method=GET",
				"level=warn code=304",
				"level=error status=500 method=POST duration=123ms",
			},
			requestedKeys:   nil, // nil means extract all keys
			expectedHeaders: []string{"code", "duration", "level", "method", "status"},
			expected: []struct {
				values []string
				nulls  []bool
			}{
				// Should get all unique keys across all lines, in consistent order
				// Keys should be: code, duration, level, method, status (alphabetical)
				{
					// code column
					values: []string{"", "304", ""},
					nulls:  []bool{true, false, true},
				},
				{
					// duration column
					values: []string{"", "", "123ms"},
					nulls:  []bool{true, true, false},
				},
				{
					// level column
					values: []string{"info", "warn", "error"},
					nulls:  []bool{false, false, false},
				},
				{
					// method column
					values: []string{"GET", "", "POST"},
					nulls:  []bool{false, true, false},
				},
				{
					// status column
					values: []string{"200", "", "500"},
					nulls:  []bool{false, true, false},
				},
			},
		},
		{
			name: "Extract All Keys With Empty Slice",
			input: []string{
				"a=1 b=2",
				"b=3 c=4",
			},
			requestedKeys:   []string{}, // empty slice also means extract all keys
			expectedHeaders: []string{"a", "b", "c"},
			expected: []struct {
				values []string
				nulls  []bool
			}{
				{
					// a column
					values: []string{"1", ""},
					nulls:  []bool{false, true},
				},
				{
					// b column
					values: []string{"2", "3"},
					nulls:  []bool{false, false},
				},
				{
					// c column
					values: []string{"", "4"},
					nulls:  []bool{true, false},
				},
			},
		},
		{
			name: "Extract All Keys With Errors When None Requested",
			input: []string{
				"level=info status=200 method=GET",       // Valid line
				"level==error code=500",                  // Double equals error
				"msg=\"unclosed duration=100ms code=400", // Unclosed quote error
				"level=debug method=POST",                // Valid line
			},
			requestedKeys: nil, // nil means extract all keys
			// When errors occur, only keys that were successfully parsed before the error are included
			expectedHeaders: []string{"level", "method", "status", "__error__", "__error_details__"},
			expected: []struct {
				values []string
				nulls  []bool
			}{
				// Should get only keys that were successfully parsed
				// Keys in alphabetical order: level, method, status (code and msg are missing due to parse errors)
				{
					// level column
					values: []string{"info", "", "", "debug"},
					nulls:  []bool{false, true, true, false}, // NULL when parse error occurs before level
				},
				{
					// method column
					values: []string{"GET", "", "", "POST"},
					nulls:  []bool{false, true, true, false},
				},
				{
					// status column
					values: []string{"200", "", "", ""},
					nulls:  []bool{false, true, true, true},
				},
				// Error columns should be appended
				{
					// __error__ column
					values: []string{"", "LogfmtParserErr", "LogfmtParserErr", ""},
					nulls:  []bool{true, false, false, true},
				},
				{
					// __error_details__ column
					values: []string{"", "logfmt syntax error at pos 7 : unexpected '='", "logfmt syntax error at pos 38 : unterminated quoted value", ""},
					nulls:  []bool{true, false, false, true},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create an Arrow string array from the input strings
			builder := array.NewStringBuilder(memory.DefaultAllocator)
			defer builder.Release()

			for _, str := range tt.input {
				builder.Append(str)
			}
			inputArray := builder.NewStringArray()
			defer inputArray.Release()

			// Call the function that should build Arrow columns
			headers, columns := BuildLogfmtColumns(inputArray, tt.requestedKeys, memory.DefaultAllocator)

			require.Equal(t, tt.expectedHeaders, headers, "Headers should match expected")
			require.Len(t, columns, len(tt.expected), "Should return expected number of columns")

			// Verify that each parsed column has the same number of rows as the input array
			for _, col := range columns {
				require.Equal(t, inputArray.Len(), col.Len())
			}

			// Verify each column
			for i, expectedCol := range tt.expected {
				column := columns[i]
				require.Equal(t, arrow.BinaryTypes.String, column.DataType())

				stringArray := column.(*array.String)
				require.Equal(t, len(expectedCol.values), stringArray.Len())

				// Check each value and null state
				for j := 0; j < stringArray.Len(); j++ {
					if expectedCol.nulls[j] {
						require.True(t, stringArray.IsNull(j), "Expected NULL at position %d in column %d, but got value: %s", j, i, stringArray.Value(j))
					} else {
						require.False(t, stringArray.IsNull(j), "Expected non-NULL at position %d in column %d", j, i)
						require.Equal(t, expectedCol.values[j], stringArray.Value(j), "Column %d, position %d", i, j)
					}
				}
			}
		})
	}
}
