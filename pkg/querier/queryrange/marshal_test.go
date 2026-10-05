package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func TestResultToResponse(t *testing.T) {
	tests := []struct {
		name     string
		result   logqlmodel.Result
		response queryrangebase.Response
	}{
		{
			name: "nil matrix",
			result: logqlmodel.Result{
				Data: promql.Matrix(nil),
			},
			response: &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: "success",
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeMatrix,
						Result:     []queryrangebase.SampleStream{},
					},
				},
			},
		},
		{
			name: "empty probabilistic quantile matrix",
			result: logqlmodel.Result{
				Data: logql.ProbabilisticQuantileMatrix([]logql.ProbabilisticQuantileVector{}),
			},
			response: &QuantileSketchResponse{
				Response: &logproto.QuantileSketchMatrix{
					Values: []*logproto.QuantileSketchVector{},
				},
				Headers: []queryrangebase.PrometheusResponseHeader(nil),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ResultToResponse(tt.result, nil)
			require.NoError(t, err)

			require.Equal(t, tt.response, actual)
		})
	}
}

func TestResponseWrap(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response queryrangebase.Response
		expected isQueryResponse_Response
	}{
		{"volume", &VolumeResponse{}, &QueryResponse_Volume{}},
		{"series", &LokiSeriesResponse{}, &QueryResponse_Series{}},
		{"label", &LokiLabelNamesResponse{}, &QueryResponse_Labels{}},
		{"stats", &IndexStatsResponse{}, &QueryResponse_Stats{}},
		{"prom", &LokiPromResponse{}, &QueryResponse_Prom{}},
		{"streams", &LokiResponse{}, &QueryResponse_Streams{}},
		{"topk", &TopKSketchesResponse{}, &QueryResponse_TopkSketches{}},
		{"quantile", &QuantileSketchResponse{}, &QueryResponse_QuantileSketches{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := QueryResponseWrap(tt.response)
			require.NoError(t, err)
			require.IsType(t, tt.expected, actual.Response)
		})
	}
}

// TestQueryRequestWrapUnwrap_EncodingFlags pins the propagation of the
// X-Loki-Response-Encoding-Flags header across the protobuf hop between the
// query-frontend and the querier. Without it, `categorize-labels` never reaches
// the querier's engine and the flag is a silent no-op on the default
// (frontend.encoding: protobuf) path.
func TestQueryRequestWrapUnwrap_EncodingFlags(t *testing.T) {
	codec := Codec{}
	req := &LokiRequest{Query: `{app="a"}`}
	// QueryRequestWrap requires an org ID on the context.
	baseCtx := user.InjectOrgID(context.Background(), "fake")

	t.Run("propagates flags set on the frontend ctx", func(t *testing.T) {
		ctx := httpreq.AddEncodingFlagsToContext(baseCtx,
			httpreq.NewEncodingFlags(httpreq.FlagCategorizeLabels))

		wrapped, err := codec.QueryRequestWrap(ctx, req)
		require.NoError(t, err)
		require.Equal(t, string(httpreq.FlagCategorizeLabels), wrapped.Metadata[httpreq.LokiEncodingFlagsHeader])

		_, unwrappedCtx, err := codec.QueryRequestUnwrap(context.Background(), wrapped)
		require.NoError(t, err)

		flags := httpreq.ExtractEncodingFlagsFromCtx(unwrappedCtx)
		require.True(t, flags.Has(httpreq.FlagCategorizeLabels),
			"categorize-labels must reach the querier ctx, otherwise the engine never categorizes")
	})

	t.Run("no flags means no metadata entry", func(t *testing.T) {
		wrapped, err := codec.QueryRequestWrap(baseCtx, req)
		require.NoError(t, err)
		require.NotContains(t, wrapped.Metadata, httpreq.LokiEncodingFlagsHeader)

		_, unwrappedCtx, err := codec.QueryRequestUnwrap(context.Background(), wrapped)
		require.NoError(t, err)
		require.Nil(t, httpreq.ExtractEncodingFlagsFromCtx(unwrappedCtx))
	})
}

func TestQueryRequestWrapUnwrap_HintRanges(t *testing.T) {
	start := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	req := &LokiRequest{
		Query:   `{app="a"} |= "error"`,
		StartTs: start,
		EndTs:   start.Add(time.Hour),
		HintRanges: []logproto.HintTimeRange{
			{Start: start.Add(5 * time.Minute), End: start.Add(6 * time.Minute)},
			{Start: start.Add(30 * time.Minute), End: start.Add(32 * time.Minute)},
		},
	}
	ctx := user.InjectOrgID(context.Background(), "fake")

	wrapped, err := (Codec{}).QueryRequestWrap(ctx, req)
	require.NoError(t, err)
	data, err := wrapped.Marshal()
	require.NoError(t, err)

	var decoded QueryRequest
	require.NoError(t, decoded.Unmarshal(data))
	unwrapped, _, err := (Codec{}).QueryRequestUnwrap(context.Background(), &decoded)
	require.NoError(t, err)
	require.Equal(t, req.HintRanges, unwrapped.(*LokiRequest).HintRanges)
}

// Benchmark_UnwrapSeries is the sibling Benchmark_CodecDecodeSeries.
func Benchmark_UnwrapSeries(b *testing.B) {
	// Setup
	original := &LokiSeriesResponse{
		Status:     "200",
		Version:    1,
		Statistics: stats.Result{},
		Data:       generateSeries(),
	}

	wrappedResponse, err := QueryResponseWrap(original)
	require.NoError(b, err)

	body, err := wrappedResponse.Marshal()
	require.NoError(b, err)

	// Actual run
	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		resp := &QueryResponse{}
		err := resp.Unmarshal(body)
		require.NoError(b, err)

		actual, err := QueryResponseUnwrap(resp)
		require.NoError(b, err)
		require.NotNil(b, actual)
	}

}
