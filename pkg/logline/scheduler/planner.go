package scheduler

import (
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// compactionKey identifies a set of indexes that may be compacted together.
// All six fields must match for indexes to be grouped into a single job.
// Unsharded indexes use ShardCount=0, ShardAlgorithm="", ShardValue=0.
type compactionKey struct {
	date             string
	shardCount       int
	shardAlgorithm   string
	shardValue       int
	version          string        // resolved format version (e.g. "v1")
	documentInterval time.Duration // time range per document ID; zero for legacy indexes
}

// compactionKeyFor returns the grouping key and true, or a zero key and false
// when the meta has an unrecognised version. Callers should skip metas that
// return false so that one bad version string does not block all compaction.
func compactionKeyFor(m store.Meta) (compactionKey, bool) {
	if err := logline.ValidateVersion(m.Version); err != nil {
		return compactionKey{}, false
	}
	return compactionKey{
		date:             m.Date,
		shardCount:       m.ShardCount,
		shardAlgorithm:   m.ShardAlgorithm,
		shardValue:       m.ShardValue,
		version:          m.Version,
		documentInterval: m.DocumentInterval,
	}, true
}

// CompactionPlanner selects groups of indexes to compact.
// Implementations are stateful — initialized with available indexes,
// each call to Plan() returns the next group until exhausted.
type CompactionPlanner interface {
	Plan() []string     // source IDs for the next compaction job, nil when exhausted
	Groups() [][]string // all planned groups (consumed and unconsumed); len() is the total job count for this plan
}

// maxSourcesPerJob caps how many source indexes a single compaction job may
// include. Each source requires ~1 MiB for the lazily-decoded term block and
// ~8 MiB for the postings block cache (2 blocks × 4 MiB), so 32 sources ≈ 288 MiB reader
// overhead. The dominant merge cost is now zstd encoder/decoder buffers and
// output bitmap accumulation, not per-source reader memory. Without this cap,
// the dateGroup planner can pack 100+ raw indexes into one job and OOM the worker.
const maxSourcesPerJob = 32

// dateGroupPlanner groups indexes by date and chooses source groups dynamically,
// using max_job_size_bytes as the primary constraint.
type dateGroupPlanner struct {
	groups  [][]string
	idx     int
	skipped int // indexes skipped because they could not be grouped under size limit
}

func newDateGroupPlanner(metas []store.Meta, maxJobSizeBytes int64) CompactionPlanner {
	byKey := make(map[compactionKey][]store.Meta)
	for _, m := range metas {
		k, ok := compactionKeyFor(m)
		if !ok {
			continue // skip metas with unrecognised versions
		}
		byKey[k] = append(byKey[k], m)
	}

	// Sort keys for deterministic output order.
	keys := make([]compactionKey, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ki, kj := keys[i], keys[j]
		if ki.date != kj.date {
			return ki.date < kj.date
		}
		if ki.version != kj.version {
			return ki.version < kj.version
		}
		if ki.documentInterval != kj.documentInterval {
			return ki.documentInterval < kj.documentInterval
		}
		if ki.shardCount != kj.shardCount {
			return ki.shardCount < kj.shardCount
		}
		if ki.shardAlgorithm != kj.shardAlgorithm {
			return ki.shardAlgorithm < kj.shardAlgorithm
		}
		return ki.shardValue < kj.shardValue
	})

	var groups [][]string
	var skipped int
	for _, k := range keys {
		ms := byKey[k]
		if len(ms) < 2 {
			continue
		}
		dateGroups, dateSkipped := planDateGroups(ms, maxJobSizeBytes)
		groups = append(groups, dateGroups...)
		skipped += dateSkipped
	}

	return &dateGroupPlanner{groups: groups, skipped: skipped}
}

type plannedGroup struct {
	ids       []string
	totalSize int64
}

