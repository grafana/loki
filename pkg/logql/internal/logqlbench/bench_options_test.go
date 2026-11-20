package logqlbench

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	objectclient "github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

// optionVariant is a set of experimental engine options to run the queries with.
type optionVariant struct {
	name  string
	apply func(*logql.EngineOpts)
}

var optionVariants = []optionVariant{
	{"baseline", func(*logql.EngineOpts) {}},
	{"identity-only", func(o *logql.EngineOpts) { o.IdentityOnlyRangeIterator = true }},
	{"hash-keyed", func(o *logql.EngineOpts) { o.HashKeyedRangeIterator = true }},
	{"push-min-max", func(o *logql.EngineOpts) { o.PushMinMaxGrouping = true }},
	{"restricted-parsing", func(o *logql.EngineOpts) { o.RestrictedParsing = true }},
	{"all", func(o *logql.EngineOpts) {
		o.IdentityOnlyRangeIterator = true
		o.HashKeyedRangeIterator = true
		o.PushMinMaxGrouping = true
		o.RestrictedParsing = true
	}},
}

// optionQueries are range queries that the experimental options act on. They run timestamp-first
// over the chunk store, since the options act on the range iterator of that order.
var optionQueries = []benchQuery{
	{
		expr:        fmt.Sprintf("count(count_over_time(%s[5m]))", selectorAll),
		description: "reads only which series exist",
	},
	{
		expr:        fmt.Sprintf("avg(max_over_time(%s | logfmt | unwrap value [5m]))", selectorAll),
		description: "reads values, no labels",
	},
	{
		expr:        fmt.Sprintf("avg by(%s) (max_over_time(%s | logfmt | unwrap value [5m]))", labelMediumCardName, selectorAll),
		description: "reads values, one label",
	},
	{
		expr:        fmt.Sprintf("max by(%s) (max_over_time(%s | logfmt | unwrap value [5m]))", labelMediumCardName, selectorAll),
		description: "merges series at the source",
	},
	{
		expr:        fmt.Sprintf("sum by(%s) (sum_over_time(%s | logfmt | unwrap value [5m]))", labelMediumCardName, selectorAll),
		description: "parses only the fields it reads",
	},
}

// BenchmarkLogQLMetricQueriesOptions compares the engine with and without the experimental
// options, over the chunk store with timestamp-first order. Run one leaf per process to read its
// peak memory, as the AGENTS.md of this directory describes.
func BenchmarkLogQLMetricQueriesOptions(b *testing.B) {
	runtime.GOMAXPROCS(1)

	dir, err := ensureFixtures(b)
	require.NoError(b, err)

	chunkStore, err := newChunkStore(dir, tenant, func(inner objectclient.ObjectClient) objectclient.ObjectClient { return inner })
	require.NoError(b, err)
	b.Cleanup(func() { _ = chunkStore.Close() })

	for _, query := range optionQueries {
		params, err := logql.NewLiteralParams(query.expr, fixtureStart, fixtureEnd, 5*time.Minute, 0, logproto.FORWARD, 0, nil, nil)
		require.NoError(b, err)

		b.Run("query="+query.name(), func(b *testing.B) {
			for _, variant := range optionVariants {
				b.Run("option="+variant.name, func(b *testing.B) {
					var engineOpts logql.EngineOpts
					flagext.DefaultValues(&engineOpts)
					variant.apply(&engineOpts)
					engine := logql.NewEngine(engineOpts, chunkStore.store, benchLimits{}, log.NewNopLogger())

					b.ReportAllocs()
					for b.Loop() {
						res, err := engine.Query(params).Exec(user.InjectOrgID(context.Background(), tenant))
						require.NoError(b, err)
						require.NotNil(b, res.Data)
					}
				})
			}
		})
	}
}
