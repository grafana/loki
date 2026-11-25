package compactor

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// PairPlanConfig holds the settings of PlanPairs. Each field matches a flag
// of the compactor.
type PairPlanConfig struct {
	// LogMaxRunsPerTask is the number of pairs in each merge (K). Same as
	// -dataobj.compaction.logs.max-runs-per-task. It must be at least 2.
	LogMaxRunsPerTask int

	// LogMinCompactionSize is the smallest total log size of a layout group
	// that planning merges, in uncompressed bytes. Same as
	// -dataobj.compaction.logs.min-compaction-size.
	LogMinCompactionSize uint64

	// LogMaxTaskSize is the largest uncompressed input of one LogMerge task.
	// Same as -dataobj.compaction.logs.max-task-size. 0 disables the split.
	LogMaxTaskSize uint64

	// LogMaxKeySize is the largest uncompressed input of one sort prefix in
	// a merge. Same as -dataobj.compaction.logs.max-key-size. 0 disables the
	// check.
	LogMaxKeySize uint64

	// SizeLevelBase is the upper bound of size level 1, in uncompressed
	// bytes. 0 uses v2.DefaultSizeLevelBase.
	SizeLevelBase uint64

	// SizeLevelRatio is the size ratio between levels. 0 uses
	// v2.DefaultSizeLevelRatio.
	SizeLevelRatio uint64

	// Now decides which window is the current one. The planner splits
	// legacy index runs only in the current window. The zero value uses the
	// time of the call.
	Now time.Time
}

// PairPlan is the work that the coupled planner (planner version 2) would start
// in one window of a tenant.
type PairPlan struct {
	// IndexRuns is the number of index runs in the ToC of the window.
	IndexRuns int

	// ReadFailures is the number of index runs whose stats failed to read.
	// The planner plans no jobs in a window with read failures.
	ReadFailures int

	// Pairs are the index runs whose log objects form one log run.
	Pairs []PlannedPair

	// Merges are the merges of K pairs of one size level.
	Merges []PlannedMerge

	// Splits are the index runs over more than one log run that the planner
	// splits into pairs. It splits them only in the current window.
	Splits []PlannedSplit

	// Skipped are the index runs that the planner skipped, because they
	// have no stats rows or share a log object with another index run.
	Skipped []SkippedIndex

	// Legacy are the index runs over more than one log run that the planner
	// leaves as they are, because the window is not the current one. In the
	// current window the planner splits them instead.
	Legacy []LegacyIndexRun
}

// LegacyIndexRun is an index run over more than one log run that the planner
// leaves as it is.
type LegacyIndexRun struct {
	// IndexPaths are the paths of the index objects of the index run.
	IndexPaths []string

	// LogRuns is the number of log runs of the index run.
	LogRuns int

	// UncompressedSize is the uncompressed size of the log runs.
	UncompressedSize uint64
}

// SkippedIndex is an index run that the planner skipped.
type SkippedIndex struct {
	// IndexPaths are the paths of the index objects of the index run.
	IndexPaths []string

	// Reason is no_stats or shared_log_object.
	Reason string

	// SharedLogObject is the first log object of the index run that another
	// index run also holds. It is empty for other reasons.
	SharedLogObject string

	// OtherIndexRuns holds the index object paths of each other index run
	// that holds SharedLogObject.
	OtherIndexRuns [][]string
}

// PlannedPair is one pair that the planner found.
type PlannedPair struct {
	// IndexPaths are the paths of the index objects of the index run.
	IndexPaths []string

	// LogObjects is the number of log objects of the log run.
	LogObjects int

	// UncompressedSize is the uncompressed size of the log run.
	UncompressedSize uint64

	// Level is the size level of the log run.
	Level int

	// Layout names the stream layout of the pair. Pairs of different
	// layouts never merge.
	Layout string
}

// PlannedMerge is one merge of K pairs into one pair.
type PlannedMerge struct {
	// Level is the size level of the source pairs, and OutputLevel the size
	// level of the new pair.
	Level, OutputLevel int

	// IndexRuns holds the index object paths of each source pair.
	IndexRuns [][]string

	// LogTasks is the number of LogMerge tasks. With more than one, the
	// merge also runs an IndexMerge of their indexes.
	LogTasks int

	// InputBytes is the uncompressed log data that the merge reads.
	InputBytes int64

	// MinTime and MaxTime bound the log data of the source pairs.
	MinTime, MaxTime time.Time
}

