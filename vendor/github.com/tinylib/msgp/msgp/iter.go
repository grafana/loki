//go:build go1.23

package msgp

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"math"
	"slices"
)

// ReadArray returns an iterator that can be used to iterate over the elements
// of an array in the MessagePack data while being read by the provided Reader.
// The type parameter V specifies the type of the elements in the array.
// The returned iterator implements the iter.Seq[V] interface,
// allowing for sequential access to the array elements.
func ReadArray[T any](m *Reader, readFn func() (T, error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		// Check if nil
		if m.IsNil() {
			m.ReadNil()
			return
		}
		// Regular array.
		var empty T
		length, err := m.ReadArrayHeader()
		if err != nil {
			yield(empty, fmt.Errorf("cannot read array header: %w", err))
			return
		}
		for range length {
			var v T
			v, err = readFn()
			if !yield(v, err) {
				return
			}
		}
	}
}

// WriteArray writes an array to the provided Writer.
// The writeFn parameter specifies the function to use to write each element of the array.
func WriteArray[T any](w *Writer, a []T, writeFn func(T) error) error {
	// Check if nil
	if a == nil {
		return w.WriteNil()
	}
	if uint64(len(a)) > math.MaxUint32 {
		return fmt.Errorf("array too large to encode: %d elements", len(a))
	}
	// Write array header
	err := w.WriteArrayHeader(uint32(len(a)))
	if err != nil {
		return err
	}
	// Write elements
	for _, v := range a {
		err = writeFn(v)
		if err != nil {
			return err
		}
	}
	return nil
}

// ReadMap returns an iterator that can be used to iterate over the elements
// of a map in the MessagePack data while being read by the provided Reader.
// The type parameters K and V specify the types of the keys and values in the map.
// The returned iterator implements the iter.Seq2[K, V] interface,
// allowing for sequential access to the map elements.
// The returned function can be used to read any error that
// occurred during iteration when iteration is done.
func ReadMap[K, V any](m *Reader, readKey func() (K, error), readVal func() (V, error)) (iter.Seq2[K, V], func() error) {
	var err error
	return func(yield func(K, V) bool) {
		var sz uint32
		if m.IsNil() {
			err = m.ReadNil()
			return
		}
		sz, err = m.ReadMapHeader()
		if err != nil {
			err = fmt.Errorf("cannot read map header: %w", err)
			return
		}

		for range sz {
			var k K
			k, err = readKey()
			if err != nil {
				err = fmt.Errorf("cannot read key: %w", err)
				return
			}
			var v V
			v, err = readVal()
			if err != nil {
				err = fmt.Errorf("cannot read value: %w", err)
				return
			}
			if !yield(k, v) {
				return
			}
		}
	}, func() error { return err }
}

// WriteMap writes a map to the provided Writer.
// The writeKey and writeVal parameters specify the functions
// to use to write each key and value of the map.
func WriteMap[K comparable, V any](w *Writer, m map[K]V, writeKey func(K) error, writeVal func(V) error) error {
	if m == nil {
		return w.WriteNil()
	}
	if uint64(len(m)) > math.MaxUint32 {
		return fmt.Errorf("map too large to encode: %d elements", len(m))
	}

	// Write map header
	err := w.WriteMapHeader(uint32(len(m)))
	if err != nil {
		return err
	}
	// Write elements
	for k, v := range m {
		err = writeKey(k)
		if err != nil {
			return err
		}
		err = writeVal(v)
		if err != nil {
			return err
		}
	}
	return nil
}

// WriteMapSorted writes a map to the provided Writer.
// The keys of the map are sorted before writing.
// This provides deterministic output, but will allocate to sort the keys.
// The writeKey and writeVal parameters specify the functions
// to use to write each key and value of the map.
func WriteMapSorted[K cmp.Ordered, V any](w *Writer, m map[K]V, writeKey func(K) error, writeVal func(V) error) error {
	if m == nil {
		return w.WriteNil()
	}
	if uint64(len(m)) > math.MaxUint32 {
		return fmt.Errorf("map too large to encode: %d elements", len(m))
	}

	// Write map header
	err := w.WriteMapHeader(uint32(len(m)))
	if err != nil {
		return err
	}
	// Write elements
	for _, k := range slices.Sorted(maps.Keys(m)) {
		err = writeKey(k)
		if err != nil {
			return err
		}
		err = writeVal(m[k])
		if err != nil {
			return err
		}
	}
	return nil
}

