package logproto

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"
	"unsafe"

	jsoniter "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go/mocktracer"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
)

// This test verifies that jsoninter uses our custom method for marshalling.
// We do that by using "test sample" recognized by marshal function when in testing mode.
func TestJsoniterMarshalForSample(t *testing.T) {
	testMarshalling(t, jsoniter.Marshal, "test sample")
}

func TestStdlibJsonMarshalForSample(t *testing.T) {
	testMarshalling(t, json.Marshal, "json: error calling MarshalJSON for type logproto.LegacySample: test sample")
}

func testMarshalling(t *testing.T, marshalFn func(v interface{}) ([]byte, error), expectedError string) {
	isTesting = true
	defer func() { isTesting = false }()

	out, err := marshalFn(LegacySample{Value: 12345, TimestampMs: 98765})
	require.NoError(t, err)
	require.Equal(t, `[98.765,"12345"]`, string(out))

	_, err = marshalFn(LegacySample{Value: math.NaN(), TimestampMs: 0})
	require.EqualError(t, err, expectedError)

	// If not testing, we get normal output.
	isTesting = false
	out, err = marshalFn(LegacySample{Value: math.NaN(), TimestampMs: 0})
	require.NoError(t, err)
	require.Equal(t, `[0,"NaN"]`, string(out))
}

// This test verifies that jsoninter uses our custom method for unmarshalling Sample.
// As with Marshal, we rely on testing mode and special value that reports error.
func TestJsoniterUnmarshalForSample(t *testing.T) {
	testUnmarshalling(t, jsoniter.Unmarshal, "test sample")
}

func TestStdlibJsonUnmarshalForSample(t *testing.T) {
	testUnmarshalling(t, json.Unmarshal, "test sample")
}

func testUnmarshalling(t *testing.T, unmarshalFn func(data []byte, v interface{}) error, expectedError string) {
	isTesting = true
	defer func() { isTesting = false }()

	sample := LegacySample{}

	err := unmarshalFn([]byte(`[98.765,"12345"]`), &sample)
	require.NoError(t, err)
	require.Equal(t, LegacySample{Value: 12345, TimestampMs: 98765}, sample)

	err = unmarshalFn([]byte(`[0.0,"NaN"]`), &sample)
	require.EqualError(t, err, expectedError)

	isTesting = false
	err = unmarshalFn([]byte(`[0.0,"NaN"]`), &sample)
	require.NoError(t, err)
	require.Equal(t, int64(0), sample.TimestampMs)
	require.True(t, math.IsNaN(sample.Value))
}

func TestFromLabelAdaptersToLabels(t *testing.T) {
	input := []LabelAdapter{{Name: "hello", Value: "world"}}
	expected := labels.Labels{labels.Label{Name: "hello", Value: "world"}}
	actual := FromLabelAdaptersToLabels(input)

	assert.Equal(t, expected, actual)

	// All strings must NOT be copied.
	assert.Equal(t, uintptr(unsafe.Pointer(&input[0].Name)), uintptr(unsafe.Pointer(&actual[0].Name)))
	assert.Equal(t, uintptr(unsafe.Pointer(&input[0].Value)), uintptr(unsafe.Pointer(&actual[0].Value)))
}

func TestLegacySampleCompatibilityMarshalling(t *testing.T) {
	ts := int64(1232132123)
	val := 12345.12345
	legacySample := LegacySample{Value: val, TimestampMs: ts}
	got, err := json.Marshal(legacySample)
	require.NoError(t, err)

	legacyExpected := fmt.Sprintf("[%d.%d,\"%.5f\"]", ts/1000, 123, val)
	require.Equal(t, []byte(legacyExpected), got)

	// proving that `logproto.Sample` marshal things differently than `logproto.LegacySample`:
	incompatibleSample := Sample{Value: val, Timestamp: ts}
	gotIncompatibleSample, err := json.Marshal(incompatibleSample)
	require.NoError(t, err)
	require.NotEqual(t, []byte(legacyExpected), gotIncompatibleSample)
}

func TestLegacySampleCompatibilityUnmarshalling(t *testing.T) {
	serializedSample := "[123123.123,\"12345.12345\"]"
	var legacySample LegacySample
	err := json.Unmarshal([]byte(serializedSample), &legacySample)
	require.NoError(t, err)
	expectedLegacySample := LegacySample{Value: 12345.12345, TimestampMs: 123123123}
	require.EqualValues(t, expectedLegacySample, legacySample)

	// proving that `logproto.Sample` unmarshal things differently than `logproto.LegacySample`:
	incompatibleSample := Sample{Value: 12345.12345, Timestamp: 123123123}
	require.NotEqualValues(t, expectedLegacySample, incompatibleSample)
}

