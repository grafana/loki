package sparse

import (
	"time"
	"unsafe"
)

type Array struct{ array }

func UnsafeArray(base unsafe.Pointer, length int, offset uintptr) Array {
	return Array{unsafeArray(base, length, offset)}
}

func (a Array) Len() int                   { return int(a.len) }
func (a Array) Index(i int) unsafe.Pointer { return a.index(i) }
func (a Array) Slice(i, j int) Array       { return Array{a.slice(i, j)} }
func (a Array) Offset(off uintptr) Array   { return Array{a.offset(off)} }
func (a Array) BoolArray() BoolArray       { return BoolArray{a.array} }
func (a Array) Int8Array() Int8Array       { return Int8Array{a.array} }
func (a Array) Int16Array() Int16Array     { return Int16Array{a.array} }
func (a Array) Int32Array() Int32Array     { return Int32Array{a.array} }
func (a Array) Int64Array() Int64Array     { return Int64Array{a.array} }
func (a Array) Float32Array() Float32Array { return Float32Array{a.array} }
func (a Array) Float64Array() Float64Array { return Float64Array{a.array} }
func (a Array) Uint8Array() Uint8Array     { return Uint8Array{a.array} }
func (a Array) Uint16Array() Uint16Array   { return Uint16Array{a.array} }
func (a Array) Uint32Array() Uint32Array   { return Uint32Array{a.array} }
func (a Array) Uint64Array() Uint64Array   { return Uint64Array{a.array} }
func (a Array) Uint128Array() Uint128Array { return Uint128Array{a.array} }
func (a Array) StringArray() StringArray   { return StringArray{a.array} }
func (a Array) TimeArray() TimeArray       { return TimeArray{a.array} }

type array struct {
	ptr unsafe.Pointer
	len uintptr
	off uintptr
}

func makeArray[T any](base []T) array {
	var z T
	return array{
		ptr: unsafe.Pointer(unsafe.SliceData(base)),
		len: uintptr(len(base)),
		off: unsafe.Sizeof(z),
	}
}

func unsafeArray(base unsafe.Pointer, length int, offset uintptr) array {
	return array{ptr: base, len: uintptr(length), off: offset}
}

func (a array) index(i int) unsafe.Pointer {
	if uintptr(i) >= a.len {
		panic("index out of bounds")
	}
	return unsafe.Add(a.ptr, a.off*uintptr(i))
}

func (a array) slice(i, j int) array {
	if uintptr(i) > a.len || uintptr(j) > a.len || i > j {
		panic("slice index out of bounds")
	}
	return array{
		ptr: unsafe.Add(a.ptr, a.off*uintptr(i)),
		len: uintptr(j - i),
		off: a.off,
	}
}

func (a array) offset(off uintptr) array {
	if a.ptr == nil {
		panic("offset of nil array")
	}
	return array{
		ptr: unsafe.Add(a.ptr, off),
		len: a.len,
		off: a.off,
	}
}

type BoolArray struct{ array }

func MakeBoolArray(values []bool) BoolArray {
	return BoolArray{makeArray(values)}
}

func UnsafeBoolArray(base unsafe.Pointer, length int, offset uintptr) BoolArray {
	return BoolArray{unsafeArray(base, length, offset)}
}

func (a BoolArray) Len() int                 { return int(a.len) }
func (a BoolArray) Index(i int) bool         { return *(*byte)(a.index(i)) != 0 }
func (a BoolArray) Slice(i, j int) BoolArray { return BoolArray{a.slice(i, j)} }
func (a BoolArray) Uint8Array() Uint8Array   { return Uint8Array{a.array} }
func (a BoolArray) UnsafeArray() Array       { return Array{a.array} }

type Int8Array struct{ array }

func MakeInt8Array(values []int8) Int8Array {
	return Int8Array{makeArray(values)}
}

func UnsafeInt8Array(base unsafe.Pointer, length int, offset uintptr) Int8Array {
	return Int8Array{unsafeArray(base, length, offset)}
}

func (a Int8Array) Len() int                 { return int(a.len) }
func (a Int8Array) Index(i int) int8         { return *(*int8)(a.index(i)) }
func (a Int8Array) Slice(i, j int) Int8Array { return Int8Array{a.slice(i, j)} }
func (a Int8Array) Uint8Array() Uint8Array   { return Uint8Array{a.array} }
func (a Int8Array) UnsafeArray() Array       { return Array{a.array} }

type Int16Array struct{ array }

func MakeInt16Array(values []int16) Int16Array {
	return Int16Array{makeArray(values)}
}

func UnsafeInt16Array(base unsafe.Pointer, length int, offset uintptr) Int16Array {
	return Int16Array{unsafeArray(base, length, offset)}
}

