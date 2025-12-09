//go:build !purego

package parquet

import (
	"fmt"
	"reflect"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
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
	protoMsg := msg.ProtoReflect()
	descriptor := protoMsg.Descriptor()
	for i := range writers {
		w := &writers[i]
		protoField := descriptor.Fields().ByName(protoreflect.Name(w.fieldName))
		if protoField == nil || !protoMsg.Has(protoField) {
			w.writeValue(columns, levels, reflect.Value{})
			continue
		}
		protoValue := protoMsg.Get(protoField)
		var fieldValue reflect.Value
		if protoField.Kind() == protoreflect.MessageKind {
			fieldValue = reflect.ValueOf(protoValue.Message().Interface())
		} else {
			fieldValue = reflect.ValueOf(protoValue.Interface())
		}
		w.writeValue(columns, levels, fieldValue)
	}
}
