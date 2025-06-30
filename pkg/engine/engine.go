package engine

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

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
func New(opts logql.EngineOpts, bucket objstore.Bucket, limits logql.Limits, reg prometheus.Registerer, logger log.Logger) *QueryEngine {

	var ms metastore.Metastore
	if bucket != nil {
		ms = metastore.NewObjectMetastore(bucket, logger)
	}

	return &QueryEngine{
		logger:    logger,
		metrics:   newMetrics(reg),
		limits:    limits,
		metastore: ms,
		bucket:    bucket,
		opts:      opts,
	}
}

// QueryEngine combines logical planning, physical planning, and execution to evaluate LogQL queries.
type QueryEngine struct {
	logger    log.Logger
	metrics   *metrics
	limits    logql.Limits
	metastore metastore.Metastore
	bucket    objstore.Bucket
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
	logger = log.With(logger, "query", params.QueryString(), "shard", strings.Join(params.Shards(), ","), "engine", "v2")

	t := time.Now() // start stopwatch for logical planning
	logicalPlan, err := logical.BuildPlan(params)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create logical plan", "err", err)
		e.metrics.subqueries.WithLabelValues(statusNotImplemented).Inc()
		return builder.empty(), ErrNotSupported
	}
	e.metrics.logicalPlanning.Observe(time.Since(t).Seconds())
	durLogicalPlanning := time.Since(t)

	level.Info(logger).Log(
		"msg", "finished logical planning",
		"plan", base64.StdEncoding.EncodeToString([]byte(logicalPlan.String())),
		"duration", durLogicalPlanning.Seconds(),
	)

	t = time.Now() // start stopwatch for physical planning
	statsCtx, ctx := stats.NewContext(ctx)
	executionContext := physical.NewContext(ctx, e.metastore, params.Start(), params.End())
	planner := physical.NewPlanner(executionContext)
	plan, err := planner.Build(logicalPlan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to create physical plan", "err", err)
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		return builder.empty(), ErrNotSupported
	}
	plan, err = planner.Optimize(plan)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to optimize physical plan", "err", err)
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		return builder.empty(), ErrNotSupported
	}
	e.metrics.physicalPlanning.Observe(time.Since(t).Seconds())
	durPhysicalPlanning := time.Since(t)

	level.Info(logger).Log(
		"msg", "finished physical planning",
		"plan", base64.StdEncoding.EncodeToString([]byte(physical.PrintAsTree(plan))),
		"duration", durLogicalPlanning.Seconds(),
	)

	level.Info(logger).Log(
		"msg", "start executing query with new engine",
	)

	t = time.Now() // start stopwatch for execution
	cfg := executor.Config{
		BatchSize: int64(e.opts.BatchSize),
		Bucket:    e.bucket,
	}
	pipeline := executor.Run(ctx, cfg, plan)
	defer pipeline.Close()

	if err := collectResult(ctx, pipeline, builder); err != nil {
		e.metrics.subqueries.WithLabelValues(statusFailure).Inc()
		return builder.empty(), err
	}

	builder.setStats(statsCtx.Result(time.Since(start), 0, builder.len()))

	e.metrics.subqueries.WithLabelValues(statusSuccess).Inc()
	e.metrics.execution.Observe(time.Since(t).Seconds())
	durExecution := time.Since(t)

	level.Debug(logger).Log(
		"msg", "finished executing with new engine",
		"duration_logical_planning", durLogicalPlanning,
		"duration_physical_planning", durPhysicalPlanning,
		"duration_execution", durExecution,
	)

	return builder.build(), nil
}

func collectResult(_ context.Context, pipeline executor.Pipeline, result *resultBuilder) error {
	for {
		if err := pipeline.Read(); err != nil {
			if errors.Is(err, executor.EOF) {
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
		colType, ok := field.Metadata.GetValue(types.MetadataKeyColumnType)

		// Ignore column values that are NULL or invalid or don't have a column typ
		if col.IsNull(i) || !col.IsValid(i) || !ok {
			continue
		}

		// Extract line
		if colName == types.ColumnNameBuiltinMessage && colType == types.ColumnTypeBuiltin.String() {
			entry.Line = col.(*array.String).Value(i)
			continue
		}

		// Extract timestamp
		if colName == types.ColumnNameBuiltinTimestamp && colType == types.ColumnTypeBuiltin.String() {
			entry.Timestamp = time.Unix(0, int64(col.(*array.Timestamp).Value(i)))
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