func (a Int16Array) Len() int                  { return int(a.len) }
func (a Int16Array) Index(i int) int16         { return *(*int16)(a.index(i)) }
func (a Int16Array) Slice(i, j int) Int16Array { return Int16Array{a.slice(i, j)} }
func (a Int16Array) Int8Array() Int8Array      { return Int8Array{a.array} }
func (a Int16Array) Uint8Array() Uint8Array    { return Uint8Array{a.array} }
func (a Int16Array) Uint16Array() Uint16Array  { return Uint16Array{a.array} }
func (a Int16Array) UnsafeArray() Array        { return Array{a.array} }

type Int32Array struct{ array }

func MakeInt32Array(values []int32) Int32Array {
	return Int32Array{makeArray(values)}
}

func UnsafeInt32Array(base unsafe.Pointer, length int, offset uintptr) Int32Array {
	return Int32Array{unsafeArray(base, length, offset)}
}

func (a Int32Array) Len() int                  { return int(a.len) }
func (a Int32Array) Index(i int) int32         { return *(*int32)(a.index(i)) }
func (a Int32Array) Slice(i, j int) Int32Array { return Int32Array{a.slice(i, j)} }
func (a Int32Array) Int8Array() Int8Array      { return Int8Array{a.array} }
func (a Int32Array) Int16Array() Int16Array    { return Int16Array{a.array} }
func (a Int32Array) Uint8Array() Uint8Array    { return Uint8Array{a.array} }
func (a Int32Array) Uint16Array() Uint16Array  { return Uint16Array{a.array} }
func (a Int32Array) Uint32Array() Uint32Array  { return Uint32Array{a.array} }
func (a Int32Array) UnsafeArray() Array        { return Array{a.array} }

type Int64Array struct{ array }

func MakeInt64Array(values []int64) Int64Array {
	return Int64Array{makeArray(values)}
}

func UnsafeInt64Array(base unsafe.Pointer, length int, offset uintptr) Int64Array {
	return Int64Array{unsafeArray(base, length, offset)}
}

func (a Int64Array) Len() int                  { return int(a.len) }
func (a Int64Array) Index(i int) int64         { return *(*int64)(a.index(i)) }
func (a Int64Array) Slice(i, j int) Int64Array { return Int64Array{a.slice(i, j)} }
func (a Int64Array) Int8Array() Int8Array      { return Int8Array{a.array} }
func (a Int64Array) Int16Array() Int16Array    { return Int16Array{a.array} }
func (a Int64Array) Int32Array() Int32Array    { return Int32Array{a.array} }
func (a Int64Array) Uint8Array() Uint8Array    { return Uint8Array{a.array} }
func (a Int64Array) Uint16Array() Uint16Array  { return Uint16Array{a.array} }
func (a Int64Array) Uint32Array() Uint32Array  { return Uint32Array{a.array} }
func (a Int64Array) Uint64Array() Uint64Array  { return Uint64Array{a.array} }
func (a Int64Array) UnsafeArray() Array        { return Array{a.array} }

type Float32Array struct{ array }

func MakeFloat32Array(values []float32) Float32Array {
	return Float32Array{makeArray(values)}
}

func UnsafeFloat32Array(base unsafe.Pointer, length int, offset uintptr) Float32Array {
	return Float32Array{unsafeArray(base, length, offset)}
}

func (a Float32Array) Len() int                    { return int(a.len) }
func (a Float32Array) Index(i int) float32         { return *(*float32)(a.index(i)) }
func (a Float32Array) Slice(i, j int) Float32Array { return Float32Array{a.slice(i, j)} }
func (a Float32Array) Array() Array                { return Array{a.array} }
func (a Float32Array) Uint32Array() Uint32Array    { return Uint32Array{a.array} }
func (a Float32Array) UnsafeArray() Array          { return Array{a.array} }

type Float64Array struct{ array }

func MakeFloat64Array(values []float64) Float64Array {
	return Float64Array{makeArray(values)}
}

func UnsafeFloat64Array(base unsafe.Pointer, length int, offset uintptr) Float64Array {
	return Float64Array{unsafeArray(base, length, offset)}
}

func (a Float64Array) Len() int                    { return int(a.len) }
func (a Float64Array) Index(i int) float64         { return *(*float64)(a.index(i)) }
func (a Float64Array) Slice(i, j int) Float64Array { return Float64Array{a.slice(i, j)} }
func (a Float64Array) Uint64Array() Uint64Array    { return Uint64Array{a.array} }
func (a Float64Array) UnsafeArray() Array          { return Array{a.array} }

type Uint8Array struct{ array }

func MakeUint8Array(values []uint8) Uint8Array {
	return Uint8Array{makeArray(values)}
}

func UnsafeUint8Array(base unsafe.Pointer, length int, offset uintptr) Uint8Array {
	return Uint8Array{unsafeArray(base, length, offset)}
}

func (a Uint8Array) Len() int                  { return int(a.len) }
func (a Uint8Array) Index(i int) uint8         { return *(*uint8)(a.index(i)) }
func (a Uint8Array) Slice(i, j int) Uint8Array { return Uint8Array{a.slice(i, j)} }
func (a Uint8Array) UnsafeArray() Array        { return Array{a.array} }

type Uint16Array struct{ array }

