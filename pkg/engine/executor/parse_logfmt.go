package executor

import (
	"sort"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// BuildLogfmtColumns builds Arrow columns from logfmt input lines
// Returns the column headers, the Arrow columns, and any error
func BuildLogfmtColumns(input []string, requestedKeys []string, allocator memory.Allocator) ([]string, []arrow.Array, error) {
	// Parse each line and store results
	parsedLines, lineErrors, hasAnyError := parseLogfmtLines(input, requestedKeys)

	// Determine which columns to build
	columnKeys := determineColumnKeys(requestedKeys, parsedLines)

	// Build columns for each key
	columnsNeeded := len(columnKeys)
	if hasAnyError {
		columnsNeeded += 2 // +2 for potential error columns
	}
	columns := make([]arrow.Array, 0, columnsNeeded)
	headers := make([]string, 0, columnsNeeded)

	for _, key := range columnKeys {
		builder := array.NewStringBuilder(allocator)
		defer builder.Release()

		// Add values for this key from each line
		for _, parsedLine := range parsedLines {
			if value, ok := parsedLine[key]; ok {
				builder.Append(value)
			} else {
				builder.AppendNull()
			}
		}

		columns = append(columns, builder.NewArray())
		headers = append(headers, key)
	}

	// Add error columns if any errors occurred
	if hasAnyError {
		errorBuilder := array.NewStringBuilder(allocator)
		defer errorBuilder.Release()
		errorDetailsBuilder := array.NewStringBuilder(allocator)
		defer errorDetailsBuilder.Release()

		for _, err := range lineErrors {
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
	}

	return headers, columns, nil
}

// parseLogfmtLines parses each input line and returns the results along with any errors
func parseLogfmtLines(input []string, requestedKeys []string) ([]map[string]string, []error, bool) {
	parsedLines := make([]map[string]string, 0, len(input))
	lineErrors := make([]error, 0, len(input))
	hasAnyError := false

	for _, line := range input {
		// Use our existing tokenizer to parse the line
		result, err := TokenizeLogfmt(line, requestedKeys)
		lineErrors = append(lineErrors, err)
		if err != nil {
			hasAnyError = true
		}
		parsedLines = append(parsedLines, result)
	}

	return parsedLines, lineErrors, hasAnyError
}

// determineColumnKeys determines which columns to build based on requested keys or parsed data
func determineColumnKeys(requestedKeys []string, parsedLines []map[string]string) []string {
	if len(requestedKeys) > 0 {
		// Use requested keys as columns
		return requestedKeys
	}

	// Extract all unique keys from all lines
	uniqueKeys := make(map[string]bool)
	for _, parsedLine := range parsedLines {
		for key := range parsedLine {
			uniqueKeys[key] = true
		}
	}

	// Sort keys alphabetically for consistent ordering
	columnKeys := make([]string, 0, len(uniqueKeys))
	for key := range uniqueKeys {
		columnKeys = append(columnKeys, key)
	}
	sort.Strings(columnKeys)

	return columnKeys
}
