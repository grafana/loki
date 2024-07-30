// Package chunks provides functionality for efficient storage and retrieval of log data.
//
// The chunks package implements a compact and performant way to store and access
// log entries. It uses various compression and encoding techniques to minimize
// storage requirements while maintaining fast access times.
//
// Key features:
//   - Efficient chunk writing with multiple encoding options
//   - Fast chunk reading with iterators for forward and backward traversal
//   - Support for time-based filtering of log entries
//   - Integration with Loki's log query language (LogQL) for advanced filtering and processing
//
// Main types and functions:
//   - WriteChunk: Writes log entries to a compressed chunk format
//   - NewChunkReader: Creates a reader for parsing and accessing chunk data
//   - NewEntryIterator: Provides an iterator for efficient traversal of log entries in a chunk
//
// This package is designed to work seamlessly with other components of the Loki
// log aggregation system, providing a crucial layer for data storage and retrieval.
package chunks
