package loghttp

import (
	"net/http"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
)

type SeriesResponse struct {
	Status string     `json:"status"`
	Data   []LabelSet `json:"data"`
}

func ParseSeriesQuery(r *http.Request) (*logproto.SeriesRequest, error) {
	start, end, err := bounds(r)
	if err != nil {
		return nil, err
	}

	xs := r.Form["match"]

	if len(xs) == 0 {
		return nil, errors.New("0 matcher groups supplied")
	}

	return &logproto.SeriesRequest{
		Start:  start,
		End:    end,
		Groups: xs,
	}, nil

}
