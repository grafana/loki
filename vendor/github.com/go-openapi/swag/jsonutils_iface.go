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

package swag

import (
	"log"

	"github.com/go-openapi/swag/jsonutils"
)

// JSONMapSlice represents a JSON object, with the order of keys maintained
//
// Deprecated: use [jsonutils.JSONMapSlice] instead, or [yamlutils.YAMLMapSlice] if you marshal YAML.
type JSONMapSlice = jsonutils.JSONMapSlice

// JSONMapItem represents a JSON object, with the order of keys maintained
//
// Deprecated: use [jsonutils.JSONMapItem] instead.
type JSONMapItem = jsonutils.JSONMapItem

// WriteJSON writes json data.
//
// Deprecated: use [jsonutils.WriteJSON] instead.
func WriteJSON(data interface{}) ([]byte, error) { return jsonutils.WriteJSON(data) }

// ReadJSON reads json data.
//
// Deprecated: use [jsonutils.ReadJSON] instead.
func ReadJSON(data []byte, value interface{}) error { return jsonutils.ReadJSON(data, value) }

// DynamicJSONToStruct converts an untyped JSON structure into a target data type.
//
// Deprecated: use [jsonutils.FromDynamicJSON] instead.
func DynamicJSONToStruct(data interface{}, target interface{}) error {
	return jsonutils.FromDynamicJSON(data, target)
}

// ConcatJSON concatenates multiple JSON objects efficiently.
//
// Deprecated: use [jsonutils.ConcatJSON] instead.
func ConcatJSON(blobs ...[]byte) []byte { return jsonutils.ConcatJSON(blobs...) }

// ToDynamicJSON turns a go value into a properly JSON untyped structure.
//
// It is the same as [FromDynamicJSON], but doesn't check for errors.
//
// Deprecated: this function is a misnomer and is unsafe. Use [jsonutils.FromDynamicJSON] instead.
func ToDynamicJSON(value interface{}) interface{} {
	var res interface{}
	if err := FromDynamicJSON(value, &res); err != nil {
		log.Println(err)
	}

	return res
}

// FromDynamicJSON turns a go value into a properly JSON typed structure.
//
// "Dynamic JSON" refers to what you get when unmarshaling JSON into an untyped interface{},
// i.e. objects are represented by map[string]interface{}, arrays by []interface{}, and all
// scalar values are interface{}.
//
// Deprecated: use [jsonutils.FromDynamicJSON] instead.
func FromDynamicJSON(data, target interface{}) error { return jsonutils.FromDynamicJSON(data, target) }
