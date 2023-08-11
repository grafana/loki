package queryrange

import (
	"context"
	"encoding/base64"
	"net/http"

	"github.com/grafana/loki/pkg/querier/queryrange/singleflight"

	"github.com/go-kit/log"
	"github.com/golang/groupcache"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

func SingleFlightHandler(name string, gc *singleflight.SingleFlight, log log.Logger, next http.RoundTripper, codec queryrangebase.Codec) queryrangebase.Handler {
	singleFlight := gc.NewGroup(name, makeRequest(next, codec, log))

	return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		r, err := codec.EncodeRequest(ctx, req)
		if err != nil {
			return nil, err
		}

		resp, err := makeResponse(req)
		if err != nil {
			return nil, nil
		}

		key := base64.StdEncoding.EncodeToString([]byte(r.RequestURI))
		if err := singleFlight.Fetch(ctx, key, groupcache.ProtoSink(resp)); err != nil {
			return nil, err
		}

		return resp, err
	})
}

func makeRequest(next http.RoundTripper, codec queryrangebase.Codec, log log.Logger) groupcache.GetterFunc {
	return func(ctx context.Context, encKey string, dest groupcache.Sink) error {
		key, err := decodeKey(encKey)
		if err != nil {
			return err
		}

		r, err := http.NewRequestWithContext(ctx, http.MethodGet, key, nil)
		if err != nil {
			return err
		}
		r.RequestURI = key // If this isn't copied, logs response decoding breaks

		resp, err := next.RoundTrip(r)
		if err != nil {
			return err
		}

		// Decode the request so it can be used to convert the response
		request, err := codec.DecodeRequest(ctx, r, nil)
		if err != nil {
			return err
		}

		lokiResp, err := codec.DecodeResponse(ctx, resp, request)
		if err != nil {
			return err
		}

		return dest.SetProto(lokiResp)
	}
}

func decodeKey(s string) (string, error) {
	k, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(k), nil
}

func makeResponse(req queryrangebase.Request) (queryrangebase.Response, error) {
	switch req := req.(type) {
	case *LokiSeriesRequest:
		return &LokiSeriesResponse{}, nil
	case *LokiLabelNamesRequest:
		return &LokiLabelNamesResponse{}, nil
	case *logproto.IndexStatsRequest:
		return &IndexStatsResponse{}, nil
	case *logproto.VolumeRequest:
		return &VolumeResponse{}, nil
	case *LokiRequest, *LokiInstantRequest:
		_, err := syntax.ParseSampleExpr(req.GetQuery())
		if err != nil {
			if errors.Is(err, syntax.ErrNotASampleExpr) {
				return &LokiResponse{}, nil
			}
			return nil, err
		}

		return &LokiPromResponse{}, nil
	default:
		return nil, errors.New("something bad")
	}
}