func planDateGroups(metas []store.Meta, maxJobSizeBytes int64) ([][]string, int) {
	ms := append([]store.Meta(nil), metas...)
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].SizeBytes != ms[j].SizeBytes {
			return ms[i].SizeBytes < ms[j].SizeBytes
		}
		return ms[i].ID() < ms[j].ID()
	})

	var skipped int
	mergeable := make([]store.Meta, 0, len(ms))
	for _, m := range ms {
		if m.SizeBytes > maxJobSizeBytes {
			skipped++
			continue
		}
		mergeable = append(mergeable, m)
	}
	ms = mergeable

	if len(ms) < 2 {
		return nil, skipped
	}

	var planned []plannedGroup
	for len(ms) >= 2 {
		hi := len(ms) - 1
		if ms[0].SizeBytes+ms[hi].SizeBytes > maxJobSizeBytes {
			skipped++
			ms = ms[:hi]
			continue
		}

		anchor := ms[hi]
		budget := maxJobSizeBytes - anchor.SizeBytes

		// Count how many of the smallest candidates fit under the budget.
		// Cap at maxSourcesPerJob-1 (anchor counts as one source) to bound
		// per-job memory from reader overhead.
		maxCandidates := maxSourcesPerJob - 1
		candidateCount := 0
		var candidateSize int64
		for candidateCount < hi && candidateCount < maxCandidates && candidateSize+ms[candidateCount].SizeBytes <= budget {
			candidateSize += ms[candidateCount].SizeBytes
			candidateCount++
		}

		if candidateCount < 1 {
			skipped++
			ms = ms[:hi]
			continue
		}

		// Select the most time-clustered consecutive run of candidateCount
		// items from the candidates. Sort candidates by MinLogTs, then pick
		// the consecutive window that maximizes time-range overlap.
		candidates := append([]store.Meta(nil), ms[:candidateCount]...)
		selected := selectTimeClustered(candidates, budget)

		ids := make([]string, 0, len(selected)+1)
		ids = append(ids, anchor.ID())
		total := anchor.SizeBytes
		consumed := make(map[string]bool, len(selected))
		for _, m := range selected {
			ids = append(ids, m.ID())
			total += m.SizeBytes
			consumed[m.ID()] = true
		}

		if len(ids) < 2 {
			skipped++
			ms = ms[:hi]
			continue
		}

		sort.Strings(ids)
		planned = append(planned, plannedGroup{
			ids:       ids,
			totalSize: total,
		})

		// Remove anchor and consumed candidates.
		next := make([]store.Meta, 0, len(ms)-len(consumed)-1)
		for i := 0; i < hi; i++ {
			if !consumed[ms[i].ID()] {
				next = append(next, ms[i])
			}
		}
		ms = next
	}

	skipped += len(ms)

	sortPlannedGroups(planned)
	groups := make([][]string, len(planned))
	for i, g := range planned {
		groups[i] = g.ids
	}

	return groups, skipped
}

// selectTimeClustered sorts candidates by MinLogTs and returns all of them.
// The time-sorting ensures that when the caller packs them into a job,
// co-located candidates (from the same flush window across different builders)
// end up together, maximizing time-range overlap and dedup benefit.
func selectTimeClustered(candidates []store.Meta, _ int64) []store.Meta {
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].MinLogTs.Equal(candidates[j].MinLogTs) {
			return candidates[i].MinLogTs.Before(candidates[j].MinLogTs)
		}
		return candidates[i].ID() < candidates[j].ID()
	})
	return candidates
}

func sortPlannedGroups(groups []plannedGroup) {
	sort.Slice(groups, func(i, j int) bool {
		// Prioritize groups with higher reduction density first.
		iReduction := int64(len(groups[i].ids) - 1)
		jReduction := int64(len(groups[j].ids) - 1)
		iDensityLeft := groups[i].totalSize * jReduction
		jDensityLeft := groups[j].totalSize * iReduction
		if iDensityLeft != jDensityLeft {
			return iDensityLeft < jDensityLeft
		}
		if groups[i].totalSize != groups[j].totalSize {
			return groups[i].totalSize < groups[j].totalSize
		}
		if len(groups[i].ids) != len(groups[j].ids) {
			return len(groups[i].ids) > len(groups[j].ids)
		}
		for k := 0; k < len(groups[i].ids) && k < len(groups[j].ids); k++ {
			if groups[i].ids[k] != groups[j].ids[k] {
				return groups[i].ids[k] < groups[j].ids[k]
			}
		}
		return len(groups[i].ids) < len(groups[j].ids)
	})
}

func (p *dateGroupPlanner) Plan() []string {
	if p.idx >= len(p.groups) {
		return nil
	}
	group := p.groups[p.idx]
	p.idx++
	return group
}

func (p *dateGroupPlanner) Groups() [][]string { return p.groups }
