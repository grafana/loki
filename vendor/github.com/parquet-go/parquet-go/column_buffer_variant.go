package parquet

import (
	"fmt"
	"reflect"

	"github.com/parquet-go/parquet-go/variant"
)

func writeValueFuncOfVariant(columnIndex uint16, node Node) (uint16, writeValueFunc) {
	if isUnshreddedVariant(node) {
		return writeValueFuncOfUnshreddedVariant(columnIndex, node)
	}
	return writeValueFuncOfShreddedVariant(columnIndex, node)
}

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

// writeValueFuncOfShreddedVariant writes Go values into a shredded variant
// group per the Variant Shredding specification. It shares the shredding
// logic with the row-based write path (variant_shredded_write.go) by
// shredding into a scratch column buffer and flushing the resulting values
// with their levels to the column buffers.
func writeValueFuncOfShreddedVariant(columnIndex uint16, node Node) (uint16, writeValueFunc) {
	// Schemas that cannot be written to (e.g. missing the metadata column)
	// may still be read from; fail when a row is written, not at build time.
	group, err := buildShreddedVariantGroup(node, 0, 0, 0)
	if err == nil && group.metadataCol < 0 {
		err = fmt.Errorf("variant: shredded variant group has no metadata field")
	}
	if err != nil {
		numCols := numLeafColumnsOf(node)
		return columnIndex + numCols, func([]ColumnBuffer, columnLevels, reflect.Value) {
			panic(fmt.Sprintf("invalid shredded variant schema: %v", err))
		}
	}
	nextColumnIndex := columnIndex + uint16(group.numCols)

	return nextColumnIndex, func(columns []ColumnBuffer, levels columnLevels, value reflect.Value) {
		// An invalid value means an enclosing optional/repeated wrapper is
		// null for this row: the whole variant group is null.
		if !value.IsValid() {
			for col := range group.numCols {
				columns[columnIndex+uint16(col)].writeNull(levels)
			}
			return
		}

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

		var b variant.MetadataBuilder
		addVariantFieldNames(&b, v)

		// One backing slab gives every scratch column capacity for the
		// common one-value-per-column case without a per-column allocation;
		// only columns under a repeated list can grow past it.
		scratch := make([][]Value, group.numCols)
		slab := make([]Value, len(scratch))
		for i := range scratch {
			scratch[i] = slab[i : i : i+1]
		}
		group.write(scratch, 0, &b, v, true, shredLevels{
			baseDef: int(levels.definitionLevel),
			baseRep: int(levels.repetitionDepth),
			rep:     levels.repetitionLevel,
		})

		_, metadata := b.Build()
		columns[columnIndex+uint16(group.metadataCol)].writeByteArray(levels, metadata)

		for col, values := range scratch {
			if col == group.metadataCol {
				continue
			}
			buffer := columns[columnIndex+uint16(col)]
			for _, val := range values {
				valueLevels := columnLevels{
					repetitionDepth: levels.repetitionDepth,
					repetitionLevel: val.repetitionLevel,
					definitionLevel: val.definitionLevel,
				}
				if val.IsNull() {
					buffer.writeNull(valueLevels)
					continue
				}
				switch val.Kind() {
				case Boolean:
					buffer.writeBoolean(valueLevels, val.Boolean())
				case Int32:
					buffer.writeInt32(valueLevels, val.Int32())
				case Int64:
					buffer.writeInt64(valueLevels, val.Int64())
				case Float:
					buffer.writeFloat(valueLevels, val.Float())
				case Double:
					buffer.writeDouble(valueLevels, val.Double())
				default:
					buffer.writeByteArray(valueLevels, val.ByteArray())
				}
			}
		}
	}
}
