// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// marshalExtensions appends extension key-value pairs from ext into the ordered map m.
func marshalExtensions(m *orderedmap.Map[string, any], ext *orderedmap.Map[string, *yaml.Node]) {
	if ext == nil {
		return
	}
	for pair := ext.First(); pair != nil; pair = pair.Next() {
		m.Set(pair.Key(), pair.Value())
	}
}
