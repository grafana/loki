package loghttp

import (
	"net/http"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func ParsePatternsQuery(r *http.Request) (*logproto.QueryPatternsRequest, error) {
	req := &logproto.QueryPatternsRequest{}

	start, end, err := bounds(r)
	if err != nil {
		return nil, err
	}
	req.Start = start
	req.End = end

	req.Query = query(r)
	return req, nil
}
