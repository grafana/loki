package parquet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/format"
)

const (
	// 170 x sizeof(Value) = 4KB
	defaultValueBufferSize = 170

	offsetOfPtr  = unsafe.Offsetof(Value{}.ptr)
	offsetOfU64  = unsafe.Offsetof(Value{}.u64)
	offsetOfU32  = offsetOfU64 + firstByteOffsetOf32BitsValue
	offsetOfBool = offsetOfU64 + firstByteOffsetOfBooleanValue
)

// The Value type is similar to the reflect.Value abstraction of Go values, but
// for parquet values. Value instances wrap underlying Go values mapped to one
// of the parquet physical types.
//
// Value instances are small, immutable objects, and usually passed by value
// between function calls.
//
// The zero-value of Value represents the null parquet value.
type Value struct {
	// data
	ptr *byte
	u64 uint64
	// type
	kind int8 // XOR(Kind) so the zero-value is <null>
	// levels
	definitionLevel byte
	repetitionLevel byte
	columnIndex     int16 // XOR so the zero-value is -1
}

// ValueReader is an interface implemented by types that support reading
// batches of values.
type ValueReader interface {
	// Read values into the buffer passed as argument and return the number of
	// values read. When all values have been read, the error will be io.EOF.
	ReadValues([]Value) (int, error)
}

// ValueReaderAt is an interface implemented by types that support reading
// values at offsets specified by the application.
type ValueReaderAt interface {
	ReadValuesAt([]Value, int64) (int, error)
}

// ValueReaderFrom is an interface implemented by value writers to read values
// from a reader.
type ValueReaderFrom interface {
	ReadValuesFrom(ValueReader) (int64, error)
}

// ValueWriter is an interface implemented by types that support reading
// batches of values.
type ValueWriter interface {
	// Write values from the buffer passed as argument and returns the number
	// of values written.
	WriteValues([]Value) (int, error)
}

// ValueWriterTo is an interface implemented by value readers to write values to
// a writer.
type ValueWriterTo interface {
	WriteValuesTo(ValueWriter) (int64, error)
}

// ValueReaderFunc is a function type implementing the ValueReader interface.
type ValueReaderFunc func([]Value) (int, error)

func (f ValueReaderFunc) ReadValues(values []Value) (int, error) { return f(values) }

// ValueWriterFunc is a function type implementing the ValueWriter interface.
type ValueWriterFunc func([]Value) (int, error)

func (f ValueWriterFunc) WriteValues(values []Value) (int, error) { return f(values) }

// CopyValues copies values from src to dst, returning the number of values
// that were written.
//
// As an optimization, the reader and writer may choose to implement
// ValueReaderFrom and ValueWriterTo to provide their own copy logic.
//
// The function returns any error it encounters reading or writing pages, except
// for io.EOF from the reader which indicates that there were no more values to
// read.
func CopyValues(dst ValueWriter, src ValueReader) (int64, error) {
	return copyValues(dst, src, nil)
}

func copyValues(dst ValueWriter, src ValueReader, buf []Value) (written int64, err error) {
	if wt, ok := src.(ValueWriterTo); ok {
		return wt.WriteValuesTo(dst)
	}

	if rf, ok := dst.(ValueReaderFrom); ok {
		return rf.ReadValuesFrom(src)
	}

	if len(buf) == 0 {
		buf = make([]Value, defaultValueBufferSize)
	}

	defer clearValues(buf)

	for {
		n, err := src.ReadValues(buf)

		if n > 0 {
			wn, werr := dst.WriteValues(buf[:n])
			written += int64(wn)
			if werr != nil {
				return written, werr
			}
		}

		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return written, err
		}

		if n == 0 {
			return written, io.ErrNoProgress
		}
	}
}

