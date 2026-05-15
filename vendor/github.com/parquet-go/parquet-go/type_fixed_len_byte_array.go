package parquet

import (
	"bytes"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

type fixedLenByteArrayType struct {
	length int
	isUUID bool
}

func (t fixedLenByteArrayType) String() string {
	return fmt.Sprintf("FIXED_LEN_BYTE_ARRAY(%d)", t.length)
}

func (t fixedLenByteArrayType) Kind() Kind { return FixedLenByteArray }

func (t fixedLenByteArrayType) Length() int { return t.length }

func (t fixedLenByteArrayType) EstimateSize(n int) int { return t.length * n }

func (t fixedLenByteArrayType) EstimateNumValues(n int) int { return n / t.length }

func (t fixedLenByteArrayType) Compare(a, b Value) int {
	return bytes.Compare(a.byteArray(), b.byteArray())
}

func (t fixedLenByteArrayType) ColumnOrder() *format.ColumnOrder { return &typeDefinedColumnOrder }

func (t fixedLenByteArrayType) LogicalType() *format.LogicalType { return nil }

func (t fixedLenByteArrayType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t fixedLenByteArrayType) PhysicalType() *format.Type { return &physicalTypes[FixedLenByteArray] }

func (t fixedLenByteArrayType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newFixedLenByteArrayColumnIndexer(t.length, sizeLimit)
}

func (t fixedLenByteArrayType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newFixedLenByteArrayColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t fixedLenByteArrayType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newFixedLenByteArrayDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t fixedLenByteArrayType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newFixedLenByteArrayPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t fixedLenByteArrayType) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.FixedLenByteArrayValues(values, t.length)
}

func (t fixedLenByteArrayType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeFixedLenByteArray(dst, src, enc)
}

func (t fixedLenByteArrayType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeFixedLenByteArray(dst, src, enc)
}

func (t fixedLenByteArrayType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t fixedLenByteArrayType) AssignValue(dst reflect.Value, src Value) error {
	v := src.byteArray()
	switch dst.Kind() {
	case reflect.Array:
		if dst.Type().Elem().Kind() == reflect.Uint8 && dst.Len() == len(v) {
			// This code could be implemented as a call to reflect.Copy but
			// it would require creating a reflect.Value from v which causes
			// the heap allocation to pack the []byte value. To avoid this
			// overhead we instead convert the reflect.Value holding the
			// destination array into a byte slice which allows us to use
			// a more efficient call to copy.
			d := unsafe.Slice((*byte)(reflectValueData(dst)), len(v))
			copy(d, v)
			return nil
		}
	case reflect.Slice:
		dst.SetBytes(copyBytes(v))
		return nil
	case reflect.String:
		if t.isUUID {
			dst.SetString(uuid.UUID(v).String())
			return nil
		}
	}

	val := reflect.ValueOf(copyBytes(v))
	dst.Set(val)
	return nil
}

func reflectValueData(v reflect.Value) unsafe.Pointer {
	return (*[2]unsafe.Pointer)(unsafe.Pointer(&v))[1]
}

func reflectValuePointer(v reflect.Value) unsafe.Pointer {
	if v.Kind() == reflect.Map {
		// Map values are inlined in the reflect.Value data area,
		// because they are a reference type and their paointer is
		// packed in the interface. However, we need to get an
		// address to the pointer itself, so we extract it and
		// return the address of this pointer. It causes a heap
		// allocation, which is unfortunate, an we would probably
		// want to optimize away eventually.
		p := v.UnsafePointer()
		return unsafe.Pointer(&p)
	}
	return reflectValueData(v)
}

func (t fixedLenByteArrayType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToFixedLenByteArray(val, t.length)
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToFixedLenByteArray(val, t.length)
	case Int32:
		return convertInt32ToFixedLenByteArray(val, t.length)
	case Int64:
		return convertInt64ToFixedLenByteArray(val, t.length)
	case Int96:
		return convertInt96ToFixedLenByteArray(val, t.length)
	case Float:
		return convertFloatToFixedLenByteArray(val, t.length)
	case Double:
		return convertDoubleToFixedLenByteArray(val, t.length)
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToFixedLenByteArray(val, t.length)
	default:
		return makeValueBytes(FixedLenByteArray, make([]byte, t.length)), nil
	}
}

// BE128 stands for "big-endian 128 bits". This type is used as a special case
// for fixed-length byte arrays of 16 bytes, which are commonly used to
// represent columns of random unique identifiers such as UUIDs.
//
// Comparisons of BE128 values use the natural byte order, the zeroth byte is
// the most significant byte.
//
// The special case is intended to provide optimizations based on the knowledge
// that the values are 16 bytes long. Stronger type checking can also be applied
// by the compiler when using [16]byte values rather than []byte, reducing the
// risk of errors on these common code paths.
type be128Type struct {
	isUUID bool
}

func (t be128Type) String() string { return "FIXED_LEN_BYTE_ARRAY(16)" }

func (t be128Type) Kind() Kind { return FixedLenByteArray }

func (t be128Type) Length() int { return 16 }

func (t be128Type) EstimateSize(n int) int { return 16 * n }

func (t be128Type) EstimateNumValues(n int) int { return n / 16 }

func (t be128Type) Compare(a, b Value) int { return compareBE128(a.be128(), b.be128()) }

func (t be128Type) ColumnOrder() *format.ColumnOrder { return &typeDefinedColumnOrder }

func (t be128Type) LogicalType() *format.LogicalType { return nil }

func (t be128Type) ConvertedType() *deprecated.ConvertedType { return nil }

func (t be128Type) PhysicalType() *format.Type { return &physicalTypes[FixedLenByteArray] }

func (t be128Type) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newBE128ColumnIndexer()
}

func (t be128Type) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newBE128ColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t be128Type) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newBE128Dictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t be128Type) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newBE128Page(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t be128Type) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.FixedLenByteArrayValues(values, 16)
}

func (t be128Type) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeFixedLenByteArray(dst, src, enc)
}

func (t be128Type) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeFixedLenByteArray(dst, src, enc)
}

func (t be128Type) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t be128Type) AssignValue(dst reflect.Value, src Value) error {
	return fixedLenByteArrayType{length: 16, isUUID: t.isUUID}.AssignValue(dst, src)
}

func (t be128Type) ConvertValue(val Value, typ Type) (Value, error) {
	return fixedLenByteArrayType{length: 16, isUUID: t.isUUID}.ConvertValue(val, typ)
}

// FixedLenByteArrayType constructs a type for fixed-length values of the given
// size (in bytes).
func FixedLenByteArrayType(length int) Type {
	switch length {
	case 16:
		return be128Type{}
	default:
		return fixedLenByteArrayType{length: length}
	}
}
