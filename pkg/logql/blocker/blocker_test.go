package blocker

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type fakeLimits struct {
	blockedQueries []*validation.BlockedQuery
}

func (f fakeLimits) BlockedQueries(_ context.Context, _ string) []*validation.BlockedQuery {
	return f.blockedQueries
}

func queryType(t *testing.T, q string) (string, string) {
	t.Helper()
	params, err := logql.NewLiteralParams(q, time.Unix(0, 0), time.Unix(100000, 0), 60*time.Second, 0, logproto.FORWARD, 1000, nil, nil)
	require.NoError(t, err)
	typ, err := logql.QueryType(params.GetExpression())
	require.NoError(t, err)
	return params.QueryString(), typ
}

func TestMatches(t *testing.T) {
	limits := &fakeLimits{}

	defaultQuery := `topk(1,rate(({app=~"foo|bar"})[1m]))`
	for _, test := range []struct {
		name    string
		q       string
		blocked []*validation.BlockedQuery
		want    bool
	}{
		{
			"exact match all types",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: defaultQuery,
				},
			}, true,
		},
		{
			"exact match all types with surrounding whitespace trimmed",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: fmt.Sprintf("       %s  ", defaultQuery),
				},
			}, true,
		},
		{
			"exact match filter type only",
			`{app=~"foo|bar"} |= "baz"`, []*validation.BlockedQuery{
				{
					Pattern: `{app=~"foo|bar"} |= "baz"`,
					Types:   []string{logql.QueryTypeFilter},
				},
			}, true,
		},
		{
			"match from multiple patterns",
			`{app=~"foo|bar"} |= "baz"`, []*validation.BlockedQuery{
				{
					Pattern: `.*"buzz".*`,
					Regex:   true,
				},
				{
					Pattern: `{app=~"foo|bar"} |= "baz"`,
					Types:   []string{logql.QueryTypeFilter},
				},
			}, true,
		},
		{
			"no block: exact match not matching filter type",
			`{app=~"foo|bar"} | json`, []*validation.BlockedQuery{
				{
					Pattern: `{app=~"foo|bar"} | json`,
					Types:   []string{logql.QueryTypeFilter},
				},
			}, false,
		},
		{
			"regex match all types",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: ".*foo.*",
					Regex:   true,
				},
			}, true,
		},
		{
			"regex match multiple types",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: ".*foo.*",
					Regex:   true,
					Types:   []string{logql.QueryTypeFilter, logql.QueryTypeMetric},
				},
			}, true,
		},
		{
			"match all queries by type",
			defaultQuery, []*validation.BlockedQuery{
				{
					Types: []string{logql.QueryTypeFilter, logql.QueryTypeMetric},
				},
			}, true,
		},
		{
			"no block: match all queries by type",
			defaultQuery, []*validation.BlockedQuery{
				{
					Types: []string{logql.QueryTypeLimited},
				},
			}, false,
		},
		{
			"regex does not compile",
			defaultQuery, []*validation.BlockedQuery{
				{
					Pattern: "[.*",
					Regex:   true,
					Types:   []string{logql.QueryTypeFilter, logql.QueryTypeMetric},
				},
			}, false,
		},
		{
			"correct FNV32 hash matches",
			defaultQuery, []*validation.BlockedQuery{
				{
					Hash: util.HashedQuery(defaultQuery),
				},
			}, true,
		},
		{
			"incorrect FNV32 hash does not match",
			defaultQuery, []*validation.BlockedQuery{
				{
					Hash: util.HashedQuery(defaultQuery) + 1,
				},
			}, false,
		},
		{
			"non-matching hash does not prevent subsequent pattern from matching",
			defaultQuery, []*validation.BlockedQuery{
				{
					Hash: util.HashedQuery(defaultQuery) + 1,
				},
				{
					Pattern: defaultQuery,
				},
			}, true,
		},
		{
			"second hash in list matches when first does not",
			defaultQuery, []*validation.BlockedQuery{
				{
					Hash: util.HashedQuery(defaultQuery) + 1,
				},
				{
					Hash: util.HashedQuery(defaultQuery),
				},
			}, true,
		},
		{
			"no blocked queries",
			defaultQuery, []*validation.BlockedQuery{}, false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits.blockedQueries = test.blocked

			query, typ := queryType(t, test.q)
			ctx := user.InjectOrgID(context.Background(), "fake")
			require.Equal(t, test.want, Matches(ctx, limits, log.NewNopLogger(), "fake", query, typ))
		})
	}
}

func TestMatches_ConcurrentAccess(t *testing.T) {
	shared := []*validation.BlockedQuery{
		{
			Pattern: "",
			Types:   []string{logql.QueryTypeMetric},
		},
	}

	limits := &fakeLimits{blockedQueries: shared}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	query, typ := queryType(t, `topk(1,rate(({app=~"foo|bar"})[1m]))`)
	for range goroutines {
		go func() {
			defer wg.Done()
			ctx := user.InjectOrgID(context.Background(), "fake")
			_ = Matches(ctx, limits, log.NewNopLogger(), "fake", query, typ)
		}()
	}

	wg.Wait()
}

func TestMatches_Tags(t *testing.T) {
	limits := &fakeLimits{}
	defaultQuery := `topk(1,rate(({app=~"foo|bar"})[1m]))`

	for _, test := range []struct {
		name       string
		q          string
		tagsHeader string
		blocked    []*validation.BlockedQuery
		want       bool
	}{
		{
			name:       "block when tags match and no types",
			q:          defaultQuery,
			tagsHeader: "Source=grafana,Feature=beta",
			blocked: []*validation.BlockedQuery{
				{
					Tags: map[string]string{"source": "grafana", "feature": "beta"},
				},
			},
			want: true,
		},
		{
			name:       "do not block when tags value mismatches",
			q:          defaultQuery,
			tagsHeader: "Source=grafana,Feature=alpha",
			blocked: []*validation.BlockedQuery{
				{
					Pattern: ".*",
					Regex:   true,
					Tags:    map[string]string{"feature": "beta"},
				},
			},
			want: false,
		},
		{
			name:       "block when types and tags match",
			q:          defaultQuery,
			tagsHeader: "Source=grafana,Feature=beta",
			blocked: []*validation.BlockedQuery{
				{
					Pattern: ".*",
					Regex:   true,
					Types:   []string{logql.QueryTypeMetric},
					Tags:    map[string]string{"source": "GRAFANA", "feature": "BETA"},
				},
			},
			want: true,
		},
		{
			name:       "do not block when types match but required tag key missing",
			q:          defaultQuery,
			tagsHeader: "Source=grafana",
			blocked: []*validation.BlockedQuery{
				{
					Pattern: ".*",
					Regex:   true,
					Types:   []string{logql.QueryTypeMetric},
					Tags:    map[string]string{"feature": "beta"},
				},
			},
			want: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits.blockedQueries = test.blocked

			query, typ := queryType(t, test.q)
			ctx := user.InjectOrgID(context.Background(), "fake")
			if test.tagsHeader != "" {
				ctx = httpreq.InjectQueryTags(ctx, test.tagsHeader)
			}

			require.Equal(t, test.want, Matches(ctx, limits, log.NewNopLogger(), "fake", query, typ))
		})
	}
}