// ValueOf constructs a parquet value from a Go value v.
//
// The physical type of the value is assumed from the Go type of v using the
// following conversion table:
//
//	Go type | Parquet physical type
//	------- | ---------------------
//	nil     | NULL
//	bool    | BOOLEAN
//	int8    | INT32
//	int16   | INT32
//	int32   | INT32
//	int64   | INT64
//	int     | INT64
//	uint8   | INT32
//	uint16  | INT32
//	uint32  | INT32
//	uint64  | INT64
//	uintptr | INT64
//	float32 | FLOAT
//	float64 | DOUBLE
//	string  | BYTE_ARRAY
//	[]byte  | BYTE_ARRAY
//	[*]byte | FIXED_LEN_BYTE_ARRAY
//
// When converting a []byte or [*]byte value, the underlying byte array is not
// copied; instead, the returned parquet value holds a reference to it.
//
// The repetition and definition levels of the returned value are both zero.
//
// The function panics if the Go value cannot be represented in parquet.
func ValueOf(v interface{}) Value {
	k := Kind(-1)
	t := reflect.TypeOf(v)

	switch value := v.(type) {
	case nil:
		return Value{}
	case uuid.UUID:
		return makeValueBytes(FixedLenByteArray, value[:])
	case deprecated.Int96:
		return makeValueInt96(value)
	case time.Time:
		k = Int64
	}

	switch t.Kind() {
	case reflect.Bool:
		k = Boolean
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		k = Int32
	case reflect.Int64, reflect.Int, reflect.Uint64, reflect.Uint, reflect.Uintptr:
		k = Int64
	case reflect.Float32:
		k = Float
	case reflect.Float64:
		k = Double
	case reflect.String:
		k = ByteArray
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			k = ByteArray
		}
	case reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			k = FixedLenByteArray
		}
	}

	if k < 0 {
		panic("cannot create parquet value from go value of type " + t.String())
	}

	return makeValue(k, nil, reflect.ValueOf(v))
}

// NulLValue constructs a null value, which is the zero-value of the Value type.
func NullValue() Value { return Value{} }

// ZeroValue constructs a zero value of the given kind.
func ZeroValue(kind Kind) Value { return makeValueKind(kind) }

// BooleanValue constructs a BOOLEAN parquet value from the bool passed as
// argument.
func BooleanValue(value bool) Value { return makeValueBoolean(value) }

// Int32Value constructs a INT32 parquet value from the int32 passed as
// argument.
func Int32Value(value int32) Value { return makeValueInt32(value) }

// Int64Value constructs a INT64 parquet value from the int64 passed as
// argument.
func Int64Value(value int64) Value { return makeValueInt64(value) }

// Int96Value constructs a INT96 parquet value from the deprecated.Int96 passed
// as argument.
func Int96Value(value deprecated.Int96) Value { return makeValueInt96(value) }

// FloatValue constructs a FLOAT parquet value from the float32 passed as
// argument.
func FloatValue(value float32) Value { return makeValueFloat(value) }

// DoubleValue constructs a DOUBLE parquet value from the float64 passed as
// argument.
func DoubleValue(value float64) Value { return makeValueDouble(value) }

// ByteArrayValue constructs a BYTE_ARRAY parquet value from the byte slice
// passed as argument.
func ByteArrayValue(value []byte) Value { return makeValueBytes(ByteArray, value) }

// FixedLenByteArrayValue constructs a BYTE_ARRAY parquet value from the byte
// slice passed as argument.
func FixedLenByteArrayValue(value []byte) Value { return makeValueBytes(FixedLenByteArray, value) }

