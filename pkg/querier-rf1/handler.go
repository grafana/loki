package querierrf1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

type Handler struct {
	api *QuerierAPI
}

func NewQuerierHandler(api *QuerierAPI) *Handler {
	return &Handler{
		api: api,
	}
}

func (h *Handler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {

	switch concrete := req.(type) {
	case *queryrange.LokiRequest:
		res, err := h.api.RangeQueryHandler(ctx, concrete)
		if err != nil {
			return nil, err
		}

		params, err := queryrange.ParamsFromRequest(req)
		if err != nil {
			return nil, err
		}

		return queryrange.ResultToResponse(res, params)
	case *queryrange.LokiInstantRequest:
		res, err := h.api.InstantQueryHandler(ctx, concrete)
		if err != nil {
			return nil, err
		}

		params, err := queryrange.ParamsFromRequest(req)
		if err != nil {
			return nil, err
		}

		return queryrange.ResultToResponse(res, params)
	case *queryrange.LokiSeriesRequest:
		request := &logproto.SeriesRequest{
			Start:  concrete.StartTs,
			End:    concrete.EndTs,
			Groups: concrete.Match,
			Shards: concrete.Shards,
		}
		result, statResult, err := h.api.SeriesHandler(ctx, request)
		if err != nil {
			return nil, err
		}

		return &queryrange.LokiSeriesResponse{
			Status:     "success",
			Version:    uint32(loghttp.VersionV1),
			Data:       result.Series,
			Statistics: statResult,
		}, nil
	case *queryrange.LabelRequest:
		res, err := h.api.LabelHandler(ctx, &concrete.LabelRequest)
		if err != nil {
			return nil, err
		}

		return &queryrange.LokiLabelNamesResponse{
			Status:  "success",
			Version: uint32(loghttp.VersionV1),
			Data:    res.Values,
		}, nil
	case *logproto.IndexStatsRequest:
		request := loghttp.NewRangeQueryWithDefaults()
		request.Start = concrete.From.Time()
		request.End = concrete.Through.Time()
		request.Query = concrete.GetQuery()
		request.UpdateStep()

		result, err := h.api.IndexStatsHandler(ctx, request)
		if err != nil {
			return nil, err
		}
		return &queryrange.IndexStatsResponse{Response: result}, nil
	case *logproto.ShardsRequest:
		request := loghttp.NewRangeQueryWithDefaults()
		request.Start = concrete.From.Time()
		request.End = concrete.Through.Time()
		request.Query = concrete.GetQuery()
		request.UpdateStep()
		result, err := h.api.IndexShardsHandler(ctx, request, concrete.TargetBytesPerShard)
		if err != nil {
			return nil, err
		}
		return &queryrange.ShardsResponse{Response: result}, nil

	case *logproto.VolumeRequest:
		result, err := h.api.VolumeHandler(ctx, concrete)
		if err != nil {
			return nil, err
		}
		return &queryrange.VolumeResponse{Response: result}, nil
	case *queryrange.DetectedFieldsRequest:
		result, err := h.api.DetectedFieldsHandler(ctx, &concrete.DetectedFieldsRequest)
		if err != nil {
			return nil, err
		}

		return &queryrange.DetectedFieldsResponse{
			Response: result,
		}, nil
	case *logproto.QueryPatternsRequest:
		result, err := h.api.PatternsHandler(ctx, concrete)
		if err != nil {
			return nil, err
		}
		return &queryrange.QueryPatternsResponse{
			Response: result,
		}, nil
	case *queryrange.DetectedLabelsRequest:
		result, err := h.api.DetectedLabelsHandler(ctx, &concrete.DetectedLabelsRequest)
		if err != nil {
			return nil, err
		}

		return &queryrange.DetectedLabelsResponse{Response: result}, nil
	default:
		return nil, fmt.Errorf("unsupported query type %T", req)
	}
}

func NewQuerierHTTPHandler(h *Handler) http.Handler {
	return queryrange.NewSerializeHTTPHandler(h, queryrange.DefaultCodec)
}
