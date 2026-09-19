package parquet

import (
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go/format"
	"github.com/parquet-go/parquet-go/variant"
)

// This file implements the construct_variant reconstruction algorithm from
// the Variant Shredding specification:
// https://github.com/apache/parquet-format/blob/master/VariantShredding.md
//
// Reconstruction is not optional — VariantShredding.md: "When typed_value is
// present, readers must reconstruct shredded values according to this
// specification."
//
// A shredded variant group has the shape:
//
//	optional group v (VARIANT) {
//	  required binary metadata;
//	  optional binary value;
//	  optional <T> typed_value;  // primitive, LIST, or object group
//	}
//
// where object typed_value groups contain one required group per shredded
// field, each with its own optional value/typed_value pair, and LIST
// typed_value groups contain a repeated list of elements with the same
// value/typed_value pair. Reconstruction walks the leaf columns of the
// variant subtree and rebuilds the variant value for each row.

// shreddedVariantGroup describes one (value, typed_value) pair in a shredded
// variant schema: the top-level variant group, an object field group, or a
// list element group. Column indices are relative to the variant group's
// first leaf column; definition levels are relative to the definition level
// at which the variant group itself is present.
type shreddedVariantGroup struct {
	metadataCol int // metadata leaf column, or -1 (top-level group only)
	valueCol    int // value leaf column, or -1
	typed       *shreddedTypedValue
	startCol    int
	numCols     int
	defLevel    int // relative definition level at which this group exists
	repDepth    int // repetition depth added within the variant subtree
}

type shreddedTypedKind int

const (
	shreddedTypedPrimitive shreddedTypedKind = iota
	shreddedTypedList
	shreddedTypedObject
)

// shreddedTypedValue describes the typed_value part of a shredded variant
// group.
type shreddedTypedValue struct {
	kind     shreddedTypedKind
	startCol int
	numCols  int
	defLevel int // relative definition level at which typed_value is present

	typ     Type                   // primitive
	element *shreddedVariantGroup  // list
	fields  []shreddedVariantField // object
	index   map[string]int         // object: field name to position in fields
}

type shreddedVariantField struct {
	name  string
	group *shreddedVariantGroup
}

// fieldByName returns the shredded field with the given name, or nil.
func (t *shreddedTypedValue) fieldByName(name string) *shreddedVariantGroup {
	if i, ok := t.index[name]; ok {
		return t.fields[i].group
	}
	return nil
}

// buildShreddedVariantGroup builds the reconstruction tree for a variant
// group node. defLevel is the relative definition level at which the group
// exists (0 for the top-level variant group), repDepth the repetition depth
// added within the variant subtree.
func buildShreddedVariantGroup(node Node, startCol, defLevel, repDepth int) (*shreddedVariantGroup, error) {
	g := &shreddedVariantGroup{
		metadataCol: -1,
		valueCol:    -1,
		startCol:    startCol,
		defLevel:    defLevel,
		repDepth:    repDepth,
	}
	col := startCol
	for _, f := range node.Fields() {
		switch f.Name() {
		case "metadata":
			// Every schema in VariantShredding.md declares
			// "required binary metadata;" — a row cannot lack a dictionary.
			if !f.Leaf() || f.Type().Kind() != ByteArray || f.Optional() || f.Repeated() {
				return nil, fmt.Errorf("variant: metadata field must be a required binary leaf")
			}
			g.metadataCol = col
			col++
		case "value":
			if !f.Leaf() || f.Type().Kind() != ByteArray {
				return nil, fmt.Errorf("variant: value field must be a binary leaf")
			}
			// VariantShredding.md: "Both value and typed_value are optional
			// fields". The reconstruction's level arithmetic relies on the
			// value column contributing one definition level.
			if !f.Optional() {
				return nil, fmt.Errorf("variant: value field of a shredded variant group must be optional")
			}
			g.valueCol = col
			col++
		case "typed_value":
			typed, err := buildShreddedTypedValue(f, col, defLevel+1, repDepth)
			if err != nil {
				return nil, err
			}
			g.typed = typed
			col += typed.numCols
		default:
			return nil, fmt.Errorf("variant: unexpected field %q in shredded variant group", f.Name())
		}
	}
	g.numCols = col - startCol
	if g.valueCol < 0 && g.typed == nil {
		return nil, fmt.Errorf("variant: shredded variant group has neither value nor typed_value")
	}
	return g, nil
}

