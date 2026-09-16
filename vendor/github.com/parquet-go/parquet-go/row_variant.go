package parquet

import (
	"fmt"
	"reflect"
	"unsafe"

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

// deconstructFuncOfShreddedVariant writes Go values into a shredded variant
// group per the Variant Shredding specification (see
// variant_shredded_write.go): values matching the typed_value schema are
// shredded into typed columns; everything else is encoded as plain variant
// values into the appropriate value column, sharing the row's metadata
// dictionary.
//
//go:noinline
func deconstructFuncOfShreddedVariant(columnIndex uint16, node Node) (uint16, deconstructFunc) {
	// Schemas that cannot be written to (e.g. missing the metadata column)
	// may still be read from; fail when a row is written, not at build time.
	group, err := buildShreddedVariantGroup(node, 0, 0, 0)
	if err == nil && group.metadataCol < 0 {
		err = fmt.Errorf("variant: shredded variant group has no metadata field")
	}
	if err != nil {
		numCols := numLeafColumnsOf(node)
		return columnIndex + numCols, func([][]Value, columnLevels, reflect.Value) {
			panic(fmt.Sprintf("invalid shredded variant schema: %v", err))
		}
	}
	nextColumnIndex := columnIndex + uint16(group.numCols)

	return nextColumnIndex, func(columns [][]Value, levels columnLevels, value reflect.Value) {
		// An invalid value means an enclosing optional/repeated wrapper is
		// null for this row: the whole variant group is null.
		if !value.IsValid() {
			appendShreddedNulls(columns, columnIndex, 0, group.numCols, levels.definitionLevel, levels.repetitionLevel)
			return
		}

		// Convert the Go value to a variant value tree. Raw variant structs
		// (Metadata/Value []byte fields) are decoded so their contents can
		// be shredded like any other value.
		var v variant.Value
		if metaBytes, valBytes, ok := extractRawVariantStruct(value); ok {
			m, err := variant.DecodeMetadata(metaBytes)
			if err != nil {
				panic(fmt.Sprintf("variant metadata: %v", err))
			}
			if v, err = variant.Decode(m, valBytes); err != nil {
				panic(fmt.Sprintf("variant decode: %v", err))
			}
		} else {
			var err error
			if v, err = variant.ValueOf(extractGoValue(value)); err != nil {
				panic(fmt.Sprintf("variant marshal: %v", err))
			}
		}

		// The row's metadata dictionary contains every object field name of
		// the value, shredded or not.
		var b variant.MetadataBuilder
		addVariantFieldNames(&b, v)

		group.write(columns, columnIndex, &b, v, true, shredLevels{
			baseDef: int(levels.definitionLevel),
			baseRep: int(levels.repetitionDepth),
			rep:     levels.repetitionLevel,
		})

		// The metadata column is written last so the dictionary includes
		// the field IDs assigned while encoding residual values.
		_, metadata := b.Build()
		metaCol := columnIndex + uint16(group.metadataCol)
		metaValue := makeValueByteArray(ByteArray, unsafe.SliceData(metadata), len(metadata))
		metaValue.repetitionLevel = levels.repetitionLevel
		metaValue.definitionLevel = levels.definitionLevel
		metaValue.columnIndex = ^metaCol
		columns[metaCol] = append(columns[metaCol], metaValue)
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

// reconstructFuncOfShreddedVariant reconstructs Go values from a shredded
// variant group using the construct_variant algorithm of the Variant
// Shredding specification (see variant_shredded_read.go).
//
//go:noinline
func reconstructFuncOfShreddedVariant(columnIndex uint16, node Node) (uint16, reconstructFunc) {
	group, err := buildShreddedVariantGroup(node, 0, 0, 0)
	if err != nil {
		numCols := numLeafColumnsOf(node)
		return columnIndex + numCols, func(reflect.Value, columnLevels, [][]Value) error {
			return err
		}
	}
	nextColumnIndex := columnIndex + uint16(group.numCols)

	return nextColumnIndex, func(value reflect.Value, levels columnLevels, columns [][]Value) error {
		if len(columns) < group.numCols {
			return fmt.Errorf("shredded variant reconstruction: expected %d columns, got %d", group.numCols, len(columns))
		}
		if group.metadataCol < 0 {
			return fmt.Errorf("shredded variant reconstruction: missing metadata column")
		}

		reader := &variantColumnReader{
			columns: columns,
			pos:     make([]int, len(columns)),
		}

		metaVal, ok := reader.next(group.metadataCol)
		if !ok || metaVal.IsNull() || int(metaVal.definitionLevel) < int(levels.definitionLevel) {
			// The variant group itself is null for this row.
			value.Set(reflect.Zero(value.Type()))
			return nil
		}
		metaBytes := metaVal.ByteArray()
		m, err := variant.DecodeMetadata(metaBytes)
		if err != nil {
			return fmt.Errorf("variant metadata: %w", err)
		}

		v, present, err := group.read(reader, m, int(levels.definitionLevel), int(levels.repetitionDepth))
		if err != nil {
			return err
		}
		if !present {
			// value and typed_value both null at the top level is invalid
			// per the spec; read it as variant null (matching
			// parquet-java's behavior).
			v = variant.Null()
		}

		// If the target is a raw variant struct (Metadata/Value []byte
		// fields), re-encode the reconstructed value.
		if isRawVariantStructTarget(value) {
			var b variant.MetadataBuilder
			encoded := variant.Encode(&b, v)
			_, metadata := b.Build()
			if setRawVariantStruct(value, metadata, encoded) {
				return nil
			}
		}

		return setVariantGoValue(value, v.GoValue())
	}
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

// isRawVariantStructTarget reports whether the reconstruction target is a
// struct with Metadata and Value []byte fields (raw variant passthrough).
func isRawVariantStructTarget(value reflect.Value) bool {
	t := value.Type()
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	byteSliceType := reflect.TypeOf([]byte(nil))
	metaField, hasMetadata := t.FieldByName("Metadata")
	valField, hasValue := t.FieldByName("Value")
	return hasMetadata && hasValue &&
		metaField.Type == byteSliceType && valField.Type == byteSliceType
}
