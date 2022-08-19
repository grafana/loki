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

// TODO(bradrydzewski) deprecate Secret
// TODO(bradrydzewski) deprecate ExternalData

type (
	// Secret is a resource that provides encrypted data
	// and pointers to external data (i.e. from vault).
	Secret struct {
		Version string `json:"version,omitempty"`
		Kind    string `json:"kind,omitempty"`
		Type    string `json:"type,omitempty"`
		Name    string `json:"name,omitempty"`

		Data string    `json:"data,omitempty"`
		Get  SecretGet `json:"get,omitempty"`
	}

	// SecretGet defines a request to get a secret from
	// an external sevice at the specified path, or with the
	// specified name.
	SecretGet struct {
		Path string `json:"path,omitempty"`
		Name string `json:"name,omitempty"`
		Key  string `json:"key,omitempty"`
	}

	// ExternalData defines the path and name of external
	// data located in an external or remote storage system.
	ExternalData struct {
		Path string `json:"path,omitempty"`
		Name string `json:"name,omitempty"`
	}
)

// GetVersion returns the resource version.
func (s *Secret) GetVersion() string { return s.Version }

// GetKind returns the resource kind.
func (s *Secret) GetKind() string { return s.Kind }

// Validate returns an error if the secret is invalid.
func (s *Secret) Validate() error {
	if len(s.Data) == 0 && len(s.Get.Path) == 0 && len(s.Get.Name) == 0 {
		return errors.New("yaml: invalid secret resource")
	}
	return nil
}
