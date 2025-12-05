package executor

import (
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

func buildLabelfmtColumns(input arrow.RecordBatch, sourceCol *array.String, requestedKeys []string, labelFmts []log.LabelFmt) ([]string, []arrow.Array) {
	parseFunc := func(row arrow.RecordBatch, line string) (map[string]string, error) {
		return tokenizeLabelfmt(line, requestedKeys, labelFmts)
	}
	return buildColumns(input, sourceCol, requestedKeys, parseFunc, types.LabelfmtParserErrorType)
}

// tokenizeLabelfmt parses labelfmt input using the standard decoder
// Returns a map of key-value pairs with first-wins semantics for duplicates
// If requestedKeys is provided, the result will be filtered to only include those keys
func tokenizeLabelfmt(input string, requestedKeys []string, labelFmts []log.LabelFmt) (map[string]string, error) {
	result := make(map[string]string)

	var requestedKeyLookup map[string]struct{}
	if len(requestedKeys) > 0 {
		requestedKeyLookup = make(map[string]struct{}, len(requestedKeys))
		for _, key := range requestedKeys {
			requestedKeyLookup[key] = struct{}{}
		}
	}
	decoder, err := log.NewLabelsFormatter(labelFmts)
	if err != nil {
		return nil, fmt.Errorf("unable to create label formatter with formats %v", labelFmts)
	}
	// labelsbuilder comes from structured metadata
	decoder.Process(time.Now().UnixNano(), unsafeBytes(input), nil)
	// decoder := log.NewLabelsFormatter()
	// for !decoder.EOL() && decoder.ScanKeyval() {
	// 	key := sanitizeLabelKey(unsafeString(decoder.Key()), true)
	// 	if requestedKeyLookup != nil {
	// 		if _, wantKey := requestedKeyLookup[key]; !wantKey {
	// 			continue
	// 		}
	// 	}

	// 	val := decoder.Value()
	// 	if !keepEmpty && len(val) == 0 {
	// 		// Skip empty values unless keepEmpty is set
	// 		continue
	// 	}

	// 	// First-wins semantics for duplicates
	// 	_, exists := result[key]
	// 	if exists {
	// 		continue
	// 	}
	// 	result[key] = unsafeString(decoder.Value())
	// }

	// // Check for parsing errors
	// if err := decoder.Err(); err != nil {
	// 	if strict {
	// 		// In strict mode, return the error immediately
	// 		return nil, err
	// 	}
	// 	// In non-strict mode, return partial results with the error
	// 	return result, err
	// }

	return result, nil
}
