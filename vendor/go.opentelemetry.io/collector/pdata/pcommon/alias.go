// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pcommon // import "go.opentelemetry.io/collector/pdata/pcommon"

// This file contains aliases to data structures that are common for all
// signal types, such as timestamps, attributes, etc.

import "go.opentelemetry.io/collector/pdata/internal"

// ValueType specifies the type of Value.
type ValueType = internal.ValueType

const (
	ValueTypeEmpty  = internal.ValueTypeEmpty
	ValueTypeString = internal.ValueTypeString
	ValueTypeInt    = internal.ValueTypeInt
	ValueTypeDouble = internal.ValueTypeDouble
	ValueTypeBool   = internal.ValueTypeBool
	ValueTypeMap    = internal.ValueTypeMap
	ValueTypeSlice  = internal.ValueTypeSlice
	ValueTypeBytes  = internal.ValueTypeBytes
)

// Value is a mutable cell containing any value. Typically used as an element of Map or Slice.
// Must use one of NewValue+ functions below to create new instances.
//
// Intended to be passed by value since internally it is just a pointer to actual
// value representation. For the same reason passing by value and calling setters
// will modify the original, e.g.:
//
//   func f1(val Value) { val.SetIntVal(234) }
//   func f2() {
//       v := NewValueString("a string")
//       f1(v)
//       _ := v.Type() // this will return ValueTypeInt
//   }
//
// Important: zero-initialized instance is not valid for use. All Value functions below must
// be called only on instances that are created via NewValue+ functions.
type Value = internal.Value

var (
	// NewValueEmpty creates a new Value with an empty value.
	NewValueEmpty = internal.NewValueEmpty

	// NewValueString creates a new Value with the given string value.
	NewValueString = internal.NewValueString

	// NewValueInt creates a new Value with the given int64 value.
	NewValueInt = internal.NewValueInt

	// NewValueDouble creates a new Value with the given float64 value.
	NewValueDouble = internal.NewValueDouble

	// NewValueBool creates a new Value with the given bool value.
	NewValueBool = internal.NewValueBool

	// NewValueMap creates a new Value of map type.
	NewValueMap = internal.NewValueMap

	// NewValueSlice creates a new Value of array type.
	NewValueSlice = internal.NewValueSlice

	// NewValueBytes creates a new Value with the given []byte value.
	// The caller must ensure the []byte passed in is not modified after the call is made, sharing the data
	// across multiple attributes is forbidden.
	NewValueBytes = internal.NewValueBytes
)

// Map stores a map of string keys to elements of Value type.
type Map = internal.Map

var (
	// NewMap creates a Map with 0 elements.
	NewMap = internal.NewMap

	// NewMapFromRaw creates a Map with values from the given map[string]interface{}.
	NewMapFromRaw = internal.NewMapFromRaw
)
