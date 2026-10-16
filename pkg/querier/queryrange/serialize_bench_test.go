package queryrange

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	serverutil "github.com/grafana/loki/v3/pkg/util/server"
)

// directSerializeHTTPHandler is the implementation of serializeHTTPHandler
// before the response was encoded through codec.EncodeResponse. It encodes into
// the http.ResponseWriter instead of into an intermediate bytes.Buffer.
//
// Note that this is not a streaming encoder for log responses: the jsoniter
// stream accumulates the whole body in its own (pooled) buffer and writes it to
// the destination writer in a single Flush at the end. The difference measured
// here is therefore the extra copy into a per-request bytes.Buffer, not
// streaming versus buffering.
type directSerializeHTTPHandler struct {
	codec queryrangebase.Codec
	next  queryrangebase.Handler
}

func (rt *directSerializeHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	request, err := rt.codec.DecodeRequest(ctx, r, nil)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	response, err := rt.next.Do(ctx, request)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	version := loghttp.GetVersion(r.RequestURI)
	encodingFlags := httpreq.ExtractEncodingFlags(r)
	if err := encodeResponseJSONTo(version, response, w, encodingFlags); err != nil {
		serverutil.WriteError(err, w)
	}
}

// countingResponseWriter discards the body and only records its size, so that
// the benchmark measures encoding and buffering instead of the cost of keeping
// the result in memory (as httptest.ResponseRecorder would).
type countingResponseWriter struct {
	header http.Header
	n      int
	code   int
}

func (w *countingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}

func (w *countingResponseWriter) WriteHeader(code int) { w.code = code }

// genLokiStreamsResponse builds a log query response with the given number of
// streams and entries per stream. Cardinality is expressed through the number
// of streams: few streams with many entries each is the low cardinality case,
// many streams with few entries each the high cardinality case.
func genLokiStreamsResponse(streams, entriesPerStream int) *LokiResponse {
	result := make(logqlmodel.Streams, 0, streams)
	ts := time.Unix(0, 0)

	for i := range streams {
		entries := make([]logproto.Entry, 0, entriesPerStream)
		for j := range entriesPerStream {
			entries = append(entries, logproto.Entry{
				Timestamp: ts.Add(time.Duration(j) * time.Millisecond),
				Line:      fmt.Sprintf(`level=info ts=%d caller=bench.go:%d msg="request completed" duration=1.2ms status=200`, j, i%400),
			})
		}
		result = append(result, logproto.Stream{
			Labels: fmt.Sprintf(
				`{cluster="prod-us-central-0", namespace="loki-ops", container="querier", pod="querier-%d-abcdef", instance="10.128.%d.%d:3100"}`,
				i, i/256, i%256,
			),
			Entries: entries,
		})
	}

	return &LokiResponse{
		Status:    loghttp.QueryStatusSuccess,
		Direction: logproto.BACKWARD,
		Limit:     uint32(streams * entriesPerStream),
		Version:   uint32(loghttp.VersionV1),
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result:     result,
		},
		Statistics: statsResult,
	}
}

// BenchmarkSerializeHTTPHandler compares encoding into the http.ResponseWriter
// with encoding into an intermediate buffer, for responses of equal size but
// different label cardinality.
func BenchmarkSerializeHTTPHandler(b *testing.B) {
	for _, tc := range []struct {
		name             string
		streams          int
		entriesPerStream int
	}{
		{name: "low_cardinality", streams: 10, entriesPerStream: 10000},
		{name: "medium_cardinality", streams: 1000, entriesPerStream: 100},
		{name: "high_cardinality", streams: 10000, entriesPerStream: 10},
	} {
		response := genLokiStreamsResponse(tc.streams, tc.entriesPerStream)
		next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
			return response, nil
		})

		handlers := map[string]http.Handler{
			"direct":   &directSerializeHTTPHandler{next: next, codec: DefaultCodec},
			"buffered": NewSerializeHTTPHandler(next, DefaultCodec),
		}

		for _, impl := range []string{"direct", "buffered"} {
			handler := handlers[impl]

			b.Run(fmt.Sprintf("%s/%s", tc.name, impl), func(b *testing.B) {
				req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query_range?start=0&end=1&query=%7Bfoo%3D%22bar%22%7D", nil)
				req = req.WithContext(user.InjectOrgID(b.Context(), "fake"))

				// Determine the encoded size once to normalize results to MB/s.
				probe := &countingResponseWriter{}
				handler.ServeHTTP(probe, req)
				b.SetBytes(int64(probe.n))

				b.ReportAllocs()
				b.ResetTimer()

				for range b.N {
					w := &countingResponseWriter{}
					handler.ServeHTTP(w, req)
				}
			})
		}
	}
}

// BenchmarkEncodeResponseJSON isolates the encoding step from the HTTP handler:
// writing into io.Discard versus filling a per-request bytes.Buffer first.
func BenchmarkEncodeResponseJSON(b *testing.B) {
	for _, tc := range []struct {
		name             string
		streams          int
		entriesPerStream int
	}{
		{name: "low_cardinality", streams: 10, entriesPerStream: 10000},
		{name: "medium_cardinality", streams: 1000, entriesPerStream: 100},
		{name: "high_cardinality", streams: 10000, entriesPerStream: 10},
	} {
		response := genLokiStreamsResponse(tc.streams, tc.entriesPerStream)

		b.Run(tc.name+"/direct", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if err := encodeResponseJSONTo(loghttp.VersionV1, response, io.Discard, nil); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(tc.name+"/buffered", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				res, err := encodeResponseJSON(b.Context(), loghttp.VersionV1, response, nil)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := io.Copy(io.Discard, res.Body); err != nil {
					b.Fatal(err)
				}
				_ = res.Body.Close()
			}
		})
	}
}
