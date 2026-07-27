package utils

import (
	"reflect"
	"strings"
	"testing"
)

func isNil(object interface{}) bool {
	if object == nil {
		return true
	}

	value := reflect.ValueOf(object)
	kind := value.Kind()
	isNilableKind := containsKind(
		[]reflect.Kind{
			reflect.Chan, reflect.Func,
			reflect.Interface, reflect.Map,
			reflect.Ptr, reflect.Slice},
		kind)

	if isNilableKind && value.IsNil() {
		return true
	}

	return false
}

func containsKind(kinds []reflect.Kind, kind reflect.Kind) bool {
	for i := 0; i < len(kinds); i++ {
		if kind == kinds[i] {
			return true
		}
	}

	return false
}

func AssertEqual(t *testing.T, a, b interface{}) {
	if !reflect.DeepEqual(a, b) {
		t.Errorf("%v != %v", a, b)
	}
}

func AssertNil(t *testing.T, object interface{}) {
	if !isNil(object) {
		t.Errorf("%v is not nil", object)
	}
}

func AssertNotNil(t *testing.T, object interface{}) {
	if isNil(object) {
		t.Errorf("%v is nil", object)
	}
}

func AssertContains(t *testing.T, contains string, msgAndArgs ...string) {
	for _, value := range msgAndArgs {
		if ok := strings.Contains(contains, value); !ok {
			t.Errorf("%s does not contain %s", contains, value)
		}
	}
}
