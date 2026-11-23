package compactor

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// Values of the kind label of pairJobsTotal.
const (
	pairJobKindMerge = "merge"
	pairJobKindSplit = "split"
)

// pairJob is one job of the coupled planner. It runs tasks, then replaces
// every index object of its source index runs with new runs in one ToC swap.
type pairJob interface {
	// replaces returns the window and the source index runs of the job.
	replaces() jobScope

	// logObjects returns the paths of the log objects that the source index
	// runs index.
	logObjects() []string

	// priority returns the priority of the first tasks of the job.
	priority() taskPriority

	// kind returns the value of the kind label of the job.
	kind() string

	// execute runs the tasks of the job and returns the runs that replace
	// its sources, and the number of tasks it ran. The ToC does not
	// reference the runs yet.
	execute(ctx context.Context, env jobEnv) (newRuns []metastore.IndexRunEntries, tasks int, err error)
}

// jobScope is the part of the ToC that one job replaces.
type jobScope struct {
	window  time.Time
	sources []indexRun
}

// sourcePaths returns the paths of every index object of the sources, in the
// order of the sources.
func (s jobScope) sourcePaths() []string {
	var paths []string
	for _, run := range s.sources {
		paths = append(paths, run.paths()...)
	}
	return paths
}

// jobEnv is what a job needs to run its tasks.
type jobEnv struct {
	tenant     string
	logger     log.Logger
	bucket     objstore.Bucket
	dispatcher *planDispatcher
	slots      *jobSlots

	// maxIndexTaskSize is the maximum uncompressed postings size of one
	// IndexMerge task. 0 disables the split.
	maxIndexTaskSize uint64
}

// run runs plans, which are tasks of class, and returns the artifacts of each
// plan in plan order.
func (env jobEnv) run(ctx context.Context, class priorityClass, plans []*physical.Plan) ([][]v2.ResultArtifact, error) {
	return env.dispatcher.RunAdmitted(ctx, env.tenant, plans, env.slots.admit(class))
}

// pairSplit replaces one index run, whose log objects form more than one log
// run, with one index run per log run. Each new index run and its log run
// form a pair. The split keeps the log data and rewrites only the index.
type pairSplit struct {
	window time.Time
	source indexRun

	// logRuns are the log runs of source, in key order.
	logRuns      []v2.Run
	shardBuckets indexpointers.ShardBuckets
}

func (s *pairSplit) replaces() jobScope {
	return jobScope{window: s.window, sources: []indexRun{s.source}}
}

func (s *pairSplit) logObjects() []string { return logObjectsOf(s.logRuns) }

func (s *pairSplit) priority() taskPriority {
	return taskPriority{class: priorityPairSplit, window: s.window}
}

func (s *pairSplit) kind() string { return pairJobKindSplit }

// execute runs one IndexFilter per log run. Each filter keeps the rows of the
// log objects of its log run. It returns an error if the new index runs do not
// hold the stats rows of every log section of the source.
func (s *pairSplit) execute(ctx context.Context, env jobEnv) ([]metastore.IndexRunEntries, int, error) {
	plans := make([]*physical.Plan, len(s.logRuns))
	planned := make([]plannedRun, len(s.logRuns))
	for i, run := range s.logRuns {
		task := indexFilterForUnmerged(s.source, []*compactionv2pb.RunRef{{Sections: run.Sections()}})
		plans[i] = task.physicalPlan(env.tenant, s.window)
		planned[i] = plannedRun{timeRange: task.inputTimeRange(), shardBuckets: s.shardBuckets}
	}
	artifacts, err := env.run(ctx, priorityPairSplit, plans)
	if err != nil {
		return nil, 0, fmt.Errorf("run index filter tasks: %w", err)
	}
	newRuns, err := outputRuns(planned, artifacts)
	if err != nil {
		return nil, 0, err
	}
	var outputPaths []string
	for _, run := range newRuns {
		for _, entry := range run {
			outputPaths = append(outputPaths, entry.Path)
		}
	}
	// A shutdown must not discard the finished filters, so the check ignores
	// it.
	if err := requireSameLogSections(context.WithoutCancel(ctx), env, s.source.paths(), outputPaths); err != nil {
		return nil, 0, err
	}
	return newRuns, len(plans), nil
}

// pairMerge merges K pairs of one size level into one pair. A LogMerge
// rewrites the log runs of the pairs into one log run. If the LogMerge tasks
// wrote more than one index, an IndexMerge of their indexes writes the one
// index run of the new log run.
type pairMerge struct {
	scope jobScope

	// level is the size level of the source pairs.
	level int

	// inputRange is the time range of the log sections of the source pairs.
	// Every entry of the new index run takes it.
	inputRange   timeRange
	shardBuckets indexpointers.ShardBuckets

	// logTasks are the LogMerge tasks, split by sort prefix. Their outputs
	// have disjoint key ranges, so together they form one log run. Each
	// task has its InputBytes set.
	logTasks []*compactionv2pb.TaskSpec
}

func (m *pairMerge) replaces() jobScope { return m.scope }

