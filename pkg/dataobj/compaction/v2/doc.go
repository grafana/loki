// Package compactionv2 plans K-way compaction from sections with inclusive
// ordering-key bounds. CalculateRuns forms strict merge inputs, while
// IsConverged treats touching-only boundaries as a terminal fixed point. Plan
// batches P runs into ceil(P/K) tasks that each produce one output sequence.
package compactionv2
