package parquet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Decimal constructs a leaf node of decimal logical type with the given
// scale, precision, and underlying type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#decimal
func Decimal(scale, precision int, typ Type) Node {
	switch typ.Kind() {
	case Int32:
		if precision < 1 || precision > 9 {
			panic("DECIMAL annotated with Int32 must have precision >= 1 and <= 9, got " + strconv.Itoa(precision))
		}
	case Int64:
		if precision < 1 || precision > 18 {
			panic("DECIMAL annotated with Int64 must have precision >= 1 and <= 18, got " + strconv.Itoa(precision))
		}
		if precision < 10 {
			log.Printf("WARNING: DECIMAL annotated with Int64 should have a precision >= 10, got %d", precision)
		}
	case ByteArray, FixedLenByteArray:
	default:
		panic("DECIMAL node must annotate Int32, Int64, ByteArray or FixedLenByteArray but got " + typ.String())
	}
	return Leaf(&decimalType{
		decimal: format.DecimalType{
			Scale:     int32(scale),
			Precision: int32(precision),
		},
		Type: typ,
	})
}

type decimalType struct {
	decimal format.DecimalType
	Type
}

func (t *decimalType) String() string { return t.decimal.String() }

// isBinary returns true when the decimal values are stored as big-endian
// two's-complement binary values, which is the case for the BYTE_ARRAY and
// FIXED_LEN_BYTE_ARRAY physical types.
//
// The physical byte array types compare values as unsigned bytes, which does
// not match the signed sort order that the parquet format defines for the
// DECIMAL logical type; every negative value would sort above every positive
// value. The decimalType methods below override the constructors of objects
// involved in the computation of statistics (column buffers, pages,
// dictionaries, column indexes) to apply the signed comparison instead.
//
// INT32 and INT64 backed decimals delegate to the physical type, which is
// already signed.
func (t *decimalType) isBinary() bool {
	kind := t.Type.Kind()
	return kind == ByteArray || kind == FixedLenByteArray
}

// Compare implements the DECIMAL sort order defined by the parquet format:
// signed comparison of the big-endian two's-complement values for decimals
// backed by BYTE_ARRAY or FIXED_LEN_BYTE_ARRAY.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#decimal
func (t *decimalType) Compare(a, b Value) int {
	if t.isBinary() {
		return compareDecimalByteArrays(a.byteArray(), b.byteArray())
	}
	return t.Type.Compare(a, b)
}

func (t *decimalType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	if t.isBinary() {
		return &decimalColumnBuffer{
			ColumnBuffer: t.Type.NewColumnBuffer(columnIndex, numValues),
			typ:          t,
		}
	}
	return t.Type.NewColumnBuffer(columnIndex, numValues)
}

func (t *decimalType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	if t.isBinary() {
		return &decimalPage{
			Page: t.Type.NewPage(columnIndex, numValues, data),
			typ:  t,
		}
	}
	return t.Type.NewPage(columnIndex, numValues, data)
}

func (t *decimalType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	if t.isBinary() {
		return &decimalDictionary{
			Dictionary: t.Type.NewDictionary(columnIndex, numValues, data),
			typ:        t,
		}
	}
	return t.Type.NewDictionary(columnIndex, numValues, data)
}

func (t *decimalType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	if t.isBinary() {
		return &decimalColumnIndexer{}
	}
	return t.Type.NewColumnIndexer(sizeLimit)
}

// compareDecimalByteArrays compares two big-endian two's-complement values,
// which may have different lengths for decimals backed by BYTE_ARRAY.
func compareDecimalByteArrays(a, b []byte) int {
	negA := len(a) > 0 && a[0]&0x80 != 0
	negB := len(b) > 0 && b[0]&0x80 != 0
	switch {
	case negA && !negB:
		return -1
	case !negA && negB:
		return +1
	}
	pad := byte(0x00)
	if negA {
		pad = 0xFF
	}
	if len(a) < len(b) {
		return -compareDecimalPadded(b, a, pad)
	}
	return compareDecimalPadded(a, b, pad)
}

// compareDecimalPadded compares a to b sign-extended to the length of a,
// assuming len(a) >= len(b) and both values have the same sign, in which case
// the comparison of the sign-extended representations reduces to an unsigned
// byte-wise comparison.
func compareDecimalPadded(a, b []byte, pad byte) int {
	for _, c := range a[:len(a)-len(b)] {
		switch {
		case c < pad:
			return -1
		case c > pad:
			return +1
		}
	}
	return bytes.Compare(a[len(a)-len(b):], b)
}

// decimalColumnBuffer wraps the column buffer of the underlying physical type
// to apply the signed decimal sort order when computing page bounds and
// sorting values.
type decimalColumnBuffer struct {
	ColumnBuffer
	typ *decimalType
}

func (col *decimalColumnBuffer) Clone() ColumnBuffer {
	return &decimalColumnBuffer{
		ColumnBuffer: col.ColumnBuffer.Clone(),
		typ:          col.typ,
	}
}

func (col *decimalColumnBuffer) Page() Page {
	return &decimalPage{Page: col.ColumnBuffer.Page(), typ: col.typ}
}

