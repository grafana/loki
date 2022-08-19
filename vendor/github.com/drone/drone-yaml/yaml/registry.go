// Copyright 2019 Drone IO, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package yaml

import "errors"

type (
	// Registry is a resource that provides encrypted
	// registry credentials and pointers to external
	// registry credentials (e.g. from vault).
	Registry struct {
		Version string `json:"version,omitempt"`
		Kind    string `json:"kind,omitempty"`
		Type    string `json:"type,omitempty"`

		Data map[string]string `json:"data,omitempty"`
	}
)

// GetVersion returns the resource version.
func (r *Registry) GetVersion() string { return r.Version }

// GetKind returns the resource kind.
func (r *Registry) GetKind() string { return r.Kind }

// Validate returns an error if the registry is invalid.
func (r *Registry) Validate() error {
	if len(r.Data) == 0 {
		return errors.New("yaml: invalid registry resource")
	}
	return nil
}
