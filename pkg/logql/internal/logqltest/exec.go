package logqltest

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

const (
	directTimestampFirstStackName               = "direct (timestamp-first)"
	directStreamFirstStackName                  = "direct (stream-first)"
	queryFrontendNoShardTimestampFirstStackName = "query-frontend + query-scheduler (no sharding, timestamp-first)"
	queryFrontendShardTimestampFirstStackName   = "query-frontend + query-scheduler (sharding, timestamp-first)"
	queryFrontendShardStreamFirstStackName      = "query-frontend + query-scheduler (sharding, stream-first)"
	directDataObjStackName                      = "direct (dataobj)"
	queryFrontendShardDataObjStackName          = "query-frontend + query-scheduler (sharding, dataobj)"
	queryFrontendShardDataObjAndChunkStackName  = "query-frontend + query-scheduler (sharding, dataobj and chunk)"
)

var (
	stackNames = []string{directTimestampFirstStackName, directStreamFirstStackName, queryFrontendNoShardTimestampFirstStackName, queryFrontendShardTimestampFirstStackName, queryFrontendShardStreamFirstStackName, directDataObjStackName, queryFrontendShardDataObjStackName, queryFrontendShardDataObjAndChunkStackName}
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

// newScriptQuerierFunc builds the querier of an execution stack over stores.
type newScriptQuerierFunc func(t *testing.T, stores *scriptStores) logql.Querier

// newChunkQuerier returns a querier that reads the streams from the chunk store.
func newChunkQuerier(t *testing.T, stores *scriptStores) logql.Querier {
	return newScriptQuerier(t, stores, time.Time{})
}

// newDataObjQuerierFunc returns a newScriptQuerierFunc whose querier reads the streams of
// stream-first queries from the data objects for the time range from dataObjStart on.
func newDataObjQuerierFunc(dataObjStart time.Time) newScriptQuerierFunc {
	return func(t *testing.T, stores *scriptStores) logql.Querier {
		return newScriptQuerier(t, stores, dataObjStart)
	}
}

// execLimits are the limits an execution stack runs queries with. They apply to every tenant. The
// series limit and the query timeout are high enough that no script reaches them.
type execLimits struct {
	streamFirstExecutionEnabled bool
}

func (execLimits) MaxQuerySeries(string) int { return math.MaxInt32 }

func (execLimits) MaxQueryRange(context.Context, string) time.Duration { return 0 }

func (execLimits) QueryTimeout(context.Context, string) time.Duration { return time.Hour }

func (execLimits) BlockedQueries(context.Context, string) []*validation.BlockedQuery { return nil }

func (l execLimits) StreamFirstExecutionEnabled(string) bool {
	return l.streamFirstExecutionEnabled
}

func (execLimits) DebugEngineTasks(string) bool { return false }

func (execLimits) DebugEngineStreams(string) bool { return false }

// checkDataObjReads returns an error when res holds a sample that only data objects can provide,
// but the query read no data-object row. It catches a routing bug that reads every sample from
// chunks, which the results alone cannot show, because chunks hold every stream too.
//
// Only a stream-first query reads data objects. A sample comes only from data objects when its
// whole range window is at or after dataObjStart. A zero-value dataObjStart disables the check.
func checkDataObjReads(query string, res logqlmodel.Result, dataObjStart time.Time) error {
	if dataObjStart.IsZero() {
		return nil
	}
	rangeAgg, ok := logql.StreamFirstRangeAggregation(query)
	if !ok {
		return nil
	}

	// The window of the sample at t is (t - offset - interval, t - offset].
	windowStart := func(t int64) time.Time {
		return time.UnixMilli(t).Add(-rangeAgg.Left.Offset - rangeAgg.Left.Interval)
	}
	needsDataObj := false
	switch data := res.Data.(type) {
	case promql.Vector:
		for _, s := range data {
			needsDataObj = needsDataObj || (s.F > 0 && !windowStart(s.T).Before(dataObjStart))
		}
	case promql.Matrix:
		for _, series := range data {
			for _, p := range series.Floats {
				needsDataObj = needsDataObj || (p.F > 0 && !windowStart(p.T).Before(dataObjStart))
			}
		}
	}

	if needsDataObj && res.Statistics.Querier.Store.Dataobj.PrePredicateDecompressedRows == 0 {
		return fmt.Errorf("query %q returned samples after the data-object start time, but read no data-object rows", query)
	}
	return nil
}
