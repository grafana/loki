package logqltest

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// chunksStreamFirstExecutionStack runs queries through the v1 engine with stream-ordered execution
// enabled, over the same chunk store as the direct stack. It exercises the per-stream read order that
// the default (timestamp-first) direct stack does not.
type chunksStreamFirstExecutionStack struct {
	t     *testing.T
	store *testingChunkStore
}

func newChunksStreamFirstStack(t *testing.T) *chunksStreamFirstExecutionStack {
	return &chunksStreamFirstExecutionStack{t: t}
}

func (*chunksStreamFirstExecutionStack) name() string { return chunksStreamFirstStackName }

func (*chunksStreamFirstExecutionStack) isQueryShardingSupported() bool { return false }

func (*chunksStreamFirstExecutionStack) isEvalSupported(evalCmd, expectations) bool { return true }

func (s *chunksStreamFirstExecutionStack) setStreams(streams []logproto.Stream) {
	if s.store != nil {
		s.store.close()
	}
	s.store = newScriptStore(s.t, streams)
}

func (s *chunksStreamFirstExecutionStack) eval(cmd evalCmd) (logqlmodel.Result, error) {
	return evalStreamOrdered(s.t, s.store.querier(), cmd)
}

// dataObjectsExecutionStack runs queries through the v1 engine with stream-ordered execution enabled,
// over a data object built from the loaded streams. A query ineligible for stream-first execution falls
// back to the chunk store inside the data-object querier, so every query is still exercised and must
// match the other stacks.
type dataObjectsExecutionStack struct {
	t       *testing.T
	store   *testingChunkStore
	querier logql.Querier
}

func newDataObjectsStack(t *testing.T) *dataObjectsExecutionStack {
	return &dataObjectsExecutionStack{t: t}
}

func (*dataObjectsExecutionStack) name() string { return dataObjectsStackName }

func (*dataObjectsExecutionStack) isQueryShardingSupported() bool { return false }

func (*dataObjectsExecutionStack) isEvalSupported(evalCmd, expectations) bool { return true }

func (s *dataObjectsExecutionStack) setStreams(streams []logproto.Stream) {
	if s.store != nil {
		s.store.close()
	}
	// The data-object querier delegates unsupported queries to the chunk store, so build both from the
	// same streams.
	s.store = newScriptStore(s.t, streams)
	s.querier = newTestingDataObjQuerier(s.t, s.store.store, streams)
}

func (s *dataObjectsExecutionStack) eval(cmd evalCmd) (logqlmodel.Result, error) {
	return evalStreamOrdered(s.t, s.querier, cmd)
}

// evalStreamOrdered runs cmd through a v1 engine with stream-ordered execution enabled over q.
func evalStreamOrdered(t *testing.T, q logql.Querier, cmd evalCmd) (logqlmodel.Result, error) {
	t.Helper()
	var opts logql.EngineOpts
	flagext.DefaultValues(&opts)
	opts.StreamOrderedExecutionEnabled = true
	engine := logql.NewEngine(opts, q, logql.NoLimits, log.NewNopLogger())

	start, end, step := cmd.getTimeRange()
	params, err := logql.NewLiteralParams(
		cmd.query,
		epoch.Add(start), epoch.Add(end), step, 0,
		logproto.FORWARD, 1000, nil, nil,
	)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	ctx := user.InjectOrgID(context.Background(), tenant)
	return engine.Query(params).Exec(ctx)
}
