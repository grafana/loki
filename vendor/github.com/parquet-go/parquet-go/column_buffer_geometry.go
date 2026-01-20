package parquet

import (
	"fmt"

	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
)

func writeGeometry(col ColumnBuffer, levels columnLevels, value geom.T, node Node) {
	if value == nil {
		col.writeNull(levels)
		return
	}

	if logicalType := node.Type().LogicalType(); logicalType != nil {
		switch {
		case logicalType.Geometry != nil:
		case logicalType.Geography != nil:
			// valid
		default:
			panic("invalid logical type for value of type geom.T, must be Geometry or Geography")
		}
	} else {
		panic("missing logical type for value of type geom.T, must be Geometry or Geography")
	}

	data, err := wkb.Marshal(value, wkb.NDR)
	if err != nil {
		panic(fmt.Errorf("failed to marshal geometry to WKB: %v", err))
	}
	col.writeByteArray(levels, data)
}
