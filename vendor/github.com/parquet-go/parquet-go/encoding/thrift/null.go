package thrift

import "reflect"

// Null represents an optional value of type T.
// Unlike pointers, Null[T] is a value type that stores the value inline,
// avoiding heap allocations during thrift decoding.
//
// Null[T] is designed to be a drop-in replacement for pointer fields in
// thrift structs where the pointer is used to distinguish between "not set"
// and "zero value".
//
// Null[T] implements the Value interface for custom thrift encoding/decoding.
type Null[T any] struct {
	V     T
	Valid bool
}

// New creates a valid Null[T] with the given value.
func New[T any](v T) Null[T] {
	return Null[T]{V: v, Valid: true}
}

// Get returns the value and whether it is valid.
func (n Null[T]) Get() (T, bool) {
	return n.V, n.Valid
}

// Set sets the value and marks it as valid.
func (n *Null[T]) Set(v T) {
	n.V = v
	n.Valid = true
}

// Reset clears the value and marks it as invalid.
func (n *Null[T]) Reset() {
	var zero T
	n.V = zero
	n.Valid = false
}

// Type returns the thrift type of the inner value T.
func (n Null[T]) Type() Type {
	return TypeOf(reflect.TypeFor[T]())
}

// EncodeFunc returns an encode function that encodes the V field.
func (n Null[T]) EncodeFunc(cache EncodeFuncCache) EncodeFunc {
	enc := EncodeFuncFor[T](cache)
	return func(w Writer, v reflect.Value, f Flags) error {
		return enc(w, v.Field(0), f) // Field 0 is V
	}
}

// DecodeFunc returns a decode function that decodes into V and sets Valid.
func (n Null[T]) DecodeFunc(cache DecodeFuncCache) DecodeFunc {
	dec := DecodeFuncFor[T](cache)
	return func(r Reader, v reflect.Value, f Flags) error {
		if err := dec(r, v.Field(0), f); err != nil { // Field 0 is V
			return err
		}
		v.Field(1).SetBool(true) // Field 1 is Valid
		return nil
	}
}

// Bool returns the inner bool value.
// This method is only meaningful for Null[bool] and implements BoolValue.
func (n Null[T]) Bool() bool {
	// Use reflection to check if T is bool and return the value
	v := reflect.ValueOf(n.V)
	if v.Kind() == reflect.Bool {
		return v.Bool()
	}
	return false
}

// NullFunc returns a function that checks if the value is not set.
func (n Null[T]) NullFunc() NullFunc {
	return func(v reflect.Value) bool {
		return !v.Field(1).Bool() // Field 1 is Valid
	}
}
