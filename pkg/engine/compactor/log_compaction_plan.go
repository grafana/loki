package compactor

import (
	"time"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// logCompactionPlan is the work that replaces one source index in one window.
// Each task produces one artifact, and the swap replaces the source index with
// one ToC entry per task.
//
// rewriteBytes is the uncompressed size of the log sections that the tasks
// rewrite. It excludes the runs that an IndexFilter keeps, because the filter
// does not rewrite their data.
//
// The zero value has no work. Executing it is a no-op.
type logCompactionPlan struct {
	window       time.Time
	sourceIndex  indexEntry
	tasks        []logCompactionTask
	rewriteBytes uint64
}

// hasWork reports whether p dispatches any task.
func (p logCompactionPlan) hasWork() bool {
	return len(p.tasks) > 0
}

// physicalPlans returns one physical plan per task, in task order.
//
// Each call builds new plans with new node IDs. The scheduler requires unique
// node IDs, so every execution of p must call physicalPlans again.
func (p logCompactionPlan) physicalPlans(tenant string) []*physical.Plan {
	plans := make([]*physical.Plan, len(p.tasks))
	for i, task := range p.tasks {
		plans[i] = task.physicalPlan(tenant, p.window)
	}
	return plans
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
	// physicalPlan returns a new single-root physical plan for the task.
	physicalPlan(tenant string, window time.Time) *physical.Plan

	// tocEntry returns the ToC entry for the artifact of the task, without a
	// path.
	tocEntry() metastore.TableOfContentsEntry
}

// logMergeTask merges the runs of spec into new log objects.
type logMergeTask struct {
	spec  *compactionv2pb.TaskSpec
	entry metastore.TableOfContentsEntry
}

func newLogMergeTask(spec *compactionv2pb.TaskSpec) logMergeTask {
	return logMergeTask{spec: spec, entry: runsToCEntry(spec.Runs)}
}

func (t logMergeTask) physicalPlan(tenant string, window time.Time) *physical.Plan {
	return buildLogMergePlan(tenant, window, t.spec)
}

func (t logMergeTask) tocEntry() metastore.TableOfContentsEntry { return t.entry }

// rewriteBytes returns the uncompressed size of the sections that t merges.
func (t logMergeTask) rewriteBytes() uint64 {
	var total uint64
	for _, run := range t.spec.Runs {
		for _, section := range run.Sections {
			total += uint64(section.UncompressedSize)
		}
	}
	return total
}

// indexFilterTask writes a new index that keeps only the rows of objectPaths
// from the source index. It keeps log data without rewriting it.
type indexFilterTask struct {
	sourceIndexPath string
	objectPaths     []string
	entry           metastore.TableOfContentsEntry
}

func (t indexFilterTask) physicalPlan(tenant string, _ time.Time) *physical.Plan {
	return buildIndexFilterPlan(tenant, t.sourceIndexPath, t.objectPaths)
}

func (t indexFilterTask) tocEntry() metastore.TableOfContentsEntry { return t.entry }

// sortObjectTask rewrites one log object with sortSchema.
type sortObjectTask struct {
	objectPath string
	sortSchema []string
	entry      metastore.TableOfContentsEntry
}

func (t sortObjectTask) physicalPlan(string, time.Time) *physical.Plan {
	return buildSortObjectPlan(t.objectPath, t.sortSchema)
}

func (t sortObjectTask) tocEntry() metastore.TableOfContentsEntry { return t.entry }
