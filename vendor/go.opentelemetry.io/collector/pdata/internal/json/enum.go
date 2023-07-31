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

package json // import "go.opentelemetry.io/collector/pdata/internal/json"

import (
	jsoniter "github.com/json-iterator/go"
)

// ReadEnumValue returns the enum integer value representation. Accepts both enum names and enum integer values.
// See https://developers.google.com/protocol-buffers/docs/proto3#json.
func ReadEnumValue(iter *jsoniter.Iterator, valueMap map[string]int32) int32 {
	switch iter.WhatIsNext() {
	case jsoniter.NumberValue:
		return iter.ReadInt32()
	case jsoniter.StringValue:
		val, ok := valueMap[iter.ReadString()]
		// Same behavior with official protbuf JSON decoder,
		// see https://github.com/open-telemetry/opentelemetry-proto-go/pull/81
		if !ok {
			iter.ReportError("ReadEnumValue", "unknown string value")
			return 0
		}
		return val
	default:
		iter.ReportError("ReadEnumValue", "unsupported value type")
		return 0
	}
}
