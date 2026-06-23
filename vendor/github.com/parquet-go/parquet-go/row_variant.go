package parquet

import (
	"encoding/binary"
	"fmt"
	"maps"
	"reflect"
	"unsafe"

	"github.com/google/uuid"
	"github.com/parquet-go/parquet-go/variant"
)

// variantMarshalOrNull marshals a reflect.Value to variant binary. If the
// value is nil/invalid/null, it returns the variant encoding of null.
// If the value is a raw variant struct (with metadata and value []byte fields),
// the pre-encoded bytes are extracted directly.
func variantMarshalOrNull(value reflect.Value) (metadata, val []byte) {
	if m, v, ok := extractRawVariantStruct(value); ok {
		return m, v
	}
	var goVal any
	if value.IsValid() && !isNullValue(value) {
		v := value
		for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
			if v.IsNil() {
				break
			}
			v = v.Elem()
		}
		if v.IsValid() && v.Kind() != reflect.Ptr && v.Kind() != reflect.Interface {
			goVal = v.Interface()
		}
	}
	metadata, val, err := variant.Marshal(goVal)
	if err != nil {
		panic(fmt.Sprintf("variant marshal: %v", err))
	}
	return metadata, val
}

// extractRawVariantStruct checks if the value is a struct with "metadata" and
// "value" []byte fields (matching the unshredded variant group layout). If so,
// it extracts the raw bytes directly, supporting passthrough of pre-encoded
// variant data.
func extractRawVariantStruct(value reflect.Value) (metadata, val []byte, ok bool) {
	if !value.IsValid() || isNullValue(value) {
		return nil, nil, false
	}
	v := value
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, nil, false
	}
	t := v.Type()
	metaField, hasMetadata := t.FieldByName("Metadata")
	valField, hasValue := t.FieldByName("Value")
	if !hasMetadata || !hasValue {
		return nil, nil, false
	}
	byteSliceType := reflect.TypeOf([]byte(nil))
	if metaField.Type != byteSliceType || valField.Type != byteSliceType {
		return nil, nil, false
	}
	m := v.FieldByName("Metadata").Bytes()
	va := v.FieldByName("Value").Bytes()
	if m == nil && va == nil {
		return nil, nil, false
	}
	return m, va, true
}

// isUnshreddedVariant returns true if the variant node has only metadata and value
// (no typed_value field), indicating an unshredded variant.
func isUnshreddedVariant(node Node) bool {
	return fieldByName(node, "typed_value") == nil
}

// deconstructFuncOfVariant handles deconstruction of Go values into variant
// columns. Dispatches to unshredded or shredded variant handling.
//
//go:noinline
func deconstructFuncOfVariant(columnIndex uint16, node Node) (uint16, deconstructFunc) {
	if isUnshreddedVariant(node) {
		return deconstructFuncOfUnshreddedVariant(columnIndex, node)
	}
	return deconstructFuncOfShreddedVariant(columnIndex, node)
}

// deconstructFuncOfUnshreddedVariant handles the simple 2-column variant:
// metadata (required ByteArray) and value (required ByteArray).
//
//go:noinline
func deconstructFuncOfUnshreddedVariant(columnIndex uint16, node Node) (uint16, deconstructFunc) {
	metadataColumnIndex := columnIndex
	valueColumnIndex := columnIndex + 1
	nextColumnIndex := columnIndex + 2

	return nextColumnIndex, func(columns [][]Value, levels columnLevels, value reflect.Value) {
		metaIdx := ^metadataColumnIndex
		valIdx := ^valueColumnIndex

		metadata, val := variantMarshalOrNull(value)

		metaValue := makeValueByteArray(ByteArray, unsafe.SliceData(metadata), len(metadata))
		metaValue.repetitionLevel = levels.repetitionLevel
		metaValue.definitionLevel = levels.definitionLevel
		metaValue.columnIndex = metaIdx

		valValue := makeValueByteArray(ByteArray, unsafe.SliceData(val), len(val))
		valValue.repetitionLevel = levels.repetitionLevel
		valValue.definitionLevel = levels.definitionLevel
		valValue.columnIndex = valIdx

		columns[metadataColumnIndex] = append(columns[metadataColumnIndex], metaValue)
		columns[valueColumnIndex] = append(columns[valueColumnIndex], valValue)
	}
}

