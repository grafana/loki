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
	columnBuilders := make(map[string]*array.StringBuilder)
	columnOrder := parseKeys(input, requestedKeys, columnBuilders, allocator)

	// Build final arrays
	columns := make([]arrow.Array, 0, len(columnOrder))
	headers := make([]string, 0, len(columnOrder))

	for _, key := range columnOrder {
		builder := columnBuilders[key]
		columns = append(columns, builder.NewArray())
		headers = append(headers, key)
		builder.Release()
	}

	return headers, columns
}

// parseKeys discovers columns dynamically as lines are parsed
func parseKeys(input *array.String, requestedKeys []string, columnBuilders map[string]*array.StringBuilder, allocator memory.Allocator) []string {
	columnOrder := []string{}

	for i := 0; i < input.Len(); i++ {
		line := input.Value(i)
		parsed, _ := tokenizeLogfmt(line, requestedKeys)

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
				builder.AppendNulls(i)
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

	return columnOrder
}

// tokenizeLogfmt parses logfmt input using the standard decoder
// Returns a map of key-value pairs with last-wins semantics for duplicates
// If requestedKeys is provided, the result will be filtered to only include those keys
func tokenizeLogfmt(input string, requestedKeys []string) (map[string]string, error) {
	result := make(map[string]string)

	var requestedKeyLookup map[string]struct{}
	if len(requestedKeys) > 0 {
		requestedKeyLookup = make(map[string]struct{}, len(requestedKeys))
		for _, key := range requestedKeys {
			requestedKeyLookup[key] = struct{}{}
		}
	}

	decoder := logfmt.NewDecoder(unsafeBytes(input))
	for !decoder.EOL() && decoder.ScanKeyval() {
		key := unsafeString(decoder.Key())
		if requestedKeyLookup != nil {
			if _, wantKey := requestedKeyLookup[key]; !wantKey {
				continue
			}
		}

		val := decoder.Value()
		if len(val) == 0 {
			// TODO: retain empty values if --keep-empty is set.
			continue
		}

		// Last-wins semantics for duplicates
		result[key] = unsafeString(decoder.Value())
	}

	// Check for parsing errors
	if err := decoder.Err(); err != nil {
		return result, err
	}

	return result, nil
}

// unsafeBytes converts a string to []byte without allocation
func unsafeBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// unsafeString converts a []byte to string without allocation
func unsafeString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
