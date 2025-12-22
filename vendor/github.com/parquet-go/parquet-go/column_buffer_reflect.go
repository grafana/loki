package parquet

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"math/bits"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/parquet-go/jsonlite"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// isNullValue determines if a reflect.Value represents a null value for parquet encoding.
// This handles various types that can represent null including:
// - Invalid reflect values
// - Nil pointers/interfaces/slices/maps
// - json.RawMessage containing "null"
// - *jsonlite.Value with Kind == jsonlite.Null
// - nil *structpb.Struct, *structpb.ListValue, *structpb.Value
// - Zero values for value types
func isNullValue(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}

	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return true
		}
		switch v := value.Interface().(type) {
		case *jsonlite.Value:
			return v.Kind() == jsonlite.Null
		case *structpb.Value:
			_, isNull := v.GetKind().(*structpb.Value_NullValue)
			return isNull
		}
		return false

	case reflect.Slice:
		if value.IsNil() {
			return true
		}
		if value.Type() == reflect.TypeFor[json.RawMessage]() {
			return string(value.Bytes()) == "null"
		}
		return false

	case reflect.Map:
		return value.IsNil()

	default:
		return value.IsZero()
	}
}

type anymap interface {
	entries() (keys, values sparse.Array)
}

type fieldWriter struct {
	fieldName  string
	writeValue writeValueFunc
}

type gomap[K cmp.Ordered] struct {
	keys []K
	vals reflect.Value // slice
	swap func(int, int)
	size uintptr
}

func (m *gomap[K]) Len() int { return len(m.keys) }

func (m *gomap[K]) Less(i, j int) bool { return cmp.Compare(m.keys[i], m.keys[j]) < 0 }

func (m *gomap[K]) Swap(i, j int) {
	m.keys[i], m.keys[j] = m.keys[j], m.keys[i]
	m.swap(i, j)
}

func (m *gomap[K]) entries() (keys, values sparse.Array) {
	return makeArrayFromSlice(m.keys), makeArray(m.vals.UnsafePointer(), m.Len(), m.size)
}

type reflectMap struct {
	keys    reflect.Value // slice
	vals    reflect.Value // slice
	numKeys int
	keySize uintptr
	valSize uintptr
}

func (m *reflectMap) entries() (keys, values sparse.Array) {
	return makeArray(m.keys.UnsafePointer(), m.numKeys, m.keySize), makeArray(m.vals.UnsafePointer(), m.numKeys, m.valSize)
}

func makeMapFuncOf(mapType reflect.Type) func(reflect.Value) anymap {
	switch mapType.Key().Kind() {
	case reflect.Int:
		return makeMapFunc[int](mapType)
	case reflect.Int8:
		return makeMapFunc[int8](mapType)
	case reflect.Int16:
		return makeMapFunc[int16](mapType)
	case reflect.Int32:
		return makeMapFunc[int32](mapType)
	case reflect.Int64:
		return makeMapFunc[int64](mapType)
	case reflect.Uint:
		return makeMapFunc[uint](mapType)
	case reflect.Uint8:
		return makeMapFunc[uint8](mapType)
	case reflect.Uint16:
		return makeMapFunc[uint16](mapType)
	case reflect.Uint32:
		return makeMapFunc[uint32](mapType)
	case reflect.Uint64:
		return makeMapFunc[uint64](mapType)
	case reflect.Uintptr:
		return makeMapFunc[uintptr](mapType)
	case reflect.Float32:
		return makeMapFunc[float32](mapType)
	case reflect.Float64:
		return makeMapFunc[float64](mapType)
	case reflect.String:
		return makeMapFunc[string](mapType)
	}

	keyType := mapType.Key()
	valType := mapType.Elem()

	mapBuffer := &reflectMap{
		keySize: keyType.Size(),
		valSize: valType.Size(),
	}

	keySliceType := reflect.SliceOf(keyType)
	valSliceType := reflect.SliceOf(valType)
	return func(mapValue reflect.Value) anymap {
		length := mapValue.Len()

		if !mapBuffer.keys.IsValid() || mapBuffer.keys.Len() < length {
			capacity := 1 << bits.Len(uint(length))
			mapBuffer.keys = reflect.MakeSlice(keySliceType, capacity, capacity)
			mapBuffer.vals = reflect.MakeSlice(valSliceType, capacity, capacity)
		}

		mapBuffer.numKeys = length
		for i, mapIter := 0, mapValue.MapRange(); mapIter.Next(); i++ {
			mapBuffer.keys.Index(i).SetIterKey(mapIter)
			mapBuffer.vals.Index(i).SetIterValue(mapIter)
		}

		return mapBuffer
	}
}

