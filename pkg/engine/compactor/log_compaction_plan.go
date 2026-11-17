package compactor

import (
	"iter"
	"time"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// logCompactionPlan is the work that replaces one source index in one window.
// The source index is an index run: one index object, or every index object
// that shares a RunID. The swap replaces every index object of the run with
// one new run per task, with an entry for each artifact of the task.
//
// The zero value has no work. Executing it is a no-op.
type logCompactionPlan struct {
	window         time.Time
	sourceIndexRun indexRun
	tasks          []logCompactionTask

	// shardBuckets is the shard layout of the source index. Compaction never
	// changes the layout of an object, so every artifact has this layout.
	shardBuckets indexpointers.ShardBuckets

	// singleObjectRuns is the number of log runs in the source index that
	// hold exactly one object. Plans with no work also set it, so the gauge
	// counts indexes that need no compaction.
	singleObjectRuns int
}

// hasWork reports whether p dispatches any task.
func (p logCompactionPlan) hasWork() bool {
	return len(p.tasks) > 0
}

// logTaskStats yields the log stats of each task of p. It skips tasks that read
// no log data.
func (p logCompactionPlan) logTaskStats() iter.Seq[logTaskStats] {
	return func(yield func(logTaskStats) bool) {
		for _, task := range p.tasks {
			stats := task.logStats()
			if stats.inputBytes == 0 {
				continue
			}
			if !yield(stats) {
				return
			}
		}
	}
}

// logTaskStats describes the log data that one task reads.
type logTaskStats struct {
	// inputBytes is the uncompressed size of the log sections that the task
	// reads.
	inputBytes uint64

	// oldestTimestamp is the earliest log timestamp in the sections.
	oldestTimestamp time.Time
}

// physicalPlans returns one physical plan per task, in task order.
func (p logCompactionPlan) physicalPlans(tenant string) []*physical.Plan {
	plans := make([]*physical.Plan, len(p.tasks))
	for i, task := range p.tasks {
		plans[i] = task.physicalPlan(tenant, p.window)
	}
	return plans
}

// outputBounds returns the output bounds of each task, in task order.
func (p logCompactionPlan) outputBounds() []outputBounds {
	bounds := make([]outputBounds, len(p.tasks))
	for i, task := range p.tasks {
		bounds[i] = outputBounds{inputs: task.inputTimeRange(), shardBuckets: p.shardBuckets}
	}
	return bounds
}

// logCompactionTask is one unit of work in a log compaction plan.
type logCompactionTask interface {
	// physicalPlan returns a new single-root physical plan for the task.
	physicalPlan(tenant string, window time.Time) *physical.Plan

	// inputTimeRange returns the time range of the inputs of the task. Every
	// output of the task lies in it. The coordinator checks the time range of
	// each artifact against it, and uses it for an artifact without one.
	inputTimeRange() timeRange

	// logStats returns the log data that the task reads. Its bytes are zero
	// if the task reads no log data.
	logStats() logTaskStats
}

// logMergeTask merges the runs of spec into new log objects.
type logMergeTask struct {
	spec   *compactionv2pb.TaskSpec
	inputs timeRange
}

func newLogMergeTask(spec *compactionv2pb.TaskSpec) logMergeTask {
	return logMergeTask{spec: spec, inputs: newTimeRange(runsTimeRange(spec.Runs))}
}

func (t logMergeTask) physicalPlan(tenant string, window time.Time) *physical.Plan {
	return buildLogMergePlan(tenant, window, t.spec)
}

func (t logMergeTask) inputTimeRange() timeRange { return t.inputs }

// logStats returns the input bytes of the task's key range from the index
// stats, so the tasks of a split merge add up to the bytes of the merge. If
// the spec has no input bytes, it falls back to the size of the task's
// sections.
func (t logMergeTask) logStats() logTaskStats {
	stats := logTaskStats{oldestTimestamp: t.inputs.start}
	if t.spec.InputBytes > 0 {
		stats.inputBytes = uint64(t.spec.InputBytes)
		return stats
	}
	for _, run := range t.spec.Runs {
		for _, section := range run.Sections {
			stats.inputBytes += uint64(section.UncompressedSize)
		}
	}
	return stats
}

// indexFilterTask writes a new index that keeps only the rows of objectPaths
// from the source index. It keeps log data without rewriting it.
type indexFilterTask struct {
	// sourceIndexPaths are the paths of every index object of the source index
	// run. A run splits postings across its index objects, so a filter over
	// part of a run loses postings.
	sourceIndexPaths []string
	objectPaths      []string
	inputs           timeRange
}

func (t indexFilterTask) physicalPlan(tenant string, _ time.Time) *physical.Plan {
	return buildIndexFilterPlan(tenant, t.sourceIndexPaths, t.objectPaths)
}

func (t indexFilterTask) inputTimeRange() timeRange { return t.inputs }

// logStats returns zero bytes, because the filter reads only the source
// index.
func (t indexFilterTask) logStats() logTaskStats { return logTaskStats{} }
