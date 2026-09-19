//go:build purego

package parquet

import (
	"fmt"
	"iter"
	"reflect"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func writeProtoAnyToGroup(msg *anypb.Any, columns []ColumnBuffer, levels columnLevels, writers []fieldWriter, node Node, value *reflect.Value) bool {
	if msg == nil {
		for i := range writers {
			w := &writers[i]
			w.writeValue(columns, levels, reflect.Value{})
		}
		return true
	}

	typeURL := msg.GetTypeUrl()
	const prefix = "type.googleapis.com/"
	if !strings.HasPrefix(typeURL, prefix) {
		panic(fmt.Sprintf("invalid type_url %q: expected %q prefix", typeURL, prefix))
	}
	path := typeURL[len(prefix):]
	_ = navigateToNestedGroup(node, path)

	unmarshaled, err := msg.UnmarshalNew()
	if err != nil {
		panic(fmt.Sprintf("failed to unmarshal Any: %v", err))
	}

	*value = reflect.ValueOf(makeNestedMap(path, unmarshaled))
	return false
}

func writeProtoMessageToGroup(msg proto.Message, columns []ColumnBuffer, levels columnLevels, writers []fieldWriter) {
	msgValue := reflect.ValueOf(msg)
	if msgValue.Kind() == reflect.Ptr {
		msgValue = msgValue.Elem()
	}
	for i := range writers {
		w := &writers[i]
		fieldValue := findFieldByProtoName(msgValue, w.fieldName)
		w.writeValue(columns, levels, fieldValue)
	}
}

func findFieldByProtoName(structValue reflect.Value, protoName string) reflect.Value {
	structType := structValue.Type()
	for i := range structType.NumField() {
		f := structType.Field(i)
		if tag := f.Tag.Get("protobuf"); tag != "" && tag != "-" {
			if name := parseProtoNameFromTag(tag); name == protoName {
				return structValue.Field(i)
			}
		}
	}
	return reflect.Value{}
}

func parseProtoNameFromTag(tag string) string {
	for name, value := range parseProtoStructTag(tag) {
		if name == "name" {
			return value
		}
	}
	return ""
}

func parseProtoStructTag(tag string) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for part := range strings.SplitSeq(tag, ",") {
			name, value, _ := strings.Cut(part, "=")
			if !yield(name, value) {
				return
			}
		}
	}
}
