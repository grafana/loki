package engine

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	ErrNotSupported = errors.New("feature not supported in new query engine")
)

// New creates a new instance of the query engine that implements the [logql.Engine] interface.
func New(opts logql.EngineOpts, metastore metastore.Metastore, limits logql.Limits, logger log.Logger) *QueryEngine {
	return &QueryEngine{
		logger:    logger,
		limits:    limits,
		metastore: metastore,
		opts:      opts,
	}
}

// QueryEngine combines logical planning, physical planning, and execution to evaluate LogQL queries.
type QueryEngine struct {
	logger    log.Logger
	limits    logql.Limits
	metastore metastore.Metastore
	opts      logql.EngineOpts
}

// Query implements [logql.Engine].
func (e *QueryEngine) Query(params logql.Params) logql.Query {
	return &queryAdapter{
		engine: e,
		params: params,
	}
}

// Execute executes a LogQL query and returns its results or alternatively an error.
// The execution is done in three steps:
//  1. Create a logical plan from the provided query parameters.
//  2. Create a physical plan from the logical plan using information from the catalog.
//  3. Evaluate the physical plan with the executor.
func (e *QueryEngine) Execute(ctx context.Context, params logql.Params) (logqlmodel.Result, error) {
	var result logqlmodel.Result
	logger := utillog.WithContext(ctx, e.logger)
	logger = log.With(logger, "query", params.QueryString(), "engine", "v2")

	logicalPlan, err := logical.BuildPlan(params)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create logical plan", "err", err)
		return result, ErrNotSupported
	}

	executionContext := physical.NewContext(ctx, e.metastore, params.Start(), params.End())
	planner := physical.NewPlanner(executionContext)
	plan, err := planner.Build(logicalPlan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create physical plan", "err", err)
		return result, ErrNotSupported
	}
	_, err = planner.Optimize(plan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to optimize physical plan", "err", err)
		return result, ErrNotSupported
	}

	// TODO(chaudum): Replace the return values with the actual return values from the execution.
	level.Info(logger).Log("msg", "execute query with new engine", "query", params.QueryString())
	return result, ErrNotSupported
}

var _ logql.Engine = (*QueryEngine)(nil)

// queryAdapter dispatches query execution to the wrapped engine.
type queryAdapter struct {
	params logql.Params
	engine *QueryEngine
}

// Exec implements [logql.Query].
func (q *queryAdapter) Exec(ctx context.Context) (logqlmodel.Result, error) {
	return q.engine.Execute(ctx, q.params)
}

var _ logql.Query = (*queryAdapter)(nil)