// PlannedSplit is the split of one index run into one pair per log run.
type PlannedSplit struct {
	// IndexPaths are the paths of the index objects of the index run.
	IndexPaths []string

	// LogRuns is the number of log runs, and so of new pairs.
	LogRuns int

	// UncompressedSize is the uncompressed size of the log runs.
	UncompressedSize uint64
}

// PlanPairs plans one window of tenant with the coupled planner and returns
// the jobs that the planner would start. It reads the ToC and the stats of
// the index objects from bucket, and writes nothing. bucket must hold them at
// the paths that the compactor uses.
//
// PlanPairs plans as if no job runs, so it plans every index run of the
// window. It splits legacy index runs only if window is the current window at
// cfg.Now, as the compactor does. It logs skipped index runs to logger.
func PlanPairs(ctx context.Context, logger log.Logger, bucket objstore.Bucket, tenant string, window time.Time, cfg PairPlanConfig) (PairPlan, error) {
	base, ratio := cfg.SizeLevelBase, cfg.SizeLevelRatio
	if base == 0 {
		base = v2.DefaultSizeLevelBase
	}
	if ratio == 0 {
		ratio = v2.DefaultSizeLevelRatio
	}
	strategy, err := v2.NewSizeLeveledStrategy(base, ratio, cfg.LogMaxRunsPerTask)
	if err != nil {
		return PairPlan{}, fmt.Errorf("size levels: %w", err)
	}
	planner := &pairPlanner{
		logger:            logger,
		bucket:            bucket,
		metrics:           newCoordinatorMetrics(prometheus.NewRegistry()),
		strategy:          strategy,
		minCompactionSize: cfg.LogMinCompactionSize,
		maxLogTaskSize:    cfg.LogMaxTaskSize,
		maxKeySize:        cfg.LogMaxKeySize,
	}
	now := cfg.Now
	if now.IsZero() {
		now = time.Now()
	}
	legacy := ignoreLegacy
	if window.UTC().Truncate(metastore.MetastoreWindowSize).Equal(now.UTC().Truncate(metastore.MetastoreWindowSize)) {
		legacy = splitLegacy
	}
	plan, err := planner.planWindow(ctx, tenant, window, leaseSnapshot{}, legacy)
	if err != nil {
		return PairPlan{}, err
	}

	out := PairPlan{IndexRuns: plan.indexRuns, ReadFailures: plan.readFailures}
	for _, source := range plan.pairs {
		out.Pairs = append(out.Pairs, PlannedPair{
			IndexPaths:       source.indexRun.paths(),
			LogObjects:       len(logObjectsOf([]v2.Run{source.logRun})),
			UncompressedSize: source.logRun.Size(),
			Level:            strategy.Level(source.logRun.Size()),
			Layout:           newLayoutKey(source.layout).String(),
		})
	}
	for _, run := range plan.legacy {
		planned := LegacyIndexRun{IndexPaths: run.indexRun.paths(), LogRuns: len(run.logRuns)}
		for _, logRun := range run.logRuns {
			planned.UncompressedSize += logRun.Size()
		}
		out.Legacy = append(out.Legacy, planned)
	}
	for _, skip := range plan.skipped {
		planned := SkippedIndex{IndexPaths: skip.indexRun.paths(), Reason: skip.reason, SharedLogObject: skip.sharedLogObject}
		for _, other := range skip.otherIndexRuns {
			planned.OtherIndexRuns = append(planned.OtherIndexRuns, other.paths())
		}
		out.Skipped = append(out.Skipped, planned)
	}
	for _, job := range plan.jobs {
		switch job := job.(type) {
		case *pairMerge:
			out.Merges = append(out.Merges, plannedMerge(job))
		case *pairSplit:
			out.Splits = append(out.Splits, plannedSplit(job))
		}
	}
	return out, nil
}

func plannedMerge(merge *pairMerge) PlannedMerge {
	planned := PlannedMerge{
		Level:      merge.level,
		LogTasks:   len(merge.logTasks),
		InputBytes: merge.inputBytes(),
		MinTime:    merge.inputRange.start,
		MaxTime:    merge.inputRange.end,
	}
	if len(merge.logTasks) > 0 {
		planned.OutputLevel = int(merge.logTasks[0].OutputLevel)
	}
	for _, source := range merge.scope.sources {
		planned.IndexRuns = append(planned.IndexRuns, source.paths())
	}
	return planned
}

func plannedSplit(split *pairSplit) PlannedSplit {
	planned := PlannedSplit{IndexPaths: split.source.paths(), LogRuns: len(split.logRuns)}
	for _, logRun := range split.logRuns {
		planned.UncompressedSize += logRun.Size()
	}
	return planned
}