func (col *decimalColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *decimalColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return decimalColumnIndex{page: col.Page().(*decimalPage)}, nil
}

func (col *decimalColumnBuffer) Less(i, j int) bool {
	u := [1]Value{}
	v := [1]Value{}
	col.ColumnBuffer.ReadValuesAt(u[:], int64(i))
	col.ColumnBuffer.ReadValuesAt(v[:], int64(j))
	return compareDecimalByteArrays(u[0].byteArray(), v[0].byteArray()) < 0
}

// writeInt64 stores value as big-endian two's complement. The physical
// fixed-length buffer zero-pads, which turns a negative int64 into a wider
// positive value when the column is longer than 8 bytes. Sign-extend instead
// so the extra leading bytes are 0xFF.
func (col *decimalColumnBuffer) writeInt64(levels columnLevels, value int64) {
	if col.typ.Kind() != FixedLenByteArray {
		col.ColumnBuffer.writeInt64(levels, value)
		return
	}
	size := col.typ.Length()
	if size <= 8 {
		col.ColumnBuffer.writeInt64(levels, value)
		return
	}
	var stack [16]byte
	var buf []byte
	if size <= len(stack) {
		buf = stack[:size]
	} else {
		buf = make([]byte, size)
	}
	if value < 0 {
		for i := range size - 8 {
			buf[i] = 0xFF
		}
	}
	binary.BigEndian.PutUint64(buf[size-8:], uint64(value))
	col.ColumnBuffer.writeByteArray(levels, buf)
}

// decimalPage wraps a page of the underlying physical type to compute bounds
// with the signed decimal sort order.
type decimalPage struct {
	Page
	typ *decimalType
}

func (page *decimalPage) Bounds() (min, max Value, ok bool) {
	values := page.Page.Values()
	buffer := make([]Value, 64)
	for {
		n, err := values.ReadValues(buffer)
		for _, v := range buffer[:n] {
			if !ok {
				min, max, ok = v, v, true
				continue
			}
			if compareDecimalByteArrays(v.byteArray(), min.byteArray()) < 0 {
				min = v
			}
			if compareDecimalByteArrays(v.byteArray(), max.byteArray()) > 0 {
				max = v
			}
		}
		if err != nil || n == 0 {
			break
		}
	}
	return min, max, ok
}

func (page *decimalPage) Slice(i, j int64) Page {
	return &decimalPage{Page: page.Page.Slice(i, j), typ: page.typ}
}

// decimalDictionary wraps the dictionary of the underlying physical type to
// compute bounds with the signed decimal sort order.
type decimalDictionary struct {
	Dictionary
	typ *decimalType
}

func (d *decimalDictionary) Type() Type { return newIndexedType(d.typ, d) }

func (d *decimalDictionary) Bounds(indexes []int32) (min, max Value) {
	if len(indexes) == 0 {
		return min, max
	}
	min = d.Index(indexes[0])
	max = min
	for _, i := range indexes[1:] {
		v := d.Index(i)
		switch {
		case compareDecimalByteArrays(v.byteArray(), min.byteArray()) < 0:
			min = v
		case compareDecimalByteArrays(v.byteArray(), max.byteArray()) > 0:
			max = v
		}
	}
	return min, max
}

// decimalColumnIndex is the column index of a single in-memory page of binary
// decimal values.
type decimalColumnIndex struct{ page *decimalPage }

func (i decimalColumnIndex) NumPages() int       { return 1 }
func (i decimalColumnIndex) NullCount(int) int64 { return 0 }
func (i decimalColumnIndex) NullPage(int) bool   { return false }
func (i decimalColumnIndex) MinValue(int) Value {
	min, _, _ := i.page.Bounds()
	return min
}
func (i decimalColumnIndex) MaxValue(int) Value {
	_, max, _ := i.page.Bounds()
	return max
}
func (i decimalColumnIndex) IsAscending() bool  { return false }
func (i decimalColumnIndex) IsDescending() bool { return false }

// decimalColumnIndexer computes the column index of binary decimal columns,
// determining the boundary order with the signed decimal sort order.
//
// Unlike the indexers of the physical byte array types, min/max values are
// never truncated: prefix truncation does not preserve the ordering of
// two's-complement values.
type decimalColumnIndexer struct {
	baseColumnIndexer
	minValues [][]byte
	maxValues [][]byte
}

func (i *decimalColumnIndexer) Reset() {
	i.reset()
	i.minValues = i.minValues[:0]
	i.maxValues = i.maxValues[:0]
}

func (i *decimalColumnIndexer) IndexPage(numValues, numNulls int64, min, max Value) {
	i.observe(numValues, numNulls)
	i.minValues = append(i.minValues, copyBytes(min.byteArray()))
	i.maxValues = append(i.maxValues, copyBytes(max.byteArray()))
}

func (i *decimalColumnIndexer) ColumnIndex() format.ColumnIndex {
	return i.columnIndex(
		slices.Clone(i.minValues),
		slices.Clone(i.maxValues),
		orderOfDecimalBytes(i.minValues),
		orderOfDecimalBytes(i.maxValues),
	)
}

