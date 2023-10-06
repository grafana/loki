package querier

import (
	"context"
	"fmt"
	"net/http"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
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
		request := &logproto.SeriesRequest{}
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
	case *queryrange.LokiLabelNamesRequest:
		// TODO: LokiLabelNamesRequest should probably be logproto.LabelRequest
		request := &logproto.LabelRequest{
			Start:  &concrete.StartTs,
			End:    &concrete.EndTs,
			Query:  concrete.Query,
			Values: concrete.Name != "",
			Name:   concrete.Name,
		}
		res, err := h.api.LabelHandler(ctx, request)
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
	default:
		// TODO: This should be a user error
		return nil, fmt.Errorf("unsupported query type %T", req)
	}
}

func NewQuerierHTTPHandler(h *Handler) http.Handler {
	return queryrange.NewSerializeHTTPHandler(h, queryrange.DefaultCodec)
}
