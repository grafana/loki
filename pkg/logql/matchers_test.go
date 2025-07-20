package logql

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func Test_match(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    [][]*labels.Matcher
		wantErr bool
	}{
		{"malformed", []string{`{a="1`}, nil, true},
		{"empty on nil input", nil, [][]*labels.Matcher{}, false},
		{"empty on empty input", []string{}, [][]*labels.Matcher{}, false},
		{
			"single",
			[]string{`{a="1"}`},
			[][]*labels.Matcher{
				{mustMatcher(labels.MatchEqual, "a", "1")},
			},
			false,
		},
		{
			"multiple groups",
			[]string{`{a="1"}`, `{b="2", c=~"3", d!="4"}`},
			[][]*labels.Matcher{
				{mustMatcher(labels.MatchEqual, "a", "1")},
				{
					mustMatcher(labels.MatchEqual, "b", "2"),
					mustMatcher(labels.MatchRegexp, "c", "3"),
					mustMatcher(labels.MatchNotEqual, "d", "4"),
				},
			},
			false,
		},
		{
			"errors on empty group",
			[]string{`{}`},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MatchForSeriesRequest(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.Len(t, got, len(tt.want))
				for i, expectedMatchers := range tt.want {
					syntax.AssertMatchers(t, expectedMatchers, got[i])
				}
			}
		})
	}
}

func mustMatcher(t labels.MatchType, n string, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(err)
	}
	return m
}
