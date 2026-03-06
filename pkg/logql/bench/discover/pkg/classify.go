package discover

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	bench "github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// CategorizeLabelsTripperware injects the categorize-labels response encoding
// header required for the detected_fields API to differentiate structured
// metadata fields from indexed stream label fields. Without this header, the
// API treats all fields as the same type.
type CategorizeLabelsTripperware struct {
	Wrapped http.RoundTripper
}

// RoundTrip implements http.RoundTripper by injecting the header and delegating
// to the wrapped transport.
func (t *CategorizeLabelsTripperware) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set(httpreq.LokiEncodingFlagsHeader, string(httpreq.FlagCategorizeLabels))
	return t.Wrapped.RoundTrip(req)
}

// streamClassification holds the per-stream classification results before they
// are aggregated into ClassifyResult inverted indexes.
type streamClassification struct {
	selector       string
	format         bench.LogFormat
	unwrappable    []string
	detectedFields []string
	structuredMeta []string
	labelKeys      []string
}

// setOf builds a map[string]struct{} membership set from a slice of strings.
// Used to convert bounded-set slices into O(1) lookup structures.
func setOf(keys []string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

// boundedSets holds pre-computed membership sets from the global bounded-set
// slices. Build once with newBoundedSets() and pass to classifyStream to avoid
// rebuilding on every call.
type boundedSets struct {
	unwrappable  map[string]struct{}
	structuredMD map[string]struct{}
	union        map[string]struct{} // UnwrappableFields ∪ StructuredMetadataKeys ∪ LabelKeys
}

func newBoundedSets() boundedSets {
	unwrappableSet := setOf(bench.UnwrappableFields)
	smSet := setOf(bench.StructuredMetadataKeys)

	union := make(map[string]struct{}, len(bench.UnwrappableFields)+len(bench.StructuredMetadataKeys)+len(bench.LabelKeys))
	for _, k := range bench.UnwrappableFields {
		union[k] = struct{}{}
	}
	for _, k := range bench.StructuredMetadataKeys {
		union[k] = struct{}{}
	}
	for _, k := range bench.LabelKeys {
		union[k] = struct{}{}
	}

	return boundedSets{
		unwrappable:  unwrappableSet,
		structuredMD: smSet,
		union:        union,
	}
}

// isNumericType returns true when the detected field type is numeric and can
// therefore be used with | unwrap in LogQL metric queries.
func isNumericType(t logproto.DetectedFieldType) bool {
	switch t {
	case logproto.DetectedFieldInt, logproto.DetectedFieldFloat,
		logproto.DetectedFieldDuration, logproto.DetectedFieldBytes:
		return true
	}
	return false
}

// classifyStream performs per-stream field classification given a canonical
// selector, the detected fields returned by the Loki API, and the set of label
// keys that are indexed stream labels for this selector.
//
// The function applies the following rules to each field in order:
//  1. Skip fields whose label ends with "_extracted" (all subsequent checks).
//  2. Format: tally f.Parsers values across all non-extracted fields; the parser
//     with the highest count wins (first-seen wins on equal counts).
//     All-empty parsers → LogFormatUnstructured.
//  3. Unwrappable: field in UnwrappableFields AND isNumericType(f.Type).
//  4. Structured metadata: len(f.Parsers)==0 AND field in StructuredMetadataKeys
//     AND field NOT in streamLabelSet.
//  5. Detected fields: len(f.Parsers)>0 AND field in any bounded set
//     (UnwrappableFields ∪ StructuredMetadataKeys ∪ LabelKeys).
//  6. Label keys: any key present in streamLabelSet is included.
func classifyStream(
	selector string,
	fields []loghttp.DetectedField,
	streamLabelSet map[string]struct{},
	sets boundedSets,
) streamClassification {
	result := streamClassification{
		selector: selector,
	}

	parserCounts := make(map[string]int)
	for _, f := range fields {
		// Rule 1: skip _extracted fields for all checks.
		if strings.HasSuffix(f.Label, "_extracted") {
			continue
		}

		// Format vote: each parser in the Parsers slice gets a vote.
		for _, p := range f.Parsers {
			if p != "" {
				parserCounts[p]++
			}
		}

		// Rule 3: Unwrappable — in UnwrappableFields AND numeric type.
		if _, inUnwrappable := sets.unwrappable[f.Label]; inUnwrappable {
			if isNumericType(f.Type) {
				result.unwrappable = append(result.unwrappable, f.Label)
			}
		}

		hasParsers := len(f.Parsers) > 0

		// Rule 4: Structured metadata — no parsers AND in StructuredMetadataKeys
		// AND not a stream label.
		if !hasParsers {
			if _, inSM := sets.structuredMD[f.Label]; inSM {
				if _, isLabel := streamLabelSet[f.Label]; !isLabel {
					result.structuredMeta = append(result.structuredMeta, f.Label)
				}
			}
		}

		// Rule 5: Detected fields — has parsers AND in any bounded set.
		if hasParsers {
			if _, inUnion := sets.union[f.Label]; inUnion {
				result.detectedFields = append(result.detectedFields, f.Label)
			}
		}
	}

	// Resolve format winner
	maxCount := 0
	winner := ""
	for parser, count := range parserCounts {
		if count > maxCount {
			maxCount = count
			winner = parser
		}
	}
	if maxCount == 0 {
		result.format = bench.LogFormatUnstructured
	} else {
		switch winner {
		case "json":
			result.format = bench.LogFormatJSON
		case "logfmt":
			result.format = bench.LogFormatLogfmt
		default:
			result.format = bench.LogFormatUnstructured
		}
	}

	// Rule 6: Label keys — read directly from streamLabelSet keys.
	for key := range streamLabelSet {
		result.labelKeys = append(result.labelKeys, key)
	}
	sort.Strings(result.labelKeys)

	return result
}

// callWithRetry calls fn up to 2 times (initial attempt + 1 retry) using
// exponential backoff. Returns the last error if all attempts fail.
func callWithRetry(ctx context.Context, fn func() error) error {
	cfg := backoff.Config{
		MinBackoff: 500 * time.Millisecond,
		MaxBackoff: 2 * time.Second,
		MaxRetries: 2,
	}
	b := backoff.New(ctx, cfg)
	var lastErr error
	for b.Ongoing() {
		if lastErr = fn(); lastErr == nil {
			return nil
		}
		b.Wait()
	}
	return lastErr
}