func makeMapFunc[K cmp.Ordered](mapType reflect.Type) func(reflect.Value) anymap {
	keyType := mapType.Key()
	valType := mapType.Elem()
	valSliceType := reflect.SliceOf(valType)
	mapBuffer := &gomap[K]{size: valType.Size()}
	return func(mapValue reflect.Value) anymap {
		length := mapValue.Len()

		if cap(mapBuffer.keys) < length {
			capacity := 1 << bits.Len(uint(length))
			mapBuffer.keys = make([]K, capacity)
			mapBuffer.vals = reflect.MakeSlice(valSliceType, capacity, capacity)
			mapBuffer.swap = reflect.Swapper(mapBuffer.vals.Interface())
		}

		mapBuffer.keys = mapBuffer.keys[:length]
		for i, mapIter := 0, mapValue.MapRange(); mapIter.Next(); i++ {
			reflect.NewAt(keyType, unsafe.Pointer(&mapBuffer.keys[i])).Elem().SetIterKey(mapIter)
			mapBuffer.vals.Index(i).SetIterValue(mapIter)
		}

		sort.Sort(mapBuffer)
		return mapBuffer
	}
}

// writeValueFunc is a function that writes a single reflect.Value to a set of
// column buffers.
// Panics if the value cannot be written (similar to reflect package behavior).
type writeValueFunc func([]ColumnBuffer, columnLevels, reflect.Value)

// timeOfDayNanos returns the nanoseconds since midnight for the given time.
func timeOfDayNanos(t time.Time) int64 {
	m := nearestMidnightLessThan(t)
	return t.Sub(m).Nanoseconds()
}

func writeTime(col ColumnBuffer, levels columnLevels, t time.Time, node Node) {
	typ := node.Type()

	if logicalType := typ.LogicalType(); logicalType != nil {
		switch {
		case logicalType.Timestamp != nil:
			// TIMESTAMP logical type -> write to int64
			unit := logicalType.Timestamp.Unit
			var val int64
			switch {
			case unit.Millis != nil:
				val = t.UnixMilli()
			case unit.Micros != nil:
				val = t.UnixMicro()
			default:
				val = t.UnixNano()
			}
			col.writeInt64(levels, val)
			return

		case logicalType.Date != nil:
			// DATE logical type -> write to int32
			col.writeInt32(levels, int32(daysSinceUnixEpoch(t)))
			return

		case logicalType.Time != nil:
			// TIME logical type -> write time of day
			unit := logicalType.Time.Unit
			nanos := timeOfDayNanos(t)
			switch {
			case unit.Millis != nil:
				col.writeInt32(levels, int32(nanos/1e6))
			case unit.Micros != nil:
				col.writeInt64(levels, nanos/1e3)
			default:
				col.writeInt64(levels, nanos)
			}
			return
		}
	}

	// No time logical type - use physical type
	switch typ.Kind() {
	case Int32:
		// int32 without logical type -> days since epoch
		col.writeInt32(levels, int32(daysSinceUnixEpoch(t)))
	case Int64:
		// int64 without logical type -> nanoseconds since epoch
		col.writeInt64(levels, t.UnixNano())
	case Float:
		// float -> fractional seconds since epoch
		col.writeFloat(levels, float32(float64(t.UnixNano())/1e9))
	case Double:
		// double -> fractional seconds since epoch
		col.writeDouble(levels, float64(t.UnixNano())/1e9)
	case ByteArray:
		// byte array -> RFC3339Nano
		s := t.Format(time.RFC3339Nano)
		col.writeByteArray(levels, unsafe.Slice(unsafe.StringData(s), len(s)))
	default:
		panic(fmt.Sprintf("cannot write time.Time to column with physical type %v", typ))
	}
}