// ReadArrayBytes returns an iterator that can be used to iterate over the elements
// of an array in the MessagePack data while being read by the provided Reader.
// The type parameter V specifies the type of the elements in the array.
// After the iterator is exhausted, the remaining bytes in the buffer
// and any error can be read by calling the returned function.
func ReadArrayBytes[T any](b []byte, readFn func([]byte) (T, []byte, error)) (iter.Seq[T], func() (remain []byte, err error)) {
	if IsNil(b) {
		b, err := ReadNilBytes(b)
		return func(yield func(T) bool) {}, func() ([]byte, error) { return b, err }
	}
	sz, b, err := ReadArrayHeaderBytes(b)
	if err != nil || sz == 0 {
		return func(yield func(T) bool) {}, func() ([]byte, error) { return b, err }
	}
	return func(yield func(T) bool) {
			for range sz {
				var v T
				v, b, err = readFn(b)
				if err != nil || !yield(v) {
					return
				}
			}
		}, func() ([]byte, error) {
			return b, err
		}
}

// AppendArray writes an array to the provided buffer.
// The writeFn parameter specifies the function to use to write each element of the array.
// The returned buffer contains the encoded array.
// The function panics if the array is larger than math.MaxUint32 elements.
func AppendArray[T any](b []byte, a []T, writeFn func(b []byte, v T) []byte) []byte {
	if a == nil {
		return AppendNil(b)
	}
	if uint64(len(a)) > math.MaxUint32 {
		panic(fmt.Sprintf("array too large to encode: %d elements", len(a)))
	}
	b = AppendArrayHeader(b, uint32(len(a)))
	for _, v := range a {
		b = writeFn(b, v)
	}
	return b
}

// ReadMapBytes returns an iterator over key/value
// pairs from a MessagePack map encoded in b.
// The iterator yields K,V pairs, and this function also returns
// a closure to get the remaining bytes and any error.
func ReadMapBytes[K any, V any](b []byte,
	readK func([]byte) (K, []byte, error),
	readV func([]byte) (V, []byte, error)) (iter.Seq2[K, V], func() (remain []byte, err error)) {
	var err error
	var sz uint32
	if IsNil(b) {
		b, err = ReadNilBytes(b)
		return func(yield func(K, V) bool) {}, func() ([]byte, error) { return b, err }
	}
	sz, b, err = ReadMapHeaderBytes(b)
	if err != nil || sz == 0 {
		return func(yield func(K, V) bool) {}, func() ([]byte, error) { return b, err }
	}

	return func(yield func(K, V) bool) {
		for range sz {
			var k K
			k, b, err = readK(b)
			if err != nil {
				err = fmt.Errorf("cannot read map key: %w", err)
				return
			}
			var v V
			v, b, err = readV(b)
			if err != nil {
				err = fmt.Errorf("cannot read map value: %w", err)
				return
			}
			if !yield(k, v) {
				return
			}
		}
	}, func() ([]byte, error) { return b, err }
}

// AppendMap writes a map to the provided buffer.
// The writeK and writeV parameters specify the functions to use to write each key and value of the map.
// The returned buffer contains the encoded map.
// The function panics if the map is larger than math.MaxUint32 elements.
func AppendMap[K comparable, V any](b []byte, m map[K]V,
	writeK func(b []byte, k K) []byte,
	writeV func(b []byte, v V) []byte) []byte {
	if m == nil {
		return AppendNil(b)
	}
	if uint64(len(m)) > math.MaxUint32 {
		panic(fmt.Sprintf("map too large to encode: %d elements", len(m)))
	}
	b = AppendMapHeader(b, uint32(len(m)))
	for k, v := range m {
		b = writeK(b, k)
		b = writeV(b, v)
	}
	return b
}

