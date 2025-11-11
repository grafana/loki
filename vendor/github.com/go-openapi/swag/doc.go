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

// Package swag contains a bunch of helper functions for go-openapi and go-swagger projects.
//
// You may also use it standalone for your projects.
//
// NOTE: all features that used to be exposed as package-level members (constants, variables,
// functions and types) are now deprecated and are superseded by equivalent features in
// more specialized sub-packages.
// Moving forward, no additional feature will be added to the [swag] API directly at the root package level,
// which remains there for backward-compatibility purposes.
//
// Child modules will continue to evolve or some new ones may be added in the future.
//
// # Modules
//
//   - [cmdutils]      utilities to work with CLIs
//
//   - [conv]          type conversion utilities
//
//   - [fileutils]     file utilities
//
//   - [jsonname]      JSON utilities
//
//   - [jsonutils]     JSON utilities
//
//   - [loading]       file loading
//
//   - [mangling]      safe name generation
//
//   - [netutils]      networking utilities
//
//   - [stringutils]   `string` utilities
//
//   - [typeutils]     `go` types utilities
//
//   - [yamlutils]     YAML utilities
//
// # Dependencies
//
// This repo has a few dependencies outside of the standard library:
//
//   - YAML utilities depend on [go.yaml.in/yaml/v3]
package swag

//go:generate mockery
