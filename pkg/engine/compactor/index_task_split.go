package compactor

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// indexMergeTask is one task of an index merge. The tasks of a merge split by
// posting key span read subsets of the sections of the merge, and together
// write one index run.
type indexMergeTask struct {
	spec *compactionv2pb.TaskSpec

	// keySpan holds the posting keys that the task merges. The zero value
	// holds every key.
	keySpan postings.KeySpan

	// statsSources are the source index objects whose stats rows the task
	// merges, in path order. Only a task with a bounded key span has them. A
	// task over every key merges the stats rows of every source.
	statsSources []string
}

// splitIndexMerge is one planned index merge and the tasks that run it.
type splitIndexMerge struct {
	// spec holds every run of the merge. The time range of the new run and
	// the source paths that the swap removes come from it.
	spec *compactionv2pb.TaskSpec

	// tasks are in posting key order. Their key spans hold every key once.
	tasks []indexMergeTask
}

// plans returns one plan for each task of m, in task order.
func (m splitIndexMerge) plans(tenant string, window time.Time) []*physical.Plan {
	plans := make([]*physical.Plan, len(m.tasks))
	for i, task := range m.tasks {
		plans[i] = buildIndexMergePlan(tenant, window, task)
	}
	return plans
}

// join returns the artifacts of the run of m. results holds the artifacts of
// each task of m, in task order. A run lists its index objects in posting key
// order, and the tasks are in key order, so join appends the artifacts of each
// task in turn.
//
// join returns an error if results does not hold one entry for each task, or
// if an artifact holds a posting key outside the key span of its task.
func (m splitIndexMerge) join(results [][]v2.ResultArtifact) ([]v2.ResultArtifact, error) {
	if len(results) != len(m.tasks) {
		return nil, fmt.Errorf("got the artifacts of %d tasks for %d tasks", len(results), len(m.tasks))
	}
	var artifacts []v2.ResultArtifact
	for i, task := range m.tasks {
		if err := checkInKeySpan(task, results[i]); err != nil {
			return nil, fmt.Errorf("task %d: %w", i, err)
		}
		artifacts = append(artifacts, results[i]...)
	}
	return artifacts, nil
}

// indexTaskSplitter turns one IndexMerge task of the planning strategy into
// the tasks that together do its work.
type indexTaskSplitter interface {
	split(spec *compactionv2pb.TaskSpec) ([]indexMergeTask, error)
}

// noopIndexTaskSplitter returns every task whole, over every posting key.
type noopIndexTaskSplitter struct{}

func (noopIndexTaskSplitter) split(spec *compactionv2pb.TaskSpec) ([]indexMergeTask, error) {
	return []indexMergeTask{{spec: spec}}, nil
}

// postingKeyLimit is the end of a split of posting keys. It sorts above every
// posting kind that the index writes. sizeIndexTaskSplitter rejects a key at
// or above it, because SplitKeyRanges panics on such a key. It never becomes a
// bound of a key span, because the last task of a split has no end.
var postingKeyLimit = postings.Key{Kind: 255}

// sizeIndexTaskSplitter splits IndexMerge tasks into tasks of about maxSize
// bytes of uncompressed postings each, by posting key span.
//
// Each task reads only the sections whose key bounds overlap its span. The
// stats rows of each source index object go to one task that reads the
// object, so each task writes the stats rows of its own sources.
type sizeIndexTaskSplitter struct {
	logger  log.Logger
	maxSize uint64

	// sections holds the planner section of each postings section, with
	// samples of its posting keys and their sizes.
	sections map[metastore.SectionKey]v2.Section[indexSortKey]
}

