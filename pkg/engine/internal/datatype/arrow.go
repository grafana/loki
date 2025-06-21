package datatype

import "github.com/apache/arrow-go/v18/arrow"

var (
	Loki = struct {
		Null      DataType
		Bool      DataType
		String    DataType
		Integer   DataType
		Float     DataType
		Timestamp DataType
		Duration  DataType
		Bytes     DataType
	}{
		Null:      tNull{},
		Bool:      tBool{},
		String:    tString{},
		Integer:   tInteger{},
		Float:     tFloat{},
		Timestamp: tTimestamp{},
		Duration:  tDuration{},
		Bytes:     tBytes{},
	}

	Arrow = struct {
		Null      arrow.DataType
		Bool      arrow.DataType
		String    arrow.DataType
		Integer   arrow.DataType
		Float     arrow.DataType
		Timestamp arrow.DataType
		Duration  arrow.DataType
		Bytes     arrow.DataType
	}{
		Null:      arrow.Null,
		Bool:      arrow.FixedWidthTypes.Boolean,
		String:    arrow.BinaryTypes.String,
		Integer:   arrow.PrimitiveTypes.Int64,
		Float:     arrow.PrimitiveTypes.Float64,
		Timestamp: arrow.FixedWidthTypes.Timestamp_ns,
		Duration:  arrow.PrimitiveTypes.Int64,
		Bytes:     arrow.PrimitiveTypes.Int64,
	}
)
