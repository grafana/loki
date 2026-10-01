package logqltest

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
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
)

var (
	stackNames = []string{directTimestampFirstStackName, directStreamFirstStackName, queryFrontendNoShardTimestampFirstStackName, queryFrontendShardTimestampFirstStackName, queryFrontendShardStreamFirstStackName}
)

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
	// setStreams (re)builds the stack's store with the provided log streams.
	setStreams(streams []logproto.Stream)
	// eval runs cmd and returns the query result.
	eval(cmd evalCmd) (logqlmodel.Result, error)
	// isQueryShardingSupported reports whether this stack runs queries with sharding enabled.
	isQueryShardingSupported() bool
	// isEvalSupported reports whether this stack can run the given cmd and exp.
	isEvalSupported(cmd evalCmd, exp expectations) bool
}

// isQueryShardingSupported reports whether the shard mapper fans a query out into >= 2 shards.
func isQueryShardingSupported(query string) bool {
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		return false
	}
	return expr.Shardable(true)
}

// newScriptStore builds a chunk store from streams and registers its close.
func newScriptStore(t *testing.T, streams []logproto.Stream) *testingChunkStore {
	store := newTestingChunkStore(t)

	// The close runs before the store's temp dir is removed: newTestingChunkStore
	// registers the temp-dir cleanup first, so this later-registered cleanup runs
	// first (t.Cleanup is LIFO).
	t.Cleanup(store.close)

	store.write(t, streams)
	store.flush(t)
	return store
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
