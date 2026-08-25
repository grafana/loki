// Package buffer describes contigous regions of dataset memory. Each buffer
// holds arbitrary data, which is interpreted contextually depending on where
// the buffer is stored.
package buffer

// Invalid refers to an invalid buffer ID.
const Invalid ID = 0

// ID is an opaque identifier for where underlying [Data] is stored. The zero
// value is reserved for an [Invalid] ID.
type ID uint64

// Data holds the raw bytes for a [Buffer]. Data is read-only and must not be
// modified.
type Data []byte
