package worker

import (
	"bufio"
	"bytes"
	"net/http"
	"net/http/httptest"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/lokifrontend/frontend/v2/frontendv2pb"
	querier_stats "github.com/grafana/loki/pkg/querier/stats"
	"github.com/weaveworks/common/httpgrpc"
)

type noopCloser struct {
	*bytes.Buffer
}

func (noopCloser) Close() error { return nil }

// BytesBuffer returns the underlaying `bytes.buffer` used to build this io.ReadCloser.
func (n noopCloser) BytesBuffer() *bytes.Buffer { return n.Buffer }

func toHeader(hs []*httpgrpc.Header, header http.Header) {
	for _, h := range hs {
		header[h.Key] = h.Values
	}
}

func fromHeader(hs http.Header) []*httpgrpc.Header {
	result := make([]*httpgrpc.Header, 0, len(hs))
	for k, vs := range hs {
		result = append(result, &httpgrpc.Header{
			Key:    k,
			Values: vs,
		})
	}
	return result
}

type chunkedResponseWriter struct {
	stats     *querier_stats.Stats
	queryID   uint64
	stream    frontendv2pb.FrontendForQuerier_QueryResultChunkedClient
	recorder  *httptest.ResponseRecorder
	sentFirst bool

	logger log.Logger
}

func newReponseWriter(stream frontendv2pb.FrontendForQuerier_QueryResultChunkedClient, stats *querier_stats.Stats, queryID uint64, logger log.Logger) bufferedResponseWriter {
	w := &chunkedResponseWriter{
		stats:     stats,
		queryID:   queryID,
		stream:    stream,
		recorder:  httptest.NewRecorder(),
		sentFirst: false,
		logger:    logger,
	}
	return newBufferedResponseWriter(w)
}

func (r *chunkedResponseWriter) Header() http.Header {
	return r.recorder.Header()
}

func (r *chunkedResponseWriter) Write(buf []byte) (int, error) {
	if !r.sentFirst {
		_, err := r.recorder.Write(buf)
		if err != nil {
			return 0, err
		}

		level.Debug(r.logger).Log("msg", "sending first chunk", "size", r.recorder.Body.Len())
		resp := &httpgrpc.HTTPResponse{
			Code:    int32(r.recorder.Code),
			Headers: fromHeader(r.recorder.Header()),
			Body:    r.recorder.Body.Bytes(),
		}
		msg := &frontendv2pb.QueryResultRequestChunked{
			Type: &frontendv2pb.QueryResultRequestChunked_First{
				First: &frontendv2pb.QueryResultRequest{
					QueryID:      r.queryID,
					HttpResponse: resp,
					Stats:        r.stats,
				},
			},
		}
		err = r.stream.Send(msg)
		if err != nil {
			return 0, err
		}
		r.sentFirst = true
	} else {
		level.Debug(r.logger).Log("msg", "sending continuation chunk", "size", len(buf))
		msg := &frontendv2pb.QueryResultRequestChunked{
			Type: &frontendv2pb.QueryResultRequestChunked_Continuation{
				Continuation: buf,
			},
		}
		r.stream.Send(msg)
	}
	return len(buf), nil
}

func (r *chunkedResponseWriter) WriteHeader(statusCode int) {
	if !r.sentFirst {
		r.recorder.WriteHeader(statusCode)
	}
}

func (r *chunkedResponseWriter) Flush() {
}

//const Mebibyte = 1048576
const bufferSize = 512 // 4 * Mebibyte

type bufferedResponseWriter struct {
	underlyingWriter http.ResponseWriter
	*bufio.Writer
}

func newBufferedResponseWriter(writer *chunkedResponseWriter) bufferedResponseWriter {
	return bufferedResponseWriter{underlyingWriter: writer, Writer: bufio.NewWriterSize(writer, bufferSize)}
}

func (r *bufferedResponseWriter) Header() http.Header {
	return r.underlyingWriter.Header()
}

func (r *bufferedResponseWriter) WriteHeader(statusCode int) {
	r.underlyingWriter.WriteHeader(statusCode)
}
