package engine

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
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
	start := time.Now()

	builder := newResultBuilder()

	logger := utillog.WithContext(ctx, e.logger)
	logger = log.With(logger, "query", params.QueryString(), "engine", "v2")

	logicalPlan, err := logical.BuildPlan(params)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create logical plan", "err", err)
		return builder.empty(), ErrNotSupported
	}

	executionContext := physical.NewContext(ctx, e.metastore, params.Start(), params.End())
	planner := physical.NewPlanner(executionContext)
	plan, err := planner.Build(logicalPlan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create physical plan", "err", err)
		return builder.empty(), ErrNotSupported
	}
	plan, err = planner.Optimize(plan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to optimize physical plan", "err", err)
		return builder.empty(), ErrNotSupported
	}

	level.Info(logger).Log("msg", "execute query with new engine", "query", params.QueryString())

	cfg := executor.Config{
		BatchSize: int64(e.opts.BatchSize),
	}
	pipeline := executor.Run(ctx, cfg, plan)
	defer pipeline.Close()

	if err := collectResult(ctx, pipeline, builder); err != nil {
		return builder.empty(), err
	}

	statsCtx := stats.FromContext(ctx)
	builder.setStats(statsCtx.Result(time.Since(start), 0, builder.len()))

	return builder.build(), nil
}

func collectResult(_ context.Context, pipeline executor.Pipeline, result *resultBuilder) error {
	for {
		if err := pipeline.Read(); err != nil {
			if err == executor.EOF {
				break
			}
			return err
		}
		if err := collectRecord(pipeline, result); err != nil {
			return err
		}
	}
	return nil
}

func collectRecord(pipeline executor.Pipeline, result *resultBuilder) error {
	rec, err := pipeline.Value()
	if err != nil {
		return err
	}
	defer rec.Release()
	for rowIdx := range int(rec.NumRows()) {
		collectRow(rec, rowIdx, result)
	}
	return nil
}

func collectRow(rec arrow.Record, i int, result *resultBuilder) {
	var entry logproto.Entry
	lbs := labels.NewBuilder(labels.EmptyLabels())
	metadata := labels.NewBuilder(labels.EmptyLabels())

	for colIdx := range int(rec.NumCols()) {
		col := rec.Column(colIdx)
		colName := rec.ColumnName(colIdx)

		// TODO(chaudum): We need to add metadata to columns to identify builtins, labels, metadata, and parsed.
		field := rec.Schema().Field(colIdx)
		colType, ok := field.Metadata.GetValue(types.ColumnTypeMetadataKey)

		// Ignore column values that are NULL or invalid or don't have a column typ
		if col.IsNull(i) || !col.IsValid(i) || !ok {
			continue
		}

		// Extract line
		if colName == types.ColumnNameBuiltinLine && colType == types.ColumnTypeBuiltin.String() {
			entry.Line = col.(*array.String).Value(i)
			continue
		}

		// Extract timestamp
		if colName == types.ColumnNameBuiltinTimestamp && colType == types.ColumnTypeBuiltin.String() {
			entry.Timestamp = time.Unix(0, int64(col.(*array.Uint64).Value(i)))
			continue
		}

		// Extract label
		if colType == types.ColumnTypeLabel.String() {
			switch arr := col.(type) {
			case *array.String:
				lbs.Set(colName, arr.Value(i))
			}
			continue
		}

		// Extract metadata
		if colType == types.ColumnTypeMetadata.String() {
			switch arr := col.(type) {
			case *array.String:
				metadata.Set(colName, arr.Value(i))
			}
			continue
		}
	}
	entry.StructuredMetadata = logproto.FromLabelsToLabelAdapters(metadata.Labels())

	stream := lbs.Labels()

	// Ignore rows that don't have stream labels, log line, or timestamp
	if stream.Len() == 0 || entry.Line == "" || entry.Timestamp.Equal(time.Time{}) {
		return
	}

	// Finally, add newly created entry to builder
	result.add(stream, entry)
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
