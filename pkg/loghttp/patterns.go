package loghttp

import (
	"net/http"
	"time"

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
	req.Step = (time.Duration(defaultQueryRangeStep(start, end)) * time.Second).Milliseconds()

	req.Query = query(r)
	return req, nil
}
