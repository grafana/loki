package core

// (C) Copyright IBM Corp. 2021.
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
	"fmt"
	"sort"
	"strings"
)

// ConstructServiceURL returns a service URL that is constructed by formatting a parameterized URL.
//
// Parameters:
//
// parameterizedUrl: URL that contains variable placeholders, e.g. "{scheme}://ibm.com".
//
// defaultUrlVariables: map from variable names to default values.
//
//	Each variable in the parameterized URL must have a default value specified in this map.
//
// providedUrlVariables: map from variable names to desired values.
//
//	If a variable is not provided in this map,
//	the default variable value will be used instead.
func ConstructServiceURL(
	parameterizedUrl string,
	defaultUrlVariables map[string]string,
	providedUrlVariables map[string]string,
) (string, error) {
	GetLogger().Debug("Constructing service URL from parameterized URL: %s\n", parameterizedUrl)

	// Verify the provided variable names.
	for providedName := range providedUrlVariables {
		if _, ok := defaultUrlVariables[providedName]; !ok {
			// Get all accepted variable names (the keys of the default variables map).
			var acceptedNames []string
			for name := range defaultUrlVariables {
				acceptedNames = append(acceptedNames, name)
			}
			sort.Strings(acceptedNames)

			return "", fmt.Errorf(
				"'%s' is an invalid variable name.\nValid variable names: %s.",
				providedName,
				acceptedNames,
			)
		}
	}

	// Format the URL with provided or default variable values.
	formattedUrl := parameterizedUrl

	for name, defaultValue := range defaultUrlVariables {
		providedValue, ok := providedUrlVariables[name]

		// Use the default variable value if none was provided.
		if !ok {
			providedValue = defaultValue
		}
		formattedUrl = strings.Replace(formattedUrl, "{"+name+"}", providedValue, 1)
	}
	GetLogger().Debug("Returning service URL: %s\n", formattedUrl)
	return formattedUrl, nil
}