// deconstructFuncOfShreddedVariant handles the 3-field shredded variant:
// metadata (required), value (optional), typed_value (optional).
//
// When the Go value matches the typed_value schema, the value is written to the
// typed_value columns and the value column is set to null. Otherwise, the variant-
// encoded bytes are written to the value column and typed_value columns are null.
//
//go:noinline
func deconstructFuncOfShreddedVariant(columnIndex uint16, node Node) (uint16, deconstructFunc) {
	// Fields are sorted alphabetically: metadata, typed_value, value
	var metadataColumnIndex, valueColumnIndex, typedValueStartColumn uint16
	var typedValueLeafCount uint16
	var typedValueNode Node

	col := columnIndex
	for _, f := range node.Fields() {
		n := numLeafColumnsOf(f)
		switch f.Name() {
		case "metadata":
			metadataColumnIndex = col
			col += n
		case "typed_value":
			typedValueStartColumn = col
			typedValueNode = f
			typedValueLeafCount = n
			col += n
		case "value":
			valueColumnIndex = col
			col += n
		}
	}
	nextColumnIndex := col

	// Determine the shredded type structure
	typedInner := Required(typedValueNode)
	matcher := buildShreddedMatcher(typedInner)

	return nextColumnIndex, func(columns [][]Value, levels columnLevels, value reflect.Value) {
		metaIdx := ^metadataColumnIndex
		valIdx := ^valueColumnIndex

		metadata, val := variantMarshalOrNull(value)

		// Always write metadata
		metaValue := makeValueByteArray(ByteArray, unsafe.SliceData(metadata), len(metadata))
		metaValue.repetitionLevel = levels.repetitionLevel
		metaValue.definitionLevel = levels.definitionLevel
		metaValue.columnIndex = metaIdx
		columns[metadataColumnIndex] = append(columns[metadataColumnIndex], metaValue)

		// Try to shred the value
		goVal := extractGoValue(value)
		if goVal != nil && matcher.canShred(goVal) {
			// Write null to value column (optional, def level not incremented)
			columns[valueColumnIndex] = append(columns[valueColumnIndex], Value{
				repetitionLevel: levels.repetitionLevel,
				definitionLevel: levels.definitionLevel,
				columnIndex:     valIdx,
			})
			// Write typed value (increment def level for optional typed_value wrapper)
			typedLevels := levels
			typedLevels.definitionLevel++
			matcher.shred(columns, typedLevels, typedValueStartColumn, goVal)
		} else {
			// Write variant bytes to value column (increment def level for optional wrapper)
			valueLevels := levels
			valueLevels.definitionLevel++
			valValue := makeValueByteArray(ByteArray, unsafe.SliceData(val), len(val))
			valValue.repetitionLevel = valueLevels.repetitionLevel
			valValue.definitionLevel = valueLevels.definitionLevel
			valValue.columnIndex = valIdx
			columns[valueColumnIndex] = append(columns[valueColumnIndex], valValue)

			// Write null to all typed_value columns
			writeNullLeaves(columns, typedValueStartColumn, typedValueStartColumn+typedValueLeafCount, levels)
		}
	}
}

// extractGoValue extracts the underlying Go value from a reflect.Value,
// dereferencing pointers and interfaces.
func extractGoValue(value reflect.Value) any {
	if !value.IsValid() || isNullValue(value) {
		return nil
	}
	v := value
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	return v.Interface()
}

// writeNullLeaves writes null values to all leaf columns in the range [start, end).
func writeNullLeaves(columns [][]Value, start, end uint16, levels columnLevels) {
	for col := start; col < end; col++ {
		columns[col] = append(columns[col], Value{
			repetitionLevel: levels.repetitionLevel,
			definitionLevel: levels.definitionLevel,
			columnIndex:     ^col,
		})
	}
}

// shreddedMatcher determines if a Go value can be shredded into a typed_value
// column and performs the shredding.
//
// A matcher is one of three kinds:
//   - primitive: typ is set (leaf column)
//   - group: fields is set (object with per-field matchers)
//   - unsupported: neither (e.g., LIST — values go to the fallback column)
//
// leafCount is always set to the number of leaf columns occupied.
type shreddedMatcher struct {
	typ       Type                   // non-nil for primitive typed_value (leaf node)
	fields    []shreddedFieldMatcher // non-nil for group typed_value (object node)
	leafCount uint16
}

