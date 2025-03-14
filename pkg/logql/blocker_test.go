package logql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

func TestEngine_ExecWithBlockedQueries(t *testing.T) {
	limits := &fakeLimits{maxSeries: 10}
	eng := NewEngine(EngineOpts{}, getLocalQuerier(100000), limits, log.NewNopLogger())

	defaultQuery := `topk(1,rate(({app=~"foo|bar"})[1m]))`
	for _, test := range []struct {
		name        string
		q           string
		blocked     []*validation.BlockedQuery
		expectedErr error
	}{
		{
			"exact match all types",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: defaultQuery,
				},
			}, logqlmodel.ErrBlocked,
		},
		{
			"exact match all types with surrounding whitespace trimmed",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: fmt.Sprintf("       %s  ", defaultQuery),
				},
			}, logqlmodel.ErrBlocked,
		},
		{
			"exact match filter type only",
			`{app=~"foo|bar"} |= "baz"`, []*validation.BlockedQuery{
				{
					Pattern: `{app=~"foo|bar"} |= "baz"`,
					Types:   []string{QueryTypeFilter},
				},
			}, logqlmodel.ErrBlocked,
		},
		{
			"match from multiple patterns",
			`{app=~"foo|bar"} |= "baz"`, []*validation.BlockedQuery{
				// won't match
				{
					Pattern: `.*"buzz".*`,
					Regex:   true,
				},
				// will match
				{
					Pattern: `{app=~"foo|bar"} |= "baz"`,
					Types:   []string{QueryTypeFilter},
				},
			}, logqlmodel.ErrBlocked,
		},
		{
			"no block: exact match not matching filter type",
			`{app=~"foo|bar"} | json`, []*validation.BlockedQuery{
				{
					Pattern: `{app=~"foo|bar"} | json`, // "limited" query
					Types:   []string{QueryTypeFilter},
				},
			}, nil,
		},
		{
			"regex match all types",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: ".*foo.*",
					Regex:   true,
				},
			}, logqlmodel.ErrBlocked,
		},
		{
			"regex match multiple types",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: ".*foo.*",
					Regex:   true,
					Types:   []string{QueryTypeFilter, QueryTypeMetric},
				},
			}, logqlmodel.ErrBlocked,
		},
		{
			"match all queries by type",
			defaultQuery, []*validation.BlockedQuery{
				{
					Types: []string{QueryTypeFilter, QueryTypeMetric},
				},
			}, logqlmodel.ErrBlocked,
		},
		{
			"no block: match all queries by type",
			defaultQuery, []*validation.BlockedQuery{
				{
					Types: []string{QueryTypeLimited},
				},
			}, nil,
		},
		{
			"regex does not compile",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: "[.*",
					Regex:   true,
					Types:   []string{QueryTypeFilter, QueryTypeMetric},
				},
			}, nil,
		},
		{
			"correct FNV32 hash matches",
			defaultQuery, []*validation.BlockedQuery{
				{
					Hash: util.HashedQuery(defaultQuery),
				},
			}, logqlmodel.ErrBlocked,
		},
		{
			"incorrect FNV32 hash does not match",
			defaultQuery, []*validation.BlockedQuery{
				{
					Hash: util.HashedQuery(defaultQuery) + 1,
				},
			}, nil,
		},
		{
			"no blocked queries",
			defaultQuery, []*validation.BlockedQuery{}, nil,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits.blockedQueries = test.blocked

			params, err := NewLiteralParams(test.q, time.Unix(0, 0), time.Unix(100000, 0), 60*time.Second, 0, logproto.FORWARD, 1000, nil, nil)
			require.NoError(t, err)
			q := eng.Query(params)
			_, err = q.Exec(user.InjectOrgID(context.Background(), "fake"))

			if test.expectedErr == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			require.Equal(t, err.Error(), test.expectedErr.Error())
		})
	}
}