// buildShreddedTypedValue builds the reconstruction tree for a typed_value
// field. defLevel is the relative definition level at which typed_value is
// present (its parent group's level + 1, since typed_value is optional).
func buildShreddedTypedValue(node Node, startCol, defLevel, repDepth int) (*shreddedTypedValue, error) {
	if !node.Optional() {
		return nil, fmt.Errorf("variant: typed_value must be optional")
	}
	t := &shreddedTypedValue{
		startCol: startCol,
		defLevel: defLevel,
	}
	switch {
	case node.Leaf():
		t.kind = shreddedTypedPrimitive
		t.typ = node.Type()
		t.numCols = 1
		if err := validateShreddedPrimitiveType(t.typ); err != nil {
			return nil, err
		}

	case isList(node):
		t.kind = shreddedTypedList
		elem := listElementOf(node)
		elemDef := defLevel + 1 // the repeated list group adds one level
		if elem.Optional() {
			// The spec requires list elements to be required groups, but
			// tolerate optional elements by accounting for the extra level.
			elemDef++
		}
		element, err := buildShreddedVariantGroup(elem, startCol, elemDef, repDepth+1)
		if err != nil {
			return nil, fmt.Errorf("variant: list element: %w", err)
		}
		if element.metadataCol >= 0 {
			return nil, fmt.Errorf("variant: list element must not contain metadata")
		}
		t.element = element
		t.numCols = element.numCols

	default:
		t.kind = shreddedTypedObject
		col := startCol
		for _, f := range node.Fields() {
			if f.Leaf() {
				return nil, fmt.Errorf("variant: object field %q must be a group", f.Name())
			}
			if f.Repeated() {
				return nil, fmt.Errorf("variant: object field %q must not be repeated", f.Name())
			}
			// The spec requires field groups to be required, but tolerate
			// optional groups (a null group reads as a missing field).
			fieldDef := defLevel
			if f.Optional() {
				fieldDef++
			}
			group, err := buildShreddedVariantGroup(f, col, fieldDef, repDepth)
			if err != nil {
				return nil, fmt.Errorf("variant: object field %q: %w", f.Name(), err)
			}
			if group.metadataCol >= 0 {
				return nil, fmt.Errorf("variant: object field %q must not contain metadata", f.Name())
			}
			t.fields = append(t.fields, shreddedVariantField{name: f.Name(), group: group})
			col += group.numCols
		}
		if len(t.fields) == 0 {
			// An empty object group has no columns to carry its own
			// presence, so it cannot be reconstructed.
			return nil, fmt.Errorf("variant: object typed_value group must have at least one field")
		}
		// Object field events resolve names against this map; a linear
		// scan makes wide shredded objects quadratic per row (measurably
		// dominant from a few dozen fields up).
		t.index = make(map[string]int, len(t.fields))
		for i := range t.fields {
			t.index[t.fields[i].name] = i
		}
		t.numCols = col - startCol
	}
	return t, nil
}

// variantColumnReader reads values from the leaf columns of a variant
// subtree, tracking a cursor per column. The column slices contain the
// values belonging to a single occurrence of the variant group.
type variantColumnReader struct {
	columns [][]Value
	pos     []int
}

func (r *variantColumnReader) peek(col int) (Value, bool) {
	if col >= len(r.columns) || r.pos[col] >= len(r.columns[col]) {
		return Value{}, false
	}
	return r.columns[col][r.pos[col]], true
}

func (r *variantColumnReader) next(col int) (Value, bool) {
	v, ok := r.peek(col)
	if ok {
		r.pos[col]++
	}
	return v, ok
}

