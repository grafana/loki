package discover

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	bench "github.com/grafana/loki/v3/pkg/logql/bench"
)

// TestClassifyStream validates all classification cases for the classifyStream
// pure function using table-driven subtests.
func TestClassifyStream(t *testing.T) {
	// Use first real bounded-set values for predictable test data.
	unwrappable0 := bench.UnwrappableFields[0] // "bytes"
	sm0 := bench.StructuredMetadataKeys[0]     // "detected_level"
	labelKey0 := bench.LabelKeys[0]            // "cluster"

	tests := []struct {
		name           string
		selector       string
		fields         []loghttp.DetectedField
		streamLabelSet map[string]struct{}
		wantFormat     bench.LogFormat
		wantUnwrap     []string
		wantDetected   []string
		wantSM         []string
		wantLabels     []string
	}{
		{
			name:     "all json parsers → LogFormatJSON",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				df("field_a", "json", logproto.DetectedFieldString),
				df("field_b", "json", logproto.DetectedFieldString),
				df("field_c", "json", logproto.DetectedFieldString),
			},
			// streamLabelSet reflects the selector's label keys (as built by RunClassification).
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatJSON,
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "all logfmt parsers → LogFormatLogfmt",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				df("field_a", "logfmt", logproto.DetectedFieldString),
				df("field_b", "logfmt", logproto.DetectedFieldString),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatLogfmt,
			wantLabels:     []string{"cluster"},
		},
		{
			name:           "no parsers → LogFormatUnstructured",
			selector:       `{cluster="us-east-1"}`,
			fields:         []loghttp.DetectedField{df("field_a", "", logproto.DetectedFieldString)},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatUnstructured,
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "tie between json and logfmt → first-seen-wins (not unstructured)",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				df("field_a", "json", logproto.DetectedFieldString),
				df("field_b", "logfmt", logproto.DetectedFieldString),
			},
			// Tie: both parsers have count=1. First-seen-wins; map iteration order
			// is non-deterministic so we only assert it's one of the two formats.
			streamLabelSet: labelsOf("cluster"),
			// wantFormat is intentionally left zero — the test loop handles this case.
			wantLabels: []string{"cluster"},
		},
		{
			name:     "json majority (3 json, 1 logfmt) → LogFormatJSON",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				df("field_a", "json", logproto.DetectedFieldString),
				df("field_b", "json", logproto.DetectedFieldString),
				df("field_c", "json", logproto.DetectedFieldString),
				df("field_d", "logfmt", logproto.DetectedFieldString),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatJSON,
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "_extracted fields excluded from parser counting",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				// Two _extracted json fields — should NOT count toward json vote.
				df("field_a_extracted", "json", logproto.DetectedFieldString),
				df("field_b_extracted", "json", logproto.DetectedFieldString),
				// One real logfmt field — should win with count=1.
				df("field_c", "logfmt", logproto.DetectedFieldString),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatLogfmt,
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "unwrappable: name match + numeric (int) type → included in unwrappable and detected",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				// unwrappable0 = "bytes"; json parser + int type → unwrappable AND detected (in boundedUnion)
				df(unwrappable0, "json", logproto.DetectedFieldInt),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatJSON,
			wantUnwrap:     []string{unwrappable0},
			wantDetected:   []string{unwrappable0}, // non-empty parser + in boundedUnion → also detected
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "unwrappable: name match + string type → in detected but NOT unwrappable",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				// string type → not unwrappable, but non-empty parser + in boundedUnion → detected
				df(unwrappable0, "json", logproto.DetectedFieldString),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatJSON,
			wantUnwrap:     nil,
			wantDetected:   []string{unwrappable0},
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "unwrappable: _extracted field with numeric type → excluded from all indexes",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				df(unwrappable0+"_extracted", "json", logproto.DetectedFieldInt),
			},
			streamLabelSet: labelsOf("cluster"),
			// _extracted excluded from parser counting → unstructured
			wantFormat: bench.LogFormatUnstructured,
			wantUnwrap: nil,
			wantLabels: []string{"cluster"},
		},
		{
			name:     "structured metadata: empty parser + in set + not label → included",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				df(sm0, "", logproto.DetectedFieldString),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatUnstructured,
			wantSM:         []string{sm0},
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "structured metadata: empty parser + in set + IS a stream label → excluded",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				df(sm0, "", logproto.DetectedFieldString),
			},
			// sm0 is a stream label → excluded from SM; "cluster" is also present.
			streamLabelSet: labelsOf("cluster", sm0),
			wantFormat:     bench.LogFormatUnstructured,
			wantSM:         nil,
			wantLabels:     []string{"cluster", sm0}, // both keys in streamLabelSet appear as labelKeys
		},
		{
			name:     "structured metadata: non-empty parser → not structured meta (goes to detected if in bounded union)",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				// sm0 = "detected_level"; json parser → in boundedUnion (StructuredMetadataKeys) → detected
				df(sm0, "json", logproto.DetectedFieldString),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatJSON,
			wantSM:         nil,
			wantDetected:   []string{sm0}, // in bounded union + non-empty parser → detected
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "detected field: non-empty parser + in LabelKeys → included",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				// labelKey0 = "cluster"; json parser + in LabelKeys → detected
				df(labelKey0, "json", logproto.DetectedFieldString),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatJSON,
			wantDetected:   []string{labelKey0},
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "detected field: non-empty parser + NOT in any bounded set → excluded",
			selector: `{cluster="us-east-1"}`,
			fields: []loghttp.DetectedField{
				df("completely_unknown_field_xyz", "json", logproto.DetectedFieldString),
			},
			streamLabelSet: labelsOf("cluster"),
			wantFormat:     bench.LogFormatJSON,
			wantDetected:   nil,
			wantLabels:     []string{"cluster"},
		},
		{
			name:     "label keys: extracted from canonical selector — both cluster and env in LabelKeys",
			selector: `{cluster="us-east-1", env="prod"}`,
			fields:   []loghttp.DetectedField{},
			// streamLabelSet reflects the selector's actual label keys.
			streamLabelSet: labelsOf("cluster", "env"),
			wantFormat:     bench.LogFormatUnstructured,
			wantLabels:     []string{"cluster", "env"},
		},
		{
			name:     "label keys: only keys in LabelKeys bounded set appear",
			selector: `{notinlabelkeys="foo"}`,
			fields:   []loghttp.DetectedField{},
			// notinlabelkeys is not in LabelKeys → no label keys classified
			streamLabelSet: noLabels(),
			wantFormat:     bench.LogFormatUnstructured,
			wantLabels:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyStream(tt.selector, tt.fields, tt.streamLabelSet)

			require.Equal(t, tt.selector, got.selector, "selector must be preserved")
			// For the tie case (wantFormat==""), just check it resolves to a known format.
			if tt.wantFormat == "" {
				require.NotEqual(t, bench.LogFormatUnstructured, got.format,
					"tie with equal non-zero counts: first-seen-wins should pick json or logfmt, not unstructured")
			} else {
				require.Equal(t, tt.wantFormat, got.format, "format mismatch")
			}

			if tt.wantUnwrap != nil {
				require.ElementsMatch(t, tt.wantUnwrap, got.unwrappable, "unwrappable fields mismatch")
			} else {
				require.Empty(t, got.unwrappable, "expected no unwrappable fields")
			}

			if tt.wantDetected != nil {
				require.ElementsMatch(t, tt.wantDetected, got.detectedFields, "detected fields mismatch")
			} else {
				require.Empty(t, got.detectedFields, "expected no detected fields")
			}

			if tt.wantSM != nil {
				require.ElementsMatch(t, tt.wantSM, got.structuredMeta, "structured metadata mismatch")
			} else {
				require.Empty(t, got.structuredMeta, "expected no structured metadata")
			}

			if tt.wantLabels != nil {
				require.ElementsMatch(t, tt.wantLabels, got.labelKeys, "label keys mismatch")
			} else {
				require.Empty(t, got.labelKeys, "expected no label keys")
			}
		})
	}
}