func makeValue(k Kind, lt *format.LogicalType, v reflect.Value) Value {
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return Value{}
		}
		if v = v.Elem(); v.Kind() == reflect.Pointer && v.IsNil() {
			return Value{}
		}
	}

	switch v.Type() {
	case reflect.TypeOf(time.Time{}):
		unit := Nanosecond.TimeUnit()
		if lt != nil && lt.Timestamp != nil {
			unit = lt.Timestamp.Unit
		}

		t := v.Interface().(time.Time)
		var val int64
		switch {
		case unit.Millis != nil:
			val = t.UnixMilli()
		case unit.Micros != nil:
			val = t.UnixMicro()
		default:
			val = t.UnixNano()
		}
		return makeValueInt64(val)
	}

	switch k {
	case Boolean:
		return makeValueBoolean(v.Bool())

	case Int32:
		switch v.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32:
			return makeValueInt32(int32(v.Int()))
		case reflect.Uint8, reflect.Uint16, reflect.Uint32:
			return makeValueInt32(int32(v.Uint()))
		}

	case Int64:
		switch v.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
			return makeValueInt64(v.Int())
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint, reflect.Uintptr:
			return makeValueUint64(v.Uint())
		}

	case Int96:
		switch v.Type() {
		case reflect.TypeOf(deprecated.Int96{}):
			return makeValueInt96(v.Interface().(deprecated.Int96))
		}

	case Float:
		switch v.Kind() {
		case reflect.Float32:
			return makeValueFloat(float32(v.Float()))
		}

	case Double:
		switch v.Kind() {
		case reflect.Float32, reflect.Float64:
			return makeValueDouble(v.Float())
		}

	case ByteArray:
		switch v.Kind() {
		case reflect.String:
			return makeValueString(k, v.String())
		case reflect.Slice:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return makeValueBytes(k, v.Bytes())
			}
		}

	case FixedLenByteArray:
		switch v.Kind() {
		case reflect.String: // uuid
			return makeValueString(k, v.String())
		case reflect.Array:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return makeValueFixedLenByteArray(v)
			}
		case reflect.Slice:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return makeValueBytes(k, v.Bytes())
			}
		}
	}

	panic("cannot create parquet value of type " + k.String() + " from go value of type " + v.Type().String())
}

func makeValueKind(kind Kind) Value {
	return Value{kind: ^int8(kind)}
}

func makeValueBoolean(value bool) Value {
	v := Value{kind: ^int8(Boolean)}
	if value {
		v.u64 = 1
	}
	return v
}

func makeValueInt32(value int32) Value {
	return Value{
		kind: ^int8(Int32),
		u64:  uint64(value),
	}
}

func makeValueInt64(value int64) Value {
	return Value{
		kind: ^int8(Int64),
		u64:  uint64(value),
	}
}

func makeValueInt96(value deprecated.Int96) Value {
	// TODO: this is highly inefficient because we need a heap allocation to
	// store the value; we don't expect INT96 to be used frequently since it
	// is a deprecated feature of parquet, and it helps keep the Value type
	// compact for all the other more common cases.
	bits := [12]byte{}
	binary.LittleEndian.PutUint32(bits[0:4], value[0])
	binary.LittleEndian.PutUint32(bits[4:8], value[1])
	binary.LittleEndian.PutUint32(bits[8:12], value[2])
	return Value{
		kind: ^int8(Int96),
		ptr:  &bits[0],
		u64:  12, // set the length so we can use the ByteArray method
	}
}

func makeValueUint32(value uint32) Value {
	return Value{
		kind: ^int8(Int32),
		u64:  uint64(value),
	}
}

func makeValueUint64(value uint64) Value {
	return Value{
		kind: ^int8(Int64),
		u64:  value,
	}
}

func makeValueFloat(value float32) Value {
	return Value{
		kind: ^int8(Float),
		u64:  uint64(math.Float32bits(value)),
	}
}

func makeValueDouble(value float64) Value {
	return Value{
		kind: ^int8(Double),
		u64:  math.Float64bits(value),
	}
}

func makeValueBytes(kind Kind, value []byte) Value {
	return makeValueByteArray(kind, unsafe.SliceData(value), len(value))
}

func makeValueString(kind Kind, value string) Value {
	return makeValueByteArray(kind, unsafe.StringData(value), len(value))
}

func makeValueFixedLenByteArray(v reflect.Value) Value {
	t := v.Type()
	// When the array is addressable, we take advantage of this
	// condition to avoid the heap allocation otherwise needed
	// to pack the reference into an interface{} value.
	if v.CanAddr() {
		v = v.Addr()
	} else {
		u := reflect.New(t)
		u.Elem().Set(v)
		v = u
	}
	return makeValueByteArray(FixedLenByteArray, (*byte)(v.UnsafePointer()), t.Len())
}

func makeValueByteArray(kind Kind, data *byte, size int) Value {
	return Value{
		kind: ^int8(kind),
		ptr:  data,
		u64:  uint64(size),
	}
}

