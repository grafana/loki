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

package loading

import (
	"encoding/json"
	"path/filepath"

	"github.com/go-openapi/swag/yamlutils"
)

// YAMLMatcher matches yaml for a file loader.
func YAMLMatcher(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}

// YAMLDoc loads a yaml document from either http or a file and converts it to json.
func YAMLDoc(path string, opts ...Option) (json.RawMessage, error) {
	yamlDoc, err := YAMLData(path, opts...)
	if err != nil {
		return nil, err
	}

	return yamlutils.YAMLToJSON(yamlDoc)
}

// YAMLData loads a yaml document from either http or a file.
func YAMLData(path string, opts ...Option) (interface{}, error) {
	data, err := LoadFromFileOrHTTP(path, opts...)
	if err != nil {
		return nil, err
	}

	return yamlutils.BytesToYAMLDoc(data)
}
