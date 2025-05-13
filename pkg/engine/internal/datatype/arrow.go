package datatype

import "github.com/apache/arrow-go/v18/arrow"

var (
	LokiType = struct {
		Null      DataType
		Bool      DataType
		String    DataType
		Integer   DataType
		Float     DataType
		Timestamp DataType
		Duration  DataType
		Bytes     DataType
	}{
		Null:      Null,
		Bool:      Bool,
		String:    String,
		Integer:   Integer,
		Float:     Float,
		Timestamp: Timestamp,
		Duration:  Duration,
		Bytes:     Bytes,
	}

	ArrowType = struct {
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
		Timestamp: arrow.PrimitiveTypes.Int64,
		Duration:  arrow.PrimitiveTypes.Int64,
		Bytes:     arrow.PrimitiveTypes.Int64,
	}

	ToArrow = map[DataType]arrow.DataType{
		Null:      ArrowType.Null,
		Bool:      ArrowType.Bool,
		String:    ArrowType.String,
		Integer:   ArrowType.Integer,
		Float:     ArrowType.Float,
		Timestamp: ArrowType.Timestamp,
		Duration:  ArrowType.Duration,
		Bytes:     ArrowType.Bytes,
	}

	ToLoki = map[arrow.DataType]DataType{
		ArrowType.Null:      Null,
		ArrowType.Bool:      Bool,
		ArrowType.String:    String,
		ArrowType.Integer:   Integer,
		ArrowType.Float:     Float,
		ArrowType.Timestamp: Timestamp,
		ArrowType.Duration:  Duration,
		ArrowType.Bytes:     Bytes,
	}
)
