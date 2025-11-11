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
	"encoding/json"

	"github.com/go-openapi/swag/yamlutils"
)

// YAMLToJSON converts YAML unmarshaled data into json compatible data
//
// Deprecated: use [yamlutils.YAMLToJSON] instead.
func YAMLToJSON(data interface{}) (json.RawMessage, error) { return yamlutils.YAMLToJSON(data) }

// BytesToYAMLDoc converts a byte slice into a YAML document
//
// Deprecated: use [yamlutils.BytesToYAMLDoc] instead.
func BytesToYAMLDoc(data []byte) (interface{}, error) { return yamlutils.BytesToYAMLDoc(data) }
