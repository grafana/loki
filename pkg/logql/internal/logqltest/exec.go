package logqltest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/logql"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

const (
	directTimestampFirstStackName                     = "direct (timestamp-first)"
	directStreamFirstStackName                        = "direct (stream-first)"
	queryFrontendNoShardTimestampFirstStackName       = "query-frontend + query-scheduler (no sharding, timestamp-first)"
	queryFrontendShardTimestampFirstStackName         = "query-frontend + query-scheduler (sharding, timestamp-first)"
	queryFrontendShardStreamFirstStackName            = "query-frontend + query-scheduler (sharding, stream-first)"
	directDataObjStackName                            = "direct (dataobj)"
	queryFrontendShardDataObjStackName                = "query-frontend + query-scheduler (sharding, dataobj)"
	queryFrontendShardDataObjAndChunkStackName        = "query-frontend + query-scheduler (sharding, dataobj and chunk)"
	queryFrontendNoShardMaxQuerySeriesCutoffStackName = "query-frontend + query-scheduler (no sharding, timestamp-first, max query series cutoff)"
)

var (
	stackNames = []string{directTimestampFirstStackName, directStreamFirstStackName, queryFrontendNoShardTimestampFirstStackName, queryFrontendShardTimestampFirstStackName, queryFrontendShardStreamFirstStackName, directDataObjStackName, queryFrontendShardDataObjStackName, queryFrontendShardDataObjAndChunkStackName, queryFrontendNoShardMaxQuerySeriesCutoffStackName}
)

// splitDataObjStart is the data-object start time of the stack that reads both data objects and
// chunks, as an offset from epoch. The stack reads the samples before it from chunks and the rest
// from data objects.
const splitDataObjStart = 90 * time.Second

func isKnownStackName(name string) bool {
	for _, n := range stackNames {
		if n == name {
			return true
		}
	}
	return false
}

// executionStack runs eval commands through one query path and reports how its results must be
// asserted.
type executionStack interface {
	// name identifies the stack in subtest output.
	name() string
	// setStores rebuilds the stack's querier over stores.
	setStores(stores *scriptStores)
	// eval runs cmd and returns the query result.
	eval(cmd evalCmd) (logqlmodel.Result, error)
	// isQueryShardingSupported reports whether this stack runs queries with sharding enabled.
	isQueryShardingSupported() bool
	// isStreamFirstEnabled reports whether this stack runs eligible queries in stream-first order.
	isStreamFirstEnabled() bool
	// isEvalSupported reports whether this stack can run the given cmd and exp.
	isEvalSupported(cmd evalCmd, exp expectations) bool
}

// isQueryShardingSupported reports whether the shard mapper fans a query out into >= 2 shards.
func isQueryShardingSupported(query string) bool {
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		return false
	}
	if !expr.Shardable(true) {
		return false
	}

	// Shardable() reads the operations alone. The shard mapper also declines an avg_over_time()
	// whose unwrap post filter reads an error label, because it decomposes the average into a sum
	// and a count, and the count leg cannot reproduce that filter.
	//
	// This restates that rule by hand, so a new rule in the mapper needs the same line here.
	shardable := true
	expr.Walk(func(e syntax.Expr) bool {
		r, ok := e.(*syntax.RangeAggregationExpr)
		if ok && r.Operation == syntax.OpRangeTypeAvg && r.Left.HasUnwrapPostFilterOnErrorLabel() {
			shardable = false
		}
		return shardable
	})
	return shardable
}

// stackOptions change an execution stack from its defaults. The zero value changes nothing.
type stackOptions struct {
	// maxQuerySeries, when positive, is the series limit of every query the stack runs, in the
	// querier and in the query-frontend.
	maxQuerySeries int

	// queryShardingEnabled turns query sharding on in the query-frontend. The direct stacks ignore it.
	queryShardingEnabled bool

	// maxQuerySeriesCutoff turns on MaxQuerySeriesCutoff in every engine the stack
	// runs, in the querier and in the query-frontend.
	maxQuerySeriesCutoff bool
}

// stackOption changes one aspect of an execution stack from its default.
type stackOption func(*stackOptions)

// withMaxQuerySeries returns the stackOption that sets the series limit of the stack.
func withMaxQuerySeries(maxQuerySeries int) stackOption {
	return func(o *stackOptions) { o.maxQuerySeries = maxQuerySeries }
}

// withQuerySharding returns the stackOption that turns query sharding on in the query-frontend.
func withQuerySharding() stackOption {
	return func(o *stackOptions) { o.queryShardingEnabled = true }
}

// withMaxQuerySeriesCutoff returns the stackOption that turns on MaxQuerySeriesCutoff in every
// engine of the stack.
func withMaxQuerySeriesCutoff() stackOption {
	return func(o *stackOptions) { o.maxQuerySeriesCutoff = true }
}

