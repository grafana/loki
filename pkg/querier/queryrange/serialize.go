package queryrange

import (
	"net/http"

	"github.com/opentracing/opentracing-go"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	serverutil "github.com/grafana/loki/pkg/util/server"
)

type serializeRoundTripper struct {
	codec queryrangebase.Codec
	next  queryrangebase.Handler
}

func NewSerializeRoundTripper(next queryrangebase.Handler, codec queryrangebase.Codec) http.RoundTripper {
	return &serializeRoundTripper{
		next:  next,
		codec: codec,
	}
}

func (rt *serializeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()
	sp, ctx := opentracing.StartSpanFromContext(ctx, "limitedRoundTripper.do")
	defer sp.Finish()

	request, err := rt.codec.DecodeRequest(ctx, r, nil)
	if err != nil {
		return nil, err
	}

	response, err := rt.next.Do(ctx, request)
	if err != nil {
		return nil, err
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

	version := loghttp.GetVersion(r.RequestURI)
	if err := encodeResponseJSONTo(ctx, version, response, w); err != nil {
		serverutil.WriteError(err, w)
	}
}
