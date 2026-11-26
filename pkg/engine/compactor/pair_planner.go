package compactor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// pair is an index run whose log objects form one log run. The coupled
// planner merges K pairs of one size level into one new pair.
type pair struct {
	indexRun indexRun
	layout   streams.Layout

	// logRun holds the log sections of indexRun, in key order.
	logRun v2.Run

	// logSections are the planner sections of the log sections of indexRun,
	// with the size of each sort prefix.
	logSections []v2.Section[logSortPrefix]
}

// pairCandidate is an index run and the log sections that its stats sections
// list.
type pairCandidate struct {
	indexRun              indexRun
	discoveredLogSections logSectionsRead
}

// pairWindowPlan is the work that the coupled planner found in one window.
type pairWindowPlan struct {
	// indexRuns is the number of index runs in the ToC of the window,
	// including the runs of jobs that still run.
	indexRuns int

	// readFailures is the number of index runs that planning skipped,
	// because a read of their stats failed. A later scan reads them again.
	readFailures int

	// pairs are the pairs that planning found, in merges or not.
	pairs []pair

	// skipped are the index runs that planning skipped, because they have
	// no stats rows or share a log object.
	skipped []skippedIndex

	// legacy are the index runs over more than one log run that planning
	// left as they are, because the window is not the current one.
	legacy []legacyIndexRun

	jobs []pairJob
}

// legacyPolicy decides what planning does with a legacy index run: an index
// run whose log objects form more than one log run. The phased planner
// writes such index runs, and the coupled planner never does.
type legacyPolicy int

const (
	// splitLegacy splits each legacy index run into one pair per log run.
	// Planning uses it in the current window, which still gets data and
	// queries, so its index runs converge.
	splitLegacy legacyPolicy = iota

	// ignoreLegacy leaves legacy index runs as they are. Planning uses it
	// in older windows. Their legacy index runs no longer change, and the
	// windows leave the planned range soon, so a split would only add index
	// runs.
	ignoreLegacy
)

// legacyIndexRun is a legacy index run that planning left as it is.
type legacyIndexRun struct {
	indexRun indexRun
	logRuns  []v2.Run
}

// Values of the reason label of pairSkippedIndexesTotal.
const (
	pairSkipReasonReadFailed      = "read_failed"
	pairSkipReasonNoStats         = "no_stats"
	pairSkipReasonSharedLogObject = "shared_log_object"
	pairSkipReasonUnknownPair     = "unknown_pair"
)

// pairPlanner plans the jobs of the coupled planner. It reads ToCs and the
// stats of index objects, and writes nothing.
type pairPlanner struct {
	logger   log.Logger
	bucket   objstore.Bucket
	metrics  *coordinatorMetrics
	strategy *v2.SizeLeveledStrategy

	// minCompactionSize is the smallest total log size of a layout group
	// that planning merges, in uncompressed bytes.
	minCompactionSize uint64

	// maxLogTaskSize is the largest uncompressed input of one LogMerge task.
	// 0 disables the split.
	maxLogTaskSize uint64

	// maxKeySize is the largest uncompressed input of one sort prefix in a
	// merge. 0 disables the check.
	maxKeySize uint64
}

