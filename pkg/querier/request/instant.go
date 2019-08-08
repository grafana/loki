package request

import (
	"net/http"
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

type InstantQuery struct {
	Query     string
	Ts        time.Time
	Limit     uint32
	Direction logproto.Direction
}

func ParseInstantQuery(r *http.Request) (*InstantQuery, error) {
	var err error
	request := &InstantQuery{
		Query: query(r),
	}
	request.Limit, err = limit(r)
	if err != nil {
		return nil, err
	}

	request.Ts, err = ts(r)
	if err != nil {
		return nil, err
	}

	request.Direction, err = direction(r)
	if err != nil {
		return nil, err
	}

	return request, nil
}
