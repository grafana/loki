package scheduler

import (
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logline/store"
)

// overlapPlanner groups indexes by time-range overlap using a sweep-line algorithm,
// then greedily packs consecutive time-sorted indexes within each cluster.
// Consecutive indexes have the highest mutual overlap, maximizing dedup benefit
// during compaction (output ratio ~0.82 for overlapping vs ~0.95 for disjoint).
// Groups contain up to maxSourcesPerJob sources (bounded by maxJobSizeBytes).
type overlapPlanner struct {
	groups  [][]string
	idx     int
	skipped int // indexes skipped because they were oversized or isolated singletons
}

func newOverlapPlanner(metas []store.Meta, maxJobSizeBytes int64) CompactionPlanner {
	byKey := make(map[compactionKey][]store.Meta)
	for _, m := range metas {
		k, ok := compactionKeyFor(m)
		if !ok {
			continue // skip metas that cannot be merged
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
		if ki.shardCount != kj.shardCount {
			return ki.shardCount < kj.shardCount
		}
		if ki.shardAlgorithm != kj.shardAlgorithm {
			return ki.shardAlgorithm < kj.shardAlgorithm
		}
		return ki.shardValue < kj.shardValue
	})

	// Collect groups per date so we can interleave across dates.
	// Within a date, groups are already sorted by reduction density.
	dateGroups := make(map[string][][]string)
	var skipped int
	for _, k := range keys {
		ms := byKey[k]
		if len(ms) < 2 {
			skipped += len(ms)
			continue
		}
		groups, keySkipped := planOverlapGroups(ms, maxJobSizeBytes)
		if len(groups) > 0 {
			dateGroups[k.date] = append(dateGroups[k.date], groups...)
		}
		skipped += keySkipped
	}

	// Sort dates newest-first so the current day gets priority in each
	// round-robin pass. This prevents today from being starved when
	// older dates continuously receive new indexes.
	dates := make([]string, 0, len(dateGroups))
	for d := range dateGroups {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	// Round-robin across dates: take one group from each date per pass.
	// Newest date goes first in each round, ensuring today's indexes get
	// compacted while older dates still make progress.
	var allGroups [][]string
	for len(dates) > 0 {
		remaining := dates[:0]
		for _, d := range dates {
			gs := dateGroups[d]
			allGroups = append(allGroups, gs[0])
			if len(gs) > 1 {
				dateGroups[d] = gs[1:]
				remaining = append(remaining, d)
			}
		}
		dates = remaining
	}

	return &overlapPlanner{groups: allGroups, skipped: skipped}
}

func (p *overlapPlanner) Plan() []string {
	if p.idx >= len(p.groups) {
		return nil
	}
	group := p.groups[p.idx]
	p.idx++
	return group
}

// GroupCount returns the total number of planned compaction jobs.
func (p *overlapPlanner) Groups() [][]string { return p.groups }

// planOverlapGroups clusters metas by time-range overlap, then greedily packs
// consecutive time-sorted indexes within each cluster into multi-source jobs.
// Consecutive indexes have the highest mutual overlap, so grouping them together
// maximizes dedup benefit while reducing total index count aggressively.
//
// After overlap-based grouping, any remaining ungrouped indexes (singleton
// clusters, trailing singletons from packing) are collected and packed by time
// proximity. This ensures disjoint indexes on settled dates still get compacted
// to reduce total index count, even when there is no time overlap to exploit.
func planOverlapGroups(metas []store.Meta, maxJobSizeBytes int64) ([][]string, int) {
	// Copy and sort for deterministic sweep: MinLogTs asc, MaxLogTs asc, ID asc.
	ms := append([]store.Meta(nil), metas...)
	sort.Slice(ms, func(i, j int) bool {
		if !ms[i].MinLogTs.Equal(ms[j].MinLogTs) {
			return ms[i].MinLogTs.Before(ms[j].MinLogTs)
		}
		if !ms[i].MaxLogTs.Equal(ms[j].MaxLogTs) {
			return ms[i].MaxLogTs.Before(ms[j].MaxLogTs)
		}
		return ms[i].ID() < ms[j].ID()
	})

	clusters := buildOverlapClusters(ms)

	var groups []plannedGroup
	var ungrouped []store.Meta
	var skipped int

	for _, cluster := range clusters {
		if len(cluster) < 2 {
			m := cluster[0]
			// Zero-timestamp indexes are unreliable — never group them.
			if m.MinLogTs.IsZero() || m.MaxLogTs.IsZero() {
				skipped++
			} else {
				ungrouped = append(ungrouped, m)
			}
			continue
		}
		g, leftover := packConsecutiveGroups(cluster, maxJobSizeBytes)
		groups = append(groups, g...)
		ungrouped = append(ungrouped, leftover...)
	}

	// Second pass: pack ungrouped indexes (disjoint singletons, trailing
	// singletons from oversized clusters) by time proximity. No overlap
	// benefit, but reduces total index count on settled dates.
	if len(ungrouped) >= 2 {
		// Already time-sorted from the initial sort.
		g, finalLeftover := packConsecutiveGroups(ungrouped, maxJobSizeBytes)
		groups = append(groups, g...)
		ungrouped = finalLeftover
	}

	// Reuse the existing reduction-density sort for job priority within
	// a planning round. Overlap groups naturally sort first (smaller total
	// size for comparable source counts = better density).
	sortPlannedGroups(groups)

	result := make([][]string, len(groups))
	for i, g := range groups {
		result[i] = g.ids
	}

	return result, skipped + len(ungrouped)
}

// packConsecutiveGroups greedily packs consecutive time-sorted indexes into
// multi-source groups. Consecutive indexes have the highest mutual time overlap,
// maximizing dedup benefit. Groups are bounded by maxJobSizeBytes and
// maxSourcesPerJob. This reduces total index count faster than pairing (N→1
// per group instead of 2→1) while preserving time co-location.
//
// Returns the planned groups and any leftover metas that could not be grouped
// (oversized individuals, trailing singletons).
func packConsecutiveGroups(sorted []store.Meta, maxJobSizeBytes int64) ([]plannedGroup, []store.Meta) {
	var leftover []store.Meta
	mergeable := make([]store.Meta, 0, len(sorted))
	for _, m := range sorted {
		if m.SizeBytes > maxJobSizeBytes {
			leftover = append(leftover, m)
			continue
		}
		mergeable = append(mergeable, m)
	}

	if len(mergeable) < 2 {
		leftover = append(leftover, mergeable...)
		return nil, leftover
	}

	var groups []plannedGroup
	i := 0
	for i < len(mergeable) {
		// Start a new group with mergeable[i].
		ids := []string{mergeable[i].ID()}
		total := mergeable[i].SizeBytes
		j := i + 1

		// Greedily extend with consecutive indexes that fit.
		for j < len(mergeable) && len(ids) < maxSourcesPerJob {
			if total+mergeable[j].SizeBytes > maxJobSizeBytes {
				break
			}
			ids = append(ids, mergeable[j].ID())
			total += mergeable[j].SizeBytes
			j++
		}

		if len(ids) >= 2 {
			sort.Strings(ids)
			groups = append(groups, plannedGroup{
				ids:       ids,
				totalSize: total,
			})
		} else {
			leftover = append(leftover, mergeable[i])
		}
		i = j
	}

	return groups, leftover
}

// buildOverlapClusters partitions metas into overlap clusters using a sweep-line.
// Input must be sorted by MinLogTs ascending. Each cluster contains all indexes
// whose time range overlaps with at least one other index in the same cluster
// (connected-components via running-max end time).
// Indexes with zero MinLogTs or MaxLogTs are placed in singleton clusters because
// their time range is unreliable — mixing them with real clusters would corrupt
// the sweep invariant.
func buildOverlapClusters(sorted []store.Meta) [][]store.Meta {
	var clusters [][]store.Meta
	var current []store.Meta
	var clusterMaxLogTs time.Time // running max of MaxLogTs seen in current cluster

	for _, m := range sorted {
		// Zero-timestamp indexes have no reliable time range. Force them into
		// singleton clusters so they never corrupt real cluster formation.
		if m.MinLogTs.IsZero() || m.MaxLogTs.IsZero() {
			if len(current) > 0 {
				clusters = append(clusters, current)
				current = nil
				clusterMaxLogTs = time.Time{}
			}
			clusters = append(clusters, []store.Meta{m})
			continue
		}

		if len(current) == 0 {
			current = append(current, m)
			clusterMaxLogTs = m.MaxLogTs
			continue
		}

		if !m.MinLogTs.After(clusterMaxLogTs) {
			// Overlaps the current cluster: extend it.
			current = append(current, m)
			if m.MaxLogTs.After(clusterMaxLogTs) {
				clusterMaxLogTs = m.MaxLogTs
			}
		} else {
			// Gap detected: finalize the current cluster and start a new one.
			clusters = append(clusters, current)
			current = []store.Meta{m}
			clusterMaxLogTs = m.MaxLogTs
		}
	}

	if len(current) > 0 {
		clusters = append(clusters, current)
	}

	return clusters
}