// planWindow plans the jobs of one window of tenant. It skips every index
// run that leased holds, because a planned job replaces it.
//
// The caller must take leased before planWindow reads the ToC. A job
// releases its leases only after its swap. If leased came after the read, a
// swap could finish in between, and the read would list removed index runs as
// free.
//
// planWindow plans two kinds of jobs:
//   - a pairSplit for each legacy index run, whose log objects form more than
//     one log run, if legacy is splitLegacy,
//   - a pairMerge for each group of K pairs of one size level and stream
//     layout.
//
// With ignoreLegacy, it leaves legacy index runs as they are and reports them
// in the plan.
//
// A window without a ToC has no work. planWindow skips and counts a
// corrupt index run, so it does not block the other index runs of the window.
// If the stats of any index run fail to read, it counts them and plans no
// jobs in the window.
func (p *pairPlanner) planWindow(ctx context.Context, tenant string, window time.Time, leased leaseSnapshot, legacy legacyPolicy) (pairWindowPlan, error) {
	entries, err := loadTenantIndexes(ctx, p.bucket, window, tenant)
	if p.bucket.IsObjNotFoundErr(err) {
		return pairWindowPlan{}, nil
	}
	if err != nil {
		return pairWindowPlan{}, fmt.Errorf("load ToC: %w", err)
	}

	indexRuns := groupIndexRuns(entries)
	freeIndexRuns := slices.DeleteFunc(slices.Clone(indexRuns), func(indexRun indexRun) bool {
		return slices.ContainsFunc(indexRun.paths(), leased.holdsIndex)
	})
	logger := log.With(p.logger, "tenant", tenant, "window", window)
	candidates, readFailures, err := p.readCandidates(ctx, logger, tenant, window, freeIndexRuns)
	if err != nil {
		return pairWindowPlan{}, err
	}

	plan := pairWindowPlan{indexRuns: len(indexRuns), readFailures: readFailures}
	if readFailures > 0 {
		// The log objects of an index run that failed to read are unknown.
		// A job could rewrite a log object that the index run shares, so
		// the window waits for a scan that reads every index run.
		level.Warn(logger).Log("msg", "pair-compaction: planning no jobs in window, because some indexes failed to read", "failed_indexes", readFailures)
		return plan, nil
	}

	classified := p.classifyCandidates(logger, tenant, window, candidates, leased, legacy)
	plan.pairs, plan.skipped, plan.legacy = classified.pairs, classified.skipped, classified.legacy
	for _, split := range classified.splits {
		plan.jobs = append(plan.jobs, split)
	}
	for _, merge := range p.planMerges(logger, tenant, window, classified.pairs) {
		plan.jobs = append(plan.jobs, merge)
	}
	level.Debug(logger).Log("msg", "pair-compaction: planned window", "index_runs", len(indexRuns), "leased_index_runs", len(indexRuns)-len(freeIndexRuns), "pairs", len(classified.pairs), "splits", len(classified.splits), "legacy_index_runs", len(classified.legacy), "jobs", len(plan.jobs))
	return plan, nil
}

// readCandidates reads the stats sections of every index run in indexRuns, up
// to indexReadConcurrency index runs at once. It returns the candidates in the
// order of indexRuns, and the number of index runs whose read failed.
//
// It skips and counts an index run that is corrupt or fails to read. It
// returns an error only when ctx is done.
func (p *pairPlanner) readCandidates(ctx context.Context, logger log.Logger, tenant string, window time.Time, indexRuns []indexRun) ([]pairCandidate, int, error) {
	candidates := make([]pairCandidate, len(indexRuns))
	skipped := make([]bool, len(indexRuns))
	failed := make([]bool, len(indexRuns))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(indexReadConcurrency)
	for i, indexRun := range indexRuns {
		group.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}
			logSections, err := readLogSections(groupCtx, p.bucket, tenant, indexRun)
			switch {
			case errors.Is(err, errCorruptIndex):
				skipCorruptIndex(logger, p.metrics, tenant, window, indexRun.String(), phaseLabelPairMerge, err)
				skipped[i] = true
			case err != nil:
				if ctx.Err() != nil {
					return ctx.Err()
				}
				level.Warn(logger).Log("msg", "pair-compaction: skipping index, because its stats failed to read", "index", indexRun.String(), "err", err)
				p.metrics.pairSkippedIndexesTotal.WithLabelValues(tenant, pairSkipReasonReadFailed).Inc()
				skipped[i], failed[i] = true, true
			default:
				candidates[i] = pairCandidate{indexRun: indexRun, discoveredLogSections: logSections}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, 0, err
	}

	out := candidates[:0]
	var readFailures int
	for i, candidate := range candidates {
		if failed[i] {
			readFailures++
		}
		if !skipped[i] {
			out = append(out, candidate)
		}
	}
	return out, readFailures, nil
}

// classifyCandidates splits candidates into pairs, and into the legacy index
// runs whose log objects form more than one log run. With splitLegacy it plans
// a split of each legacy index run, and with ignoreLegacy it leaves them.
//
// It skips, counts and logs as errors these index runs, which stay in the ToC
// as they are:
//   - an index run without stats rows, because planning can't tell its log
//     runs,
//   - every index run that shares a log object with another candidate, or
//     with an index run of a planned job.
//
// Neither case happens unless an index object or the ToC is wrong.
//
// Each log object belongs to one index run. A job that rewrote a shared
// object would leave the other index run pointing at the old object, so the
// rows of the object would be reachable twice.
//
// The check sees only the log objects in stats rows. It can't see the log
// objects of a corrupt index run, or of an index run without stats rows.
// classifiedCandidates are the candidates of one window, sorted by what
// planning does with them.
type classifiedCandidates struct {
	pairs   []pair
	splits  []*pairSplit
	legacy  []legacyIndexRun
	skipped []skippedIndex
}