// These methods are internal versions of methods exported by the Value type,
// they are usually inlined by the compiler and intended to be used inside the
// parquet-go package because they tend to generate better code than their
// exported counter part, which requires making a copy of the receiver.
func (v *Value) isNull() bool            { return v.kind == 0 }
func (v *Value) byte() byte              { return byte(v.u64) }
func (v *Value) boolean() bool           { return v.u64 != 0 }
func (v *Value) int32() int32            { return int32(v.u64) }
func (v *Value) int64() int64            { return int64(v.u64) }
func (v *Value) int96() deprecated.Int96 { return makeInt96(v.byteArray()) }
func (v *Value) float() float32          { return math.Float32frombits(uint32(v.u64)) }
func (v *Value) double() float64         { return math.Float64frombits(uint64(v.u64)) }
func (v *Value) uint32() uint32          { return uint32(v.u64) }
func (v *Value) uint64() uint64          { return v.u64 }
func (v *Value) byteArray() []byte       { return unsafe.Slice(v.ptr, v.u64) }
func (v *Value) string() string          { return unsafe.String(v.ptr, v.u64) }
func (v *Value) be128() *[16]byte        { return (*[16]byte)(unsafe.Pointer(v.ptr)) }
func (v *Value) column() int             { return int(^v.columnIndex) }

func (v Value) convertToBoolean(x bool) Value {
	v.kind = ^int8(Boolean)
	v.ptr = nil
	v.u64 = 0
	if x {
		v.u64 = 1
	}
	return v
}

func (v Value) convertToInt32(x int32) Value {
	v.kind = ^int8(Int32)
	v.ptr = nil
	v.u64 = uint64(x)
	return v
}

func (v Value) convertToInt64(x int64) Value {
	v.kind = ^int8(Int64)
	v.ptr = nil
	v.u64 = uint64(x)
	return v
}

func (v Value) convertToInt96(x deprecated.Int96) Value {
	i96 := makeValueInt96(x)
	v.kind = i96.kind
	v.ptr = i96.ptr
	v.u64 = i96.u64
	return v
}

func (v Value) convertToFloat(x float32) Value {
	v.kind = ^int8(Float)
	v.ptr = nil
	v.u64 = uint64(math.Float32bits(x))
	return v
}

func (v Value) convertToDouble(x float64) Value {
	v.kind = ^int8(Double)
	v.ptr = nil
	v.u64 = math.Float64bits(x)
	return v
}

func (v Value) convertToByteArray(x []byte) Value {
	v.kind = ^int8(ByteArray)
	v.ptr = unsafe.SliceData(x)
	v.u64 = uint64(len(x))
	return v
}

func (v Value) convertToFixedLenByteArray(x []byte) Value {
	v.kind = ^int8(FixedLenByteArray)
	v.ptr = unsafe.SliceData(x)
	v.u64 = uint64(len(x))
	return v
}

// Kind returns the kind of v, which represents its parquet physical type.
func (v Value) Kind() Kind { return ^Kind(v.kind) }

// IsNull returns true if v is the null value.
func (v Value) IsNull() bool { return v.isNull() }

// Byte returns v as a byte, which may truncate the underlying byte.
func (v Value) Byte() byte { return v.byte() }

// Boolean returns v as a bool, assuming the underlying type is BOOLEAN.
func (v Value) Boolean() bool { return v.boolean() }

// Int32 returns v as a int32, assuming the underlying type is INT32.
func (v Value) Int32() int32 { return v.int32() }

// Int64 returns v as a int64, assuming the underlying type is INT64.
func (v Value) Int64() int64 { return v.int64() }

// Int96 returns v as a int96, assuming the underlying type is INT96.
func (v Value) Int96() deprecated.Int96 {
	var val deprecated.Int96
	if !v.isNull() {
		val = v.int96()
	}
	return val
}

// Float returns v as a float32, assuming the underlying type is FLOAT.
func (v Value) Float() float32 { return v.float() }

// Double returns v as a float64, assuming the underlying type is DOUBLE.
func (v Value) Double() float64 { return v.double() }

// Uint32 returns v as a uint32, assuming the underlying type is INT32.
func (v Value) Uint32() uint32 { return v.uint32() }