func TestLegacyLabelPairCompatibilityMarshalling(t *testing.T) {
	legacyLabelPair := LegacyLabelPair{Name: []byte("labelname"), Value: []byte("labelvalue")}
	got, err := json.Marshal(legacyLabelPair)
	require.NoError(t, err)

	expectedStr := `{"name":"bGFiZWxuYW1l","value":"bGFiZWx2YWx1ZQ=="}`
	require.Equal(t, []byte(expectedStr), got)

	// proving that `logproto.LegacyLabelPair` marshal things differently than `logproto.LabelPair`:
	incompatibleLabelPair := LabelPair{Name: "labelname", Value: "labelvalue"}
	gotIncompatible, err := json.Marshal(incompatibleLabelPair)
	require.NoError(t, err)
	require.NotEqual(t, []byte(expectedStr), gotIncompatible)
}

func TestLegacyLabelPairCompatibilityUnmarshalling(t *testing.T) {
	serializedLabelPair := `{"name":"bGFiZWxuYW1l","value":"bGFiZWx2YWx1ZQ=="}`
	var legacyLabelPair LegacyLabelPair
	err := json.Unmarshal([]byte(serializedLabelPair), &legacyLabelPair)
	require.NoError(t, err)
	expectedLabelPair := LegacyLabelPair{Name: []byte("labelname"), Value: []byte("labelvalue")}
	require.EqualValues(t, expectedLabelPair, legacyLabelPair)

	// proving that `logproto.LegacyLabelPair` unmarshal things differently than `logproto.LabelPair`:
	var incompatibleLabelPair LabelPair
	err = json.Unmarshal([]byte(serializedLabelPair), &incompatibleLabelPair)
	require.NoError(t, err)
	require.NotEqualValues(t, expectedLabelPair, incompatibleLabelPair)
}

func TestMergeLabelResponses(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		responses []*LabelResponse
		expected  []*LabelResponse
		err       error
	}{
		{
			desc: "merge two label responses",
			responses: []*LabelResponse{
				{Values: []string{"test"}},
				{Values: []string{"test2"}},
			},
			expected: []*LabelResponse{
				{Values: []string{"test", "test2"}},
			},
		},
		{
			desc: "merge three label responses",
			responses: []*LabelResponse{
				{Values: []string{"test"}},
				{Values: []string{"test2"}},
				{Values: []string{"test3"}},
			},
			expected: []*LabelResponse{
				{Values: []string{"test", "test2", "test3"}},
			},
		},
		{
			desc: "merge three label responses with one non-unique",
			responses: []*LabelResponse{
				{Values: []string{"test"}},
				{Values: []string{"test"}},
				{Values: []string{"test2"}},
				{Values: []string{"test3"}},
			},
			expected: []*LabelResponse{
				{Values: []string{"test", "test2", "test3"}},
			},
		},
		{
			desc: "merge one and expect one",
			responses: []*LabelResponse{
				{Values: []string{"test"}},
			},
			expected: []*LabelResponse{
				{Values: []string{"test"}},
			},
		},
		{
			desc:      "merge empty and expect empty",
			responses: []*LabelResponse{},
			expected:  []*LabelResponse{},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			merged, err := MergeLabelResponses(tc.responses)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else if len(tc.expected) == 0 {
				require.Empty(t, merged)
			} else {
				require.ElementsMatch(t, tc.expected[0].Values, merged.Values)
			}
		})
	}
}

