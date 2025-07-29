// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	"go.opentelemetry.io/collector/pdata/internal/json"
)

func UnmarshalJSONIterEntityRef(ms EntityRef, iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "schemaUrl", "schema_url":
			ms.orig.SchemaUrl = iter.ReadString()
		case "type":
			ms.orig.Type = iter.ReadString()
		case "idKeys", "id_keys":
			UnmarshalJSONIterStringSlice(NewStringSlice(&ms.orig.IdKeys, ms.state), iter)
		case "descriptionKeys", "description_keys":
			UnmarshalJSONIterStringSlice(NewStringSlice(&ms.orig.DescriptionKeys, ms.state), iter)
		default:
			iter.Skip()
		}
		return true
	})
}
