// Copyright The OpenTelemetry Authors
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

package json // import "go.opentelemetry.io/collector/pdata/internal/json"

import (
	jsoniter "github.com/json-iterator/go"

	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
)

func ReadScope(iter *jsoniter.Iterator, scope *otlpcommon.InstrumentationScope) {
	iter.ReadObjectCB(func(iter *jsoniter.Iterator, f string) bool {
		switch f {
		case "name":
			scope.Name = iter.ReadString()
		case "version":
			scope.Version = iter.ReadString()
		case "attributes":
			iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
				scope.Attributes = append(scope.Attributes, ReadAttribute(iter))
				return true
			})
		case "droppedAttributesCount", "dropped_attributes_count":
			scope.DroppedAttributesCount = ReadUint32(iter)
		default:
			iter.Skip()
		}
		return true
	})
}