// fakeClassifyClient is a test double for ClassifyAPI.
// It records calls and returns canned per-selector responses.
type fakeClassifyClient struct {
	mu       sync.Mutex
	calls    []string                                   // selectors called, for assertion
	response map[string]*loghttp.DetectedFieldsResponse // per-selector response
	errOn    map[string]error                           // selector -> error to return
}

func newFakeClassifyClient() *fakeClassifyClient {
	return &fakeClassifyClient{
		response: make(map[string]*loghttp.DetectedFieldsResponse),
		errOn:    make(map[string]error),
	}
}

func (f *fakeClassifyClient) GetDetectedFields(
	queryStr string, _ int, _, _ time.Time,
) (*loghttp.DetectedFieldsResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, queryStr)
	f.mu.Unlock()
	if err, ok := f.errOn[queryStr]; ok {
		return nil, err
	}
	if resp, ok := f.response[queryStr]; ok {
		return resp, nil
	}
	return &loghttp.DetectedFieldsResponse{}, nil
}

// df builds a DetectedField for test cases. When parser is non-empty, it is
// placed into a single-element Parsers slice; when empty, Parsers is nil.
func df(label, parser string, typ logproto.DetectedFieldType) loghttp.DetectedField {
	var parsers []string
	if parser != "" {
		parsers = []string{parser}
	}
	return loghttp.DetectedField{
		Label:   label,
		Parsers: parsers,
		Type:    typ,
	}
}

// noLabels returns an empty stream label set.
func noLabels() map[string]struct{} {
	return make(map[string]struct{})
}

// labelsOf returns a stream label set containing the given keys.
func labelsOf(keys ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}
