package thrift

import "reflect"

// NullFunc checks if a value should be skipped during encoding.
type NullFunc func(reflect.Value) bool

// Value is implemented by types that provide custom thrift encoding/decoding.
// Types implementing this interface control how they are serialized.
//
// The NullFunc method returns a function that checks whether a value should
// be skipped during encoding. Optional types return true when not set;
// collection types return true when nil.
type Value interface {
	// Type returns the thrift type for this value.
	Type() Type

	// EncodeFunc returns the encode function for this type.
	// The cache parameter allows recursive encoding of nested types.
	EncodeFunc(cache EncodeFuncCache) EncodeFunc

	// DecodeFunc returns the decode function for this type.
	DecodeFunc(cache DecodeFuncCache) DecodeFunc

	// NullFunc returns a function that checks if the value should be skipped
	// during encoding. For optional types like Null[T], this checks if the
	// value is not set. For collection types like Slice[T], this checks if
	// the slice is nil.
	NullFunc() NullFunc
}

// BoolValue extends Value for types that wrap a boolean value.
// This is used for coalesced bool field encoding where the bool
// value is encoded in the field type (TRUE/FALSE) rather than
// as a separate value.
type BoolValue interface {
	Value
	Bool() bool
}

// Cached interface types for reflection checks
var (
	valueType     = reflect.TypeFor[Value]()
	boolValueType = reflect.TypeFor[BoolValue]()
)
