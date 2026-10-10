package queryrange

import (
	"io"
	"net/http"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
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
	stripClientHintRanges(request)

	response, err := rt.next.Do(ctx, request)
	if err != nil {
		return nil, err
	}

	if r.Header.Get("Accept") == ParquetType && !rt.parquetSupport {
		return nil, serverutil.UserError("support for Parquet encoded responses is disabled. Enable with -frontend.support-parquet-encoding=true")
	}

	return rt.codec.EncodeResponse(ctx, r, response)
}

// stripClientHintRanges removes hint ranges decoded from an HTTP request.
// Query-frontend and standalone-querier HTTP entrypoints both call this.
// The scheduler path keeps ranges that internal middleware attached after decode.
func stripClientHintRanges(request queryrangebase.Request) {
	if request, ok := request.(*LokiRequest); ok {
		request.HintRanges = nil
	}
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
	stripClientHintRanges(request)

	response, err := rt.next.Do(ctx, request)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	resp, err := rt.codec.EncodeResponse(ctx, r, response)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	_, err = writeResponse(w, resp)
	if err != nil {
		sp.RecordError(err)
	}
}

func writeResponse(rw http.ResponseWriter, resp *http.Response) (int64, error) {
	defer resp.Body.Close()
	for k, v := range resp.Header {
		rw.Header()[k] = v
	}
	rw.WriteHeader(resp.StatusCode)
	return io.Copy(rw, resp.Body)
}
