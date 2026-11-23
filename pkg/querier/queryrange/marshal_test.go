package queryrange

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
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
		{"histograms", &HistogramResponse{}, &QueryResponse_Histograms{}},
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
	hintRanges := []logproto.HintTimeRange{
		{Start: start.Add(5 * time.Minute), End: start.Add(6 * time.Minute)},
		{Start: start.Add(30 * time.Minute), End: start.Add(32 * time.Minute), DocumentShard: 17, DocumentShardBits: 5},
	}
	ctx := user.InjectOrgID(context.Background(), "fake")

	for _, tc := range []struct {
		name string
		req  queryrangebase.Request
	}{
		{
			name: "range",
			req: &LokiRequest{
				Query:      `{app="a"} |= "error"`,
				StartTs:    start,
				EndTs:      start.Add(time.Hour),
				HintRanges: hintRanges,
			},
		},
		{
			name: "instant",
			req: &LokiInstantRequest{
				Query:      `sum(count_over_time({app="a"} |= "error" [1h]))`,
				TimeTs:     start.Add(time.Hour),
				HintRanges: hintRanges,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped, err := (Codec{}).QueryRequestWrap(ctx, tc.req)
			require.NoError(t, err)
			data, err := wrapped.Marshal()
			require.NoError(t, err)

			var decoded QueryRequest
			require.NoError(t, decoded.Unmarshal(data))
			unwrapped, _, err := (Codec{}).QueryRequestUnwrap(context.Background(), &decoded)
			require.NoError(t, err)
			require.IsType(t, tc.req, unwrapped)
			require.Equal(t, hintRanges, unwrapped.(interface {
				GetHintRanges() []logproto.HintTimeRange
			}).GetHintRanges())
		})
	}
}

func TestQueryRequestWrapUnwrap_DataObjOptions(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), "fake")

	unwrap := func(t *testing.T, req queryrangebase.Request) queryrangebase.Request {
		t.Helper()
		wrapped, err := (Codec{}).QueryRequestWrap(ctx, req)
		require.NoError(t, err)
		data, err := wrapped.Marshal()
		require.NoError(t, err)

		var decoded QueryRequest
		require.NoError(t, decoded.Unmarshal(data))
		unwrapped, _, err := (Codec{}).QueryRequestUnwrap(t.Context(), &decoded)
		require.NoError(t, err)
		return unwrapped
	}

	t.Run("keeps the options of a range request", func(t *testing.T) {
		require.True(t, unwrap(t, dataObjRangeRequest(true)).(*LokiRequest).DataObjOptions.GetEligible())
	})

	t.Run("keeps the options of an instant request", func(t *testing.T) {
		require.True(t, unwrap(t, dataObjInstantRequest(true)).(*LokiInstantRequest).DataObjOptions.GetEligible())
	})

	t.Run("keeps the options unset for a request that is not eligible", func(t *testing.T) {
		require.Nil(t, unwrap(t, dataObjRangeRequest(false)).(*LokiRequest).DataObjOptions)
		require.Nil(t, unwrap(t, dataObjInstantRequest(false)).(*LokiInstantRequest).DataObjOptions)
	})
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

// testHistogramMatrix is the result of a shard of a histogram query: two steps, one of which has no series.
func testHistogramMatrix() logql.HistogramMatrix {
	h := &histogram.FloatHistogram{
		CounterResetHint: histogram.GaugeType,
		Schema:           3,
		ZeroThreshold:    2.938735877055719e-39,
		Count:            3,
		Sum:              6,
		PositiveSpans:    []histogram.Span{{Offset: 0, Length: 2}},
		PositiveBuckets:  []float64{1, 2},
	}
	return logql.HistogramMatrix{
		{promql.Sample{T: 60_000, Metric: labels.FromStrings("app", "a"), H: h}},
		{},
	}
}

func TestResultToResponse_histogramMatrix(t *testing.T) {
	m := testHistogramMatrix()
	want, err := m.ToProto()
	require.NoError(t, err)

	actual, err := ResultToResponse(logqlmodel.Result{Data: m, Warnings: []string{"w"}}, nil)
	require.NoError(t, err)
	require.Equal(t, &HistogramResponse{Response: want, Warnings: []string{"w"}}, actual)
}