func (m *shreddedMatcher) isPrimitive() bool { return m.typ != nil }
func (m *shreddedMatcher) isGroup() bool     { return len(m.fields) > 0 }
func (m *shreddedMatcher) kind() Kind        { return m.typ.Kind() }
func (m *shreddedMatcher) isString() bool {
	lt := m.typ.LogicalType()
	return lt != nil && lt.UTF8 != nil
}
func (m *shreddedMatcher) isUUID() bool {
	lt := m.typ.LogicalType()
	return lt != nil && lt.UUID != nil
}

type shreddedFieldMatcher struct {
	name    string
	matcher shreddedMatcher
}

func buildShreddedMatcher(node Node) shreddedMatcher {
	if node.Leaf() {
		return shreddedMatcher{
			typ:       node.Type(),
			leafCount: 1,
		}
	}

	// Check if this is a group with (value, typed_value) pairs for each field
	fields := node.Fields()
	matchers := make([]shreddedFieldMatcher, 0, len(fields))
	for _, f := range fields {
		subFields := f.Fields()
		if len(subFields) == 2 {
			// This is a (value, typed_value) pair for a named field
			valueNode := fieldByName(f, "value")
			typedNode := fieldByName(f, "typed_value")
			if valueNode != nil && typedNode != nil {
				matchers = append(matchers, shreddedFieldMatcher{
					name:    f.Name(),
					matcher: buildShreddedMatcher(Required(typedNode)),
				})
			}
		}
	}

	if len(matchers) > 0 {
		return shreddedMatcher{
			fields:    matchers,
			leafCount: countMatcherLeaves(matchers),
		}
	}

	// Unsupported typed_value structure (e.g., LIST). Return a matcher
	// that never shreds so values always go to the value column.
	// Still compute leaf count so column offsets are correct.
	return shreddedMatcher{
		leafCount: numLeafColumnsOf(node),
	}
}

func (m *shreddedMatcher) canShred(v any) bool {
	if v == nil {
		return false
	}
	if m.isPrimitive() {
		return canShredPrimitive(v, m)
	}
	if m.isGroup() {
		return canShredObject(v, m.fields)
	}
	return false
}

func canShredPrimitive(v any, m *shreddedMatcher) bool {
	switch m.kind() {
	case ByteArray:
		if m.isString() {
			switch v.(type) {
			case string:
				return true
			}
		} else {
			switch v.(type) {
			case []byte:
				return true
			}
		}
	case Int32:
		switch v.(type) {
		case int8, int16, int32, uint8, uint16:
			return true
		}
	case Int64:
		switch v.(type) {
		case int8, int16, int32, int64, int, uint8, uint16, uint32:
			return true
		}
	case Float:
		switch v.(type) {
		case float32:
			return true
		}
	case Double:
		switch v.(type) {
		case float32, float64:
			return true
		}
	case Boolean:
		switch v.(type) {
		case bool:
			return true
		}
	case FixedLenByteArray:
		if m.isUUID() {
			switch v.(type) {
			case uuid.UUID, [16]byte:
				return true
			}
		} else {
			switch v.(type) {
			case [16]byte:
				return true
			}
		}
	}
	return false
}