// AppendMapSorted writes a map to the provided buffer.
// Keys are sorted before writing.
// This provides deterministic output, but will allocate to sort the keys.
// The writeK and writeV parameters specify the functions to use to write each key and value of the map.
// The returned buffer contains the encoded map.
// The function panics if the map is larger than math.MaxUint32 elements.
func AppendMapSorted[K cmp.Ordered, V any](b []byte, m map[K]V,
	writeK func(b []byte, k K) []byte,
	writeV func(b []byte, v V) []byte) []byte {
	if m == nil {
		return AppendNil(b)
	}
	if uint64(len(m)) > math.MaxUint32 {
		panic(fmt.Sprintf("map too large to encode: %d elements", len(m)))
	}
	b = AppendMapHeader(b, uint32(len(m)))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		b = writeK(b, k)
		b = writeV(b, m[k])
	}
	return b
}

// DecodePtr is a convenience type for decoding into a pointer.
type DecodePtr[T any] interface {
	*T
	Decodable
}

// DecoderFrom allows augmenting any type with a DecodeMsg method into a method
// that reads from Reader and returns a T.
// Provide an instance of T. This value isn't used.
// See ReadArray/ReadMap "struct" examples for usage.
func DecoderFrom[T any, PT DecodePtr[T]](r *Reader, _ T) func() (T, error) {
	return func() (T, error) {
		var t T
		tPtr := PT(&t)
		err := tPtr.DecodeMsg(r)
		return t, err
	}
}

// FlexibleEncoder is a constraint for types where either T or *T implements Encodable
type FlexibleEncoder[T any] interface {
	Encodable
	*T
}

// EncoderTo allows augmenting any type with an EncodeMsg
// method into a method that writes to Writer on each call.
// Provide an instance of T. This value isn't used.
// See ReadArray or ReadMap "struct" examples for usage.
func EncoderTo[T any, _ FlexibleEncoder[T]](w *Writer, _ T) func(T) error {
	return func(t T) error {
		// Check if T implements Marshaler
		if marshaler, ok := any(t).(Encodable); ok {
			return marshaler.EncodeMsg(w)
		}
		// Check if *T implements Marshaler
		if ptrMarshaler, ok := any(&t).(Encodable); ok {
			return ptrMarshaler.EncodeMsg(w)
		}
		// The compiler should have asserted this.
		panic("type does not implement Marshaler")
	}
}

// UnmarshalPtr is a convenience type for unmarshaling into a pointer.
type UnmarshalPtr[T any] interface {
	*T
	Unmarshaler
}

// DecoderFromBytes allows augmenting any type with an UnmarshalMsg
// method into a method that reads from []byte and returns a T.
// Provide an instance of T. This value isn't used.
// See ReadArrayBytes or ReadMapBytes "struct" examples for usage.
func DecoderFromBytes[T any, PT UnmarshalPtr[T]](_ T) func([]byte) (T, []byte, error) {
	return func(b []byte) (T, []byte, error) {
		var t T
		tPtr := PT(&t)
		b, err := tPtr.UnmarshalMsg(b)
		return t, b, err
	}
}

// FlexibleMarshaler is a constraint for types where either T or *T implements Marshaler
type FlexibleMarshaler[T any] interface {
	Marshaler
	*T // Include *T in the interface
}

// EncoderToBytes allows augmenting any type with a MarshalMsg method into a method
// that reads from T and returns a []byte.
// Provide an instance of T. This value isn't used.
// See ReadArrayBytes or ReadMapBytes "struct" examples for usage.
func EncoderToBytes[T any, _ FlexibleMarshaler[T]](_ T) func([]byte, T) []byte {
	return func(b []byte, t T) []byte {
		// Check if T implements Marshaler
		if marshaler, ok := any(t).(Marshaler); ok {
			b, _ = marshaler.MarshalMsg(b)
			return b
		}
		// Check if *T implements Marshaler
		if ptrMarshaler, ok := any(&t).(Marshaler); ok {
			b, _ = ptrMarshaler.MarshalMsg(b)
			return b
		}
		// The compiler should have asserted this.
		panic("type does not implement Marshaler")
	}
}
