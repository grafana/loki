package loghttp

import (
	"net/http"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/pkg/labels"
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

	groups, err := match(xs)
	if err != nil {
		return nil, err
	}

	return &logproto.SeriesRequest{
		Start:  start,
		End:    end,
		Groups: toLabelMatchers(groups),
	}, nil

}

func toLabelMatchers(groups [][]*labels.Matcher) []*logproto.LabelMatchers {
	res := make([]*logproto.LabelMatchers, 0, len(groups))
	for _, group := range groups {
		mapped := &logproto.LabelMatchers{}
		for _, m := range group {
			matcher := &logproto.LabelMatcher{}
			matcher.Type = logproto.MatchType(int32(m.Type))
			matcher.Name = m.Name
			matcher.Value = m.Value
			mapped.Matchers = append(mapped.Matchers, matcher)
		}
		res = append(res, mapped)
	}

	return res
}
