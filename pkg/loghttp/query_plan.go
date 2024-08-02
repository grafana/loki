package loghttp

import (
	"net/http"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func ParseQueryPlanRequest(r *http.Request) (*logproto.QueryPlanRequest, error) {
	req := &logproto.QueryPlanRequest{}

	req.Query = query(r)
	start, end, err := bounds(r)
	if err != nil {
		return nil, err
	}

	req.Start = start
	req.End = end
	dir, err := direction(r)
	if err != nil {
		return nil, err
	}
	req.Direction = dir
	return req, nil
}
