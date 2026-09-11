package parquet

import (
	"math/bits"

	"github.com/parquet-go/parquet-go/format"
	"github.com/parquet-go/parquet-go/variant"
)

// This file implements the spec-compliant shredded variant write path. It
// reuses the shreddedVariantGroup tree from variant_shredded_read.go for
// column layout and level bookkeeping, and writes values per the case tables
// of the Variant Shredding specification:
//
//   - Values that exactly match the shredded type go into typed_value with a
//     null value column.
//   - Values that do not match are encoded as plain variant values into the
//     value column (sharing the row's top-level metadata dictionary), with
//     all typed_value columns null.
//   - Objects shred field-wise: shredded fields go into the typed_value
//     group (missing fields have both columns null), residual fields are
//     encoded as an object into the value column. Shredded field names never
//     appear in the value column.
//   - Arrays shred element-wise through the 3-level LIST structure; variant
//     null elements are encoded as variant null (0x00) in the element's
//     value column.
//
// The row's metadata dictionary contains every object field name of the
// value, shredded or not, so residual values and reconstruction share one
// dictionary. VariantShredding.md: "All value columns within the Variant
// must use the same metadata. All field names of a Variant, whether shredded
// or not, must be present in the metadata."

// addVariantFieldNames pre-registers all object field names of v in the
// metadata builder, so the row metadata covers shredded and unshredded
// fields alike.
func addVariantFieldNames(b *variant.MetadataBuilder, v variant.Value) {
	switch v.Basic() {
	case variant.BasicObject:
		for _, f := range v.ObjectValue().Fields {
			b.Add(f.Name)
			addVariantFieldNames(b, f.Value)
		}
	case variant.BasicArray:
		for _, e := range v.ArrayValue().Elements {
			addVariantFieldNames(b, e)
		}
	}
}

// shredLevels carries the absolute level context for one occurrence of a
// shredded variant group during writing.
type shredLevels struct {
	baseDef int  // definition level at which the variant group is present
	baseRep int  // repetition depth of the variant group
	rep     byte // repetition level for the first value of this occurrence
}

func (l shredLevels) def(rel int) byte { return byte(l.baseDef + rel) }

// appendShredded appends a leaf value with the given levels.
func appendShredded(columns [][]Value, colBase uint16, col int, v Value, def, rep byte) {
	c := colBase + uint16(col)
	v.definitionLevel = def
	v.repetitionLevel = rep
	v.columnIndex = ^c
	columns[c] = append(columns[c], v)
}

// appendShreddedNulls appends a null to every leaf column in [start, end) at
// the given levels.
func appendShreddedNulls(columns [][]Value, colBase uint16, start, end int, def, rep byte) {
	for col := start; col < end; col++ {
		appendShredded(columns, colBase, col, Value{}, def, rep)
	}
}

// write encodes one occurrence of the variant group. present=false writes a
// missing occurrence (both value and typed_value null), which is how absent
// object fields are represented.
func (g *shreddedVariantGroup) write(columns [][]Value, colBase uint16, b *variant.MetadataBuilder, v variant.Value, present bool, l shredLevels) {
	if !present {
		if g.valueCol >= 0 {
			appendShredded(columns, colBase, g.valueCol, Value{}, l.def(g.defLevel), l.rep)
		}
		if g.typed != nil {
			appendShreddedNulls(columns, colBase, g.typed.startCol, g.typed.startCol+g.typed.numCols, l.def(g.defLevel), l.rep)
		}
		return
	}

	if g.typed == nil {
		g.writeValueFallback(columns, colBase, b, v, l)
		return
	}

	switch g.typed.kind {
	case shreddedTypedPrimitive:
		if pv, ok := variantToParquetValue(v, g.typed.typ); ok {
			appendShredded(columns, colBase, g.typed.startCol, pv, l.def(g.typed.defLevel), l.rep)
			if g.valueCol >= 0 {
				appendShredded(columns, colBase, g.valueCol, Value{}, l.def(g.defLevel), l.rep)
			}
			return
		}

	case shreddedTypedList:
		if v.Basic() == variant.BasicArray {
			g.typed.writeList(columns, colBase, b, v, l)
			if g.valueCol >= 0 {
				appendShredded(columns, colBase, g.valueCol, Value{}, l.def(g.defLevel), l.rep)
			}
			return
		}

	case shreddedTypedObject:
		if v.Basic() == variant.BasicObject {
			g.typed.writeObject(columns, colBase, b, v, g, l)
			return
		}
	}

	// The value does not match the shredded type: write it to the value
	// column and null out the typed columns.
	g.writeValueFallback(columns, colBase, b, v, l)
	appendShreddedNulls(columns, colBase, g.typed.startCol, g.typed.startCol+g.typed.numCols, l.def(g.defLevel), l.rep)
}

