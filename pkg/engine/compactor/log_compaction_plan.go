package compactor

import (
	"iter"
	"time"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// logCompactionPlan is the work that replaces one source index in one window.
// Each task produces one artifact, and the swap replaces the source index with
// one ToC entry per task.
//
// The zero value has no work. Executing it is a no-op.
type logCompactionPlan struct {
	window      time.Time
	sourceIndex indexEntry
	tasks       []logCompactionTask

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

// dispatchTasks returns the tasks of p in task order.
func (p logCompactionPlan) dispatchTasks() []dispatchTask {
	// Go cannot convert a []logCompactionTask to a []dispatchTask directly.
	tasks := make([]dispatchTask, len(p.tasks))
	for i, task := range p.tasks {
		tasks[i] = task
	}
	return tasks
}

// tocEntries returns one ToC entry per task, in task order. The entry paths
// are empty, because the paths come from the task artifacts.
func (p logCompactionPlan) tocEntries() []metastore.TableOfContentsEntry {
	entries := make([]metastore.TableOfContentsEntry, len(p.tasks))
	for i, task := range p.tasks {
		entries[i] = task.tocEntry()
	}
	return entries
}

// logCompactionTask is one unit of work in a log compaction plan.
type logCompactionTask interface {
	dispatchTask

	// tocEntry returns the ToC entry for the artifact of the task, without a
	// path.
	tocEntry() metastore.TableOfContentsEntry

	// logStats returns the log data that the task reads. Its bytes are zero
	// if the task reads no log data.
	logStats() logTaskStats
}

// logMergeTask merges the runs of spec into new log objects for the ToC window
// that starts at window.
type logMergeTask struct {
	window time.Time
	spec   *compactionv2pb.TaskSpec
	entry  metastore.TableOfContentsEntry
}

func newLogMergeTask(window time.Time, spec *compactionv2pb.TaskSpec) logMergeTask {
	return logMergeTask{window: window, spec: spec, entry: runsToCEntry(spec.Runs)}
}

func (t logMergeTask) physicalPlan(tenant string) *physical.Plan {
	return buildLogMergePlan(tenant, t.window, t.spec)
}

func (t logMergeTask) tocEntry() metastore.TableOfContentsEntry { return t.entry }

// logStats returns the input bytes of the task's key range from the index
// stats, so the tasks of a split merge add up to the bytes of the merge. If
// the spec has no input bytes, it falls back to the size of the task's
// sections.
func (t logMergeTask) logStats() logTaskStats {
	stats := logTaskStats{oldestTimestamp: t.entry.StartTime}
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
	sourceIndexPath string
	objectPaths     []string
	entry           metastore.TableOfContentsEntry
}

func (t indexFilterTask) physicalPlan(tenant string) *physical.Plan {
	return buildIndexFilterPlan(tenant, t.sourceIndexPath, t.objectPaths)
}

func (t indexFilterTask) tocEntry() metastore.TableOfContentsEntry { return t.entry }

// logStats returns zero bytes, because the filter reads only the source
// index.
func (t indexFilterTask) logStats() logTaskStats { return logTaskStats{} }
