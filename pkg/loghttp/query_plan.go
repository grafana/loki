package loghttp

import (
	"net/http"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/pkg/errors"
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
	b, err := buckets(r)
	if err != nil {
		return nil, err
	}
	req.Buckets = b
	return req, nil
}

func buckets(r *http.Request) (uint32, error) {
	b, err := parseInt(r.Form.Get("buckets"), 1)
	if err != nil {
		return 0, err
	}
	if b <= 0 {
		return 0, errors.New("buckets must be a positive value")
	}
	return uint32(b), nil
}
