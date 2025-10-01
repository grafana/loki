package executor

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
)

func buildLogfmtColumns(input *array.String, requestedKeys []string, allocator memory.Allocator) ([]string, []arrow.Array) {
	return buildColumns(input, requestedKeys, allocator, tokenizeLogfmt, types.LogfmtParserErrorType)
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
			//TODO: retain empty values if --keep-empty is set
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