// Uint64 returns v as a uint64, assuming the underlying type is INT64.
func (v Value) Uint64() uint64 { return v.uint64() }

// ByteArray returns v as a []byte, assuming the underlying type is either
// BYTE_ARRAY or FIXED_LEN_BYTE_ARRAY.
//
// The application must treat the returned byte slice as a read-only value,
// mutating the content will result in undefined behaviors.
func (v Value) ByteArray() []byte { return v.byteArray() }

// RepetitionLevel returns the repetition level of v.
func (v Value) RepetitionLevel() int { return int(v.repetitionLevel) }

// DefinitionLevel returns the definition level of v.
func (v Value) DefinitionLevel() int { return int(v.definitionLevel) }

// Column returns the column index within the row that v was created from.
//
// Returns -1 if the value does not carry a column index.
func (v Value) Column() int { return v.column() }

// Bytes returns the binary representation of v.
//
// If v is the null value, an nil byte slice is returned.
func (v Value) Bytes() []byte {
	switch v.Kind() {
	case Boolean:
		buf := [8]byte{}
		binary.LittleEndian.PutUint32(buf[:4], v.uint32())
		return buf[0:1]
	case Int32, Float:
		buf := [8]byte{}
		binary.LittleEndian.PutUint32(buf[:4], v.uint32())
		return buf[:4]
	case Int64, Double:
		buf := [8]byte{}
		binary.LittleEndian.PutUint64(buf[:8], v.uint64())
		return buf[:8]
	case ByteArray, FixedLenByteArray, Int96:
		return v.byteArray()
	default:
		return nil
	}
}

// AppendBytes appends the binary representation of v to b.
//
// If v is the null value, b is returned unchanged.
func (v Value) AppendBytes(b []byte) []byte {
	buf := [8]byte{}
	switch v.Kind() {
	case Boolean:
		binary.LittleEndian.PutUint32(buf[:4], v.uint32())
		return append(b, buf[0])
	case Int32, Float:
		binary.LittleEndian.PutUint32(buf[:4], v.uint32())
		return append(b, buf[:4]...)
	case Int64, Double:
		binary.LittleEndian.PutUint64(buf[:8], v.uint64())
		return append(b, buf[:8]...)
	case ByteArray, FixedLenByteArray, Int96:
		return append(b, v.byteArray()...)
	default:
		return b
	}
}

// Format outputs a human-readable representation of v to w, using r as the
// formatting verb to describe how the value should be printed.
//
// The following formatting options are supported:
//
//	%c	prints the column index
//	%+c	prints the column index, prefixed with "C:"
//	%d	prints the definition level
//	%+d	prints the definition level, prefixed with "D:"
//	%r	prints the repetition level
//	%+r	prints the repetition level, prefixed with "R:"
//	%q	prints the quoted representation of v
//	%+q	prints the quoted representation of v, prefixed with "V:"
//	%s	prints the string representation of v
//	%+s	prints the string representation of v, prefixed with "V:"
//	%v	same as %s
//	%+v	prints a verbose representation of v
//	%#v	prints a Go value representation of v
//
// Format satisfies the fmt.Formatter interface.
func (v Value) Format(w fmt.State, r rune) {
	switch r {
	case 'c':
		if w.Flag('+') {
			io.WriteString(w, "C:")
		}
		fmt.Fprint(w, v.column())

	case 'd':
		if w.Flag('+') {
			io.WriteString(w, "D:")
		}
		fmt.Fprint(w, v.definitionLevel)

	case 'r':
		if w.Flag('+') {
			io.WriteString(w, "R:")
		}
		fmt.Fprint(w, v.repetitionLevel)

	case 'q':
		if w.Flag('+') {
			io.WriteString(w, "V:")
		}
		switch v.Kind() {
		case ByteArray, FixedLenByteArray:
			fmt.Fprintf(w, "%q", v.byteArray())
		default:
			fmt.Fprintf(w, `"%s"`, v)
		}

	case 's':
		if w.Flag('+') {
			io.WriteString(w, "V:")
		}
		switch v.Kind() {
		case Boolean:
			fmt.Fprint(w, v.boolean())
		case Int32:
			fmt.Fprint(w, v.int32())
		case Int64:
			fmt.Fprint(w, v.int64())
		case Int96:
			fmt.Fprint(w, v.int96())
		case Float:
			fmt.Fprint(w, v.float())
		case Double:
			fmt.Fprint(w, v.double())
		case ByteArray, FixedLenByteArray:
			w.Write(v.byteArray())
		default:
			io.WriteString(w, "<null>")
		}

	case 'v':
		switch {
		case w.Flag('+'):
			fmt.Fprintf(w, "%+[1]c %+[1]d %+[1]r %+[1]s", v)
		case w.Flag('#'):
			v.formatGoString(w)
		default:
			v.Format(w, 's')
		}
	}
}

