package parquet

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/parquet-go/jsonlite"
	"github.com/parquet-go/parquet-go/format"
	"github.com/parquet-go/parquet-go/internal/memory"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func writeProtoTimestamp(col ColumnBuffer, levels columnLevels, ts *timestamppb.Timestamp, node Node) {
	if ts == nil {
		col.writeNull(levels)
		return
	}
	var typ = node.Type()
	var unit format.TimeUnit
	if lt := typ.LogicalType(); lt != nil && lt.Timestamp != nil {
		unit = lt.Timestamp.Unit
	} else {
		unit = Nanosecond.TimeUnit()
	}
	var t = ts.AsTime()
	var value int64
	switch {
	case unit.Millis != nil:
		value = t.UnixMilli()
	case unit.Micros != nil:
		value = t.UnixMicro()
	default:
		value = t.UnixNano()
	}
	switch kind := typ.Kind(); kind {
	case Int32, Int64:
		col.writeInt64(levels, value)
	case Float, Double:
		col.writeDouble(levels, t.Sub(time.Unix(0, 0)).Seconds())
	case ByteArray:
		col.writeByteArray(levels, t.AppendFormat(nil, time.RFC3339Nano))
	default:
		panic(fmt.Sprintf("unsupported physical type for timestamp: %v", kind))
	}
}

func writeProtoDuration(col ColumnBuffer, levels columnLevels, dur *durationpb.Duration, node Node) {
	if dur == nil {
		col.writeNull(levels)
		return
	}
	d := dur.AsDuration()
	switch kind := node.Type().Kind(); kind {
	case Int32, Int64:
		col.writeInt64(levels, d.Nanoseconds())
	case Float, Double:
		col.writeDouble(levels, d.Seconds())
	case ByteArray:
		col.writeByteArray(levels, unsafeByteArrayFromString(d.String()))
	default:
		panic(fmt.Sprintf("unsupported physical type for duration: %v", kind))
	}
}

func writeProtoStruct(col ColumnBuffer, levels columnLevels, s *structpb.Struct, node Node) {
	b := memory.SliceBuffer[byte]{}
	b.Grow(2 * proto.Size(s))
	writeProtoStructJSON(&b, s)
	col.writeByteArray(levels, b.Slice())
	b.Reset()
}

func writeProtoList(col ColumnBuffer, levels columnLevels, l *structpb.ListValue, node Node) {
	b := memory.SliceBuffer[byte]{}
	b.Grow(2 * proto.Size(l))
	writeProtoListValueJSON(&b, l)
	col.writeByteArray(levels, b.Slice())
	b.Reset()
}

func writeProtoStructJSON(b *memory.SliceBuffer[byte], s *structpb.Struct) {
	if s == nil {
		b.Append('n', 'u', 'l', 'l')
		return
	}

	fields := s.GetFields()
	if len(fields) == 0 {
		b.Append('{', '}')
		return
	}

	keys := make([]string, 0, 20)
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	b.AppendValue('{')
	for i, key := range keys {
		if i > 0 {
			b.AppendValue(',')
		}
		b.AppendFunc(func(buf []byte) []byte {
			return jsonlite.AppendQuote(buf, key)
		})
		b.AppendValue(':')
		writeProtoValueJSON(b, fields[key])
	}
	b.AppendValue('}')
}

func writeProtoValueJSON(b *memory.SliceBuffer[byte], v *structpb.Value) {
	switch k := v.GetKind().(type) {
	case *structpb.Value_StringValue:
		b.AppendFunc(func(buf []byte) []byte {
			return jsonlite.AppendQuote(buf, k.StringValue)
		})
	case *structpb.Value_BoolValue:
		b.AppendFunc(func(buf []byte) []byte {
			return strconv.AppendBool(buf, k.BoolValue)
		})
	case *structpb.Value_NumberValue:
		b.AppendFunc(func(buf []byte) []byte {
			return strconv.AppendFloat(buf, k.NumberValue, 'g', -1, 64)
		})
	case *structpb.Value_StructValue:
		writeProtoStructJSON(b, k.StructValue)
	case *structpb.Value_ListValue:
		writeProtoListValueJSON(b, k.ListValue)
	default:
		b.Append('n', 'u', 'l', 'l')
	}
}

func writeProtoListValueJSON(b *memory.SliceBuffer[byte], l *structpb.ListValue) {
	if l == nil {
		b.Append('n', 'u', 'l', 'l')
		return
	}
	values := l.GetValues()
	b.AppendValue('[')
	for i, v := range values {
		if i > 0 {
			b.AppendValue(',')
		}
		writeProtoValueJSON(b, v)
	}
	b.AppendValue(']')
}

func writeProtoAny(col ColumnBuffer, levels columnLevels, a *anypb.Any, node Node) {
	if a == nil {
		col.writeNull(levels)
		return
	}
	data, err := proto.Marshal(a)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal anypb.Any: %v", err))
	}
	col.writeByteArray(levels, data)
}

// makeNestedMap creates a nested map structure from a dot-separated path.
// For example, "testproto.ProtoPayload" with value v creates:
// map["testproto"] = map["ProtoPayload"] = v
func makeNestedMap(path string, value any) any {
	components := make([]string, 0, 8)
	for component := range strings.SplitSeq(path, ".") {
		components = append(components, component)
	}

	result := value
	for i := len(components) - 1; i >= 0; i-- {
		result = map[string]any{
			components[i]: result,
		}
	}
	return result
}

// navigateToNestedGroup walks through a nested group structure following the given path.
// The path is expected to be a dot-separated string (e.g., "testproto.ProtoPayload").
// Returns the node at the end of the path, or panics if the path doesn't match the schema.
func navigateToNestedGroup(node Node, path string) Node {
	for component := range strings.SplitSeq(path, ".") {
		var found bool
		for _, field := range node.Fields() {
			if field.Name() == component {
				node, found = field, true
				break
			}
		}
		if !found {
			panic(fmt.Sprintf("field %q not found in schema while navigating path %q", component, path))
		}
	}
	return node
}