// orderOfDecimalBytes mirrors orderOfBytes but uses the signed decimal sort
// order instead of the unsigned byte order.
func orderOfDecimalBytes(data [][]byte) int {
	switch len(data) {
	case 0, 1:
		return 0
	}
	i := 1
	for i < len(data) && compareDecimalByteArrays(data[i-1], data[i]) == 0 {
		i++
	}
	data = data[i-1:]
	if len(data) < 2 {
		return 1
	}
	switch ordering := compareDecimalByteArrays(data[0], data[1]); {
	case ordering < 0:
		for j := 2; j < len(data); j++ {
			if compareDecimalByteArrays(data[j-1], data[j]) > 0 {
				return 0
			}
		}
		return +1
	case ordering > 0:
		for j := 2; j < len(data); j++ {
			if compareDecimalByteArrays(data[j-1], data[j]) < 0 {
				return 0
			}
		}
		return -1
	}
	return 0
}

func (t *decimalType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Value: &t.decimal}
}

func (t *decimalType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Decimal]
}

// ConvertValue decodes a BYTE_ARRAY or FIXED_LEN_BYTE_ARRAY decimal into an
// integer decimal. Those physical types store a big-endian two's-complement
// value. The generic byte-array conversion reads little-endian, which turns
// the unscaled integer 100000 into a different int64.
func (t *decimalType) ConvertValue(val Value, typ Type) (Value, error) {
	if val.IsNull() || t.isBinary() {
		return t.Type.ConvertValue(val, typ)
	}
	if typ.Kind() != ByteArray && typ.Kind() != FixedLenByteArray {
		return t.Type.ConvertValue(val, typ)
	}
	n, err := int64FromDecimalBytes(val.byteArray())
	if err != nil {
		return val, err
	}
	switch t.Kind() {
	case Int64:
		return val.convertToInt64(n), nil
	case Int32:
		if n > math.MaxInt32 || n < math.MinInt32 {
			return val, fmt.Errorf("decimal value %d does not fit in int32", n)
		}
		return val.convertToInt32(int32(n)), nil
	default:
		return t.Type.ConvertValue(val, typ)
	}
}

// int64FromDecimalBytes reads a big-endian two's-complement decimal. A value
// wider than 8 bytes is accepted only when the extra leading bytes are the
// sign extension of an int64.
func int64FromDecimalBytes(data []byte) (int64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if len(data) > 8 {
		sign := byte(0)
		if data[len(data)-8]&0x80 != 0 {
			sign = 0xFF
		}
		for _, b := range data[:len(data)-8] {
			if b != sign {
				return 0, fmt.Errorf("decimal value does not fit in int64")
			}
		}
		data = data[len(data)-8:]
	}
	var b [8]byte
	if data[0]&0x80 != 0 {
		for i := range b {
			b[i] = 0xFF
		}
	}
	copy(b[8-len(data):], data)
	return int64(binary.BigEndian.Uint64(b[:])), nil
}

func (t *decimalType) AssignValue(dst reflect.Value, src Value) error {
	switch t.Type {
	case Int32Type:
		switch dst.Kind() {
		case reflect.Int32:
			dst.SetInt(int64(src.int32()))
		default:
			dst.Set(reflect.ValueOf(float32(src.int32()) / float32(math.Pow10(int(t.decimal.Scale)))))
		}
	case Int64Type:
		switch dst.Kind() {
		case reflect.Int64:
			dst.SetInt(src.int64())
		default:
			dst.Set(reflect.ValueOf(float64(src.int64()) / math.Pow10(int(t.decimal.Scale))))
		}
	default:
		// ByteArray and FixedLenByteArray
		if t.Type.Kind() != ByteArray && t.Type.Kind() != FixedLenByteArray {
			return nil
		}
		switch dst.Kind() {
		case reflect.Slice, reflect.Array, reflect.String:
			// Destinations like []byte, [N]byte, or string receive the raw
			// big-endian two's-complement representation of the decimal
			// value, as they did when decimalType inherited AssignValue
			// from its underlying byte array type.
			return t.Type.AssignValue(dst, src)
		}
		data := src.ByteArray()
		val := new(big.Int)
		if len(data) > 0 && data[0]&0x80 != 0 {
			// Negative number: convert from two's complement
			tmp := make([]byte, len(data))
			for i, b := range data {
				tmp[i] = ^b
			}
			val.SetBytes(tmp)
			val.Add(val, big.NewInt(1))
			val.Neg(val)
		} else {
			val.SetBytes(data)
		}
		// Use enough precision to represent the decimal value accurately
		// precision * log2(10) ≈ precision * 3.32, round up generously
		prec := max(uint(t.decimal.Precision)*4+64, 192)
		f := new(big.Float).SetPrec(prec).SetInt(val)
		scaleFactor := new(big.Float).SetPrec(prec)
		scaleFactor.SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(t.decimal.Scale)), nil))
		f.Quo(f, scaleFactor)
		switch dst.Kind() {
		case reflect.Float32, reflect.Float64:
			v, _ := f.Float64()
			dst.SetFloat(v)
		default:
			dst.Set(reflect.ValueOf(f))
		}
	}
	return nil
}
