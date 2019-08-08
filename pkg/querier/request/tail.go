package request

import (
	"net/http"

	"github.com/grafana/loki/pkg/logproto"
)

func ParseTailQuery(r *http.Request) (*logproto.TailRequest, error) {
	var err error
	req := logproto.TailRequest{
		Query: query(r),
	}

	req.Limit, err = limit(r)
	if err != nil {
		return nil, err
	}

	req.Start, _, err = bounds(r)
	if err != nil {
		return nil, err
	}
	req.DelayFor, err = tailDelay(r)
	if err != nil {
		return nil, err
	}
	return &req, nil
}