// writeValueFallback encodes v as a plain variant value into the value
// column, sharing the row's metadata dictionary.
func (g *shreddedVariantGroup) writeValueFallback(columns [][]Value, colBase uint16, b *variant.MetadataBuilder, v variant.Value, l shredLevels) {
	if g.valueCol < 0 {
		panic("variant: value does not match the shredded type and the schema has no value column to fall back to")
	}
	encoded := variant.Encode(b, v)
	pv := ByteArrayValue(encoded)
	appendShredded(columns, colBase, g.valueCol, pv, l.def(g.defLevel)+1, l.rep)
}

// writeList shreds an array value through the 3-level LIST structure.
func (t *shreddedTypedValue) writeList(columns [][]Value, colBase uint16, b *variant.MetadataBuilder, v variant.Value, l shredLevels) {
	elements := v.ArrayValue().Elements
	if len(elements) == 0 {
		// typed_value present but the list is empty: every leaf records the
		// list's definition level.
		for col := t.startCol; col < t.startCol+t.numCols; col++ {
			appendShredded(columns, colBase, col, Value{}, l.def(t.defLevel), l.rep)
		}
		return
	}
	elemRep := byte(l.baseRep + t.element.repDepth)
	for i, elem := range elements {
		el := l
		if i > 0 {
			el.rep = elemRep
		}
		// Variant null elements are written as variant null (0x00) in the
		// element's value column, via the fallback path.
		t.element.write(columns, colBase, b, elem, true, el)
	}
}

// writeObject shreds an object value: schema fields go into the typed_value
// group, residual fields into the value column as a variant object.
func (t *shreddedTypedValue) writeObject(columns [][]Value, colBase uint16, b *variant.MetadataBuilder, v variant.Value, g *shreddedVariantGroup, l shredLevels) {
	objFields := v.ObjectValue().Fields
	for _, f := range t.fields {
		fv, ok := findVariantField(objFields, f.name)
		f.group.write(columns, colBase, b, fv, ok, l)
	}

	var residual []variant.Field
	for _, f := range objFields {
		if t.fieldByName(f.Name) == nil {
			residual = append(residual, f)
		}
	}
	if len(residual) > 0 {
		g.writeValueFallback(columns, colBase, b, variant.MakeObject(residual), l)
	} else if g.valueCol >= 0 {
		appendShredded(columns, colBase, g.valueCol, Value{}, l.def(g.defLevel), l.rep)
	}
}

// findVariantField returns the value of the named field and whether it was
// found. Objects are typically small, so a linear scan beats building a map
// per row.
func findVariantField(fields []variant.Field, name string) (variant.Value, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f.Value, true
		}
	}
	return variant.Value{}, false
}

// isSignedIntOrNil reports whether lt is absent, or carries a signed INT
// annotation of the given bit width. A physical int column with no annotation
// is treated as a signed integer of its natural width.
func isSignedIntOrNil(lt *format.LogicalType, bitWidth int8) bool {
	if lt == nil {
		return true
	}
	it, ok := logicalTypeOf[*format.IntType](lt)
	return ok && it.IsSigned && it.BitWidth == bitWidth
}

