package parquet

import (
	"fmt"
	"reflect"

	"github.com/parquet-go/parquet-go/variant"
)

// writeValueFuncOfVariant handles writing Go values to variant column buffers.
// Dispatches to unshredded or shredded variant handling.
func writeValueFuncOfVariant(columnIndex uint16, node Node) (uint16, writeValueFunc) {
	if isUnshreddedVariant(node) {
		return writeValueFuncOfUnshreddedVariant(columnIndex, node)
	}
	return writeValueFuncOfShreddedVariant(columnIndex, node)
}

// writeValueFuncOfUnshreddedVariant handles the simple 2-column variant.
func writeValueFuncOfUnshreddedVariant(columnIndex uint16, node Node) (uint16, writeValueFunc) {
	metadataColumnIndex := columnIndex
	valueColumnIndex := columnIndex + 1
	nextColumnIndex := columnIndex + 2

	return nextColumnIndex, func(columns []ColumnBuffer, levels columnLevels, value reflect.Value) {
		metadata, val := variantMarshalOrNull(value)
		columns[metadataColumnIndex].writeByteArray(levels, metadata)
		columns[valueColumnIndex].writeByteArray(levels, val)
	}
}

// writeValueFuncOfShreddedVariant handles the 3-field shredded variant.
func writeValueFuncOfShreddedVariant(columnIndex uint16, node Node) (uint16, writeValueFunc) {
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

	typedInner := Required(typedValueNode)
	matcher := buildShreddedMatcher(typedInner)

	return nextColumnIndex, func(columns []ColumnBuffer, levels columnLevels, value reflect.Value) {
		metadata, val := variantMarshalOrNull(value)
		columns[metadataColumnIndex].writeByteArray(levels, metadata)

		goVal := extractGoValue(value)
		if goVal != nil && matcher.canShred(goVal) {
			// Write null to value column
			columns[valueColumnIndex].writeNull(levels)
			// Write typed value (increment def level for optional wrapper)
			typedLevels := levels
			typedLevels.definitionLevel++
			writeShredded(columns, typedLevels, typedValueStartColumn, &matcher, goVal)
		} else {
			// Write variant bytes to value column
			valueLevels := levels
			valueLevels.definitionLevel++
			columns[valueColumnIndex].writeByteArray(valueLevels, val)
			// Write null to all typed_value columns
			for col := typedValueStartColumn; col < typedValueStartColumn+typedValueLeafCount; col++ {
				columns[col].writeNull(levels)
			}
		}
	}
}

// writeShredded writes a shredded value to the column buffers.
func writeShredded(columns []ColumnBuffer, levels columnLevels, startColumn uint16, m *shreddedMatcher, v any) {
	if m.isPrimitive() {
		pv := goValueToParquetValue(v, m.kind())
		col := columns[startColumn]
		switch m.kind() {
		case Boolean:
			col.writeBoolean(levels, pv.Boolean())
		case Int32:
			col.writeInt32(levels, pv.Int32())
		case Int64:
			col.writeInt64(levels, pv.Int64())
		case Float:
			col.writeFloat(levels, pv.Float())
		case Double:
			col.writeDouble(levels, pv.Double())
		case ByteArray:
			col.writeByteArray(levels, pv.ByteArray())
		default:
			col.writeByteArray(levels, pv.ByteArray())
		}
		return
	}
	if m.isGroup() {
		writeShreddedObject(columns, levels, startColumn, m, v)
	}
}

func writeShreddedObject(columns []ColumnBuffer, levels columnLevels, startColumn uint16, m *shreddedMatcher, v any) {
	obj, ok := v.(map[string]any)
	if !ok {
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
			columns[valueCol].writeNull(levels)
			typedLevels := levels
			typedLevels.definitionLevel++
			writeShredded(columns, typedLevels, typedValueCol, &f.matcher, fieldVal)
		} else if exists && fieldVal != nil {
			valueLevels := levels
			valueLevels.definitionLevel++
			metadata, val, err := variant.Marshal(fieldVal)
			if err != nil {
				panic(fmt.Sprintf("variant marshal field %q: %v", f.name, err))
			}
			combined := encodeFieldVariant(metadata, val)
			columns[valueCol].writeByteArray(valueLevels, combined)
			for c := typedValueCol; c < typedValueCol+typedCount; c++ {
				columns[c].writeNull(levels)
			}
		} else {
			for c := typedValueCol; c < nextFieldCol; c++ {
				columns[c].writeNull(levels)
			}
		}

		col = nextFieldCol
	}
}