func (p *pairPlanner) classifyCandidates(logger log.Logger, tenant string, window time.Time, candidates []pairCandidate, leased leaseSnapshot, legacy legacyPolicy) classifiedCandidates {
	var out classifiedCandidates
	owners := make(map[string][]indexRun)
	for _, candidate := range candidates {
		for _, path := range candidate.logObjects() {
			owners[path] = append(owners[path], candidate.indexRun)
		}
	}
	skip := func(skip skippedIndex, msg string) {
		level.Error(logger).Log(append([]any{"msg", msg}, skip.logFields()...)...)
		p.metrics.pairSkippedIndexesTotal.WithLabelValues(tenant, skip.reason).Inc()
		out.skipped = append(out.skipped, skip)
	}

	for _, candidate := range candidates {
		logSections := candidate.discoveredLogSections
		if len(logSections.sections) == 0 {
			skip(skippedIndex{indexRun: candidate.indexRun, reason: pairSkipReasonNoStats}, "pair-compaction: skipping index without stats rows")
			continue
		}
		if shared, ok := sharedLogObject(candidate, owners, leased); ok {
			skip(shared, "pair-compaction: skipping index that shares a log object with another index")
			continue
		}

		logRuns := v2.CalculateRuns(logSections.sections, compareLogSortPrefix)
		if len(logRuns) > 1 && legacy == ignoreLegacy {
			level.Info(logger).Log("msg", "pair-compaction: leaving legacy index as it is, because the window is not the current one", "index", candidate.indexRun.String(), "log_runs", len(logRuns))
			out.legacy = append(out.legacy, legacyIndexRun{indexRun: candidate.indexRun, logRuns: logRuns})
			continue
		}
		if len(logRuns) > 1 {
			// An IndexFilter reads the source index run as one sequence in
			// posting key order, so a run that has no such order can't be
			// split. No retry fixes it.
			if _, err := sourcePathsInKeyOrder(candidate.indexRun); err != nil {
				skipCorruptIndex(logger, p.metrics, tenant, window, candidate.indexRun.String(), phaseLabelPairMerge, err)
				continue
			}
			out.splits = append(out.splits, &pairSplit{
				window:       window,
				source:       candidate.indexRun,
				logRuns:      logRuns,
				shardBuckets: indexpointers.NewShardBuckets(logSections.layout.ShardLayout()),
			})
			continue
		}
		out.pairs = append(out.pairs, pair{
			indexRun:    candidate.indexRun,
			layout:      logSections.layout,
			logRun:      logRuns[0],
			logSections: logSections.sections,
		})
	}
	return out
}

// skippedIndex is an index run that classifyCandidates skipped.
type skippedIndex struct {
	indexRun indexRun

	// reason is a value of the reason label of pairSkippedIndexesTotal.
	reason string

	// sharedLogObject is the first log object of indexRun that another
	// index run also holds. It is empty unless reason is
	// pairSkipReasonSharedLogObject.
	sharedLogObject string

	// otherIndexRuns are the other candidates that hold sharedLogObject.
	otherIndexRuns []indexRun

	// leased reports whether a planned job holds sharedLogObject.
	leased bool
}

// maxLoggedOtherIndexRuns caps the other index runs that a skip log line
// names.
const maxLoggedOtherIndexRuns = 4

func (s skippedIndex) logFields() []any {
	fields := []any{"index", s.indexRun.String(), "reason", s.reason}
	if s.sharedLogObject == "" {
		return fields
	}
	others := make([]string, 0, min(len(s.otherIndexRuns), maxLoggedOtherIndexRuns))
	for _, other := range s.otherIndexRuns[:min(len(s.otherIndexRuns), maxLoggedOtherIndexRuns)] {
		others = append(others, other.String())
	}
	return append(fields,
		"log_object", s.sharedLogObject,
		"other_indexes", strings.Join(others, ","),
		"other_index_count", len(s.otherIndexRuns),
		"planned_job_holds_log_object", s.leased,
	)
}

// sharedLogObject returns the skip of candidate if one of its log objects
// belongs to another candidate in owners, or to a planned job in leased.
func sharedLogObject(candidate pairCandidate, owners map[string][]indexRun, leased leaseSnapshot) (skippedIndex, bool) {
	for _, path := range candidate.logObjects() {
		var others []indexRun
		for _, owner := range owners[path] {
			if owner.planningUnit() != candidate.indexRun.planningUnit() {
				others = append(others, owner)
			}
		}
		if len(others) == 0 && !leased.holdsLogObject(path) {
			continue
		}
		return skippedIndex{
			indexRun:        candidate.indexRun,
			reason:          pairSkipReasonSharedLogObject,
			sharedLogObject: path,
			otherIndexRuns:  others,
			leased:          leased.holdsLogObject(path),
		}, true
	}
	return skippedIndex{}, false
}