// variantToParquetValue converts a variant value to a parquet value of the
// given shredded column type when the types match exactly (following
// parquet-java's strict matching semantics: no numeric widening or
// narrowing). The boolean result reports whether the types matched.
func variantToParquetValue(v variant.Value, typ Type) (Value, bool) {
	if v.Basic() != variant.BasicPrimitive && v.Basic() != variant.BasicShortString {
		return Value{}, false
	}
	lt := typ.LogicalType()
	switch v.Type() {
	case variant.PrimitiveTrue, variant.PrimitiveFalse:
		if lt == nil && typ.Kind() == Boolean {
			return BooleanValue(v.BoolValue()), true
		}
	case variant.PrimitiveInt8:
		if it, ok := logicalTypeOf[*format.IntType](lt); ok && it.IsSigned && it.BitWidth == 8 {
			return Int32Value(int32(v.Int())), true
		}
	case variant.PrimitiveInt16:
		if it, ok := logicalTypeOf[*format.IntType](lt); ok && it.IsSigned && it.BitWidth == 16 {
			return Int32Value(int32(v.Int())), true
		}
	case variant.PrimitiveInt32:
		if typ.Kind() == Int32 && isSignedIntOrNil(lt, 32) {
			return Int32Value(int32(v.Int())), true
		}
	case variant.PrimitiveInt64:
		if typ.Kind() == Int64 && isSignedIntOrNil(lt, 64) {
			return Int64Value(v.Int()), true
		}
	case variant.PrimitiveFloat:
		if lt == nil && typ.Kind() == Float {
			return FloatValue(float32(v.FloatValue())), true
		}
	case variant.PrimitiveDouble:
		if lt == nil && typ.Kind() == Double {
			return DoubleValue(v.FloatValue()), true
		}
	case variant.PrimitiveString:
		if logicalTypeIs[*format.StringType](lt) {
			// Viewing the immutable string data in place avoids an
			// allocation per string. Callers either copy the value into
			// column buffers or expose it as a read-only parquet.Value
			// (the deconstruct path), matching the library's aliasing
			// semantics for byte array values.
			return ByteArrayValue(unsafeByteArrayFromString(v.Str())), true
		}
	case variant.PrimitiveBinary:
		if lt == nil && typ.Kind() == ByteArray {
			return ByteArrayValue(v.Bytes()), true
		}
	case variant.PrimitiveDate:
		if logicalTypeIs[*format.DateType](lt) {
			return Int32Value(int32(v.Int())), true
		}
	case variant.PrimitiveTime:
		if tt, ok := logicalTypeOf[*format.TimeType](lt); ok && isMicros(tt.Unit) {
			return Int64Value(v.Int()), true
		}
	case variant.PrimitiveTimestamp:
		if ts, ok := logicalTypeOf[*format.TimestampType](lt); ok && isMicros(ts.Unit) && ts.IsAdjustedToUTC {
			return Int64Value(v.Int()), true
		}
	case variant.PrimitiveTimestampNTZ:
		if ts, ok := logicalTypeOf[*format.TimestampType](lt); ok && isMicros(ts.Unit) && !ts.IsAdjustedToUTC {
			return Int64Value(v.Int()), true
		}
	case variant.PrimitiveTimestampNanos:
		if ts, ok := logicalTypeOf[*format.TimestampType](lt); ok && isNanos(ts.Unit) && ts.IsAdjustedToUTC {
			return Int64Value(v.Int()), true
		}
	case variant.PrimitiveTimestampNTZNanos:
		if ts, ok := logicalTypeOf[*format.TimestampType](lt); ok && isNanos(ts.Unit) && !ts.IsAdjustedToUTC {
			return Int64Value(v.Int()), true
		}
	case variant.PrimitiveDecimal4:
		if d, ok := logicalTypeOf[*format.DecimalType](lt); ok && typ.Kind() == Int32 && d.Scale == int32(v.Scale()) &&
			decimal64FitsPrecision(v.Int(), d.Precision) {
			return Int32Value(int32(v.Int())), true
		}
	case variant.PrimitiveDecimal8:
		if d, ok := logicalTypeOf[*format.DecimalType](lt); ok && typ.Kind() == Int64 && d.Scale == int32(v.Scale()) &&
			decimal64FitsPrecision(v.Int(), d.Precision) {
			return Int64Value(v.Int()), true
		}
	case variant.PrimitiveDecimal16:
		if dec, ok := logicalTypeOf[*format.DecimalType](lt); ok && dec.Scale == int32(v.Scale()) &&
			(typ.Kind() == ByteArray || (typ.Kind() == FixedLenByteArray && typ.Length() == 16)) &&
			decimal128FitsPrecision(v.Decimal16Value(), dec.Precision) {
			d := v.Decimal16Value()
			be := littleEndianToBigEndian16(d)
			if typ.Kind() == ByteArray {
				return ByteArrayValue(be[:]), true
			}
			return FixedLenByteArrayValue(be[:]), true
		}
	case variant.PrimitiveUUID:
		if logicalTypeIs[*format.UUIDType](lt) {
			u := v.UUIDValue()
			return FixedLenByteArrayValue(u[:]), true
		}
	}
	return Value{}, false
}

// littleEndianToBigEndian16 converts the little-endian 16-byte decimal
// representation used by variant to parquet's big-endian representation.
func littleEndianToBigEndian16(b [16]byte) [16]byte {
	var out [16]byte
	for i := range b {
		out[i] = b[15-i]
	}
	return out
}

// decimal64FitsPrecision reports whether an unscaled decimal value fits in
// a DECIMAL(P, S) column, i.e. has at most P decimal digits. A DECIMAL(P, S)
// column declares that bound to readers, so writing a wider value would
// corrupt it for engines that trust the declaration (DuckDB renders such a
// column as a different decimal type); the value must fall back to the
// value column instead.
func decimal64FitsPrecision(unscaled int64, precision int32) bool {
	if precision >= 19 {
		return true // int64 has at most 19 digits
	}
	bound := int64(1)
	for range precision {
		bound *= 10
	}
	return unscaled > -bound && unscaled < bound
}

// decimal128FitsPrecision is decimal64FitsPrecision for the little-endian
// 16-byte representation used by variant decimal16.
func decimal128FitsPrecision(le [16]byte, precision int32) bool {
	if precision >= 39 {
		return true // 128 bits hold at most 39 digits
	}
	neg := le[15]&0x80 != 0
	var hi, lo uint64
	for i := range 8 {
		lo |= uint64(le[i]) << (8 * i)
		hi |= uint64(le[8+i]) << (8 * i)
	}
	if neg { // two's complement negation to get the magnitude
		lo = ^lo + 1
		hi = ^hi
		if lo == 0 {
			hi++
		}
	}
	// magnitude < 10^precision, computed in 128 bits
	boundHi, boundLo := uint64(0), uint64(1)
	for range precision {
		var carry uint64
		carry, boundLo = bits.Mul64(boundLo, 10)
		boundHi = boundHi*10 + carry
	}
	return hi < boundHi || (hi == boundHi && lo < boundLo)
}
