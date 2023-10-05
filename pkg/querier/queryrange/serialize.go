package queryrange

import (
	"net/http"

	"github.com/opentracing/opentracing-go"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
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
		// TODO: should be HTTP 400
		serverutil.WriteError(err, w)
	}

	response, err := rt.next.Do(ctx, request)
	if err != nil {
		serverutil.WriteError(err, w)
	}

	params, err := ParamsFromRequest(request)
	if err != nil {
		serverutil.WriteError(err, w)
	}

	// TODO: we must only wrap a few responses. Ideally the serializers would support these instead of the logmodel.Result
	// Yet another thing to simplify.
	switch resp := response.(type) {
	case *LokiResponse, *LokiPromResponse, *TopKSketchesResponse, *QuantileSketchResponse:
		var v logqlmodel.Result
		v, err = ResponseToResult(response)
		if err == nil {
			err = WriteResponse(r, params, v, w)
		}
	case *LokiSeriesResponse:
		series := &logproto.SeriesResponse{Series: resp.Data}
		err = WriteResponse(r, params, series, w)
	default:
		err = WriteResponse(r, params, response, w)
	}

	if err != nil {
		serverutil.WriteError(err, w)
	}
}
