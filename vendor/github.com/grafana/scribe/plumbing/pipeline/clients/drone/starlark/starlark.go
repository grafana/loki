package starlark

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"github.com/drone/drone-yaml/yaml"
)

func NewStarlark() *Starlark {
	s := Starlark{}
	s.buf = &bytes.Buffer{}
	s.isNewline = true
	return &s
}

func (s *Starlark) MarshalPipeline(pipeline *yaml.Pipeline) {

	name := s.methodName(pipeline.Name, "pipeline")
	s.MethodStart(name)
	s.Return()
	s.Marshal(pipeline)
	s.MethodEnd()

	for _, step := range pipeline.Steps {
		s.MarshalStep(step)
	}
}

func (s *Starlark) MarshalStep(step *yaml.Container) {

	name := s.methodName(step.Name, "step")
	s.MethodStart(name)
	s.Return()
	s.Marshal(step)
	s.MethodEnd()
}

func (s *Starlark) JSONName(data interface{}, fieldName string) string {
	value := reflect.ValueOf(data)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	t := value.Type()
	f, found := t.FieldByName(fieldName)
	if found {
		tag := f.Tag.Get("json")
		parts := strings.Split(tag, ",")
		return parts[0]
	} else {
		return fieldName
	}
}

func (s *Starlark) Marshal(data interface{}) {
	value := reflect.ValueOf(data)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	if value.IsZero() {
		return
	}
	s.MarshalStruct(value, false)
}

func (s *Starlark) MarshalStruct(value reflect.Value, comma bool) {
	s.StartDict()
	for _, field := range reflect.VisibleFields(value.Type()) {
		v := value.FieldByName(field.Name)
		if s.IsEmpty(v) {
			continue
		}
		k := v.Kind()
		if (k == reflect.Interface || k == reflect.Map || k == reflect.Ptr || k == reflect.Slice) &&
			v.IsNil() {
			continue
		}
		name := s.JSONName(value.Interface(), field.Name)
		s.DictFieldName(name)
		s.MarshalField(v)
	}
	s.EndDict(comma)
}

func (s *Starlark) IsEmpty(value reflect.Value) bool {
	switch value.Kind() {
	case 0:
		return true
	case reflect.Slice, reflect.Map:
		return value.Len() == 0

	case reflect.String:
		return value.String() == ""

	case reflect.Bool:
		return !value.Bool()

	case reflect.Struct:
		return value.IsZero()

	default:
		if value.Type().String() == "yaml.BytesSize" {
			return value.Int() == 0
		}
	}
	return false
}

func (s *Starlark) MarshalField(value reflect.Value) {

	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Interface:
		s.MarshalStruct(value, true)

	case reflect.Map:
		s.MarshalMap(value)

	case reflect.Slice:
		s.MarshalSlice(value)

	case reflect.Struct:
		s.MarshalStruct(value, true)

	case reflect.String:
		s.MarshalString(value)

	default:
		s.MarshalOther(value)
	}
}

func (s *Starlark) MarshalString(value reflect.Value) {
	s.Write(fmt.Sprintf("\"%s\",\n", value))
}

func (s *Starlark) MarshalOther(value reflect.Value) {
	s.Write(fmt.Sprintf("%s,\n", value))
}

func (s *Starlark) MarshalMap(v reflect.Value) {
	s.StartDict()
	for _, key := range v.MapKeys() {
		value := v.MapIndex(key)
		s.MarshalMapKey(key.String())
		if value.Type().String() == "*yaml.Variable" {
			secretVal := value.Elem().FieldByName("Secret")
			if secretVal.String() == "" {
				envVal := value.Elem().FieldByName("Value")
				s.MarshalString(envVal)
				continue
			}
		}

		s.MarshalField(value)
	}
	s.EndDict(true)
}

func (s *Starlark) MarshalMapKey(key string) {
	s.Write(fmt.Sprintf(`"%s": `, key))
}

func (s *Starlark) MarshalSlice(value reflect.Value) {
	if value.Len() == 0 {
		return
	}
	s.StartArray()
	for i := 0; i < value.Len(); i++ {
		v := value.Index(i)
		if v.Type().String() == "*yaml.Container" {
			stepName := v.Elem().FieldByName("Name").String()
			s.MethodCall(stepName, "step")

		} else {
			s.MarshalField(v)
		}
	}
	s.EndArray()
}