// newSizeIndexTaskSplitter returns a size splitter for tasks built from
// sections. It panics if maxSize is 0.
func newSizeIndexTaskSplitter(logger log.Logger, maxSize uint64, sections []v2.Section[indexSortKey]) sizeIndexTaskSplitter {
	if maxSize == 0 {
		panic("index task splitter needs a maximum task size above 0")
	}
	byKey := make(map[metastore.SectionKey]v2.Section[indexSortKey], len(sections))
	for _, section := range sections {
		byKey[sectionKeyOf(section.Ref)] = section
	}
	return sizeIndexTaskSplitter{logger: logger, maxSize: maxSize, sections: byKey}
}

// split returns the tasks that together do the work of spec, in posting key
// order.
//
// Each span starts at a sampled posting key, so every task merges postings
// rows. No posting key spans two tasks. The first task has no start and the
// last has no end, so the tasks together hold every key.
//
// split returns spec alone over every key if the split gives one span. It
// returns an error if spec references a section without planner data.
func (s sizeIndexTaskSplitter) split(spec *compactionv2pb.TaskSpec) ([]indexMergeTask, error) {
	var keys []v2.KeySize[postings.Key]
	for _, run := range spec.Runs {
		for _, ref := range run.Sections {
			section, err := s.sectionOf(ref)
			if err != nil {
				return nil, err
			}
			for _, sample := range section.Keys {
				key := sample.Key.postingKey()
				if key.Compare(postingKeyLimit) >= 0 {
					return nil, fmt.Errorf("index section %s/%d holds posting kind %d", ref.ObjectPath, ref.SectionIndex, key.Kind)
				}
				keys = append(keys, v2.KeySize[postings.Key]{Key: key, UncompressedSize: sample.UncompressedSize})
			}
		}
	}
	splits := v2.SplitKeyRanges(keys, comparePostingKeys, s.maxSize, postingKeyLimit)
	for _, split := range splits {
		if split.Oversized {
			level.Warn(s.logger).Log(
				"msg", "index-compaction: posting key is larger than the maximum task size, giving it a task of its own",
				"posting_key", fmt.Sprintf("%v", split.Range.Start()),
				"size", split.UncompressedSize,
				"max_task_size", s.maxSize,
			)
		}
	}
	if len(splits) <= 1 {
		return noopIndexTaskSplitter{}.split(spec)
	}

	tasks := make([]indexMergeTask, 0, len(splits))
	for i, split := range splits {
		var start, end *postings.Key
		if i > 0 {
			startKey := split.Range.Start()
			start = &startKey
		}
		if i < len(splits)-1 {
			endKey := split.Range.End()
			end = &endKey
		}
		keySpan, err := postings.NewKeySpan(start, end)
		if err != nil {
			return nil, fmt.Errorf("index task key span %d: %w", i, err)
		}
		pruned, err := s.sectionsInSpan(spec, keySpan)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, indexMergeTask{spec: pruned, keySpan: keySpan})
	}
	if err := s.assignStatsSources(spec, tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// assignStatsSources gives the stats rows of each source index object of spec
// to one of the tasks that read the object. It gives each object, largest
// first, to the reading task with the fewest source bytes so far, so the stats
// work spreads over the tasks. The size of an object is the size of its
// sampled postings, because planning does not read the stats.
//
// assignStatsSources returns an error if no task reads an object of spec.
func (s sizeIndexTaskSplitter) assignStatsSources(spec *compactionv2pb.TaskSpec, tasks []indexMergeTask) error {
	objectBytes := make(map[string]uint64)
	for _, run := range spec.Runs {
		for _, ref := range run.Sections {
			section, err := s.sectionOf(ref)
			if err != nil {
				return err
			}
			if _, ok := objectBytes[ref.ObjectPath]; !ok {
				objectBytes[ref.ObjectPath] = 0
			}
			for _, sample := range section.Keys {
				objectBytes[ref.ObjectPath] += sample.UncompressedSize
			}
		}
	}
	objects := slices.Collect(maps.Keys(objectBytes))
	slices.SortFunc(objects, func(a, b string) int {
		if c := cmp.Compare(objectBytes[b], objectBytes[a]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})

	readers := make([]map[string]struct{}, len(tasks))
	for i, task := range tasks {
		readers[i] = make(map[string]struct{})
		for _, run := range task.spec.Runs {
			for _, ref := range run.Sections {
				readers[i][ref.ObjectPath] = struct{}{}
			}
		}
	}
	assigned := make([]uint64, len(tasks))
	for _, object := range objects {
		owner := -1
		for i := range tasks {
			if _, ok := readers[i][object]; ok && (owner < 0 || assigned[i] < assigned[owner]) {
				owner = i
			}
		}
		if owner < 0 {
			return fmt.Errorf("no task of the split reads source index %s", object)
		}
		assigned[owner] += objectBytes[object]
		tasks[owner].statsSources = append(tasks[owner].statsSources, object)
	}
	for i := range tasks {
		slices.Sort(tasks[i].statsSources)
	}
	return nil
}

func (s sizeIndexTaskSplitter) sectionOf(ref *compactionv2pb.SectionRef) (v2.Section[indexSortKey], error) {
	section, ok := s.sections[sectionKeyOf(ref)]
	if !ok {
		return v2.Section[indexSortKey]{}, fmt.Errorf("task references unknown index section %s/%d", ref.ObjectPath, ref.SectionIndex)
	}
	return section, nil
}

// sectionsInSpan returns a copy of spec with only the sections whose posting
// key bounds overlap keySpan. It drops a run without such sections.
func (s sizeIndexTaskSplitter) sectionsInSpan(spec *compactionv2pb.TaskSpec, keySpan postings.KeySpan) (*compactionv2pb.TaskSpec, error) {
	pruned := &compactionv2pb.TaskSpec{Tenant: spec.Tenant}
	for _, run := range spec.Runs {
		var kept []*compactionv2pb.SectionRef
		for _, ref := range run.Sections {
			section, err := s.sectionOf(ref)
			if err != nil {
				return nil, err
			}
			if keySpan.Overlaps(section.Min.postingKey(), section.Max.postingKey()) {
				kept = append(kept, ref)
			}
		}
		if len(kept) > 0 {
			pruned.Runs = append(pruned.Runs, &compactionv2pb.RunRef{Sections: kept})
		}
	}
	return pruned, nil
}

// checkInKeySpan returns an error if an artifact of task holds a posting key
// outside the key span of task. The output of such a task would repeat the
// rows of the other tasks of its merge.
func checkInKeySpan(task indexMergeTask, artifacts []v2.ResultArtifact) error {
	if task.keySpan.IsAll() {
		return nil
	}
	for _, artifact := range artifacts {
		if !task.keySpan.HoldsRange(artifact.PostingKeys) {
			return fmt.Errorf("artifact %s holds posting keys %v, outside the key span %v of its task", artifact.Path, artifact.PostingKeys, task.keySpan)
		}
	}
	return nil
}

// reusedPaths returns the paths of newRuns that are also in oldPaths, in the
// order of newRuns.
//
// Index object paths are content hashes. A task of a split merge can write
// the same rows as one source index object, for example an index object of a
// run from an earlier split. Its output then has the path of that source. The
// ToC swap rejects such new runs.
func reusedPaths(oldPaths []string, newRuns []metastore.IndexRunEntries) []string {
	old := make(map[string]struct{}, len(oldPaths))
	for _, path := range oldPaths {
		old[path] = struct{}{}
	}
	var reused []string
	for _, run := range newRuns {
		for _, entry := range run {
			if _, ok := old[entry.Path]; ok {
				reused = append(reused, entry.Path)
			}
		}
	}
	return reused
}

func comparePostingKeys(a, b postings.Key) int { return a.Compare(b) }

// postingKey returns the posting key of k, without its timestamps.
func (k indexSortKey) postingKey() postings.Key {
	return postings.Key{Kind: k.kind, ColumnName: k.columnName, LabelValue: k.labelValue}
}
