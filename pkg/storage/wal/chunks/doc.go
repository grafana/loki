// Package chunks provides functionality for efficient storage and retrieval of log data and metrics.
//
// The chunks package implements a compact and performant way to store and access
// log entries and metric samples. It uses various compression and encoding techniques to minimize
// storage requirements while maintaining fast access times.
//
// Key features:
//   - Efficient chunk writing with multiple encoding options
//   - Fast chunk reading with iterators for forward and backward traversal
//   - Support for time-based filtering of log entries and metric samples
//   - Integration with Loki's log query language (LogQL) for advanced filtering and processing
//   - Separate iterators for log entries and metric samples
//
// Main types and functions:
//   - WriteChunk: Writes log entries to a compressed chunk format
//   - NewChunkReader: Creates a reader for parsing and accessing chunk data
//   - NewEntryIterator: Provides an iterator for efficient traversal of log entries in a chunk
//   - NewSampleIterator: Provides an iterator for efficient traversal of metric samples in a chunk
//
// Entry Iterator:
// The EntryIterator allows efficient traversal of log entries within a chunk. It supports
// both forward and backward iteration, time-based filtering, and integration with LogQL pipelines
// for advanced log processing.
//
// Sample Iterator:
// The SampleIterator enables efficient traversal of metric samples within a chunk. It supports
// time-based filtering and integration with LogQL extractors for advanced metric processing.
// This iterator is particularly useful for handling numeric data extracted from logs or
// pre-aggregated metrics.
//
// Both iterators implement methods for accessing the current entry or sample, checking for errors,
// and retrieving associated labels and stream hashes.
//
// This package is designed to work seamlessly with other components of the Loki
// log aggregation system, providing a crucial layer for data storage and retrieval of
// both logs and metrics.
package chunks