// The result of a shard goes from the querier to the query frontend as protobuf, on the same path as the
// sketches of the other sharded queries, and arrives as the matrix that it was.
func TestHistogramResponse_fromTheQuerierToTheFrontend(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	m := testHistogramMatrix()

	response, err := ResultToResponse(logqlmodel.Result{Data: m}, nil)
	require.NoError(t, err)
	response = response.(*HistogramResponse).WithHeaders([]queryrangebase.PrometheusResponseHeader{{Name: "X-Test", Values: []string{"1"}}})

	requireMatrix := func(t *testing.T, resp queryrangebase.Response) {
		t.Helper()
		result, err := ResponseToResult(resp)
		require.NoError(t, err)
		got, ok := result.Data.(logql.HistogramMatrix)
		require.True(t, ok, "%T", result.Data)
		require.Len(t, got, 2)
		require.Len(t, got[0], 1)
		require.Empty(t, got[1])
		require.EqualValues(t, 60_000, got[0][0].T)
		require.Equal(t, m[0][0].Metric, got[0][0].Metric)
		require.True(t, m[0][0].H.Equals(got[0][0].H))
		require.Equal(t, histogram.GaugeType, got[0][0].H.CounterResetHint)
	}

	t.Run("wrapped as a query response", func(t *testing.T) {
		wrapped, err := QueryResponseWrap(response)
		require.NoError(t, err)
		b, err := wrapped.Marshal()
		require.NoError(t, err)

		var read QueryResponse
		require.NoError(t, read.Unmarshal(b))
		unwrapped, err := QueryResponseUnwrap(&read)
		require.NoError(t, err)
		requireMatrix(t, unwrapped)
	})

	t.Run("encoded and decoded by the codec", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/loki/api/v1/query_range", nil)
		require.NoError(t, err)
		req.Header.Set("Accept", ProtobufType)

		encoded, err := DefaultCodec.EncodeResponse(ctx, req, response)
		require.NoError(t, err)
		require.Equal(t, ProtobufType, encoded.Header.Get("Content-Type"))
		// The headers of the response are the headers of the HTTP response, and not a part of the protobuf.
		encoded.Header.Set("X-Test", "1")

		decoded, err := DefaultCodec.DecodeResponse(ctx, encoded, &LokiRequest{})
		require.NoError(t, err)
		require.IsType(t, &HistogramResponse{}, decoded)
		requireMatrix(t, decoded)

		var headers []string
		for _, h := range decoded.GetHeaders() {
			if h.Name == "X-Test" {
				headers = h.Values
			}
		}
		require.Equal(t, []string{"1"}, headers)
	})
}

// A histogram is a part of a query, and it is not an answer that the API knows how to give.
func TestHistogramResponse_isNotAResponseToAUser(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	response, err := ResultToResponse(logqlmodel.Result{Data: testHistogramMatrix()}, nil)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/loki/api/v1/query_range", nil)
	require.NoError(t, err)
	_, err = DefaultCodec.EncodeResponse(ctx, req, response)
	require.Error(t, err)
}

func TestHistogramResponse_headers(t *testing.T) {
	r := &HistogramResponse{}
	require.Empty(t, r.GetHeaders())

	r.SetHeader("X-Test", "a")
	require.Len(t, r.GetHeaders(), 1)
	require.Equal(t, "X-Test", r.GetHeaders()[0].Name)

	got := r.WithHeaders([]queryrangebase.PrometheusResponseHeader{{Name: "Y", Values: []string{"b"}}})
	require.Same(t, r, got)
	require.Equal(t, "Y", got.GetHeaders()[0].Name)

	var nilResponse *HistogramResponse
	require.Nil(t, nilResponse.GetHeaders())
}

// A matrix that does not decode is an error of the response, and it must not reach the evaluator.
func TestHistogramResponse_refusesWhatIsNotAHistogram(t *testing.T) {
	_, err := ResponseToResult(&HistogramResponse{Response: &logproto.HistogramMatrix{
		Values: []*logproto.HistogramVector{{Samples: []*logproto.HistogramSample{{H: &logproto.NativeHistogram{Schema: 3, Count: 5}}}}},
	}})
	require.ErrorContains(t, err, "cannot decode histogram matrix")

	_, err = ResponseToResult(&HistogramResponse{})
	require.ErrorContains(t, err, "no matrix")
}
