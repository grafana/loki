package labelfilter

import (
	"github.com/grafana/loki/pkg/logql/log"

	"github.com/prometheus/prometheus/pkg/labels"
)

type String struct {
	*labels.Matcher
}

func NewString(m *labels.Matcher) *String {
	return &String{
		Matcher: m,
	}
}

func (s *String) Process(line []byte, lbs log.Labels) ([]byte, bool) {
	for k, v := range lbs {
		if k == s.Name {
			return line, s.Matches(v)
		}
	}
	return line, false
}
