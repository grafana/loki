// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package array

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

// ValueAsAnyer is an optional interface for arrays that can return a native
// Go value for a slot. Built-in array types implement it. It is not part of
// arrow.Array so that adding this capability is not a source-incompatible
// change for downstream Array implementations.
type ValueAsAnyer interface {
	ValueAsAny(i int) any
}

// ValueAsAny returns the native Go value at index i, or nil if the slot is null.
// Unlike GetOneForMarshal, values are not converted for JSON encoding
// (for example int8 stays int8, timestamps stay arrow.Timestamp, lists are
// []any of native values, and structs are []any of [name, value] pairs so
// field order and duplicate names are preserved).
//
// If arr does not implement ValueAsAnyer, ValueAsAny panics.
func ValueAsAny(arr arrow.Array, i int) any {
	if v, ok := arr.(ValueAsAnyer); ok {
		return v.ValueAsAny(i)
	}
	panic(fmt.Sprintf("arrow/array: %T does not implement ValueAsAny", arr))
}

// valueAsAnyFromListLike builds a []any of native child values for one list slot.
func valueAsAnyFromListLike(a ListLike, i int) any {
	if a.IsNull(i) {
		return nil
	}

	start, end := a.ValueOffsets(i)
	vals := a.ListValues()
	out := make([]any, end-start)
	for j := start; j < end; j++ {
		out[j-start] = ValueAsAny(vals, int(j))
	}
	return out
}
