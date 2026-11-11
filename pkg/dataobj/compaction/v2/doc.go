// Package compactionv2 plans K-way compaction from sections with inclusive
// ordering-key bounds. CalculateRuns keeps units, by default physical objects,
// intact while forming strict merge inputs. IsConverged treats touching-only unit boundaries
// as a terminal fixed point. Plan batches P runs into ceil(P/K) tasks that each
// produce one output sequence.
package compactionv2
