package core

// (C) Copyright IBM Corp. 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	yaml "sigs.k8s.io/yaml/goyaml.v2"
)

// OrderedMaps provides a wrapper around the yaml package's
// MapItem type and provides convenience functionality beyond
// what would be available with the MapSlice type, which is similar.
// It enables the ordering of fields in a map for controlling the
// order of keys in the printed YAML strings.
type OrderedMaps struct {
	maps []yaml.MapItem
}

// Add appends a key/value pair to the ordered list of maps.
func (m *OrderedMaps) Add(key string, value interface{}) {
	m.maps = append(m.maps, yaml.MapItem{
		Key:   key,
		Value: value,
	})
}

// GetMaps returns the actual list of ordered maps stored in
// the OrderedMaps instance. Each element is MapItem type,
// which will be serialized by the yaml package in a special
// way that allows the ordering of fields in the YAML.
func (m *OrderedMaps) GetMaps() []yaml.MapItem {
	return m.maps
}

// NewOrderedMaps initializes and returns a new instance of OrderedMaps.
func NewOrderedMaps() *OrderedMaps {
	return &OrderedMaps{}
}
