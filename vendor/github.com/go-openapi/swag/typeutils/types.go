// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package typeutils

import "reflect"

type zeroable interface {
	IsZero() bool
}

// IsZero returns true when the value passed into the function is a zero value.
// This allows for safer checking of interface values.
func IsZero(data any) bool {
	v := reflect.ValueOf(data)
	// check for nil data
	switch v.Kind() { //nolint:exhaustive
	case
		reflect.Interface,
		reflect.Func,
		reflect.Chan,
		reflect.Pointer,
		reflect.UnsafePointer,
		reflect.Map,
		reflect.Slice:
		if v.IsNil() {
			return true
		}
	}

	// check for things that have an IsZero method instead
	if vv, ok := data.(zeroable); ok {
		return vv.IsZero()
	}

	// continue with slightly more complex reflection
	switch v.Kind() { //nolint:exhaustive
	case reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Struct, reflect.Array:
		return reflect.DeepEqual(data, reflect.Zero(v.Type()).Interface())
	case reflect.Invalid:
		return true
	default:
		return false
	}
}

// IsNil checks if input is nil.
//
// For types chan, func, interface, map, pointer, or slice it returns true if its argument is nil.
//
// See [reflect.Value.IsNil].
func IsNil(input any) bool {
	if input == nil {
		return true
	}

	kind := reflect.TypeOf(input).Kind()
	switch kind { //nolint:exhaustive
	case reflect.Pointer,
		reflect.UnsafePointer,
		reflect.Map,
		reflect.Slice,
		reflect.Chan,
		reflect.Interface,
		reflect.Func:
		return reflect.ValueOf(input).IsNil()
	default:
		return false
	}
}