func MakeUint16Array(values []uint16) Uint16Array {
	return Uint16Array{makeArray(values)}
}

func UnsafeUint16Array(base unsafe.Pointer, length int, offset uintptr) Uint16Array {
	return Uint16Array{unsafeArray(base, length, offset)}
}

func (a Uint16Array) Len() int                   { return int(a.len) }
func (a Uint16Array) Index(i int) uint16         { return *(*uint16)(a.index(i)) }
func (a Uint16Array) Slice(i, j int) Uint16Array { return Uint16Array{a.slice(i, j)} }
func (a Uint16Array) Uint8Array() Uint8Array     { return Uint8Array{a.array} }
func (a Uint16Array) UnsafeArray() Array         { return Array{a.array} }

type Uint32Array struct{ array }

func MakeUint32Array(values []uint32) Uint32Array {
	return Uint32Array{makeArray(values)}
}

func UnsafeUint32Array(base unsafe.Pointer, length int, offset uintptr) Uint32Array {
	return Uint32Array{unsafeArray(base, length, offset)}
}

func (a Uint32Array) Len() int                   { return int(a.len) }
func (a Uint32Array) Index(i int) uint32         { return *(*uint32)(a.index(i)) }
func (a Uint32Array) Slice(i, j int) Uint32Array { return Uint32Array{a.slice(i, j)} }
func (a Uint32Array) Uint8Array() Uint8Array     { return Uint8Array{a.array} }
func (a Uint32Array) Uint16Array() Uint16Array   { return Uint16Array{a.array} }
func (a Uint32Array) UnsafeArray() Array         { return Array{a.array} }

type Uint64Array struct{ array }

func MakeUint64Array(values []uint64) Uint64Array {
	return Uint64Array{makeArray(values)}
}

func UnsafeUint64Array(base unsafe.Pointer, length int, offset uintptr) Uint64Array {
	return Uint64Array{unsafeArray(base, length, offset)}
}

func (a Uint64Array) Len() int                   { return int(a.len) }
func (a Uint64Array) Index(i int) uint64         { return *(*uint64)(a.index(i)) }
func (a Uint64Array) Slice(i, j int) Uint64Array { return Uint64Array{a.slice(i, j)} }
func (a Uint64Array) Uint8Array() Uint8Array     { return Uint8Array{a.array} }
func (a Uint64Array) Uint16Array() Uint16Array   { return Uint16Array{a.array} }
func (a Uint64Array) Uint32Array() Uint32Array   { return Uint32Array{a.array} }
func (a Uint64Array) UnsafeArray() Array         { return Array{a.array} }

type Uint128Array struct{ array }

func MakeUint128Array(values [][16]byte) Uint128Array {
	return Uint128Array{makeArray(values)}
}

func UnsafeUint128Array(base unsafe.Pointer, length int, offset uintptr) Uint128Array {
	return Uint128Array{unsafeArray(base, length, offset)}
}

func (a Uint128Array) Len() int                    { return int(a.len) }
func (a Uint128Array) Index(i int) [16]byte        { return *(*[16]byte)(a.index(i)) }
func (a Uint128Array) Slice(i, j int) Uint128Array { return Uint128Array{a.slice(i, j)} }
func (a Uint128Array) Uint8Array() Uint8Array      { return Uint8Array{a.array} }
func (a Uint128Array) Uint16Array() Uint16Array    { return Uint16Array{a.array} }
func (a Uint128Array) Uint32Array() Uint32Array    { return Uint32Array{a.array} }
func (a Uint128Array) Uint64Array() Uint64Array    { return Uint64Array{a.array} }
func (a Uint128Array) UnsafeArray() Array          { return Array{a.array} }

type StringArray struct{ array }

func MakeStringArray(values []string) StringArray {
	const sizeOfString = unsafe.Sizeof("")
	return StringArray{makeArray(values)}
}

func UnsafeStringArray(base unsafe.Pointer, length int, offset uintptr) StringArray {
	return StringArray{unsafeArray(base, length, offset)}
}

func (a StringArray) Len() int                   { return int(a.len) }
func (a StringArray) Index(i int) string         { return *(*string)(a.index(i)) }
func (a StringArray) Slice(i, j int) StringArray { return StringArray{a.slice(i, j)} }
func (a StringArray) UnsafeArray() Array         { return Array{a.array} }

type TimeArray struct{ array }

func MakeTimeArray(values []time.Time) TimeArray {
	return TimeArray{makeArray(values)}
}

func UnsafeTimeArray(base unsafe.Pointer, length int, offset uintptr) TimeArray {
	return TimeArray{unsafeArray(base, length, offset)}
}

func (a TimeArray) Len() int                 { return int(a.len) }
func (a TimeArray) Index(i int) time.Time    { return *(*time.Time)(a.index(i)) }
func (a TimeArray) Slice(i, j int) TimeArray { return TimeArray{a.slice(i, j)} }
func (a TimeArray) UnsafeArray() Array       { return Array{a.array} }