func writeDuration(col ColumnBuffer, levels columnLevels, d time.Duration, node Node) {
	typ := node.Type()

	if logicalType := typ.LogicalType(); logicalType != nil && logicalType.Time != nil {
		// TIME logical type
		unit := logicalType.Time.Unit
		switch {
		case unit.Millis != nil:
			col.writeInt32(levels, int32(d.Milliseconds()))
		case unit.Micros != nil:
			col.writeInt64(levels, d.Microseconds())
		default:
			col.writeInt64(levels, d.Nanoseconds())
		}
		return
	}

	// No TIME logical type - use physical type
	switch typ.Kind() {
	case Int32:
		panic("cannot write time.Duration to int32 column without TIME logical type")
	case Int64:
		// int64 -> nanoseconds
		col.writeInt64(levels, d.Nanoseconds())
	case Float:
		// float -> seconds
		col.writeFloat(levels, float32(d.Seconds()))
	case Double:
		// double -> seconds
		col.writeDouble(levels, d.Seconds())
	case ByteArray:
		// byte array -> String()
		s := d.String()
		col.writeByteArray(levels, unsafe.Slice(unsafe.StringData(s), len(s)))
	default:
		panic(fmt.Sprintf("cannot write time.Duration to column with physical type %v", typ))
	}
}

// writeValueFuncOf constructs a function that writes reflect.Values to column buffers.
// It follows the deconstructFuncOf pattern, recursively building functions for the schema tree.
// Returns (nextColumnIndex, writeFunc).
func writeValueFuncOf(columnIndex int16, node Node) (int16, writeValueFunc) {
	switch {
	case node.Optional():
		return writeValueFuncOfOptional(columnIndex, node)
	case node.Repeated():
		return writeValueFuncOfRepeated(columnIndex, node)
	case isList(node):
		return writeValueFuncOfList(columnIndex, node)
	case isMap(node):
		return writeValueFuncOfMap(columnIndex, node)
	default:
		return writeValueFuncOfRequired(columnIndex, node)
	}
}

func writeValueFuncOfOptional(columnIndex int16, node Node) (int16, writeValueFunc) {
	nextColumnIndex, writeValue := writeValueFuncOf(columnIndex, Required(node))
	return nextColumnIndex, func(columns []ColumnBuffer, levels columnLevels, value reflect.Value) {
		if isNullValue(value) {
			writeValue(columns, levels, value)
		} else {
			levels.definitionLevel++
			writeValue(columns, levels, value)
		}
	}
}