// newEngineOpts returns the default engine options, changed by o.
func newEngineOpts(o stackOptions) logql.EngineOpts {
	var engineOpts logql.EngineOpts
	flagext.DefaultValues(&engineOpts)
	engineOpts.MaxQuerySeriesCutoff = o.maxQuerySeriesCutoff
	return engineOpts
}

// newStackOptions returns the stackOptions that opts build on top of the defaults.
func newStackOptions(opts ...stackOption) stackOptions {
	var o stackOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// newScriptQuerierFunc builds the querier of an execution stack over stores.
type newScriptQuerierFunc func(t *testing.T, stores *scriptStores) logql.Querier

// newChunkQuerier returns a querier that reads the streams from the chunk store.
func newChunkQuerier(t *testing.T, stores *scriptStores) logql.Querier {
	return newScriptQuerier(t, stores, time.Time{})
}

// newDataObjQuerierFunc returns a newScriptQuerierFunc whose querier reads the streams of
// stream-first requests from data objects, from dataObjQueryStart on. It does so only for requests
// that the caller marked eligible.
func newDataObjQuerierFunc(dataObjQueryStart time.Time) newScriptQuerierFunc {
	return func(t *testing.T, stores *scriptStores) logql.Querier {
		return newScriptQuerier(t, stores, dataObjQueryStart)
	}
}

// execLimits are the limits an execution stack runs queries with. They apply to every tenant. The
// query timeout is high enough that no script reaches it. The series limit is the default limit of
// a tenant, unless maxQuerySeries sets another.
type execLimits struct {
	streamFirstExecutionEnabled bool
	maxQuerySeries              int
}

func (l execLimits) MaxQuerySeries(string) int {
	if l.maxQuerySeries > 0 {
		return l.maxQuerySeries
	}
	return defaultMaxQuerySeries()
}

func (execLimits) MaxQueryRange(context.Context, string) time.Duration { return 0 }

func (execLimits) QueryTimeout(context.Context, string) time.Duration { return time.Hour }

func (execLimits) BlockedQueries(context.Context, string) []*validation.BlockedQuery { return nil }

func (l execLimits) StreamFirstExecutionEnabled(string) bool {
	return l.streamFirstExecutionEnabled
}

// checkDataObjReads returns an error when res holds a sample that only data objects can provide,
// but the query read no data-object row. It catches a routing bug that reads every sample from
// chunks, which the results alone cannot show, because chunks hold every stream too.
//
// Only the stream-first parts of a query read data objects. A sample comes only from data objects
// when every such part has its whole range window at or after dataObjQueryStart.
//
// The check skips a query in two cases. The first is a zero dataObjQueryStart. The second is another
// source of samples: a vector() expression, or a range aggregation that does not run in stream-first
// order. The row count is one total for the query, so it cannot tell which part read data objects.
func checkDataObjReads(query string, res logqlmodel.Result, dataObjQueryStart time.Time) error {
	if dataObjQueryStart.IsZero() {
		return nil
	}
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		return fmt.Errorf("parsing query %q: %w", query, err)
	}
	rangeAggs := logql.StreamFirstRangeAggregations(expr)
	if len(rangeAggs) == 0 || hasOtherSampleSource(expr) {
		return nil
	}

	// The window of the sample at t is (t - offset - interval, t - offset]. The part with the
	// largest offset plus interval has the earliest window start. Its window decides the check.
	var reach time.Duration
	for _, rangeAgg := range rangeAggs {
		reach = max(reach, rangeAgg.Left.Offset+rangeAgg.Left.Interval)
	}
	windowStart := func(t int64) time.Time {
		return time.UnixMilli(t).Add(-reach)
	}
	needsDataObj := false
	switch data := res.Data.(type) {
	case promql.Vector:
		for _, s := range data {
			needsDataObj = needsDataObj || !windowStart(s.T).Before(dataObjQueryStart)
		}
	case promql.Matrix:
		for _, series := range data {
			for _, p := range series.Floats {
				needsDataObj = needsDataObj || !windowStart(p.T).Before(dataObjQueryStart)
			}
		}
	}

	if needsDataObj && res.Statistics.Querier.Store.Dataobj.PrePredicateDecompressedRows == 0 {
		return fmt.Errorf("query %q returned samples after the data-object start time, but read no data-object rows", query)
	}
	return nil
}

// hasOtherSampleSource reports whether expr can yield a sample from something other than its
// stream-first parts: a vector() expression, or a log range that no stream-first part holds.
func hasOtherSampleSource(expr syntax.Expr) bool {
	var (
		hasVector bool
		logRanges int
	)
	expr.Walk(func(e syntax.Expr) bool {
		switch e.(type) {
		case *syntax.VectorExpr:
			hasVector = true
		case *syntax.LogRangeExpr:
			logRanges++
		}
		return true
	})
	return hasVector || logRanges > len(logql.StreamFirstRangeAggregations(expr))
}