func TestMergeSeriesResponses(t *testing.T) {
	mockSeriesResponse := func(series [][]SeriesIdentifier_LabelsEntry) *SeriesResponse {
		resp := &SeriesResponse{}
		for _, s := range series {
			resp.Series = append(resp.Series, SeriesIdentifier{
				Labels: s,
			})
		}
		return resp
	}

	for _, tc := range []struct {
		desc      string
		responses []*SeriesResponse
		expected  []*SeriesResponse
		err       error
	}{
		{
			desc: "merge one series response and expect one",
			responses: []*SeriesResponse{
				{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test", "test")}}},
			},
			expected: []*SeriesResponse{
				mockSeriesResponse([][]SeriesIdentifier_LabelsEntry{{{"test", "test"}}}),
			},
		},
		{
			desc: "merge two series responses",
			responses: []*SeriesResponse{
				{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test", "test")}}},
				{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test2", "test2")}}},
			},
			expected: []*SeriesResponse{
				mockSeriesResponse([][]SeriesIdentifier_LabelsEntry{{{"test", "test"}}, {{"test2", "test2"}}}),
			},
		},
		{
			desc: "merge three series responses",
			responses: []*SeriesResponse{
				{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test", "test")}}},
				{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test2", "test2")}}},
				{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test3", "test3")}}},
			},
			expected: []*SeriesResponse{
				mockSeriesResponse([][]SeriesIdentifier_LabelsEntry{{{"test", "test"}}, {{"test2", "test2"}}, {{"test3", "test3"}}}),
			},
		},
		{
			desc:      "merge empty and expect empty",
			responses: []*SeriesResponse{},
			expected:  []*SeriesResponse{},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			merged, err := MergeSeriesResponses(tc.responses)
			if err != nil {
				require.Equal(t, tc.err, err)
			} else if len(tc.expected) == 0 {
				require.Empty(t, merged)
			} else {
				require.ElementsMatch(t, tc.expected[0].Series, merged.Series)
			}
		})
	}
}

func TestFilterChunkRefRequestGetQuery(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		request  FilterChunkRefRequest
		expected string
	}{
		{
			desc:     "empty request",
			expected: `0/0`,
		},
		{
			desc: "request no filters",
			request: FilterChunkRefRequest{
				Refs: []*GroupedChunkRefs{
					{
						Fingerprint: 1,
						Tenant:      "test",
					},
				},
			},
			expected: `9962287286179718960/0`,
		},
		{
			desc: "request with filters but no chunks",
			request: FilterChunkRefRequest{
				Plan: plan.QueryPlan{
					AST: syntax.MustParseExpr(`{foo="bar"} |= "uuid"`),
				},
			},
			expected: `0/938557591`,
		},
		{
			desc: "request with filters and chunks",
			request: FilterChunkRefRequest{
				Refs: []*GroupedChunkRefs{
					{
						Fingerprint: 1,
						Tenant:      "test",
					},
					{
						Fingerprint: 2,
						Tenant:      "test",
					},
				},
				Plan: plan.QueryPlan{
					AST: syntax.MustParseExpr(`{foo="bar"} |= "uuid" != "trace"`),
				},
			},
			expected: `8827404902424034886/2710035654`,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			actual := tc.request.GetQuery()
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestIndexStatsRequestSpanLogging(t *testing.T) {
	now := time.Now()
	end := now.Add(1000 * time.Second)
	req := IndexStatsRequest{
		From:    model.Time(now.UnixMilli()),
		Through: model.Time(end.UnixMilli()),
	}

	span := mocktracer.MockSpan{}
	req.LogToSpan(&span)

	for _, l := range span.Logs() {
		for _, field := range l.Fields {
			if field.Key == "start" {
				require.Equal(t, timestamp.Time(now.UnixMilli()).String(), field.ValueString)
			}
			if field.Key == "end" {
				require.Equal(t, timestamp.Time(end.UnixMilli()).String(), field.ValueString)

			}
		}
	}
}

func TestVolumeRequest(t *testing.T) {
	now := time.Now()
	end := now.Add(1000 * time.Second)
	req := VolumeRequest{
		From:    model.Time(now.UnixMilli()),
		Through: model.Time(end.UnixMilli()),
	}

	span := mocktracer.MockSpan{}
	req.LogToSpan(&span)

	for _, l := range span.Logs() {
		for _, field := range l.Fields {
			if field.Key == "start" {
				require.Equal(t, timestamp.Time(now.UnixMilli()).String(), field.ValueString)
			}
			if field.Key == "end" {
				require.Equal(t, timestamp.Time(end.UnixMilli()).String(), field.ValueString)

			}
		}
	}
}

func benchmarkMergeLabelResponses(b *testing.B, responses []*LabelResponse) {
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		MergeLabelResponses(responses) //nolint:errcheck
	}
}

func benchmarkMergeSeriesResponses(b *testing.B, responses []*SeriesResponse) {
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		MergeSeriesResponses(responses) //nolint:errcheck
	}
}

func BenchmarkMergeALabelResponse(b *testing.B) {
	response := []*LabelResponse{{Values: []string{"test"}}}
	benchmarkMergeLabelResponses(b, response)
}

func BenchmarkMergeASeriesResponse(b *testing.B) {
	response := []*SeriesResponse{{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test", "test")}}}}
	benchmarkMergeSeriesResponses(b, response)
}

func BenchmarkMergeSomeLabelResponses(b *testing.B) {
	responses := []*LabelResponse{
		{Values: []string{"test"}},
		{Values: []string{"test2"}},
		{Values: []string{"test3"}},
	}
	benchmarkMergeLabelResponses(b, responses)
}

func BenchmarkMergeSomeSeriesResponses(b *testing.B) {
	responses := []*SeriesResponse{
		{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test", "test")}}},
		{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test2", "test2")}}},
		{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries("test3", "test3")}}},
	}
	benchmarkMergeSeriesResponses(b, responses)
}

func BenchmarkMergeManyLabelResponses(b *testing.B) {
	responses := []*LabelResponse{}
	for i := 0; i < 20; i++ {
		responses = append(responses, &LabelResponse{Values: []string{fmt.Sprintf("test%d", i)}})
	}
	benchmarkMergeLabelResponses(b, responses)
}

func BenchmarkMergeManySeriesResponses(b *testing.B) {
	responses := []*SeriesResponse{}
	for i := 0; i < 20; i++ {
		test := fmt.Sprintf("test%d", i)
		responses = append(responses, &SeriesResponse{Series: []SeriesIdentifier{{Labels: MustNewSeriesEntries(test, test)}}})
	}
	benchmarkMergeSeriesResponses(b, responses)
}