// read reconstructs the variant value for one occurrence of the group. It
// consumes exactly one value from every leaf column of the group's subtree
// (more beneath repeated lists). baseDef and baseRep are the definition
// level and repetition depth of the variant group in the enclosing schema.
//
// The boolean result reports whether the value is present; a false result
// means both value and typed_value were null, which the spec defines as a
// missing object field (and is invalid elsewhere; callers decide).
func (g *shreddedVariantGroup) read(r *variantColumnReader, m variant.Metadata, baseDef, baseRep int) (variant.Value, bool, error) {
	var valueBytes []byte
	valuePresent := false
	if g.valueCol >= 0 {
		v, ok := r.next(g.valueCol)
		if ok && !v.IsNull() && int(v.definitionLevel) >= baseDef+g.defLevel+1 {
			valuePresent = true
			valueBytes = v.ByteArray()
		}
	}

	if g.typed == nil {
		if !valuePresent {
			return variant.Null(), false, nil
		}
		val, err := variant.Decode(m, valueBytes)
		if err != nil {
			return variant.Null(), false, fmt.Errorf("variant: decoding value: %w", err)
		}
		return val, true, nil
	}

	// For primitives and arrays, value and typed_value are mutually
	// exclusive. VariantShredding.md: "Unless the value is shredded as an
	// object (see Objects), typed_value or value (but not both) must be
	// non-null."
	switch g.typed.kind {
	case shreddedTypedPrimitive:
		v, ok := r.next(g.typed.startCol)
		typedPresent := ok && !v.IsNull() && int(v.definitionLevel) >= baseDef+g.typed.defLevel
		if typedPresent {
			if valuePresent {
				return variant.Null(), false, fmt.Errorf("variant: value and primitive typed_value are both non-null")
			}
			val, err := parquetToVariantValue(v, g.typed.typ)
			if err != nil {
				return variant.Null(), false, err
			}
			return val, true, nil
		}

	case shreddedTypedList:
		arr, listPresent, err := g.typed.readList(r, m, baseDef, baseRep)
		if err != nil {
			return variant.Null(), false, err
		}
		if listPresent {
			if valuePresent {
				return variant.Null(), false, fmt.Errorf("variant: value and array typed_value are both non-null")
			}
			return arr, true, nil
		}

	case shreddedTypedObject:
		obj, objPresent, err := g.typed.readObject(r, m, baseDef, baseRep)
		if err != nil {
			return variant.Null(), false, err
		}
		if objPresent {
			if !valuePresent {
				return obj, true, nil
			}
			// Partially shredded object: the residual value must be an
			// object. VariantShredding.md: "The value column of a
			// partially shredded object must never contain fields
			// represented by the Parquet columns in typed_value (shredded
			// fields). Readers may always assume that data is written
			// correctly" — so if a writer repeats a shredded name anyway,
			// readers use the shredded field and ignore the residual copy
			// (matching parquet-java).
			residual, err := variant.Decode(m, valueBytes)
			if err != nil {
				return variant.Null(), false, fmt.Errorf("variant: decoding partial object value: %w", err)
			}
			if residual.Basic() != variant.BasicObject {
				return variant.Null(), false, fmt.Errorf("variant: object typed_value with non-object value")
			}
			fields := slices.Clone(obj.ObjectValue().Fields)
			for _, f := range residual.ObjectValue().Fields {
				if g.typed.fieldByName(f.Name) != nil {
					continue
				}
				fields = append(fields, f)
			}
			return variant.MakeObject(fields), true, nil
		}
	}

	// typed_value is null: fall back to the value column.
	if !valuePresent {
		return variant.Null(), false, nil
	}
	val, err := variant.Decode(m, valueBytes)
	if err != nil {
		return variant.Null(), false, fmt.Errorf("variant: decoding value: %w", err)
	}
	return val, true, nil
}

// readList reconstructs a shredded array. The boolean result reports whether
// the typed_value list was present (a present empty list yields an empty
// array).
func (t *shreddedTypedValue) readList(r *variantColumnReader, m variant.Metadata, baseDef, baseRep int) (variant.Value, bool, error) {
	first, ok := r.peek(t.startCol)
	listPresent := ok && int(first.definitionLevel) >= baseDef+t.defLevel
	// The repeated list group adds one definition level when the list has at
	// least one element (independent of the element group's own levels).
	hasElements := ok && int(first.definitionLevel) >= baseDef+t.defLevel+1

	if !hasElements {
		// Null or empty list: one (null) value per leaf column to consume.
		for col := t.startCol; col < t.startCol+t.numCols; col++ {
			r.next(col)
		}
		if listPresent {
			return variant.MakeArray(nil), true, nil
		}
		return variant.Null(), false, nil
	}

	elemRep := baseRep + t.element.repDepth
	var elements []variant.Value
	for {
		elem, present, err := t.element.read(r, m, baseDef, baseRep)
		if err != nil {
			return variant.Null(), false, err
		}
		if !present {
			// Elements with both value and typed_value null are invalid
			// per the spec; read them as variant null (matching
			// parquet-java's behavior).
			elem = variant.Null()
		}
		elements = append(elements, elem)

		next, ok := r.peek(t.startCol)
		if !ok || int(next.repetitionLevel) != elemRep {
			break
		}
	}
	return variant.MakeArray(elements), true, nil
}

// readObject reconstructs the shredded fields of an object typed_value. The
// boolean result reports whether the typed_value group was present. Field
// groups are always consumed, even when the object is absent, because their
// leaf columns carry one (null) value per occurrence either way.
func (t *shreddedTypedValue) readObject(r *variantColumnReader, m variant.Metadata, baseDef, baseRep int) (variant.Value, bool, error) {
	objPresent := false
	if first, ok := r.peek(t.startCol); ok {
		objPresent = int(first.definitionLevel) >= baseDef+t.defLevel
	}

	fields := make([]variant.Field, 0, len(t.fields))
	for _, f := range t.fields {
		val, present, err := f.group.read(r, m, baseDef, baseRep)
		if err != nil {
			return variant.Null(), false, fmt.Errorf("variant: object field %q: %w", f.name, err)
		}
		// A missing field (value and typed_value both null) is simply
		// omitted from the reconstructed object.
		if objPresent && present {
			fields = append(fields, variant.Field{Name: f.name, Value: val})
		}
	}
	if !objPresent {
		return variant.Null(), false, nil
	}
	return variant.MakeObject(fields), true, nil
}

