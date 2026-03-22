package queryrangebase

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestResponse(t *testing.T) {
	r := *parsedResponse
	r.Headers = respHeaders
	for i, tc := range []struct {
		body     string
		expected *PrometheusResponse
	}{
		{
			body:     responseBody,
			expected: &r,
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			response := &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewBuffer([]byte(tc.body))),
			}
			resp, err := PrometheusCodecForRangeQueries.DecodeResponse(context.Background(), response, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, resp)

			// Reset response, as the above call will have consumed the body reader.
			response = &http.Response{
				StatusCode:    200,
				Header:        http.Header{"Content-Type": []string{"application/json"}},
				Body:          io.NopCloser(bytes.NewBuffer([]byte(tc.body))),
				ContentLength: int64(len(tc.body)),
			}
			resp2, err := PrometheusCodecForRangeQueries.EncodeResponse(context.Background(), nil, resp)
			require.NoError(t, err)
			assert.Equal(t, response, resp2)
		})
	}
}

func TestMergeAPIResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    []Response
		expected Response
	}{
		{
			name:  "No responses shouldn't panic and return a non-null result and result type.",
			input: []Response{},
			expected: &PrometheusResponse{
				Status: StatusSuccess,
				Data: PrometheusData{
					ResultType: matrix,
					Result:     []SampleStream{},
				},
			},
		},

		{
			name: "A single empty response shouldn't panic.",
			input: []Response{
				&PrometheusResponse{
					Data: PrometheusData{
						ResultType: matrix,
						Result:     []SampleStream{},
					},
				},
			},
			expected: &PrometheusResponse{
				Status: StatusSuccess,
				Data: PrometheusData{
					ResultType: matrix,
					Result:     []SampleStream{},
				},
			},
		},

		{
			name: "Multiple empty responses shouldn't panic.",
			input: []Response{
				&PrometheusResponse{
					Data: PrometheusData{
						ResultType: matrix,
						Result:     []SampleStream{},
					},
				},
				&PrometheusResponse{
					Data: PrometheusData{
						ResultType: matrix,
						Result:     []SampleStream{},
					},
				},
			},
			expected: &PrometheusResponse{
				Status: StatusSuccess,
				Data: PrometheusData{
					ResultType: matrix,
					Result:     []SampleStream{},
				},
			},
		},

		{
			name: "Basic merging of two responses.",
			input: []Response{
				&PrometheusResponse{
					Warnings: []string{"warning1", "warning2"},
					Data: PrometheusData{
						ResultType: matrix,
						Result: []SampleStream{
							{
								Labels: []logproto.LabelAdapter{},
								Samples: []logproto.LegacySample{
									{Value: 0, TimestampMs: 0},
									{Value: 1, TimestampMs: 1},
								},
							},
						},
					},
				},
				&PrometheusResponse{
					Warnings: []string{"warning2", "warning3"},
					Data: PrometheusData{
						ResultType: matrix,
						Result: []SampleStream{
							{
								Labels: []logproto.LabelAdapter{},
								Samples: []logproto.LegacySample{
									{Value: 2, TimestampMs: 2},
									{Value: 3, TimestampMs: 3},
								},
							},
						},
					},
				},
			},
			expected: &PrometheusResponse{
				Status:   StatusSuccess,
				Warnings: []string{"warning1", "warning2", "warning3"},
				Data: PrometheusData{
					ResultType: matrix,
					Result: []SampleStream{
						{
							Labels: []logproto.LabelAdapter{},
							Samples: []logproto.LegacySample{
								{Value: 0, TimestampMs: 0},
								{Value: 1, TimestampMs: 1},
								{Value: 2, TimestampMs: 2},
								{Value: 3, TimestampMs: 3},
							},
						},
					},
				},
			},
		},

		{
			name: "Merging of responses when labels are in different order.",
			input: []Response{
				mustParse(t, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"a":"b","c":"d"},"values":[[0,"0"],[1,"1"]]}]}}`),
				mustParse(t, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"c":"d","a":"b"},"values":[[2,"2"],[3,"3"]]}]}}`),
			},
			expected: &PrometheusResponse{
				Status: StatusSuccess,
				Data: PrometheusData{
					ResultType: matrix,
					Result: []SampleStream{
						{
							Labels: []logproto.LabelAdapter{{Name: "a", Value: "b"}, {Name: "c", Value: "d"}},
							Samples: []logproto.LegacySample{
								{Value: 0, TimestampMs: 0},
								{Value: 1, TimestampMs: 1000},
								{Value: 2, TimestampMs: 2000},
								{Value: 3, TimestampMs: 3000},
							},
						},
					},
				},
			},
		},

		{
			name: "Merging of samples where there is single overlap.",
			input: []Response{
				mustParse(t, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"a":"b","c":"d"},"values":[[1,"1"],[2,"2"]]}]}}`),
				mustParse(t, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"c":"d","a":"b"},"values":[[2,"2"],[3,"3"]]}]}}`),
			},
			expected: &PrometheusResponse{
				Status: StatusSuccess,
				Data: PrometheusData{
					ResultType: matrix,
					Result: []SampleStream{
						{
							Labels: []logproto.LabelAdapter{{Name: "a", Value: "b"}, {Name: "c", Value: "d"}},
							Samples: []logproto.LegacySample{
								{Value: 1, TimestampMs: 1000},
								{Value: 2, TimestampMs: 2000},
								{Value: 3, TimestampMs: 3000},
							},
						},
					},
				},
			},
		},
		{
			name: "Merging of samples where there is multiple partial overlaps.",
			input: []Response{
				mustParse(t, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"a":"b","c":"d"},"values":[[1,"1"],[2,"2"],[3,"3"]]}]}}`),
				mustParse(t, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"c":"d","a":"b"},"values":[[2,"2"],[3,"3"],[4,"4"],[5,"5"]]}]}}`),
			},
			expected: &PrometheusResponse{
				Status: StatusSuccess,
				Data: PrometheusData{
					ResultType: matrix,
					Result: []SampleStream{
						{
							Labels: []logproto.LabelAdapter{{Name: "a", Value: "b"}, {Name: "c", Value: "d"}},
							Samples: []logproto.LegacySample{
								{Value: 1, TimestampMs: 1000},
								{Value: 2, TimestampMs: 2000},
								{Value: 3, TimestampMs: 3000},
								{Value: 4, TimestampMs: 4000},
								{Value: 5, TimestampMs: 5000},
							},
						},
					},
				},
			},
		},
		{
			name: "Merging of samples where there is complete overlap.",
			input: []Response{
				mustParse(t, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"a":"b","c":"d"},"values":[[2,"2"],[3,"3"]]}]}}`),
				mustParse(t, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"c":"d","a":"b"},"values":[[2,"2"],[3,"3"],[4,"4"],[5,"5"]]}]}}`),
			},
			expected: &PrometheusResponse{
				Status: StatusSuccess,
				Data: PrometheusData{
					ResultType: matrix,
					Result: []SampleStream{
						{
							Labels: []logproto.LabelAdapter{{Name: "a", Value: "b"}, {Name: "c", Value: "d"}},
							Samples: []logproto.LegacySample{
								{Value: 2, TimestampMs: 2000},
								{Value: 3, TimestampMs: 3000},
								{Value: 4, TimestampMs: 4000},
								{Value: 5, TimestampMs: 5000},
							},
						},
					},
				},
			},
		}} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := PrometheusCodecForRangeQueries.MergeResponse(tc.input...)
			require.NoError(t, err)
			require.Equal(t, tc.expected, output)
		})
	}
}

func TestPrometheusRequestSpanLogging(t *testing.T) {
	now := time.Now()
	end := now.Add(1000 * time.Second)
	req := PrometheusRequest{
		Start: now,
		End:   end,
	}

	exporter := tracetest.NewInMemoryExporter()
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSpanProcessor(tracesdk.NewSimpleSpanProcessor(exporter)),
	)
	_, sp := tp.Tracer("test").Start(context.Background(), "request")
	req.LogToSpan(sp)
	sp.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0]
	found := 0
	for _, l := range span.Attributes {
		if l.Key == "start" {
			require.Equal(t, attribute.StringValue(timestamp.Time(now.UnixMilli()).String()), l.Value)
			found++
		}
		if l.Key == "end" {
			require.Equal(t, attribute.StringValue(timestamp.Time(end.UnixMilli()).String()), l.Value)
			found++
		}
	}
	require.Equal(t, 2, found, "expected to find start and end attributes in span")
}

func mustParse(t *testing.T, response string) Response {
	var resp PrometheusResponse
	// Needed as goimports automatically add a json import otherwise.
	json := jsoniter.ConfigCompatibleWithStandardLibrary
	require.NoError(t, json.Unmarshal([]byte(response), &resp))
	return &resp
}
