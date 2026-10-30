package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/grafana/loki/v4/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v4/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v4/pkg/scratch"
)

var tracer = otel.Tracer("pkg/engine/internal/executor")

type Config struct {
	// ScratchStore holds the temporary data of compaction tasks. Optional; nil
	// keeps the data in memory.
	ScratchStore scratch.Store
	// IndexobjCfg is the builder config for index objects.
	IndexobjCfg logsobj.BuilderBaseConfig
	// LogsobjCfg is the builder config for log objects.
	LogsobjCfg logsobj.BuilderBaseConfig
	// BuilderMetrics is shared by logs object builders across worker tasks.
	BuilderMetrics *logsobj.BuilderMetrics

	// IndexMergeObserver is used  by compaction to populate output-size
	// histograms. Optional; nil disables observation.
	IndexMergeObserver IndexMergeObserver

	// LogMergeObserver is used by compaction to populate log-merge histograms.
	// Optional; nil disables observation.
	LogMergeObserver LogMergeObserver

	Bucket objstore.Bucket

	// DataBucket reads source log objects during LogMerge compaction. Unlike
	// Bucket (which the compaction wiring prefixes with the index-storage
	// prefix for index I/O and ToC), source log objects are stored at the
	// unprefixed dataobj root, so they must be read through this bucket.
	// Optional; when nil the executor falls back to Bucket.
	DataBucket objstore.Bucket

	// GetExternalInputs is an optional function called for each node in the
	// plan. Compaction nodes accept no inputs, so a node fails if
	// GetExternalInputs returns any Pipeline for it.
	GetExternalInputs func(ctx context.Context, node physical.Node) []Pipeline
}

type IndexMergeObserver interface {
	ObserveIndexMergeOutput(tenant string, compressedBytes, uncompressedBytes int64)
}

// LogMergeObserver receives LogMerge statistics from one worker thread.
type LogMergeObserver interface {
	// ObserveLogMerge receives the summary of one finished task.
	ObserveLogMerge(tenant string, stats LogMergeObservedStats, duration time.Duration)

	// ObserveLogMergeInputBytes receives the size of a batch of merged records
	// while the task runs. It reports during the task, not once at the end,
	// so that rate() stays steady for tasks that run for many minutes.
	ObserveLogMergeInputBytes(bytes int64)
}

// nopLogMergeObserver discards LogMerge statistics.
type nopLogMergeObserver struct{}

func (nopLogMergeObserver) ObserveLogMerge(string, LogMergeObservedStats, time.Duration) {}
func (nopLogMergeObserver) ObserveLogMergeInputBytes(int64)                              {}

// NewLogMergeObserverFunc returns the LogMergeObserver for the worker thread
// with the given index.
type NewLogMergeObserverFunc func(thread int) LogMergeObserver

func Run(ctx context.Context, cfg Config, plan *physical.Plan, logger log.Logger) Pipeline {
	c := &Context{
		plan:               plan,
		bucket:             cfg.Bucket,
		dataBucket:         cfg.DataBucket,
		logger:             logger,
		getExternalInputs:  cfg.GetExternalInputs,
		scratchStore:       cfg.ScratchStore,
		indexobjCfg:        cfg.IndexobjCfg,
		logsobjCfg:         cfg.LogsobjCfg,
		builderMetrics:     cfg.BuilderMetrics,
		indexMergeObserver: cfg.IndexMergeObserver,
		logMergeObserver:   cfg.LogMergeObserver,
	}
	if c.logMergeObserver == nil {
		c.logMergeObserver = nopLogMergeObserver{}
	}
	if plan == nil {
		return errorPipeline(ctx, errors.New("plan is nil"))
	}
	node, err := plan.Root()
	if err != nil {
		return errorPipeline(ctx, err)
	}

	return c.execute(ctx, node)
}

// Context is the execution context
type Context struct {
	logger     log.Logger
	plan       *physical.Plan
	bucket     objstore.Bucket
	dataBucket objstore.Bucket

	getExternalInputs func(ctx context.Context, node physical.Node) []Pipeline

	scratchStore   scratch.Store
	indexobjCfg    logsobj.BuilderBaseConfig
	logsobjCfg     logsobj.BuilderBaseConfig
	builderMetrics *logsobj.BuilderMetrics

	indexMergeObserver IndexMergeObserver
	logMergeObserver   LogMergeObserver
}

func (c *Context) execute(ctx context.Context, node physical.Node) Pipeline {
	children := c.plan.Children(node)
	inputs := make([]Pipeline, 0, len(children))
	for _, child := range children {
		inputs = append(inputs, c.execute(ctx, child))
	}

	if c.getExternalInputs != nil {
		inputs = append(inputs, c.getExternalInputs(ctx, node)...)
	}

	if len(inputs) > 0 {
		for _, input := range inputs {
			input.Close()
		}
		return errorPipeline(ctx, fmt.Errorf("%s does not accept inputs, got %d", node.Type(), len(inputs)))
	}

	switch n := node.(type) {
	case *physical.IndexMerge:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeIndexMerge(ctx, n))
	case *physical.LogMerge:
		// LogMerge runs a K-way sort-merge over the LOG sections referenced by the
		// node's Runs, producing schema-sorted compacted log object(s). See
		// executeLogMerge / doLogObjectMerge in log_merge.go.
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeLogMerge(n))
	case *physical.IndexFilter:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeIndexFilter(n))
	default:
		return errorPipeline(ctx, fmt.Errorf("invalid node type: %T", node))
	}
}

// nodeAttributes returns the OTel span attributes of the given physical plan
// node.
func nodeAttributes(n physical.Node) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("node_id", n.ID().String()),
	}
}