func (v Value) formatGoString(w fmt.State) {
	io.WriteString(w, "parquet.")
	switch v.Kind() {
	case Boolean:
		fmt.Fprintf(w, "BooleanValue(%t)", v.boolean())
	case Int32:
		fmt.Fprintf(w, "Int32Value(%d)", v.int32())
	case Int64:
		fmt.Fprintf(w, "Int64Value(%d)", v.int64())
	case Int96:
		fmt.Fprintf(w, "Int96Value(%#v)", v.int96())
	case Float:
		fmt.Fprintf(w, "FloatValue(%g)", v.float())
	case Double:
		fmt.Fprintf(w, "DoubleValue(%g)", v.double())
	case ByteArray:
		fmt.Fprintf(w, "ByteArrayValue(%q)", v.byteArray())
	case FixedLenByteArray:
		fmt.Fprintf(w, "FixedLenByteArrayValue(%#v)", v.byteArray())
	default:
		io.WriteString(w, "Value{}")
		return
	}
	fmt.Fprintf(w, ".Level(%d,%d,%d)",
		v.RepetitionLevel(),
		v.DefinitionLevel(),
		v.Column(),
	)
}

// String returns a string representation of v.
func (v Value) String() string {
	switch v.Kind() {
	case Boolean:
		return strconv.FormatBool(v.boolean())
	case Int32:
		return strconv.FormatInt(int64(v.int32()), 10)
	case Int64:
		return strconv.FormatInt(v.int64(), 10)
	case Int96:
		return v.Int96().String()
	case Float:
		return strconv.FormatFloat(float64(v.float()), 'g', -1, 32)
	case Double:
		return strconv.FormatFloat(v.double(), 'g', -1, 32)
	case ByteArray, FixedLenByteArray:
		return string(v.byteArray())
	default:
		return "<null>"
	}
}

// GoString returns a Go value string representation of v.
func (v Value) GoString() string { return fmt.Sprintf("%#v", v) }

// Level returns v with the repetition level, definition level, and column index
// set to the values passed as arguments.
//
// The method panics if either argument is negative.
func (v Value) Level(repetitionLevel, definitionLevel, columnIndex int) Value {
	v.repetitionLevel = makeRepetitionLevel(repetitionLevel)
	v.definitionLevel = makeDefinitionLevel(definitionLevel)
	v.columnIndex = ^makeColumnIndex(columnIndex)
	return v
}

// Clone returns a copy of v which does not share any pointers with it.
func (v Value) Clone() Value {
	switch k := v.Kind(); k {
	case ByteArray, FixedLenByteArray:
		v.ptr = unsafe.SliceData(copyBytes(v.byteArray()))
	}
	return v
}

func makeInt96(bits []byte) (i96 deprecated.Int96) {
	return deprecated.Int96{
		2: binary.LittleEndian.Uint32(bits[8:12]),
		1: binary.LittleEndian.Uint32(bits[4:8]),
		0: binary.LittleEndian.Uint32(bits[0:4]),
	}
}