func writeValueFuncOfRepeated(columnIndex int16, node Node) (int16, writeValueFunc) {
	nextColumnIndex, writeValue := writeValueFuncOf(columnIndex, Required(node))
	return nextColumnIndex, func(columns []ColumnBuffer, levels columnLevels, value reflect.Value) {
	writeRepatedValue:
		if !value.IsValid() {
			writeValue(columns, levels, reflect.Value{})
			return
		}

		switch msg := value.Interface().(type) {
		case *jsonlite.Value:
			writeJSONToRepeated(columns, levels, msg, writeValue)
			return

		case json.RawMessage:
			val, err := jsonParse(msg)
			if err != nil {
				panic(fmt.Errorf("failed to parse JSON: %w", err))
			}
			writeJSONToRepeated(columns, levels, val, writeValue)
			return

		case *structpb.Struct:
			if msg == nil {
				writeValue(columns, levels, reflect.Value{})
				return
			}
			levels.repetitionDepth++
			levels.definitionLevel++
			writeValue(columns, levels, value)
			return

		case *structpb.ListValue:
			n := len(msg.GetValues())
			if n == 0 {
				writeValue(columns, levels, reflect.Value{})
				return
			}

			levels.repetitionDepth++
			levels.definitionLevel++

			for _, v := range msg.GetValues() {
				writeValue(columns, levels, structpbValueToReflectValue(v))
				levels.repetitionLevel = levels.repetitionDepth
			}
			return

		case protoreflect.List:
			n := msg.Len()
			if n == 0 {
				writeValue(columns, levels, reflect.Value{})
				return
			}

			levels.repetitionDepth++
			levels.definitionLevel++

			for i := range n {
				var e = msg.Get(i)
				var v reflect.Value
				if e.IsValid() {
					v = reflect.ValueOf(e.Interface())
				}
				writeValue(columns, levels, v)
				levels.repetitionLevel = levels.repetitionDepth
			}
			return
		}

		switch value.Kind() {
		case reflect.Interface, reflect.Pointer:
			if value.IsNil() {
				writeValue(columns, levels, reflect.Value{})
				return
			}
			value = value.Elem()
			goto writeRepatedValue

		case reflect.Slice, reflect.Array:
			n := value.Len()
			if n == 0 {
				writeValue(columns, levels, reflect.Value{})
				return
			}

			levels.repetitionDepth++
			levels.definitionLevel++

			for i := range n {
				writeValue(columns, levels, value.Index(i))
				levels.repetitionLevel = levels.repetitionDepth
			}

		default:
			levels.repetitionDepth++
			levels.definitionLevel++

			// If this is a repeated group with a single field, and the value is a scalar,
			// wrap the scalar into a struct with that field name.
			if !node.Leaf() && value.IsValid() && value.Kind() != reflect.Struct && value.Kind() != reflect.Map {
				fields := Required(node).Fields()
				if len(fields) == 1 {
					field := fields[0]
					fieldType := field.GoType()
					fieldName := field.Name()

					if value.Type().AssignableTo(fieldType) || value.Type().ConvertibleTo(fieldType) {
						structType := reflect.StructOf([]reflect.StructField{
							{Name: fieldName, Type: fieldType},
						})
						wrappedValue := reflect.New(structType).Elem()

						if value.Type().AssignableTo(fieldType) {
							wrappedValue.Field(0).Set(value)
						} else {
							wrappedValue.Field(0).Set(value.Convert(fieldType))
						}

						value = wrappedValue
					}
				}
			}

			writeValue(columns, levels, value)
		}
	}
}

func writeValueFuncOfRequired(columnIndex int16, node Node) (int16, writeValueFunc) {
	switch {
	case node.Leaf():
		return writeValueFuncOfLeaf(columnIndex, node)
	default:
		return writeValueFuncOfGroup(columnIndex, node)
	}
}

func writeValueFuncOfList(columnIndex int16, node Node) (int16, writeValueFunc) {
	return writeValueFuncOf(columnIndex, Repeated(listElementOf(node)))
}

func writeValueFuncOfMap(columnIndex int16, node Node) (int16, writeValueFunc) {
	keyValue := mapKeyValueOf(node)
	keyValueType := keyValue.GoType()
	keyValueElem := keyValueType.Elem()
	keyType := keyValueElem.Field(0).Type
	valueType := keyValueElem.Field(1).Type
	nextColumnIndex, writeValue := writeValueFuncOf(columnIndex, schemaOf(keyValueElem))
	zeroKeyValue := reflect.Zero(keyValueElem)

	return nextColumnIndex, func(columns []ColumnBuffer, levels columnLevels, mapValue reflect.Value) {
		// Check for invalid or nil map first to avoid panic on Interface() or Len()
		if !mapValue.IsValid() || mapValue.IsNil() {
			writeValue(columns, levels, zeroKeyValue)
			return
		}

		switch m := mapValue.Interface().(type) {
		case protoreflect.Map:
			n := m.Len()
			if n == 0 {
				writeValue(columns, levels, zeroKeyValue)
				return
			}

			levels.repetitionDepth++
			levels.definitionLevel++

			elem := reflect.New(keyValueElem).Elem()
			k := elem.Field(0)
			v := elem.Field(1)

			for mapKey, mapVal := range m.Range {
				k.Set(reflect.ValueOf(mapKey.Interface()).Convert(keyType))
				v.Set(reflect.ValueOf(mapVal.Interface()).Convert(valueType))
				writeValue(columns, levels, elem)
				levels.repetitionLevel = levels.repetitionDepth
			}
			return
		}

		if mapValue.Len() == 0 {
			writeValue(columns, levels, zeroKeyValue)
			return
		}

		levels.repetitionDepth++
		levels.definitionLevel++

		mapType := mapValue.Type()
		mapKey := reflect.New(mapType.Key()).Elem()
		mapElem := reflect.New(mapType.Elem()).Elem()

		elem := reflect.New(keyValueElem).Elem()
		k := elem.Field(0)
		v := elem.Field(1)

		for it := mapValue.MapRange(); it.Next(); {
			mapKey.SetIterKey(it)
			mapElem.SetIterValue(it)
			k.Set(mapKey.Convert(keyType))
			v.Set(mapElem.Convert(valueType))
			writeValue(columns, levels, elem)
			levels.repetitionLevel = levels.repetitionDepth
		}
	}
}

