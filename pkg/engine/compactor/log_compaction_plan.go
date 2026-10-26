package compactor

import (
	"errors"
	"fmt"
	"iter"
	"time"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
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

	// merges holds one entry per merge of the planning strategy. The
	// splitter can cut one merge into many tasks.
	merges []logMergeGroup
}

// errRecordCountMismatch reports that the tasks of a merge did not write each
// input record exactly once.
var errRecordCountMismatch = errors.New("log merge record counts do not match")

// sectionKey identifies one logs section by object path and section index.
type sectionKey struct {
	path  string
	index int64
}

func sectionKeyOf(ref *compactionv2pb.SectionRef) sectionKey {
	return sectionKey{path: ref.ObjectPath, index: ref.SectionIndex}
}

// logMergeGroup is the tasks of one merge and the sections they read
// together.
type logMergeGroup struct {
	// tasks holds the indexes in logCompactionPlan.tasks of the tasks of the
	// merge. Only [logCompactionPlan.addMerge] sets it.
	tasks []int

	// sections holds every input section of the merge, once each.
	sections map[sectionKey]struct{}
}

// addMerge appends the tasks of merge to p and records them as one group for
// [logCompactionPlan.checkRecordCounts]. tasks must together do the work of
// merge.
func (p *logCompactionPlan) addMerge(merge *compactionv2pb.TaskSpec, tasks []*compactionv2pb.TaskSpec) {
	group := logMergeGroup{sections: make(map[sectionKey]struct{})}
	for _, run := range merge.Runs {
		for _, ref := range run.Sections {
			group.sections[sectionKeyOf(ref)] = struct{}{}
		}
	}
	for _, task := range tasks {
		group.tasks = append(group.tasks, len(p.tasks))
		p.tasks = append(p.tasks, newLogMergeTask(task))
	}
	p.merges = append(p.merges, group)
}

// checkRecordCounts checks that the tasks of every merge together wrote each
// input record exactly once. artifacts must be the artifacts of p.tasks, in
// task order. A failed check returns an error that wraps
// errRecordCountMismatch.
//
// The record count of a section comes from the section itself, not from the
// index that the planner reads. So the check also catches a split that left a
// section out of a task that needed it.
//
// A merge of one task passes without counts, because workers that predate
// key ranges report none and merge every stream of their sections. A merge of
// many tasks fails without counts, because such a worker ignores the key
// ranges and writes the records of shared sections more than once.
func (p logCompactionPlan) checkRecordCounts(artifacts []v2.ResultArtifact) error {
	if len(artifacts) != len(p.tasks) {
		return fmt.Errorf("got %d artifacts for %d tasks", len(artifacts), len(p.tasks))
	}
	for _, merge := range p.merges {
		if err := merge.checkRecordCounts(artifacts); err != nil {
			return fmt.Errorf("%w: %w", errRecordCountMismatch, err)
		}
	}
	return nil
}

func (g logMergeGroup) checkRecordCounts(artifacts []v2.ResultArtifact) error {
	var kept int64
	reported := make(map[sectionKey]int64)
	for _, task := range g.tasks {
		counts := artifacts[task].LogCounts
		if counts == nil {
			if len(g.tasks) == 1 {
				return nil
			}
			return fmt.Errorf("log merge task %d of a split merge reported no record counts; its worker may ignore key ranges", task)
		}
		kept += counts.KeptRecords
		for _, section := range counts.Sections {
			key := sectionKey{path: section.ObjectPath, index: section.SectionIndex}
			if _, ok := g.sections[key]; !ok {
				return fmt.Errorf("log merge task %d reported logs section %s#%d, which is not an input of its merge", task, key.path, key.index)
			}
			if previous, ok := reported[key]; ok && previous != section.Records {
				return fmt.Errorf("tasks report %d and %d records for logs section %s#%d", previous, section.Records, key.path, key.index)
			}
			reported[key] = section.Records
		}
	}

	var want int64
	for key := range g.sections {
		records, ok := reported[key]
		if !ok {
			return fmt.Errorf("no task of the merge read logs section %s#%d", key.path, key.index)
		}
		want += records
	}
	if kept != want {
		return fmt.Errorf("tasks of the merge wrote %d records, but its %d input sections hold %d", kept, len(g.sections), want)
	}
	return nil
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

	// logTaskStats returns the log data that the task reads. Its bytes are zero
	// if the task reads no log data.
	logStats() logTaskStats
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

func (t logMergeTask) logStats() logTaskStats {
	stats := logTaskStats{oldestTimestamp: t.entry.StartTime}
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

func (t indexFilterTask) physicalPlan(tenant string, _ time.Time) *physical.Plan {
	return buildIndexFilterPlan(tenant, t.sourceIndexPath, t.objectPaths)
}

func (t indexFilterTask) tocEntry() metastore.TableOfContentsEntry { return t.entry }

// logTaskStats returns zero bytes, because the filter reads only the source
// index.
func (t indexFilterTask) logStats() logTaskStats { return logTaskStats{} }
