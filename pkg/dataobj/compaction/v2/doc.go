// Package compactionv2 implements a sorted run-based compaction planner.
//
// [CalculateRuns] groups a set of sections into sorted [Run]s and [Plan]
// batches those runs into ceil(P/K) tasks for downstream K-way merges.
package compactionv2