var structFieldsCache atomic.Value // map[reflect.Type]map[string][]int

func writeValueFuncOfGroup(columnIndex int16, node Node) (int16, writeValueFunc) {
	fields := node.Fields()
	writers := make([]fieldWriter, len(fields))
	for i, field := range fields {
		writers[i].fieldName = field.Name()
		columnIndex, writers[i].writeValue = writeValueFuncOf(columnIndex, field)
	}

	return columnIndex, func(columns []ColumnBuffer, levels columnLevels, value reflect.Value) {
	writeGroupValue:
		if !value.IsValid() {
			for i := range writers {
				w := &writers[i]
				w.writeValue(columns, levels, reflect.Value{})
			}
			return
		}

		switch t := value.Type(); t.Kind() {
		case reflect.Map:
			switch {
			case t.ConvertibleTo(reflect.TypeFor[map[string]string]()):
				m := value.Convert(reflect.TypeFor[map[string]string]()).Interface().(map[string]string)
				v := new(string)
				for i := range writers {
					w := &writers[i]
					*v = m[w.fieldName]
					w.writeValue(columns, levels, reflect.ValueOf(v).Elem())
				}

			case t.ConvertibleTo(reflect.TypeFor[map[string]any]()):
				m := value.Convert(reflect.TypeFor[map[string]any]()).Interface().(map[string]any)
				for i := range writers {
					w := &writers[i]
					v := m[w.fieldName]
					w.writeValue(columns, levels, reflect.ValueOf(v))
				}

			default:
				for i := range writers {
					w := &writers[i]
					fieldName := reflect.ValueOf(&w.fieldName).Elem()
					fieldValue := value.MapIndex(fieldName)
					w.writeValue(columns, levels, fieldValue)
				}
			}

		case reflect.Struct:
			cachedFields, _ := structFieldsCache.Load().(map[reflect.Type]map[string][]int)
			structFields, ok := cachedFields[t]
			if !ok {
				visibleStructFields := reflect.VisibleFields(t)
				cachedFieldsBefore := cachedFields
				structFields = make(map[string][]int, len(visibleStructFields))
				cachedFields = make(map[reflect.Type]map[string][]int, len(cachedFieldsBefore)+1)
				cachedFields[t] = structFields
				maps.Copy(cachedFields, cachedFieldsBefore)

				for _, visibleStructField := range visibleStructFields {
					name := visibleStructField.Name
					if tag, ok := visibleStructField.Tag.Lookup("parquet"); ok {
						if tagName, _, _ := strings.Cut(tag, ","); tagName != "" {
							name = tagName
						}
					}
					structFields[name] = visibleStructField.Index
				}

				structFieldsCache.Store(cachedFields)
			}

			for i := range writers {
				w := &writers[i]
				fieldValue := reflect.Value{}
				fieldIndex, ok := structFields[w.fieldName]
				if ok {
					fieldValue = value.FieldByIndex(fieldIndex)
				}
				w.writeValue(columns, levels, fieldValue)
			}

		case reflect.Pointer, reflect.Interface:
			if value.IsNil() {
				value = reflect.Value{}
				goto writeGroupValue
			}

			switch msg := value.Interface().(type) {
			case *jsonlite.Value:
				writeJSONToGroup(columns, levels, msg, node, writers)
			case *structpb.Struct:
				var fields map[string]*structpb.Value
				if msg != nil {
					fields = msg.Fields
				}
				for i := range writers {
					w := &writers[i]
					v := structpbValueToReflectValue(fields[w.fieldName])
					w.writeValue(columns, levels, v)
				}
			case *anypb.Any:
				if writeProtoAnyToGroup(msg, columns, levels, writers, node, &value) {
					return
				}
				goto writeGroupValue
			case proto.Message:
				writeProtoMessageToGroup(msg, columns, levels, writers)
			default:
				value = value.Elem()
				goto writeGroupValue
			}

		case reflect.Slice:
			if t == reflect.TypeFor[json.RawMessage]() {
				val, err := jsonParse(value.Bytes())
				if err != nil {
					panic(fmt.Errorf("failed to parse JSON: %w", err))
				}
				writeJSONToGroup(columns, levels, val, node, writers)
			} else {
				value = reflect.Value{}
				goto writeGroupValue
			}

		default:
			value = reflect.Value{}
			goto writeGroupValue
		}
	}
}

