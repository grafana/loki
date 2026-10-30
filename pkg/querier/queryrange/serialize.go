package queryrange

import (
	"net/http"

	"github.com/grafana/loki/v4/pkg/loghttp"
	"github.com/grafana/loki/v4/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v4/pkg/util/httpreq"
	serverutil "github.com/grafana/loki/v4/pkg/util/server"
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

	// TODO(karsten): use rt.codec.EncodeResponse(ctx, r, response) which is the central encoding logic instead.
	if r.Header.Get("Accept") == ParquetType {
		w.Header().Add("Content-Type", ParquetType)
		if err := encodeResponseParquetTo(ctx, response, w); err != nil {
			serverutil.WriteError(err, w)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	version := loghttp.GetVersion(r.RequestURI)
	encodingFlags := httpreq.ExtractEncodingFlags(r)
	if err := encodeResponseJSONTo(version, response, w, encodingFlags); err != nil {
		serverutil.WriteError(err, w)
	}
}