func parseValue(kind Kind, data []byte) (val Value, err error) {
	switch kind {
	case Boolean:
		if len(data) == 1 {
			val = makeValueBoolean(data[0] != 0)
		}
	case Int32:
		if len(data) == 4 {
			val = makeValueInt32(int32(binary.LittleEndian.Uint32(data)))
		}
	case Int64:
		if len(data) == 8 {
			val = makeValueInt64(int64(binary.LittleEndian.Uint64(data)))
		}
	case Int96:
		if len(data) == 12 {
			val = makeValueInt96(makeInt96(data))
		}
	case Float:
		if len(data) == 4 {
			val = makeValueFloat(float32(math.Float32frombits(binary.LittleEndian.Uint32(data))))
		}
	case Double:
		if len(data) == 8 {
			val = makeValueDouble(float64(math.Float64frombits(binary.LittleEndian.Uint64(data))))
		}
	case ByteArray, FixedLenByteArray:
		val = makeValueBytes(kind, data)
	}
	if val.isNull() {
		err = fmt.Errorf("cannot decode %s value from input of length %d", kind, len(data))
	}
	return val, err
}

func copyBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// Equal returns true if v1 and v2 are equal.
//
// Values are considered equal if they are of the same physical type and hold
// the same Go values. For BYTE_ARRAY and FIXED_LEN_BYTE_ARRAY, the content of
// the underlying byte arrays are tested for equality.
//
// Note that the repetition levels, definition levels, and column indexes are
// not compared by this function, use DeepEqual instead.
func Equal(v1, v2 Value) bool {
	if v1.kind != v2.kind {
		return false
	}
	switch ^Kind(v1.kind) {
	case Boolean:
		return v1.boolean() == v2.boolean()
	case Int32:
		return v1.int32() == v2.int32()
	case Int64:
		return v1.int64() == v2.int64()
	case Int96:
		return v1.int96() == v2.int96()
	case Float:
		return v1.float() == v2.float()
	case Double:
		return v1.double() == v2.double()
	case ByteArray, FixedLenByteArray:
		return bytes.Equal(v1.byteArray(), v2.byteArray())
	case -1: // null
		return true
	default:
		return false
	}
}

// DeepEqual returns true if v1 and v2 are equal, including their repetition
// levels, definition levels, and column indexes.
//
// See Equal for details about how value equality is determined.
func DeepEqual(v1, v2 Value) bool {
	return Equal(v1, v2) &&
		v1.repetitionLevel == v2.repetitionLevel &&
		v1.definitionLevel == v2.definitionLevel &&
		v1.columnIndex == v2.columnIndex
}

var (
	_ fmt.Formatter = Value{}
	_ fmt.Stringer  = Value{}
)

func clearValues(values []Value) {
	for i := range values {
		values[i] = Value{}
	}
}

// BooleanReader is an interface implemented by ValueReader instances which
// expose the content of a column of boolean values.
type BooleanReader interface {
	// Read boolean values into the buffer passed as argument.
	//
	// The method returns io.EOF when all values have been read.
	ReadBooleans(values []bool) (int, error)
}

// BooleanWriter is an interface implemented by ValueWriter instances which
// support writing columns of boolean values.
type BooleanWriter interface {
	// Write boolean values.
	//
	// The method returns the number of values written, and any error that
	// occurred while writing the values.
	WriteBooleans(values []bool) (int, error)
}

// Int32Reader is an interface implemented by ValueReader instances which expose
// the content of a column of int32 values.
type Int32Reader interface {
	// Read 32 bits integer values into the buffer passed as argument.
	//
	// The method returns io.EOF when all values have been read.
	ReadInt32s(values []int32) (int, error)
}

// Int32Writer is an interface implemented by ValueWriter instances which
// support writing columns of 32 bits signed integer values.
type Int32Writer interface {
	// Write 32 bits signed integer values.
	//
	// The method returns the number of values written, and any error that
	// occurred while writing the values.
	WriteInt32s(values []int32) (int, error)
}

// Int64Reader is an interface implemented by ValueReader instances which expose
// the content of a column of int64 values.
type Int64Reader interface {
	// Read 64 bits integer values into the buffer passed as argument.
	//
	// The method returns io.EOF when all values have been read.
	ReadInt64s(values []int64) (int, error)
}

