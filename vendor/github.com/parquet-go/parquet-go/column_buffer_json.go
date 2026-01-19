package parquet

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/jsonlite"
)

var jsonNull jsonlite.Value

func init() {
	v, _ := jsonlite.Parse("null")
	jsonNull = *v
}

func jsonParse(data []byte) (*jsonlite.Value, error) {
	if len(data) == 0 {
		return &jsonNull, nil
	}
	return jsonlite.Parse(unsafecast.String(data))
}

func writeJSONToLeaf(col ColumnBuffer, levels columnLevels, val *jsonlite.Value, node Node) {
	typ := node.Type()
	if typ.Kind() == ByteArray {
		if logicalType := typ.LogicalType(); logicalType != nil && logicalType.Json != nil {
			writeJSONToByteArray(col, levels, val, node)
			return
		}
	}
	switch val.Kind() {
	case jsonlite.Null:
		col.writeNull(levels)
	case jsonlite.True, jsonlite.False:
		col.writeBoolean(levels, val.Kind() == jsonlite.True)
	case jsonlite.Number:
		writeJSONNumber(col, levels, val.Number(), node)
	case jsonlite.String:
		writeJSONString(col, levels, val.String(), node)
	default:
		writeJSONToByteArray(col, levels, val, node)
	}
}

func writeJSONToByteArray(col ColumnBuffer, levels columnLevels, val *jsonlite.Value, node Node) {
	if val.Kind() == jsonlite.Null && node.Optional() {
		col.writeNull(levels)
	} else {
		col.writeByteArray(levels, unsafeByteArrayFromString(val.JSON()))
	}
}

func writeJSONToGroup(columns []ColumnBuffer, levels columnLevels, val *jsonlite.Value, node Node, writers []fieldWriter) {
	if val.Kind() != jsonlite.Object {
		for i := range writers {
			w := &writers[i]
			w.writeValue(columns, levels, reflect.Value{})
		}
		return
	}

	for i := range writers {
		w := &writers[i]
		f := val.Lookup(w.fieldName)
		if f == nil {
			w.writeValue(columns, levels, reflect.Value{})
		} else {
			w.writeValue(columns, levels, reflect.ValueOf(f))
		}
	}
}

func writeJSONToRepeated(columns []ColumnBuffer, levels columnLevels, val *jsonlite.Value, elementWriter writeValueFunc) {
	if val.Kind() == jsonlite.Array {
		if val.Len() == 0 {
			elementWriter(columns, levels, reflect.Value{})
			return
		}

		levels.repetitionDepth++
		levels.definitionLevel++

		for elem := range val.Array {
			elementWriter(columns, levels, reflect.ValueOf(elem))
			levels.repetitionLevel = levels.repetitionDepth
		}
		return
	}

	// Auto-wrap scalar to single-element array
	levels.repetitionDepth++
	levels.definitionLevel++
	elementWriter(columns, levels, reflect.ValueOf(val))
}

func writeJSONString(col ColumnBuffer, levels columnLevels, str string, node Node) {
	typ := node.Type()

	if logicalType := typ.LogicalType(); logicalType != nil {
		switch {
		case logicalType.Timestamp != nil:
			t, err := time.Parse(time.RFC3339, str)
			if err != nil {
				panic(fmt.Errorf("cannot parse JSON string %q as timestamp: %w", str, err))
			}
			writeTime(col, levels, t, node)
			return

		case logicalType.Date != nil:
			t, err := time.Parse("2006-01-02", str)
			if err != nil {
				panic(fmt.Errorf("cannot parse JSON string %q as date: %w", str, err))
			}
			writeTime(col, levels, t, node)
			return

		case logicalType.Time != nil:
			t, err := time.Parse("15:04:05.000000000", str)
			if err != nil {
				panic(fmt.Errorf("cannot parse JSON string %q as time: %w", str, err))
			}
			d := time.Duration(t.Hour())*time.Hour +
				time.Duration(t.Minute())*time.Minute +
				time.Duration(t.Second())*time.Second +
				time.Duration(t.Nanosecond())*time.Nanosecond
			writeDuration(col, levels, d, node)
			return

		case logicalType.UUID != nil:
			// Only parse UUID strings when writing to binary UUID columns
			// (FIXED_LEN_BYTE_ARRAY with 16 bytes). If writing to a STRING
			// column with UUID logical type, write the string as-is.
			writeUUID(col, levels, str, typ)
			return
		}
	}

	col.writeByteArray(levels, unsafeByteArrayFromString(str))
}

func writeJSONNumber(col ColumnBuffer, levels columnLevels, num json.Number, node Node) {
	typ := node.Type()
	str := num.String()

	if logicalType := typ.LogicalType(); logicalType != nil {
		switch {
		case logicalType.Timestamp != nil:
			// Interpret number as seconds since Unix epoch (with sub-second precision)
			f, err := num.Float64()
			if err != nil {
				panic(fmt.Errorf("cannot convert json.Number %q to float64 for timestamp: %w", num, err))
			}
			sec, frac := math.Modf(f)
			t := time.Unix(int64(sec), int64(frac*1e9)).UTC()
			writeTime(col, levels, t, node)
			return

		case logicalType.Date != nil:
			// Interpret number as seconds since Unix epoch
			i, err := num.Int64()
			if err != nil {
				panic(fmt.Errorf("cannot convert json.Number %q to int64 for date: %w", num, err))
			}
			t := time.Unix(i, 0).UTC()
			writeTime(col, levels, t, node)
			return

		case logicalType.Time != nil:
			// Interpret number as seconds since midnight
			f, err := num.Float64()
			if err != nil {
				panic(fmt.Errorf("cannot convert json.Number %q to float64 for time: %w", num, err))
			}
			d := time.Duration(f * float64(time.Second))
			writeDuration(col, levels, d, node)
			return
		}
	}

	switch kind := typ.Kind(); kind {
	case Boolean:
		f, err := num.Float64()
		if err != nil {
			panic(fmt.Errorf("cannot convert json.Number %q to float64 for boolean: %w", num, err))
		}
		col.writeBoolean(levels, f != 0)

	case Int32, Int64:
		switch jsonlite.NumberTypeOf(str) {
		case jsonlite.Int:
			i, err := num.Int64()
			if err != nil {
				panic(fmt.Errorf("cannot convert json.Number %q to int: %w", num, err))
			}
			col.writeInt64(levels, i)
		case jsonlite.Uint:
			u, err := strconv.ParseUint(str, 10, 64)
			if err != nil {
				panic(fmt.Errorf("cannot convert json.Number %q to int: %w", num, err))
			}
			col.writeInt64(levels, int64(u))
		case jsonlite.Float:
			f, err := num.Float64()
			if err != nil {
				panic(fmt.Errorf("cannot convert json.Number %q to float: %w", num, err))
			}
			col.writeInt64(levels, int64(f))
		}

	case Float, Double:
		f, err := num.Float64()
		if err != nil {
			panic(fmt.Errorf("cannot convert json.Number %q to float64: %w", num, err))
		}
		col.writeDouble(levels, f)

	default:
		col.writeByteArray(levels, unsafeByteArrayFromString(str))
	}
}
