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
	stripClientHints(request)

	response, err := rt.next.Do(ctx, request)
	if err != nil {
		return nil, err
	}

	if r.Header.Get("Accept") == ParquetType && !rt.parquetSupport {
		return nil, serverutil.UserError("support for Parquet encoded responses is disabled. Enable with -frontend.support-parquet-encoding=true")
	}

	return rt.codec.EncodeResponse(ctx, r, response)
}

// stripClientHints clears the hint ranges and the data-object options of a request decoded from
// HTTP.
//
// Only Loki components may set these fields. A client that sets them could change what a query
// reads, and Loki must stay in control of that:
//   - Hint ranges limit the time ranges for which the querier returns samples.
//   - Data-object options mark a request as eligible for reads from data objects. Only the
//     query-frontend decides this for each request.
//
// The query-frontend and the standalone querier call this at their HTTP entrypoints. The scheduler
// path does not call it, because it carries the values that internal middleware attached after
// decode.
func stripClientHints(request queryrangebase.Request) {
	switch request := request.(type) {
	case *LokiRequest:
		request.HintRanges = nil
		request.DataObjOptions = nil
	case *LokiInstantRequest:
		request.DataObjOptions = nil
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
	stripClientHints(request)

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
