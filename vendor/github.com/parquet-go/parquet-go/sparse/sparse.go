// Package sparse contains abstractions to help work on arrays of values in
// sparse memory locations.
//
// Conversion between array types is supported when converting integers to a
// lower size (e.g. int32 to int16, or uint64 to uint8), or converting from
// signed integers to unsigned. Float types can also be converted to unsigned
// integers of the same size, in which case the conversion is similar to using
// the standard library's math.Float32bits and math.Float64bits functions.
//
// All array types can be converted to a generic Array type that can be used to erase
// type information and bypass type conversion rules. This conversion is similar
// to using Go's unsafe package to bypass Go's type system and should usually be
// avoided and a sign that the application is attempting to break type safety
// boundaries.
//
// The package provides Gather* functions which retrieve values from sparse
// arrays into contiguous memory buffers. On platforms that support it, these
// operations are implemented using SIMD gather instructions (e.g. VPGATHER on
// Intel CPUs).
package sparse
