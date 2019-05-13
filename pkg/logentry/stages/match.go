package stages

import (
	"time"

	"github.com/grafana/loki/pkg/logql"

	"github.com/prometheus/common/model"
)

// withMatcher runs a stage if matcher matches an entry labelset.
func withMatcher(s Stage, matcher string) (Stage, error) {
	if matcher == "" {
		return s, nil
	}
	matchers, err := logql.ParseMatchers(matcher)
	if err != nil {
		return nil, err
	}

	return StageFunc(func(labels model.LabelSet, t *time.Time, entry *string) {
		for _, filter := range matchers {
			if !filter.Matches(string(labels[model.LabelName(filter.Name)])) {
				return
			}
		}
		s.Process(labels, t, entry)
	}), nil
}
