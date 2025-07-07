// Package encoding provides the generic APIs implemented by parquet encodings
// in its sub-packages.
package encoding

import (
	"math"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/format"
)

const (
	MaxFixedLenByteArraySize = math.MaxInt16
)

// The Encoding interface is implemented by types representing parquet column
// encodings.
//
// Encoding instances must be safe to use concurrently from multiple goroutines.
type Encoding interface {
	// Returns a human-readable name for the encoding.
	String() string

	// Returns the parquet code representing the encoding.
	Encoding() format.Encoding

	// Encode methods serialize the source sequence of values into the
	// destination buffer, potentially reallocating it if it was too short to
	// contain the output.
	//
	// The methods panic if the type of src values differ from the type of
	// values being encoded.
	EncodeLevels(dst []byte, src []uint8) ([]byte, error)
	EncodeBoolean(dst []byte, src []byte) ([]byte, error)
	EncodeInt32(dst []byte, src []int32) ([]byte, error)
	EncodeInt64(dst []byte, src []int64) ([]byte, error)
	EncodeInt96(dst []byte, src []deprecated.Int96) ([]byte, error)
	EncodeFloat(dst []byte, src []float32) ([]byte, error)
	EncodeDouble(dst []byte, src []float64) ([]byte, error)
	EncodeByteArray(dst []byte, src []byte, offsets []uint32) ([]byte, error)
	EncodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error)

	// Decode methods deserialize from the source buffer into the destination
	// slice, potentially growing it if it was too short to contain the result.
	//
	// The methods panic if the type of dst values differ from the type of
	// values being decoded.
	DecodeLevels(dst []uint8, src []byte) ([]uint8, error)
	DecodeBoolean(dst []byte, src []byte) ([]byte, error)
	DecodeInt32(dst []int32, src []byte) ([]int32, error)
	DecodeInt64(dst []int64, src []byte) ([]int64, error)
	DecodeInt96(dst []deprecated.Int96, src []byte) ([]deprecated.Int96, error)
	DecodeFloat(dst []float32, src []byte) ([]float32, error)
	DecodeDouble(dst []float64, src []byte) ([]float64, error)
	DecodeByteArray(dst []byte, src []byte, offsets []uint32) ([]byte, []uint32, error)
	DecodeFixedLenByteArray(dst []byte, src []byte, size int) ([]byte, error)

	// Computes an estimation of the output size of decoding the encoded page
	// of values passed as argument.
	//
	// Note that this is an estimate, it is useful to preallocate the output
	// buffer that will be passed to the decode method, but the actual output
	// size may be different.
	//
	// The estimate never errors since it is not intended to be used as an
	// input validation method.
	EstimateDecodeByteArraySize(src []byte) int

	// When this method returns true, the encoding supports receiving the same
	// buffer as source and destination.
	CanDecodeInPlace() bool
}
