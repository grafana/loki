package loghttp

import (
	"net/http"

	"github.com/grafana/loki/pkg/logproto"
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

	// ensure matchers are valid before fanning out to ingesters/store as well as returning valuable parsing errors
	// instead of 500s
	_, err = Match(xs)
	if err != nil {
		return nil, err
	}

	return &logproto.SeriesRequest{
		Start:  start,
		End:    end,
		Groups: xs,
	}, nil

}