func canShredObject(v any, fields []shreddedFieldMatcher) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	// An object can be shredded if ALL its fields exist in the shredded schema
	for key := range m {
		found := false
		for _, f := range fields {
			if f.name == key {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (m *shreddedMatcher) shred(columns [][]Value, levels columnLevels, startColumn uint16, v any) {
	if m.isPrimitive() {
		col := startColumn
		pv := goValueToParquetValue(v, m.kind())
		pv.repetitionLevel = levels.repetitionLevel
		pv.definitionLevel = levels.definitionLevel
		pv.columnIndex = ^col
		columns[col] = append(columns[col], pv)
		return
	}
	if m.isGroup() {
		m.shredObject(columns, levels, startColumn, v)
	}
}

func (m *shreddedMatcher) shredObject(columns [][]Value, levels columnLevels, startColumn uint16, v any) {
	obj, ok := v.(map[string]any)
	if !ok {
		// Should not happen if canShred returned true
		return
	}

	col := startColumn
	for _, f := range m.fields {
		// Within each field's sub-group, fields are sorted alphabetically:
		// typed_value comes before value.
		typedValueCol := col
		typedCount := f.matcher.leafCount
		valueCol := col + typedCount
		nextFieldCol := valueCol + 1

		fieldVal, exists := obj[f.name]
		if exists && fieldVal != nil && f.matcher.canShred(fieldVal) {
			// Write null to field's value column
			columns[valueCol] = append(columns[valueCol], Value{
				repetitionLevel: levels.repetitionLevel,
				definitionLevel: levels.definitionLevel,
				columnIndex:     ^valueCol,
			})
			// Write typed value (increment def for optional typed_value)
			typedLevels := levels
			typedLevels.definitionLevel++
			f.matcher.shred(columns, typedLevels, typedValueCol, fieldVal)
		} else if exists && fieldVal != nil {
			// Value doesn't match typed schema: write variant to field's value.
			// Encode metadata+value together so nested objects can be decoded later.
			valueLevels := levels
			valueLevels.definitionLevel++
			metadata, val, err := variant.Marshal(fieldVal)
			if err != nil {
				panic(fmt.Sprintf("variant marshal field %q: %v", f.name, err))
			}
			combined := encodeFieldVariant(metadata, val)
			valValue := makeValueByteArray(ByteArray, unsafe.SliceData(combined), len(combined))
			valValue.repetitionLevel = valueLevels.repetitionLevel
			valValue.definitionLevel = valueLevels.definitionLevel
			valValue.columnIndex = ^valueCol
			columns[valueCol] = append(columns[valueCol], valValue)
			// Write null to typed_value columns
			writeNullLeaves(columns, typedValueCol, typedValueCol+typedCount, levels)
		} else {
			// Field not present or null: write null to both
			writeNullLeaves(columns, typedValueCol, nextFieldCol, levels)
		}

		col = nextFieldCol
	}
}

// encodeFieldVariant encodes metadata and value bytes together for per-field
// fallback storage. Format: 4-byte LE metadata length + metadata + value.
// This ensures nested objects retain their metadata dictionary for decoding.
func encodeFieldVariant(metadata, value []byte) []byte {
	mLen := len(metadata)
	buf := make([]byte, 4+mLen+len(value))
	binary.LittleEndian.PutUint32(buf, uint32(mLen))
	copy(buf[4:], metadata)
	copy(buf[4+mLen:], value)
	return buf
}

// decodeFieldVariant splits per-field variant bytes back into metadata and value.
func decodeFieldVariant(data []byte) (metadata, value []byte, ok bool) {
	if len(data) < 4 {
		return nil, data, false
	}
	mLen := int(binary.LittleEndian.Uint32(data))
	if 4+mLen > len(data) {
		return nil, data, false
	}
	return data[4 : 4+mLen], data[4+mLen:], true
}

// countMatcherColumns returns the total number of leaf columns for a matcher,
// including the value column for each field in a group.
func countMatcherColumns(m *shreddedMatcher) uint16 {
	if m.isGroup() {
		return countMatcherLeaves(m.fields)
	}
	return m.leafCount
}

// countMatcherLeaves sums the leaf columns for a list of field matchers.
// Each field contributes its typed_value leaf count plus one value column.
func countMatcherLeaves(fields []shreddedFieldMatcher) uint16 {
	var count uint16
	for _, f := range fields {
		count += f.matcher.leafCount + 1 // typed_value leaves + value column
	}
	return count
}

func goValueToParquetValue(v any, kind Kind) Value {
	switch kind {
	case Boolean:
		return BooleanValue(v.(bool))
	case Int32:
		switch val := v.(type) {
		case int8:
			return Int32Value(int32(val))
		case int16:
			return Int32Value(int32(val))
		case int32:
			return Int32Value(val)
		case uint8:
			return Int32Value(int32(val))
		case uint16:
			return Int32Value(int32(val))
		default:
			return Int32Value(0)
		}
	case Int64:
		switch val := v.(type) {
		case int8:
			return Int64Value(int64(val))
		case int16:
			return Int64Value(int64(val))
		case int32:
			return Int64Value(int64(val))
		case int64:
			return Int64Value(val)
		case int:
			return Int64Value(int64(val))
		case uint8:
			return Int64Value(int64(val))
		case uint16:
			return Int64Value(int64(val))
		case uint32:
			return Int64Value(int64(val))
		default:
			return Int64Value(0)
		}
	case Float:
		return FloatValue(v.(float32))
	case Double:
		switch val := v.(type) {
		case float32:
			return DoubleValue(float64(val))
		case float64:
			return DoubleValue(val)
		default:
			return DoubleValue(0)
		}
	case ByteArray:
		switch val := v.(type) {
		case string:
			return ByteArrayValue([]byte(val))
		case []byte:
			return ByteArrayValue(val)
		default:
			return ByteArrayValue(nil)
		}
	case FixedLenByteArray:
		switch val := v.(type) {
		case uuid.UUID:
			b := [16]byte(val)
			return FixedLenByteArrayValue(b[:])
		case [16]byte:
			return FixedLenByteArrayValue(val[:])
		default:
			return FixedLenByteArrayValue(nil)
		}
	default:
		return Value{}
	}
}

// reconstructFuncOfVariant handles reconstruction of variant columns back into
// Go values. Dispatches to unshredded or shredded variant handling.
//
//go:noinline
func reconstructFuncOfVariant(columnIndex uint16, node Node) (uint16, reconstructFunc) {
	if isUnshreddedVariant(node) {
		return reconstructFuncOfUnshreddedVariant(columnIndex, node)
	}
	return reconstructFuncOfShreddedVariant(columnIndex, node)
}

// reconstructFuncOfUnshreddedVariant handles the simple 2-column variant.
//
//go:noinline
func reconstructFuncOfUnshreddedVariant(columnIndex uint16, node Node) (uint16, reconstructFunc) {
	nextColumnIndex := columnIndex + 2

	return nextColumnIndex, func(value reflect.Value, levels columnLevels, columns [][]Value) error {
		if len(columns) < 2 {
			return fmt.Errorf("variant reconstruction: expected 2 columns, got %d", len(columns))
		}

		metaColumn := columns[0]
		valColumn := columns[1]

		if len(metaColumn) == 0 || len(valColumn) == 0 {
			return fmt.Errorf("variant reconstruction: no values in columns for column %d", columnIndex)
		}

		metaVal := metaColumn[0]
		valVal := valColumn[0]

		if metaVal.IsNull() || valVal.IsNull() {
			value.Set(reflect.Zero(value.Type()))
			return nil
		}

		metaBytes := metaVal.ByteArray()
		valBytes := valVal.ByteArray()

		// If the target is a raw variant struct (Metadata/Value []byte fields),
		// copy the raw bytes directly without decoding.
		if setRawVariantStruct(value, metaBytes, valBytes) {
			return nil
		}

		// Handle empty value bytes gracefully. This can happen when reading
		// data that was written with a shredded schema using an unshredded
		// schema — the typed_value columns are dropped by the conversion and
		// the value column becomes a zero-length byte array.
		if len(valBytes) == 0 {
			value.Set(reflect.Zero(value.Type()))
			return nil
		}

		goVal, err := variant.Unmarshal(metaBytes, valBytes)
		if err != nil {
			return fmt.Errorf("variant unmarshal: %w", err)
		}

		return setVariantGoValue(value, goVal)
	}
}

// reconstructFuncOfShreddedVariant handles the 3-field shredded variant.
//
//go:noinline
func reconstructFuncOfShreddedVariant(columnIndex uint16, node Node) (uint16, reconstructFunc) {
	// Fields are sorted alphabetically: metadata, typed_value, value
	var metadataOffset, valueOffset, typedValueOffset uint16
	var typedValueLeafCount uint16
	var typedValueNode Node

	col := uint16(0)
	for _, f := range node.Fields() {
		n := numLeafColumnsOf(f)
		switch f.Name() {
		case "metadata":
			metadataOffset = col
			col += n
		case "typed_value":
			typedValueOffset = col
			typedValueNode = f
			typedValueLeafCount = n
			col += n
		case "value":
			valueOffset = col
			col += n
		}
	}
	totalCols := int(col)
	nextColumnIndex := columnIndex + col

	typedInner := Required(typedValueNode)
	extractor := buildShreddedExtractor(typedInner)

	return nextColumnIndex, func(value reflect.Value, levels columnLevels, columns [][]Value) error {
		if len(columns) < totalCols {
			return fmt.Errorf("shredded variant reconstruction: expected %d columns, got %d", totalCols, len(columns))
		}

		metaCol := columns[metadataOffset]
		valCol := columns[valueOffset]
		typedColumns := columns[typedValueOffset : typedValueOffset+typedValueLeafCount]

		if len(metaCol) == 0 {
			return fmt.Errorf("variant reconstruction: no values in metadata column %d", columnIndex)
		}

		metaVal := metaCol[0]
		hasValue := len(valCol) > 0 && !valCol[0].IsNull()
		hasTypedValue := hasNonNullInColumns(typedColumns)

		if metaVal.IsNull() && !hasValue && !hasTypedValue {
			// All null: missing/null
			value.Set(reflect.Zero(value.Type()))
			return nil
		}

		if hasValue && !hasTypedValue {
			// Unshredded value
			valBytes := valCol[0].ByteArray()
			if len(valBytes) == 0 {
				value.Set(reflect.Zero(value.Type()))
				return nil
			}
			metaBytes := metaVal.ByteArray()
			if setRawVariantStruct(value, metaBytes, valBytes) {
				return nil
			}
			goVal, err := variant.Unmarshal(metaBytes, valBytes)
			if err != nil {
				return fmt.Errorf("variant unmarshal: %w", err)
			}
			return setVariantGoValue(value, goVal)
		}

		if !hasValue && hasTypedValue {
			// Shredded value: extract from typed columns
			goVal := extractor.extract(typedColumns)
			return setVariantGoValue(value, goVal)
		}

		if hasValue && hasTypedValue {
			// Partially shredded: typed_value has some fields, value has the rest
			typedVal := extractor.extract(typedColumns)
			valBytes := valCol[0].ByteArray()
			metaBytes := metaVal.ByteArray()
			unshreddedVal, err := variant.Unmarshal(metaBytes, valBytes)
			if err != nil {
				return fmt.Errorf("variant unmarshal: %w", err)
			}
			merged := mergeVariantObjects(typedVal, unshreddedVal)
			return setVariantGoValue(value, merged)
		}

		value.Set(reflect.Zero(value.Type()))
		return nil
	}
}

func hasNonNullInColumns(columns [][]Value) bool {
	for _, col := range columns {
		if len(col) > 0 && !col[0].IsNull() {
			return true
		}
	}
	return false
}

// setRawVariantStruct checks if the target value is a struct with Metadata and
// Value []byte fields. If so, it copies the raw bytes directly and returns true.
func setRawVariantStruct(value reflect.Value, metadata, val []byte) bool {
	v := value
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	t := v.Type()
	byteSliceType := reflect.TypeOf([]byte(nil))
	metaField, hasMetadata := t.FieldByName("Metadata")
	valField, hasValue := t.FieldByName("Value")
	if !hasMetadata || !hasValue || metaField.Type != byteSliceType || valField.Type != byteSliceType {
		return false
	}
	metaCopy := make([]byte, len(metadata))
	copy(metaCopy, metadata)
	valCopy := make([]byte, len(val))
	copy(valCopy, val)
	v.FieldByName("Metadata").SetBytes(metaCopy)
	v.FieldByName("Value").SetBytes(valCopy)
	return true
}

func setVariantGoValue(value reflect.Value, goVal any) error {
	if goVal == nil {
		value.Set(reflect.Zero(value.Type()))
		return nil
	}
	goValue := reflect.ValueOf(goVal)
	if value.Kind() == reflect.Interface {
		value.Set(goValue)
	} else if goValue.Type().AssignableTo(value.Type()) {
		value.Set(goValue)
	} else if goValue.Type().ConvertibleTo(value.Type()) {
		value.Set(goValue.Convert(value.Type()))
	} else {
		return fmt.Errorf("variant: cannot assign %T to %s", goVal, value.Type())
	}
	return nil
}

// shreddedExtractor reconstructs Go values from typed_value columns.
// Same three-kind structure as shreddedMatcher: primitive (typ set),
// group (fields set), or unsupported (neither).
type shreddedExtractor struct {
	typ       Type                     // non-nil for primitive (leaf column)
	fields    []shreddedFieldExtractor // non-nil for group (object)
	leafCount int
}

func (e *shreddedExtractor) isPrimitive() bool { return e.typ != nil }
func (e *shreddedExtractor) isGroup() bool     { return len(e.fields) > 0 }
func (e *shreddedExtractor) kind() Kind        { return e.typ.Kind() }
func (e *shreddedExtractor) isString() bool {
	lt := e.typ.LogicalType()
	return lt != nil && lt.UTF8 != nil
}
func (e *shreddedExtractor) isUUID() bool {
	lt := e.typ.LogicalType()
	return lt != nil && lt.UUID != nil
}

type shreddedFieldExtractor struct {
	name      string
	extractor shreddedExtractor
}

func buildShreddedExtractor(node Node) shreddedExtractor {
	if node.Leaf() {
		return shreddedExtractor{
			typ:       node.Type(),
			leafCount: 1,
		}
	}

	fields := node.Fields()
	extractors := make([]shreddedFieldExtractor, 0, len(fields))
	for _, f := range fields {
		subFields := f.Fields()
		if len(subFields) == 2 {
			typedNode := fieldByName(f, "typed_value")
			if typedNode != nil {
				extractors = append(extractors, shreddedFieldExtractor{
					name:      f.Name(),
					extractor: buildShreddedExtractor(Required(typedNode)),
				})
			}
		}
	}

	if len(extractors) > 0 {
		leaves := 0
		for _, f := range extractors {
			leaves += f.extractor.leafCount + 1 // typed_value leaves + value column
		}
		return shreddedExtractor{
			fields:    extractors,
			leafCount: leaves,
		}
	}

	// Unsupported typed_value structure (e.g., LIST). Return an extractor
	// that always returns nil so values are read from the value column.
	return shreddedExtractor{
		leafCount: int(numLeafColumnsOf(node)),
	}
}

func (e *shreddedExtractor) extract(columns [][]Value) any {
	if e.isPrimitive() {
		if len(columns) == 0 || len(columns[0]) == 0 || columns[0][0].IsNull() {
			return nil
		}
		return parquetValueToGo(columns[0][0], e)
	}
	if e.isGroup() {
		return e.extractObject(columns)
	}
	return nil
}

func (e *shreddedExtractor) extractObject(columns [][]Value) any {
	result := make(map[string]any)
	col := 0
	for _, f := range e.fields {
		// Within each field's sub-group, fields are sorted alphabetically:
		// typed_value comes before value.
		typedStart := col
		typedCount := f.extractor.leafCount
		valueCol := col + typedCount
		nextFieldCol := valueCol + 1

		hasValue := valueCol < len(columns) && len(columns[valueCol]) > 0 && !columns[valueCol][0].IsNull()
		hasTyped := hasNonNullInRange(columns, typedStart, typedStart+typedCount)

		if hasTyped {
			val := f.extractor.extract(columns[typedStart : typedStart+typedCount])
			if val != nil {
				result[f.name] = val
			}
		} else if hasValue {
			// Field stored as unshredded variant with metadata+value encoded together.
			data := columns[valueCol][0].ByteArray()
			metaBytes, valBytes, ok := decodeFieldVariant(data)
			if ok {
				goVal, err := variant.Unmarshal(metaBytes, valBytes)
				if err == nil && goVal != nil {
					result[f.name] = goVal
				}
			}
		}

		col = nextFieldCol
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func hasNonNullInRange(columns [][]Value, start, end int) bool {
	for i := start; i < end && i < len(columns); i++ {
		if len(columns[i]) > 0 && !columns[i][0].IsNull() {
			return true
		}
	}
	return false
}

func parquetValueToGo(v Value, e *shreddedExtractor) any {
	switch e.kind() {
	case Boolean:
		return v.Boolean()
	case Int32:
		return v.Int32()
	case Int64:
		return v.Int64()
	case Float:
		return v.Float()
	case Double:
		return v.Double()
	case ByteArray:
		b := v.ByteArray()
		if e.isString() {
			return string(b)
		}
		dst := make([]byte, len(b))
		copy(dst, b)
		return dst
	case FixedLenByteArray:
		b := v.ByteArray()
		if e.isUUID() && len(b) == 16 {
			return uuid.UUID(b)
		}
		if len(b) == 16 {
			var u [16]byte
			copy(u[:], b)
			return u
		}
		dst := make([]byte, len(b))
		copy(dst, b)
		return dst
	default:
		return nil
	}
}

// mergeVariantObjects merges two values, preferring typed (shredded) values
// over unshredded ones. Used for partially shredded objects.
func mergeVariantObjects(typed, unshredded any) any {
	typedMap, tOk := typed.(map[string]any)
	unshreddedMap, uOk := unshredded.(map[string]any)
	if tOk && uOk {
		result := make(map[string]any, len(typedMap)+len(unshreddedMap))
		maps.Copy(result, unshreddedMap)
		maps.Copy(result, typedMap)
		return result
	}
	if tOk {
		return typed
	}
	return unshredded
}
