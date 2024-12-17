package logql

import (
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// MatchForSeriesRequest extracts and parses multiple matcher groups from a slice of strings.
// Does not perform validation as it's used for series queries
// which allow empty matchers
func MatchForSeriesRequest(xs []string) ([][]*labels.Matcher, error) {
	groups := make([][]*labels.Matcher, 0, len(xs))
	for _, x := range xs {
		ms, err := syntax.ParseMatchers(x, false)
		if err != nil {
			return nil, err
		}
		if len(ms) == 0 {
			return nil, errors.Errorf("0 matchers in group: %s", x)
		}
		groups = append(groups, ms)
	}

	return groups, nil
}
