package executor

import (
	"sort"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
)

// BuildLogfmtColumns builds Arrow columns from logfmt input lines
// Returns the column headers, the Arrow columns, and any error
func BuildLogfmtColumns(input *array.String, requestedKeys []string, allocator memory.Allocator) ([]string, []arrow.Array) {
	var columnOrder []string
	var errorList []error
	columnBuilders := make(map[string]*array.StringBuilder)
	hasAnyError := false

	if len(requestedKeys) > 0 {
		// Fast path: parse only requested keys
		columnOrder, errorList, hasAnyError = parseRequestedKeys(input, requestedKeys, columnBuilders, allocator)
	} else {
		// Dynamic path: discover columns as we parse
		columnOrder, errorList, hasAnyError = parseAllKeys(input, columnBuilders, allocator)
	}

	// Build final arrays
	columns := make([]arrow.Array, 0, len(columnOrder)+2)
	headers := make([]string, 0, len(columnOrder)+2)

	for _, key := range columnOrder {
		builder := columnBuilders[key]
		columns = append(columns, builder.NewArray())
		headers = append(headers, key)
		builder.Release()
	}

	// Add error columns if needed
	if hasAnyError {
		errorBuilder := array.NewStringBuilder(allocator)
		errorDetailsBuilder := array.NewStringBuilder(allocator)

		for _, err := range errorList {
			if err != nil {
				errorBuilder.Append("LogfmtParserErr")
				errorDetailsBuilder.Append(err.Error())
			} else {
				errorBuilder.AppendNull()
				errorDetailsBuilder.AppendNull()
			}
		}

		columns = append(columns, errorBuilder.NewArray())
		columns = append(columns, errorDetailsBuilder.NewArray())
		headers = append(headers, "__error__", "__error_details__")

		errorBuilder.Release()
		errorDetailsBuilder.Release()
	}

	return headers, columns
}

// parseRequestedKeys handles the fast path when specific keys are requested
// Pre-creates all column builders and processes lines with known columns
func parseRequestedKeys(input *array.String, requestedKeys []string, columnBuilders map[string]*array.StringBuilder, allocator memory.Allocator) ([]string, []error, bool) {
	columnOrder := make([]string, 0, len(requestedKeys))
	errorList := make([]error, 0, input.Len())
	hasAnyError := false

	// Pre-create all builders
	for _, key := range requestedKeys {
		columnBuilders[key] = array.NewStringBuilder(allocator)
		columnOrder = append(columnOrder, key)
	}

	// Process each line
	for i := 0; i < input.Len(); i++ {
		line := input.Value(i)
		parsed, err := TokenizeLogfmt(line, requestedKeys) // Already filtered
		errorList = append(errorList, err)
		if err != nil {
			hasAnyError = true
		}

		// Append values for all requested keys
		for _, key := range requestedKeys {
			if value, ok := parsed[key]; ok {
				columnBuilders[key].Append(value)
			} else {
				columnBuilders[key].AppendNull()
			}
		}
	}

	return columnOrder, errorList, hasAnyError
}

// parseAllKeys handles the dynamic path when no specific keys are requested
// Discovers columns dynamically as lines are parsed
func parseAllKeys(input *array.String, columnBuilders map[string]*array.StringBuilder, allocator memory.Allocator) ([]string, []error, bool) {
	columnOrder := []string{}
	errorList := make([]error, 0, input.Len())
	hasAnyError := false

	for i := 0; i < input.Len(); i++ {
		line := input.Value(i)
		parsed, err := TokenizeLogfmt(line, nil) // Get all keys
		errorList = append(errorList, err)
		if err != nil {
			hasAnyError = true
		}

		// Track which keys we've seen this row
		seenKeys := make(map[string]bool)

		// Add values for parsed keys
		for key, value := range parsed {
			seenKeys[key] = true
			builder, exists := columnBuilders[key]
			if !exists {
				// New column discovered - create and backfill
				builder = array.NewStringBuilder(allocator)
				columnBuilders[key] = builder
				columnOrder = append(columnOrder, key)

				// Backfill NULLs for previous rows
				for j := 0; j < i; j++ {
					builder.AppendNull()
				}
			}
			builder.Append(value)
		}

		// Append NULLs for columns not in this row
		for _, key := range columnOrder {
			if !seenKeys[key] {
				columnBuilders[key].AppendNull()
			}
		}
	}

	// Sort column order for consistency
	sort.Strings(columnOrder)

	return columnOrder, errorList, hasAnyError
}

// TokenizeLogfmt parses logfmt input using the standard decoder
// Returns a map of key-value pairs with last-wins semantics for duplicates
// If requestedKeys is provided, the result will be filtered to only include those keys
func TokenizeLogfmt(input string, requestedKeys []string) (map[string]string, error) {
	result := make(map[string]string)
	decoder := logfmt.NewDecoder(unsafeBytes(input))

	// Parse all key-value pairs
	for !decoder.EOL() && decoder.ScanKeyval() {
		key := string(decoder.Key())
		value := string(decoder.Value())
		// Last-wins semantics for duplicates
		result[key] = value
	}

	// Check for parsing errors
	if err := decoder.Err(); err != nil {
		return result, err
	}

	// Filter to requested keys if specified
	if len(requestedKeys) > 0 {
		filteredResult := make(map[string]string)
		for _, key := range requestedKeys {
			if value, ok := result[key]; ok {
				filteredResult[key] = value
			}
		}
		return filteredResult, nil
	}

	return result, nil
}

// unsafeBytes converts a string to []byte without allocation
func unsafeBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
