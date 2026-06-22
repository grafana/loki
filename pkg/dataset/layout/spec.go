package layout

import "github.com/grafana/loki/v3/pkg/dataset/array"

// Spec describes how to encode data for a [Layout].
//
// While [Kind] categorizes the spec, the invidiual Spec implementations can
// hold additionally encoding-specific metadata that is used for encoding and
// decoding.
type Spec interface {
	isSpec() // Sealed interface.

	// Kind returns the encoding kind of this Spec.
	Kind() Kind
}

// Spec definitions.
type (
	// SpecArray specifies how to encode an individual [array.Array].
	SpecArray struct {
		// Spec for encoding data in the Array.
		Spec array.Spec
	}

	// SpecChunked specifies how to encode chunks.
	//
	// New chunks will be created when the current chunk reaches the size hint
	// or max row count. A single Append call with an oversized array will be
	// split across multiple chunks. At least one row is always written to a
	// chunk, even if that row alone exceeds the size hint.
	SpecChunked struct {
		// SizeHint is a soft limit for the size of individual chunks. The
		// Chunked layout will try to fill each chunk as close to this size as
		// possible, but the actual size may be slightly larger or smaller.
		//
		// Ignored when 0 or negative.
		SizeHint int

		// MaxRowCount is the limit for the number of rows in each chunk.
		// Ignored when 0 or negative.
		MaxRowCount int

		// Chunk is the spec used for all chunks in the layout.
		Chunk Spec
	}
)

// Kind returns [KindArray].
func (spec *SpecArray) Kind() Kind {
	return KindArray
}

// Kind returns [KindChunked].
func (spec *SpecChunked) Kind() Kind {
	return KindChunked
}

//
// Sealed marker implementations.
//

func (spec *SpecArray) isSpec()   {}
func (spec *SpecChunked) isSpec() {}
