package logql

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
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
					mustMatcher(labels.MatchEqual, "c", "3"),
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
			got, err := Match(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.Equal(t, tt.want, got)
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