// logObjects returns the paths of the log objects of c, sorted and without
// duplicates.
func (c pairCandidate) logObjects() []string {
	paths := make([]string, 0, len(c.discoveredLogSections.sections))
	for _, section := range c.discoveredLogSections.sections {
		paths = append(paths, section.Ref.ObjectPath)
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// planMerges plans the merges of pairs. Pairs of different stream layouts
// never merge.
//
// In each layout group, the size-leveled strategy takes groups of exactly K
// pairs from one level. planMerges skips a layout group whose pairs together
// are below minCompactionSize, and a merge with a sort prefix above
// maxKeySize. The pairs of a skipped merge stay as they are.
func (p *pairPlanner) planMerges(logger log.Logger, tenant string, window time.Time, pairs []pair) []*pairMerge {
	groups := make(map[layoutKey][]pair)
	for _, source := range pairs {
		key := newLayoutKey(source.layout)
		groups[key] = append(groups[key], source)
	}
	keys := make([]layoutKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b layoutKey) int { return cmp.Compare(a.String(), b.String()) })

	var merges []*pairMerge
	for _, key := range keys {
		merges = append(merges, p.planLayoutGroupMerges(log.With(logger, "layout", key), tenant, window, groups[key])...)
	}
	return merges
}

// planLayoutGroupMerges plans the merges of pairs, which share one stream
// layout.
func (p *pairPlanner) planLayoutGroupMerges(logger log.Logger, tenant string, window time.Time, pairs []pair) []*pairMerge {
	if len(pairs) == 0 {
		return nil
	}
	logRuns := make([]v2.Run, len(pairs))
	indexRunOfObject := make(map[string]indexRun, len(pairs))
	var logSections []v2.Section[logSortPrefix]
	for i, source := range pairs {
		logRuns[i] = source.logRun
		logSections = append(logSections, source.logSections...)
		for _, section := range source.logSections {
			indexRunOfObject[section.Ref.ObjectPath] = source.indexRun
		}
	}
	if v2.BelowMinCompactionSize(logRuns, p.minCompactionSize) {
		level.Debug(logger).Log("msg", "pair-compaction: layout group below the minimum compaction size, skipping", "pairs", len(pairs))
		return nil
	}

	specs, _ := p.strategy.Plan(logRuns, tenant)
	keys := newSectionKeySizes(logSections)
	var splitter logTaskSplitter = newNoopLogTaskSplitter(keys)
	if p.maxLogTaskSize > 0 {
		splitter = newSizeLogTaskSplitter(logger, p.maxLogTaskSize, keys)
	}
	shardBuckets := indexpointers.NewShardBuckets(pairs[0].layout.ShardLayout())

	var merges []*pairMerge
	for _, spec := range specs {
		if kept, _ := skipOversizedMerges(logger, p.metrics, p.maxKeySize, tenant, keys, []*compactionv2pb.TaskSpec{spec}); len(kept) == 0 {
			continue
		}
		sourceIndexRuns, err := sourcesOfMerge(spec, indexRunOfObject)
		if err != nil {
			level.Error(logger).Log("msg", "pair-compaction: skipping merge", "err", err)
			p.metrics.pairSkippedIndexesTotal.WithLabelValues(tenant, pairSkipReasonUnknownPair).Add(float64(len(spec.Runs)))
			continue
		}
		merges = append(merges, &pairMerge{
			scope:        jobScope{window: window, sources: sourceIndexRuns},
			level:        p.strategy.Level(runRefSize(spec.Runs[0])),
			inputRange:   runsTimeRange(spec.Runs),
			shardBuckets: shardBuckets,
			logTasks:     splitter.split(spec),
		})
	}
	return merges
}

// sourcesOfMerge returns the index runs of the log runs of spec, in the order
// of the log runs. indexRunOfObject maps each log object path to its index
// run. It returns an error if spec has no log runs, or if a log run has no
// sections or its first section belongs to no index run.
func sourcesOfMerge(spec *compactionv2pb.TaskSpec, indexRunOfObject map[string]indexRun) ([]indexRun, error) {
	if len(spec.Runs) == 0 {
		return nil, errors.New("the merge has no runs")
	}
	sources := make([]indexRun, 0, len(spec.Runs))
	for i, logRun := range spec.Runs {
		if len(logRun.Sections) == 0 {
			return nil, fmt.Errorf("run %d of the merge has no sections", i)
		}
		source, ok := indexRunOfObject[logRun.Sections[0].ObjectPath]
		if !ok {
			return nil, fmt.Errorf("log object %s of the merge belongs to no pair", logRun.Sections[0].ObjectPath)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

// runRefSize returns the uncompressed size of the sections of logRun.
func runRefSize(logRun *compactionv2pb.RunRef) uint64 {
	var size uint64
	for _, section := range logRun.Sections {
		size += uint64(section.UncompressedSize)
	}
	return size
}
