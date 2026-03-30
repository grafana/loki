// Package array defines the [Array], which is a structured set of values stored
// in a dataset.
package array

import (
	"github.com/grafana/loki/v3/pkg/dataset/types"
)

// Array describes encoded data stored within a dataset. Arrays are trees
// composed of buffers and other Arrays, where the entire tree forms a unit of
// data that can be read.
//
// This hierarchy is used so Arrays map as closely as possible to in-memory
// representation. For example, a nullable int64 array stores int64 data in its
// Buffer, while using a child Array to store the validity of each row.
//
// The exact layout and meaning of buffers and children arrays is dependant on
// the Array's [type.Type] and its [Encoding].
type Array struct {
	// Encoding describes how to interpret the data in Buffers.
	Encoding Encoding

	// Type holds the Array's logical type. Type must be compatible with the
	// Array's encoding.
	Type types.Type

	// Buffers holds metadata about how to retrieve the Array's data from a
	// [Source].
	Buffers []Buffer

	// Stats holds optional statistics for this array.
	Stats Stats

	// Children holds the child arrays. The Encoding and Type fields determine
	// how to interpret children.
	Children []Array
}

// Buffer is the individual unit of storage for array data. Use a [Source] to
// retrieve the raw bytes for a Buffer ([BufferData]).
type Buffer struct {
	// ID is an opaque Source-specific identifier where the underlying
	// [BufferData] is stored.
	ID int64
}

// BufferData holds the raw bytes for a [Buffer]. BufferData is read-only and
// must not be modified.
type BufferData []byte

// Stats holds optional statistics for an [Array]. Child statistics are not
// rolled up; read the Stats for individual children if needed.
type Stats struct {
	// TODO(rfratto): Other fields like MinValue, MaxValue, etc.

	RowCount  int // RowCount is the number of rows in the Array.
	NullCount int // NullCount is the number of null values in the Array.
}
