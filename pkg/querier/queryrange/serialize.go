package queryrange

import (
	"context"
	"io"
	"net/http"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	serverutil "github.com/grafana/loki/v3/pkg/util/server"
)

type serializeRoundTripper struct {
	codec          queryrangebase.Codec
	next           queryrangebase.Handler
	parquetSupport bool
}

func NewSerializeRoundTripper(next queryrangebase.Handler, codec queryrangebase.Codec, parquetSupport bool) http.RoundTripper {
	return &serializeRoundTripper{
		next:           next,
		codec:          codec,
		parquetSupport: parquetSupport,
	}
}

func (rt *serializeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()
	ctx, sp := tracer.Start(ctx, "serializeRoundTripper.do")
	defer sp.End()

	request, err := rt.codec.DecodeRequest(ctx, r, nil)
	if err != nil {
		return nil, err
	}

	response, err := rt.next.Do(ctx, request)
	if err != nil {
		return nil, err
	}

	if r.Header.Get("Accept") == ParquetType && !rt.parquetSupport {
		return nil, serverutil.UserError("support for Parquet encoded responses is disabled. Enable with -frontend.support-parquet-encoding=true")
	}

	return rt.codec.EncodeResponse(ctx, r, response)
}

type serializeHTTPHandler struct {
	codec queryrangebase.Codec
	next  queryrangebase.Handler
}

func NewSerializeHTTPHandler(next queryrangebase.Handler, codec queryrangebase.Codec) http.Handler {
	return &serializeHTTPHandler{
		next:  next,
		codec: codec,
	}
}

func (rt *serializeHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, sp := tracer.Start(ctx, "serializeHTTPHandler.ServerHTTP")
	defer sp.End()

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

	// TODO(karsten): use rt.codec.EncodeResponse(ctx, r, response) which is the central encoding logic instead.
	tracked := &trackedWriter{w: w}
	if r.Header.Get("Accept") == ParquetType {
		w.Header().Set("Content-Type", ParquetType)
		writeEncodeError(ctx, w, tracked, encodeResponseParquetTo(ctx, response, tracked))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	version := loghttp.GetVersion(r.RequestURI)
	encodingFlags := httpreq.ExtractEncodingFlags(r)
	writeEncodeError(ctx, w, tracked, encodeResponseJSONTo(version, response, tracked, encodingFlags))
}

// writeEncodeError reports an encoding failure to the client, but only while the
// encoder has not written anything yet. Once the first byte is out, the status
// code and the Content-Type of the successful response are already committed,
// so appending a plain-text error would produce a truncated body that
// contradicts both headers. In that case the error is only logged.
func writeEncodeError(ctx context.Context, w http.ResponseWriter, tracked *trackedWriter, err error) {
	if err == nil {
		return
	}
	if !tracked.written {
		serverutil.WriteError(err, w)
		return
	}
	level.Error(util_log.WithContext(ctx, util_log.Logger)).Log(
		"msg", "failed encoding response after the first byte was written, response body is truncated",
		"err", err,
	)
}

// trackedWriter records whether any byte reached the underlying writer, which
// tells the caller whether the response headers are already committed.
type trackedWriter struct {
	w       io.Writer
	written bool
}

func (t *trackedWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.written = true
	}
	return n, err
}
