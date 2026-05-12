// Package compactionv2 implements the patience-sort run-based compaction planner
// described in the dataobj-compactor design spec (revision 5.14).
//
// Given a set of sections with (min_key, max_key, stable_id) sort-key bounds, Plan
// produces a deterministic list of patience-sort piles (runs of non-overlapping
// sections) and groups them into ⌈P/K⌉ task batches for downstream K-way merges.
//
// This package is intentionally parallel to the v1 planner at pkg/dataobj/compaction/.
// It does NOT import or extend v1 types; v2 introduces its own proto definitions at
// pkg/dataobj/compaction/v2/proto/.
package compactionv2
