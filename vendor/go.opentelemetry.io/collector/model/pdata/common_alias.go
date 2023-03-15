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

package pdata // import "go.opentelemetry.io/collector/model/pdata"

// This file contains aliases to data structures that are common for all
// signal types, such as timestamps, attributes, etc.

import "go.opentelemetry.io/collector/pdata/pcommon"

// ValueType is an alias for pcommon.ValueType type.
// Deprecated: [v0.49.0] Use pcommon.ValueType instead.
type ValueType = pcommon.ValueType

const (
	// Deprecated: [v0.49.0] Use pcommon.ValueTypeEmpty instead.
	ValueTypeEmpty = pcommon.ValueTypeEmpty

	// Deprecated: [v0.49.0] Use pcommon.ValueTypeString instead.
	ValueTypeString = pcommon.ValueTypeString

	// Deprecated: [v0.49.0] Use pcommon.ValueTypeInt instead.
	ValueTypeInt = pcommon.ValueTypeInt

	// Deprecated: [v0.49.0] Use pcommon.ValueTypeDouble instead.
	ValueTypeDouble = pcommon.ValueTypeDouble

	// Deprecated: [v0.49.0] Use pcommon.ValueTypeBool instead.
	ValueTypeBool = pcommon.ValueTypeBool

	// Deprecated: [v0.49.0] Use pcommon.ValueTypeMap instead.
	ValueTypeMap = pcommon.ValueTypeMap

	// Deprecated: [v0.49.0] Use pcommon.ValueTypeSlice instead.
	ValueTypeSlice = pcommon.ValueTypeSlice

	// Deprecated: [v0.49.0] Use pcommon.ValueTypeBytes instead.
	ValueTypeBytes = pcommon.ValueTypeBytes
)

// Value is an alias for pcommon.Value struct.
// Deprecated: [v0.49.0] Use pcommon.Value instead.
type Value = pcommon.Value

// Aliases for functions to create pcommon.Value.
var (

	// Deprecated: [v0.49.0] Use pcommon.NewValueEmpty instead.
	NewValueEmpty = pcommon.NewValueEmpty

	// Deprecated: [v0.49.0] Use pcommon.NewValueString instead.
	NewValueString = pcommon.NewValueString

	// Deprecated: [v0.49.0] Use pcommon.NewValueInt instead.
	NewValueInt = pcommon.NewValueInt

	// Deprecated: [v0.49.0] Use pcommon.NewValueDouble instead.
	NewValueDouble = pcommon.NewValueDouble

	// Deprecated: [v0.49.0] Use pcommon.NewValueBool instead.
	NewValueBool = pcommon.NewValueBool

	// Deprecated: [v0.49.0] Use pcommon.NewValueMap instead.
	NewValueMap = pcommon.NewValueMap

	// Deprecated: [v0.49.0] Use pcommon.NewValueSlice instead.
	NewValueSlice = pcommon.NewValueSlice

	// Deprecated: [v0.49.0] Use pcommon.NewValueBytes instead.
	NewValueBytes = pcommon.NewValueBytes
)

// Map is an alias for pcommon.Map struct.
// Deprecated: [v0.49.0] Use pcommon.Map instead.
type Map = pcommon.Map

// Aliases for functions to create pcommon.Map.
var (
	// Deprecated: [v0.49.0] Use pcommon.NewMap instead.
	NewMap = pcommon.NewMap

	// Deprecated: [v0.49.0] Use pcommon.NewMapFromRaw instead.
	NewMapFromRaw = pcommon.NewMapFromRaw
)
