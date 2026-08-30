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

// directExecutionStack runs queries straight through the v1 engine, with no query-frontend in
// front of it.
type directExecutionStack struct {
	t     *testing.T
	store *testingChunkStore
}

func newDirectStack(t *testing.T) *directExecutionStack {
	return &directExecutionStack{t: t}
}

func (*directExecutionStack) name() string {
	return directStackName
}

func (*directExecutionStack) isQueryShardingSupported() bool {
	return false
}

func (*directExecutionStack) isEvalSupported(evalCmd, expectations) bool {
	return true
}

func (s *directExecutionStack) setStreams(streams []logproto.Stream) {
	// Stop the previous store so a multi-scenario script does not leave one running per refresh.
	if s.store != nil {
		s.store.close()
	}
	s.store = newScriptStore(s.t, streams)
}

func (s *directExecutionStack) eval(cmd evalCmd) (logqlmodel.Result, error) {
	var opts logql.EngineOpts
	flagext.DefaultValues(&opts)
	engine := logql.NewEngine(opts, s.store.querier(), logql.NoLimits, log.NewNopLogger())

	start, end, step := cmd.getTimeRange()
	params, err := logql.NewLiteralParams(
		cmd.query,
		epoch.Add(start), epoch.Add(end), step, 0,
		cmd.direction, 1000, nil, nil,
	)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	ctx := user.InjectOrgID(context.Background(), tenant)
	return engine.Query(params).Exec(ctx)
}
