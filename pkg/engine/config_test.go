package engine

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

func TestConfig_Validate_V1OnlyStreamSelector(t *testing.T) {
	tests := []struct {
		name             string
		selector         string
		expectedErr      string
		expectedMatchers []*labels.Matcher
	}{
		{
			name: "empty selector disables the feature",
		},
		{
			name:     "valid equality selector",
			selector: `{app="foo"}`,
			expectedMatchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
		},
		{
			name:     "multiple equality matchers",
			selector: `{app="foo", env="prod"}`,
			expectedMatchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
				labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
			},
		},
		{
			name:        "invalid LogQL selector",
			selector:    `{app=}`,
			expectedErr: "invalid v1-only stream selector",
		},
		{
			name:        "regex matcher rejected even alongside an equality matcher",
			selector:    `{app="foo", env=~"prod.*"}`,
			expectedErr: "must contain only equality matchers",
		},
		{
			name:        "negative matcher rejected",
			selector:    `{app="foo", env!="dev"}`,
			expectedErr: "must contain only equality matchers",
		},
		{
			name:        "negative regex matcher rejected",
			selector:    `{app="foo", env!~"dev.*"}`,
			expectedErr: "must contain only equality matchers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{V1OnlyStreamSelector: tt.selector}
			err := cfg.Validate()

			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				return
			}

			require.NoError(t, err)
			require.ElementsMatch(t, tt.expectedMatchers, cfg.V1OnlyMatchers)
		})
	}
}

func TestConfig_MatchesV1OnlySelector(t *testing.T) {
	newParams := func(t *testing.T, query string) logql.Params {
		t.Helper()
		now := time.Now()
		params, err := logql.NewLiteralParams(query, now.Add(-time.Hour), now, 0, 0, logproto.BACKWARD, 100, nil, nil)
		require.NoError(t, err)
		return params
	}

	tests := []struct {
		name     string
		selector string
		query    string
		expected bool
	}{
		{
			name:     "exact match",
			selector: `{app="foo"}`,
			query:    `{app="foo"}`,
			expected: true,
		},
		{
			name:     "query group with extra matchers matches",
			selector: `{app="foo"}`,
			query:    `{app="foo", env="prod"}`,
			expected: true,
		},
		{
			name:     "different value does not match",
			selector: `{app="foo"}`,
			query:    `{app="bar"}`,
			expected: false,
		},
		{
			name:     "regex matcher in query does not match",
			selector: `{app="foo"}`,
			query:    `{app=~"foo"}`,
			expected: false,
		},
		{
			name:     "multi-matcher selector requires all matchers",
			selector: `{app="foo", env="prod"}`,
			query:    `{app="foo", cluster="us"}`,
			expected: false,
		},
		{
			name:     "multi-matcher selector with all matchers present",
			selector: `{app="foo", env="prod"}`,
			query:    `{env="prod", cluster="us", app="foo"}`,
			expected: true,
		},
		{
			name:     "metric query wrapping a matching selector",
			selector: `{app="foo"}`,
			query:    `count_over_time({app="foo"}[5m])`,
			expected: true,
		},
		{
			name:     "binary op with one matching leg",
			selector: `{app="foo"}`,
			query:    `sum(rate({app="foo"}[5m])) + sum(rate({app="bar"}[5m]))`,
			expected: true,
		},
		{
			name:     "binary op with no matching leg",
			selector: `{app="foo"}`,
			query:    `sum(rate({app="bar"}[5m])) + sum(rate({app="baz"}[5m]))`,
			expected: false,
		},
		{
			name:     "query without stream selector",
			selector: `{app="foo"}`,
			query:    `vector(0)`,
			expected: false,
		},
		{
			name:     "no selector configured",
			selector: "",
			query:    `{app="foo"}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{V1OnlyStreamSelector: tt.selector}
			require.NoError(t, cfg.Validate())

			require.Equal(t, tt.expected, cfg.MatchesV1OnlySelector(newParams(t, tt.query)))
		})
	}
}
