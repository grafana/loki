package logqltest

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

const (
	directStackName               = "direct"
	queryFrontendNoShardStackName = "query-frontend + query-scheduler (no sharding)"
	queryFrontendShardStackName   = "query-frontend + query-scheduler (sharding)"
)

var (
	stackNames = []string{directStackName, queryFrontendNoShardStackName, queryFrontendShardStackName}
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