// logObjects returns the log objects of the LogMerge tasks. Every log section
// of the sources holds at least one sort prefix, so some task reads it.
func (m *pairMerge) logObjects() []string {
	var paths []string
	for _, task := range m.logTasks {
		for _, logRun := range task.Runs {
			for _, section := range logRun.Sections {
				paths = append(paths, section.ObjectPath)
			}
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func (m *pairMerge) priority() taskPriority {
	return taskPriority{class: priorityLogMerge, level: m.level, window: m.scope.window}
}

func (m *pairMerge) kind() string { return pairJobKindMerge }

// inputBytes returns the uncompressed log bytes that the LogMerge tasks read,
// as the index stats of the sources count them.
func (m *pairMerge) inputBytes() int64 {
	var total int64
	for _, task := range m.logTasks {
		total += task.InputBytes
	}
	return total
}

// execute runs the LogMerge tasks, then the IndexMerge of their indexes. Each
// LogMerge task writes one index of its own output.
//
// With one index from the LogMerge tasks, that index is the index of the new
// pair and no IndexMerge runs. It would only copy the index.
//
// execute returns an error if the stats rows of the LogMerge indexes do not
// count the input bytes of the merge, or if the new index run does not hold
// the stats rows of every log section of the LogMerge indexes.
func (m *pairMerge) execute(ctx context.Context, env jobEnv) ([]metastore.IndexRunEntries, int, error) {
	// A shutdown must not discard the finished LogMerge tasks, so the reads
	// ignore it. The dispatcher still stops retries.
	readCtx := context.WithoutCancel(ctx)
	logIndexes, err := m.mergeLogs(ctx, readCtx, env)
	if err != nil {
		return nil, 0, err
	}
	logTasks := len(m.logTasks)

	planned := plannedRun{timeRange: m.inputRange, shardBuckets: m.shardBuckets}
	if len(logIndexes) == 1 {
		run, err := planned.entries(logIndexes)
		if err != nil {
			return nil, 0, err
		}
		return []metastore.IndexRunEntries{run}, logTasks, nil
	}

	indexes, indexTasks, err := m.mergeIndexes(ctx, readCtx, env, artifactPaths(logIndexes))
	if err != nil {
		return nil, 0, err
	}
	run, err := planned.entries(indexes)
	if err != nil {
		return nil, 0, err
	}
	return []metastore.IndexRunEntries{run}, logTasks + indexTasks, nil
}

// mergeLogs runs the LogMerge tasks and returns the index that each task wrote
// of its output.
//
// It returns an error if the tasks wrote no index, or if the stats rows of
// their indexes do not count the input bytes of the merge. ctx stops the
// retries of the tasks, and readCtx bounds the reads.
func (m *pairMerge) mergeLogs(ctx, readCtx context.Context, env jobEnv) ([]v2.ResultArtifact, error) {
	plans := make([]*physical.Plan, len(m.logTasks))
	for i, spec := range m.logTasks {
		plans[i] = buildLogMergePlan(env.tenant, m.scope.window, spec)
	}
	results, err := env.run(ctx, priorityLogMerge, plans)
	if err != nil {
		return nil, fmt.Errorf("run log merge tasks: %w", err)
	}
	var logIndexes []v2.ResultArtifact
	for _, artifacts := range results {
		logIndexes = append(logIndexes, artifacts...)
	}
	if len(logIndexes) == 0 {
		return nil, errors.New("log merge tasks wrote no index")
	}
	if err := requireLogBytes(readCtx, env, artifactPaths(logIndexes), m.inputBytes()); err != nil {
		return nil, err
	}
	return logIndexes, nil
}

// mergeIndexes merges the indexes at paths into one index run and returns its
// artifacts in posting key order, and the number of tasks it ran. It splits
// the merge by posting key if env.maxIndexTaskSize is above 0.
//
// It reads the postings bounds of every index to build the task spec and the
// split points. It returns an error if the new index run does not hold the
// stats rows of every log section of the indexes at paths. ctx stops the
// retries of the tasks, and readCtx bounds the reads.
func (m *pairMerge) mergeIndexes(ctx, readCtx context.Context, env jobEnv, paths []string) ([]v2.ResultArtifact, int, error) {
	reads, err := readIndexes(readCtx, env.bucket, env.tenant, paths)
	if err != nil {
		return nil, 0, err
	}
	spec := &compactionv2pb.TaskSpec{Tenant: env.tenant}
	var sections []v2.Section[indexSortKey]
	for i, read := range reads {
		if len(read.sections) == 0 {
			return nil, 0, fmt.Errorf("index %s of a log merge task has no postings rows", paths[i])
		}
		refs := make([]*compactionv2pb.SectionRef, len(read.sections))
		for j, section := range read.sections {
			refs[j] = section.Ref
		}
		spec.Runs = append(spec.Runs, &compactionv2pb.RunRef{Sections: refs})
		sections = append(sections, read.sections...)
	}

	var splitter indexTaskSplitter = noopIndexTaskSplitter{}
	if env.maxIndexTaskSize > 0 {
		splitter = newSizeIndexTaskSplitter(env.logger, env.maxIndexTaskSize, sections)
	}
	tasks, err := splitter.split(spec)
	if err != nil {
		return nil, 0, fmt.Errorf("split index merge: %w", err)
	}
	merge := splitIndexMerge{spec: spec, tasks: tasks}
	if err := merge.validate(); err != nil {
		return nil, 0, fmt.Errorf("split index merge: %w", err)
	}

	plans := make([]*physical.Plan, len(tasks))
	for i, task := range tasks {
		plans[i] = buildIndexMergePlan(env.tenant, m.scope.window, task)
	}
	results, err := env.run(ctx, priorityIndexMerge, plans)
	if err != nil {
		return nil, 0, fmt.Errorf("run index merge tasks: %w", err)
	}
	artifacts, err := merge.join(results)
	if err != nil {
		return nil, 0, err
	}
	if err := requireSameLogSections(readCtx, env, paths, artifactPaths(artifacts)); err != nil {
		return nil, 0, err
	}
	return artifacts, len(plans), nil
}

// readIndexes reads the postings bounds of each index at paths with
// readIndexRun, up to indexReadConcurrency indexes at once. Each index is a
// run of its own. It returns the reads in the order of paths.
func readIndexes(ctx context.Context, bucket objstore.Bucket, tenant string, paths []string) ([]indexRunRead, error) {
	reads := make([]indexRunRead, len(paths))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(indexReadConcurrency)
	for i, path := range paths {
		group.Go(func() error {
			read, err := readIndexRun(groupCtx, bucket, tenant, runOfPaths(path))
			if err != nil {
				return fmt.Errorf("read index %s: %w", path, err)
			}
			reads[i] = read
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return reads, nil
}

// requireSameLogSections returns an error if the stats rows of the indexes at
// gotPaths do not hold the same log sections, with the same sizes, as the
// stats rows of the indexes at wantPaths.
//
// A log section without stats rows is not reachable through the index, so a
// swap to such an index would lose its data.
func requireSameLogSections(ctx context.Context, env jobEnv, wantPaths, gotPaths []string) error {
	want, err := logSectionSizes(ctx, env, wantPaths)
	if err != nil {
		return err
	}
	got, err := logSectionSizes(ctx, env, gotPaths)
	if err != nil {
		return err
	}
	if !maps.Equal(want, got) {
		return fmt.Errorf("the new indexes hold stats rows of %d log sections, but the old indexes hold %d, or their sizes differ", len(got), len(want))
	}
	return nil
}

// requireLogBytes returns an error if the stats rows of the indexes at paths
// do not count want uncompressed log bytes.
//
// Each LogMerge task checks that it read the input bytes of its key range. So
// a difference means that an index lacks the stats rows of some log data it
// wrote, and a swap to it would lose that data.
func requireLogBytes(ctx context.Context, env jobEnv, paths []string, want int64) error {
	sizes, err := logSectionSizes(ctx, env, paths)
	if err != nil {
		return err
	}
	var got int64
	for _, size := range sizes {
		got += size
	}
	if got != want {
		return fmt.Errorf("the indexes of the log merge count %d log bytes, but the merge read %d", got, want)
	}
	return nil
}

// logSectionSizes returns the uncompressed size of each log section in the
// stats rows of the indexes at paths. It returns an error if paths is empty,
// or if the indexes hold no stats rows of the tenant.
func logSectionSizes(ctx context.Context, env jobEnv, paths []string) (map[metastore.SectionKey]int64, error) {
	if len(paths) == 0 {
		return nil, errors.New("no indexes to read stats from")
	}
	read, err := readLogSections(ctx, env.bucket, env.tenant, runOfPaths(paths...))
	if err != nil {
		return nil, fmt.Errorf("read stats of indexes %v: %w", paths, err)
	}
	if len(read.sections) == 0 {
		return nil, fmt.Errorf("indexes %v hold no stats rows", paths)
	}
	sizes := make(map[metastore.SectionKey]int64, len(read.sections))
	for _, section := range read.sections {
		sizes[sectionKeyOf(section.Ref)] = section.Ref.UncompressedSize
	}
	return sizes, nil
}

// runOfPaths returns an index run of the index objects at paths, which the
// ToC need not list. The caller must pass at least one path, because an
// indexRun is never empty.
func runOfPaths(paths ...string) indexRun {
	entries := make([]indexpointers.IndexPointer, len(paths))
	for i, path := range paths {
		entries[i] = indexpointers.IndexPointer{Path: path}
	}
	return indexRun{entries: entries}
}

func artifactPaths(artifacts []v2.ResultArtifact) []string {
	paths := make([]string, len(artifacts))
	for i, artifact := range artifacts {
		paths[i] = artifact.Path
	}
	return paths
}

// logObjectsOf returns the paths of the log objects of runs, sorted and
// without duplicates.
func logObjectsOf(runs []v2.Run) []string {
	var paths []string
	for _, run := range runs {
		for _, section := range run.Sections() {
			paths = append(paths, section.ObjectPath)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}
