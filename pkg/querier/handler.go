package querier

import (
	"context"
	"fmt"
	"net/http"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

type QuerierHandler struct {
	api *QuerierAPI
}

func NewQuerierHandler(api *QuerierAPI) *QuerierHandler {
	return &QuerierHandler{
		api: api,
	}
}

func (h *QuerierHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	switch concrete := req.(type) {
	case *queryrange.LokiRequest:
		res, err := h.api.RangeQueryHandler(ctx, concrete)
		if err != nil {
			return nil, err
		}

		params, err := queryrange.ParamsFromRequest(req)
		if err != nil {
		}

		return queryrange.ValueToResponse(res.Data, params)
	case *queryrange.LokiInstantRequest:
		res, err := h.api.InstantQueryHandler(ctx, concrete)
		if err != nil {
			return nil, err
		}

		params, err := queryrange.ParamsFromRequest(req)
		if err != nil {
		}

		return queryrange.ValueToResponse(res.Data, params)
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
			Status: "success",
			Data:   res.Values,
		}, nil
	default:
		// TODO: This should be a user error
		return nil, fmt.Errorf("unsupported query type %T", req)
	}
}

func NewQuerierHTTPHandler(api *QuerierAPI) http.Handler {
	return queryrange.NewSerializeHTTPHandler(NewQuerierHandler(api), queryrange.DefaultCodec)
}
