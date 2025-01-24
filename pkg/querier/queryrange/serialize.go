package queryrange

import (
	"net/http"

	"github.com/opentracing/opentracing-go"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
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
	sp, ctx := opentracing.StartSpanFromContext(ctx, "serializeRoundTripper.do")
	defer sp.Finish()

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
	sp, ctx := opentracing.StartSpanFromContext(ctx, "serializeHTTPHandler.ServerHTTP")
	defer sp.Finish()

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
	if r.Header.Get("Accept") == ParquetType {
		w.Header().Add("Content-Type", ParquetType)
		if err := encodeResponseParquetTo(ctx, response, w); err != nil {
			serverutil.WriteError(err, w)
		}
		return
	}
	version := loghttp.GetVersion(r.RequestURI)
	encodingFlags := httpreq.ExtractEncodingFlags(r)
	if err := encodeResponseJSONTo(version, response, w, encodingFlags); err != nil {
		serverutil.WriteError(err, w)
	}
}