// validateShreddedPrimitiveType checks that a typed_value leaf type is one
// of the shredded types permitted by the Variant Shredding specification
// (e.g. unsigned integers and non-UUID fixed-length byte arrays are not).
func validateShreddedPrimitiveType(typ Type) error {
	lt := typ.LogicalType()
	switch value := logicalTypeValueOf(lt).(type) {
	case nil:
		switch typ.Kind() {
		case Boolean, Int32, Int64, Float, Double, ByteArray:
			return nil
		}
	case *format.StringType:
		return nil
	case *format.IntType:
		if !value.IsSigned {
			return fmt.Errorf("variant: unsupported shredded type %s (unsigned integer)", typ)
		}
		switch value.BitWidth {
		case 8, 16, 32, 64:
			return nil
		}
	case *format.DateType:
		return nil
	case *format.TimeType:
		if _, ok := value.Unit.Value.(*format.MicroSeconds); ok {
			return nil
		}
	case *format.TimestampType:
		switch value.Unit.Value.(type) {
		case *format.MicroSeconds, *format.NanoSeconds:
			return nil
		}
	case *format.DecimalType:
		switch typ.Kind() {
		case Int32, Int64, FixedLenByteArray, ByteArray:
			return nil
		}
	case *format.UUIDType:
		return nil
	}
	return fmt.Errorf("variant: unsupported shredded type %s", typ)
}

// parquetToVariantValue converts a parquet leaf value of a shredded
// typed_value column to the corresponding variant value, per the shredded
// types table of the Variant Shredding specification.
func parquetToVariantValue(v Value, typ Type) (variant.Value, error) {
	lt := typ.LogicalType()
	switch value := logicalTypeValueOf(lt).(type) {
	case nil:
		switch typ.Kind() {
		case Boolean:
			return variant.Bool(v.Boolean()), nil
		case Int32:
			return variant.Int32(v.Int32()), nil
		case Int64:
			return variant.Int64(v.Int64()), nil
		case Float:
			return variant.Float(v.Float()), nil
		case Double:
			return variant.Double(v.Double()), nil
		case ByteArray:
			return variant.Binary(copyBytes(v.ByteArray())), nil
		}

	case *format.StringType:
		return variant.String(string(v.ByteArray())), nil

	case *format.IntType:
		switch value.BitWidth {
		case 8:
			return variant.Int8(int8(v.Int32())), nil
		case 16:
			return variant.Int16(int16(v.Int32())), nil
		case 32:
			return variant.Int32(v.Int32()), nil
		case 64:
			return variant.Int64(v.Int64()), nil
		}

	case *format.DateType:
		return variant.Date(v.Int32()), nil

	case *format.TimeType:
		if _, ok := value.Unit.Value.(*format.MicroSeconds); ok {
			return variant.Time(v.Int64()), nil
		}

	case *format.TimestampType:
		switch value.Unit.Value.(type) {
		case *format.MicroSeconds:
			if value.IsAdjustedToUTC {
				return variant.Timestamp(v.Int64()), nil
			}
			return variant.TimestampNTZ(v.Int64()), nil
		case *format.NanoSeconds:
			if value.IsAdjustedToUTC {
				return variant.TimestampNanos(v.Int64()), nil
			}
			return variant.TimestampNTZNanos(v.Int64()), nil
		}

	case *format.DecimalType:
		scale := uint8(value.Scale)
		switch typ.Kind() {
		case Int32:
			return variant.Decimal4(v.Int32(), scale), nil
		case Int64:
			return variant.Decimal8(v.Int64(), scale), nil
		case FixedLenByteArray, ByteArray:
			b := v.ByteArray()
			if len(b) > 16 {
				return variant.Null(), fmt.Errorf("variant: decimal value wider than 16 bytes")
			}
			return variant.Decimal16(bigEndianToLittleEndian16(b), scale), nil
		}

	case *format.UUIDType:
		b := v.ByteArray()
		if len(b) != 16 {
			return variant.Null(), fmt.Errorf("variant: UUID value must be 16 bytes, got %d", len(b))
		}
		return variant.UUID(uuid.UUID(b)), nil
	}
	return variant.Null(), fmt.Errorf("variant: unsupported shredded type %s", typ)
}

// bigEndianToLittleEndian16 converts a big-endian two's complement integer
// of up to 16 bytes (parquet decimal representation) to the little-endian
// 16-byte representation used by variant decimal16.
func bigEndianToLittleEndian16(b []byte) [16]byte {
	var out [16]byte
	for i := range b {
		out[i] = b[len(b)-1-i]
	}
	if len(b) > 0 && b[0]&0x80 != 0 {
		for i := len(b); i < 16; i++ {
			out[i] = 0xFF
		}
	}
	return out
}
