// Copyright 2015 go-swagger maintainers
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

// Package mangling provides name mangling capabilities.
//
// Name mangling is an important stage when generating code:
// it helps construct safe program identifiers that abide by the language rules
// and play along with linters.
//
// Examples:
//
// Suppose we get an object name taken from an API spec: "json_object",
//
// We may generate a legit go type name using [NameMangler.ToGoName]: "JsonObject".
//
// We may then locate this type in a source file named using [NameMangler.ToFileName]: "json_object.go".
//
// The methods exposed by the NameMangler are used to generate code in many different contexts, such as:
//
//   - generating exported or unexported go identifiers from a JSON schema or an API spec
//   - generating file names
//   - generating human-readable comments for types and variables
//   - generating JSON-like API identifiers from go code
//   - ...
package mangling
