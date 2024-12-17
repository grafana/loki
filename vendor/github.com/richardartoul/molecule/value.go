package molecule

import (
	"fmt"
	"math"
	"reflect"
	"unsafe"

	"github.com/richardartoul/molecule/src/codec"
)

// Value represents a protobuf value. It contains the original wiretype that the value
// was encoded with as well as a variety of helper methods for interpreting the raw
// value based on the field's actual type.
type Value struct {
	// WireType is the protobuf wire type that was used to encode the field.
	WireType codec.WireType
	// Number will contain the value for any fields encoded with the
	// following wire types:
	//
	// 1. varint
	// 2. Fixed32
	// 3. Fixed64
	Number uint64
	// Bytes will contain the value for any fields encoded with the
	// following wire types:
	//
	// 1. bytes
	//
	// Bytes is an unsafe view over the bytes in the buffer. To obtain a "safe" copy
	// call value.AsSafeBytes() or copy Bytes directly.
	Bytes []byte
}

// AsDouble interprets the value as a double.
func (v *Value) AsDouble() (float64, error) {
	return math.Float64frombits(v.Number), nil
}

// AsFloat interprets the value as a float.
func (v *Value) AsFloat() (float32, error) {
	if v.Number > math.MaxUint32 {
		return 0, fmt.Errorf("AsFloat: %d overflows float32", v.Number)
	}
	return math.Float32frombits(uint32(v.Number)), nil
}

// AsInt32 interprets the value as an int32.
func (v *Value) AsInt32() (int32, error) {
	s := int64(v.Number)
	if s > math.MaxInt32 {
		return 0, fmt.Errorf("AsInt32: %d overflows int32", s)
	}
	if s < math.MinInt32 {
		return 0, fmt.Errorf("AsInt32: %d underflows int32", s)
	}
	return int32(v.Number), nil
}

// AsInt64 interprets the value as an int64.
func (v *Value) AsInt64() (int64, error) {
	return int64(v.Number), nil
}

// AsUint32 interprets the value as a uint32.
func (v *Value) AsUint32() (uint32, error) {
	if v.Number > math.MaxUint32 {
		return 0, fmt.Errorf("AsUInt32: %d overflows uint32", v.Number)
	}
	return uint32(v.Number), nil
}

// AsUint64 interprets the value as a uint64.
func (v *Value) AsUint64() (uint64, error) {
	return v.Number, nil
}

// AsSint32 interprets the value as a sint32.
func (v *Value) AsSint32() (int32, error) {
	if v.Number > math.MaxUint32 {
		return 0, fmt.Errorf("AsSint32: %d overflows int32", v.Number)
	}
	return codec.DecodeZigZag32(v.Number), nil
}

// AsSint64 interprets the value as a sint64.
func (v *Value) AsSint64() (int64, error) {
	return codec.DecodeZigZag64(v.Number), nil
}

// AsFixed32 interprets the value as a fixed32.
func (v *Value) AsFixed32() (uint32, error) {
	if v.Number > math.MaxUint32 {
		return 0, fmt.Errorf("AsFixed32: %d overflows int32", v.Number)
	}
	return uint32(v.Number), nil
}

// AsFixed64 interprets the value as a fixed64.
func (v *Value) AsFixed64() (uint64, error) {
	return uint64(v.Number), nil
}

// AsSFixed32 interprets the value as a SFixed32.
func (v *Value) AsSFixed32() (int32, error) {
	if v.Number > math.MaxUint32 {
		return 0, fmt.Errorf("AsSFixed32: %d overflows int32", v.Number)
	}
	return int32(v.Number), nil
}

// AsSFixed64 interprets the value as a SFixed64.
func (v *Value) AsSFixed64() (int64, error) {
	return int64(v.Number), nil
}

// AsBool interprets the value as a bool.
func (v *Value) AsBool() (bool, error) {
	return v.Number == 1, nil
}

// AsStringUnsafe interprets the value as a string. The returned string is an unsafe view over
// the underlying bytes. Use AsStringSafe() to obtain a "safe" string that is a copy of the
// underlying data.
func (v *Value) AsStringUnsafe() (string, error) {
	return unsafeBytesToString(v.Bytes), nil
}

// AsStringSafe interprets the value as a string by allocating a safe copy of the underlying data.
func (v *Value) AsStringSafe() (string, error) {
	return string(v.Bytes), nil
}

// AsBytesUnsafe interprets the value as a byte slice. The returned []byte is an unsafe view over
// the underlying bytes. Use AsBytesSafe() to obtain a "safe" [] that is a copy of the
// underlying data.
func (v *Value) AsBytesUnsafe() ([]byte, error) {
	return v.Bytes, nil
}

// AsBytesSafe interprets the value as a byte slice by allocating a safe copy of the underlying data.
func (v *Value) AsBytesSafe() ([]byte, error) {
	return append([]byte(nil), v.Bytes...), nil
}

func unsafeBytesToString(b []byte) string {
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&b))

	var s string
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	sh.Data = bh.Data
	sh.Len = bh.Len
	return s
}
