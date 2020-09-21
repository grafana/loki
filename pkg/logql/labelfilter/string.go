package labelfilter

import (
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

func (s *String) Filter(lbs labels.Labels) (bool, error) {
	for _, l := range lbs {
		if l.Name == s.Name {
			return s.Matches(l.Value), nil
		}
	}
	return false, nil
}