// Int64Writer is an interface implemented by ValueWriter instances which
// support writing columns of 64 bits signed integer values.
type Int64Writer interface {
	// Write 64 bits signed integer values.
	//
	// The method returns the number of values written, and any error that
	// occurred while writing the values.
	WriteInt64s(values []int64) (int, error)
}

// Int96Reader is an interface implemented by ValueReader instances which expose
// the content of a column of int96 values.
type Int96Reader interface {
	// Read 96 bits integer values into the buffer passed as argument.
	//
	// The method returns io.EOF when all values have been read.
	ReadInt96s(values []deprecated.Int96) (int, error)
}

// Int96Writer is an interface implemented by ValueWriter instances which
// support writing columns of 96 bits signed integer values.
type Int96Writer interface {
	// Write 96 bits signed integer values.
	//
	// The method returns the number of values written, and any error that
	// occurred while writing the values.
	WriteInt96s(values []deprecated.Int96) (int, error)
}

// FloatReader is an interface implemented by ValueReader instances which expose
// the content of a column of single-precision floating point values.
type FloatReader interface {
	// Read single-precision floating point values into the buffer passed as
	// argument.
	//
	// The method returns io.EOF when all values have been read.
	ReadFloats(values []float32) (int, error)
}

// FloatWriter is an interface implemented by ValueWriter instances which
// support writing columns of single-precision floating point values.
type FloatWriter interface {
	// Write single-precision floating point values.
	//
	// The method returns the number of values written, and any error that
	// occurred while writing the values.
	WriteFloats(values []float32) (int, error)
}

// DoubleReader is an interface implemented by ValueReader instances which
// expose the content of a column of double-precision float point values.
type DoubleReader interface {
	// Read double-precision floating point values into the buffer passed as
	// argument.
	//
	// The method returns io.EOF when all values have been read.
	ReadDoubles(values []float64) (int, error)
}

// DoubleWriter is an interface implemented by ValueWriter instances which
// support writing columns of double-precision floating point values.
type DoubleWriter interface {
	// Write double-precision floating point values.
	//
	// The method returns the number of values written, and any error that
	// occurred while writing the values.
	WriteDoubles(values []float64) (int, error)
}

// ByteArrayReader is an interface implemented by ValueReader instances which
// expose the content of a column of variable length byte array values.
type ByteArrayReader interface {
	// Read values into the byte buffer passed as argument, returning the number
	// of values written to the buffer (not the number of bytes). Values are
	// written using the PLAIN encoding, each byte array prefixed with its
	// length encoded as a 4 bytes little endian unsigned integer.
	//
	// The method returns io.EOF when all values have been read.
	//
	// If the buffer was not empty, but too small to hold at least one value,
	// io.ErrShortBuffer is returned.
	ReadByteArrays(values []byte) (int, error)
}

// ByteArrayWriter is an interface implemented by ValueWriter instances which
// support writing columns of variable length byte array values.
type ByteArrayWriter interface {
	// Write variable length byte array values.
	//
	// The values passed as input must be laid out using the PLAIN encoding,
	// with each byte array prefixed with the four bytes little endian unsigned
	// integer length.
	//
	// The method returns the number of values written to the underlying column
	// (not the number of bytes), or any error that occurred while attempting to
	// write the values.
	WriteByteArrays(values []byte) (int, error)
}

// FixedLenByteArrayReader is an interface implemented by ValueReader instances
// which expose the content of a column of fixed length byte array values.
type FixedLenByteArrayReader interface {
	// Read values into the byte buffer passed as argument, returning the number
	// of values written to the buffer (not the number of bytes).
	//
	// The method returns io.EOF when all values have been read.
	//
	// If the buffer was not empty, but too small to hold at least one value,
	// io.ErrShortBuffer is returned.
	ReadFixedLenByteArrays(values []byte) (int, error)
}

// FixedLenByteArrayWriter is an interface implemented by ValueWriter instances
// which support writing columns of fixed length byte array values.
type FixedLenByteArrayWriter interface {
	// Writes the fixed length byte array values.
	//
	// The size of the values is assumed to be the same as the expected size of
	// items in the column. The method errors if the length of the input values
	// is not a multiple of the expected item size.
	WriteFixedLenByteArrays(values []byte) (int, error)
}
