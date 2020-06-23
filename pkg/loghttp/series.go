package loghttp

import (
	"net/http"
	"sort"
	"strings"

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
	// Prometheus encodes with `match[]`; we use both for compatibility.
	ys := r.Form["match[]"]

	deduped := union(xs, ys)
	sort.Strings(deduped)

	// Special case to allow for an empty label matcher `{}` in a Series query,
	// we support either not specifying the `match` query parameter at all or specifying it with a single `{}`
	// empty label matcher, we treat the latter case here as if no `match` was supplied at all.
	if len(deduped) == 1 {
		matcher := deduped[0]
		matcher = strings.Replace(matcher, " ", "", -1)
		if matcher == "{}" {
			deduped = deduped[:0]
		}
	}

	// ensure matchers are valid before fanning out to ingesters/store as well as returning valuable parsing errors
	// instead of 500s
	_, err = Match(deduped)
	if err != nil {
		return nil, err
	}

	return &logproto.SeriesRequest{
		Start:  start,
		End:    end,
		Groups: deduped,
	}, nil

}

func union(cols ...[]string) []string {
	m := map[string]struct{}{}

	for _, col := range cols {
		for _, s := range col {
			m[s] = struct{}{}
		}
	}

	res := make([]string, 0, len(m))
	for k := range m {
		res = append(res, k)
	}

	return res
}