func writeValueFuncOfLeaf(columnIndex int16, node Node) (int16, writeValueFunc) {
	if columnIndex < 0 {
		panic("writeValueFuncOfLeaf called with invalid columnIndex -1 (empty group)")
	}
	if columnIndex > MaxColumnIndex {
		panic("row cannot be written because it has more than 127 columns")
	}
	return columnIndex + 1, func(columns []ColumnBuffer, levels columnLevels, value reflect.Value) {
		col := columns[columnIndex]
	writeValue:
		if !value.IsValid() {
			col.writeNull(levels)
			return
		}

		switch value.Kind() {
		case reflect.Pointer, reflect.Interface:
			if value.IsNil() {
				col.writeNull(levels)
				return
			}
			switch msg := value.Interface().(type) {
			case *jsonlite.Value:
				writeJSONToLeaf(col, levels, msg, node)
			case *json.Number:
				writeJSONNumber(col, levels, *msg, node)
			case *time.Time:
				writeTime(col, levels, *msg, node)
			case *time.Duration:
				writeDuration(col, levels, *msg, node)
			case *timestamppb.Timestamp:
				writeProtoTimestamp(col, levels, msg, node)
			case *durationpb.Duration:
				writeProtoDuration(col, levels, msg, node)
			case *wrapperspb.BoolValue:
				col.writeBoolean(levels, msg.GetValue())
			case *wrapperspb.Int32Value:
				col.writeInt32(levels, msg.GetValue())
			case *wrapperspb.Int64Value:
				col.writeInt64(levels, msg.GetValue())
			case *wrapperspb.UInt32Value:
				col.writeInt32(levels, int32(msg.GetValue()))
			case *wrapperspb.UInt64Value:
				col.writeInt64(levels, int64(msg.GetValue()))
			case *wrapperspb.FloatValue:
				col.writeFloat(levels, msg.GetValue())
			case *wrapperspb.DoubleValue:
				col.writeDouble(levels, msg.GetValue())
			case *wrapperspb.StringValue:
				col.writeByteArray(levels, unsafeByteArrayFromString(msg.GetValue()))
			case *wrapperspb.BytesValue:
				col.writeByteArray(levels, msg.GetValue())
			case *structpb.Struct:
				writeProtoStruct(col, levels, msg, node)
			case *structpb.ListValue:
				writeProtoList(col, levels, msg, node)
			case *anypb.Any:
				writeProtoAny(col, levels, msg, node)
			default:
				value = value.Elem()
				goto writeValue
			}
			return

		case reflect.Bool:
			col.writeBoolean(levels, value.Bool())
			return

		case reflect.Int8, reflect.Int16, reflect.Int32:
			col.writeInt32(levels, int32(value.Int()))
			return

		case reflect.Int:
			col.writeInt64(levels, value.Int())
			return

		case reflect.Int64:
			if value.Type() == reflect.TypeFor[time.Duration]() {
				writeDuration(col, levels, time.Duration(value.Int()), node)
			} else {
				col.writeInt64(levels, value.Int())
			}
			return

		case reflect.Uint8, reflect.Uint16, reflect.Uint32:
			col.writeInt32(levels, int32(value.Uint()))
			return

		case reflect.Uint, reflect.Uint64:
			col.writeInt64(levels, int64(value.Uint()))
			return

		case reflect.Float32:
			col.writeFloat(levels, float32(value.Float()))
			return

		case reflect.Float64:
			col.writeDouble(levels, value.Float())
			return

		case reflect.String:
			v := value.String()
			switch value.Type() {
			case reflect.TypeFor[json.Number]():
				writeJSONNumber(col, levels, json.Number(v), node)
			default:
				typ := node.Type()
				logicalType := typ.LogicalType()
				if logicalType != nil && logicalType.UUID != nil {
					writeUUID(col, levels, v, typ)
					return
				}
				col.writeByteArray(levels, unsafeByteArrayFromString(v))
			}
			return

		case reflect.Slice:
			if t := value.Type(); t.Elem().Kind() == reflect.Uint8 {
				switch t {
				case reflect.TypeFor[json.RawMessage]():
					val, err := jsonParse(value.Bytes())
					if err != nil {
						panic(fmt.Errorf("failed to parse JSON: %w", err))
					}
					writeJSONToLeaf(col, levels, val, node)
				default:
					col.writeByteArray(levels, value.Bytes())
				}
				return
			}

		case reflect.Array:
			col.writeByteArray(levels, value.Bytes())
			return

		case reflect.Struct:
			switch v := value.Interface().(type) {
			case time.Time:
				writeTime(col, levels, v, node)
				return
			case deprecated.Int96:
				col.writeInt96(levels, v)
				return
			}
		}

		if node.Type().Kind() != ByteArray {
			panic(fmt.Sprintf("cannot write value of type %s to leaf column", value.Type()))
		}

		if node.Optional() && isNullValue(value) {
			col.writeNull(levels)
			return
		}

		b := memory.SliceBuffer[byte]{}
		w := memory.SliceWriter{Buffer: &b}
		defer b.Reset()

		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)

		if err := enc.Encode(value.Interface()); err != nil {
			panic(err)
		}

		data := b.Slice()
		col.writeByteArray(levels, data[:len(data)-1])
	}
}

