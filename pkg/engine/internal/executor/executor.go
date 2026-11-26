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

	"github.com/grafana/loki/v3/pkg/dataobj/deletion"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var tracer = otel.Tracer("pkg/engine/internal/executor")

// BloomDenyLister returns the names of the structured metadata columns of a
// tenant that get no bloom filter in the index.
type BloomDenyLister interface {
	DataObjBloomDenyList(tenant string) []string
}

type Config struct {
	// ScratchStore holds the temporary data of compaction tasks. Optional; nil
	// keeps the data in memory.
	ScratchStore scratch.Store
	// IndexobjCfg is the builder config for index objects.
	IndexobjCfg logsobj.BuilderBaseConfig
	// BloomDenyList gives the per-tenant bloom deny list of index objects.
	BloomDenyList BloomDenyLister
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

	// DeletionSweepMetrics counts the deletes of DeletionSweep tasks. Share
	// it across tasks, so that the metrics are registered once. A
	// DeletionSweep task that deletes fails if it is nil, so that no delete
	// goes uncounted. A dry run does not use it.
	DeletionSweepMetrics *deletion.SweepMetrics

	Bucket objstore.Bucket

	// DataBucket is the unprefixed dataobj root. LogMerge reads source log
	// objects from it, and DeletionSweep scans objects and stores file lists
	// in it. Unlike Bucket, which the compaction wiring prefixes with the
	// index-storage prefix for index I/O and ToC, it holds objects/ and the
	// index storage. LogMerge falls back to Bucket when it is nil.
	// DeletionSweep requires it.
	DataBucket objstore.Bucket

	// GetExternalInputs is an optional function called for each node in the
	// plan. Compaction nodes accept no inputs, so a node fails if
	// GetExternalInputs returns any Pipeline for it.
	GetExternalInputs func(ctx context.Context, node physical.Node) []Pipeline
}

// IndexMergeObserver receives IndexMerge statistics.
type IndexMergeObserver interface {
	// ObserveIndexMergeOutput receives the size of the index objects of one
	// finished task.
	ObserveIndexMergeOutput(tenant string, compressedBytes, uncompressedBytes int64)

	// ObserveIndexMergeInput receives the compressed and the uncompressed size
	// of a batch of source pages that the task downloaded, while the task
	// runs. Pages that the reader prunes are not counted.
	ObserveIndexMergeInput(tenant string, compressedBytes, uncompressedBytes int64)
}

// LogMergeObserver receives LogMerge statistics from one worker thread.
type LogMergeObserver interface {
	// ObserveLogMerge receives the summary of one finished task.
	ObserveLogMerge(tenant string, stats LogMergeObservedStats, duration time.Duration)

	// ObserveLogMergeInputBytesUncompressed receives the size of a batch of
	// merged records while the task runs. The size is the line bytes plus the
	// structured metadata value bytes. It reports during the task, not once at
	// the end, so that rate() stays steady for tasks that run for many
	// minutes.
	ObserveLogMergeInputBytesUncompressed(bytes int64)

	// ObserveLogMergeInputBytesCompressed receives the compressed size of a
	// batch of source pages that the task downloaded, while the task runs.
	//
	// The reader downloads ahead of the merge. A batch can include pages that
	// the merge has not consumed yet.
	ObserveLogMergeInputBytesCompressed(bytes int64)
}

// nopLogMergeObserver discards LogMerge statistics.
type nopLogMergeObserver struct{}

func (nopLogMergeObserver) ObserveLogMerge(string, LogMergeObservedStats, time.Duration) {}
func (nopLogMergeObserver) ObserveLogMergeInputBytesUncompressed(int64)                  {}
func (nopLogMergeObserver) ObserveLogMergeInputBytesCompressed(int64)                    {}

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
		bloomDenyList:      cfg.BloomDenyList,
		logsobjCfg:         cfg.LogsobjCfg,
		builderMetrics:     cfg.BuilderMetrics,
		indexMergeObserver: cfg.IndexMergeObserver,
		logMergeObserver:   cfg.LogMergeObserver,

		deletionSweepMetrics: cfg.DeletionSweepMetrics,
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
	bloomDenyList  BloomDenyLister
	logsobjCfg     logsobj.BuilderBaseConfig
	builderMetrics *logsobj.BuilderMetrics

	indexMergeObserver IndexMergeObserver
	logMergeObserver   LogMergeObserver

	deletionSweepMetrics *deletion.SweepMetrics
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
	case *physical.DeletionSweep:
		return NewObservedPipeline(n.Type().String(), nodeAttributes(n), c.executeDeletionSweep(n))
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
