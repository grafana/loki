// Package compactionv2 implements a sorted run-based compaction planner.
//
// Given a set of sections with (min_key, max_key, stable_id) sort-key bounds, Plan
// produces a deterministic list of sorted piles (runs of non-overlapping
// sections) and groups them into ⌈P/K⌉ task batches for downstream K-way merges.
package compactionv2