func structpbValueToReflectValue(v *structpb.Value) reflect.Value {
	switch kind := v.GetKind().(type) {
	case nil:
		return reflect.Value{}
	case *structpb.Value_NullValue:
		return reflect.Value{}
	case *structpb.Value_NumberValue:
		return reflect.ValueOf(&kind.NumberValue)
	case *structpb.Value_StringValue:
		return reflect.ValueOf(&kind.StringValue)
	case *structpb.Value_BoolValue:
		return reflect.ValueOf(&kind.BoolValue)
	case *structpb.Value_StructValue:
		return reflect.ValueOf(kind.StructValue)
	case *structpb.Value_ListValue:
		return reflect.ValueOf(kind.ListValue)
	default:
		panic(fmt.Sprintf("unsupported structpb.Value kind: %T", kind))
	}
}

func unsafeByteArrayFromString(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func writeUUID(col ColumnBuffer, levels columnLevels, str string, typ Type) {
	if typ.Kind() != FixedLenByteArray || typ.Length() != 16 {
		panic(fmt.Errorf("cannot write UUID string to non-FIXED_LEN_BYTE_ARRAY(16) column: %q", str))
	}
	parsedUUID, err := uuid.Parse(str)
	if err != nil {
		panic(fmt.Errorf("cannot parse string %q as UUID: %w", str, err))
	}
	buf := memory.SliceBuffer[byte]{}
	buf.Grow(16)
	buf.Append(parsedUUID[:]...)
	col.writeByteArray(levels, buf.Slice())
	buf.Reset()
}
