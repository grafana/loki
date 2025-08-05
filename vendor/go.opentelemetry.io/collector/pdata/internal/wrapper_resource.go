// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	"go.opentelemetry.io/collector/pdata/internal/json"
)

func UnmarshalJSONIterResource(ms Resource, iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "attributes":
			UnmarshalJSONIterMap(NewMap(&ms.orig.Attributes, ms.state), iter)
		case "droppedAttributesCount", "dropped_attributes_count":
			ms.orig.DroppedAttributesCount = iter.ReadUint32()
		case "entityRefs", "entity_refs":
			UnmarshalJSONIterEntityRefSlice(NewEntityRefSlice(&ms.orig.EntityRefs, ms.state), iter)
		default:
			iter.Skip()
		}
		return true
	})
}
