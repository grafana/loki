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
	// Signature is a resource that provides an hmac
	// signature of combined resources. This signature
	// can be used to validate authenticity and prevent
	// tampering.
	Signature struct {
		Version string `json:"version,omitempty"`
		Kind    string `json:"kind"`
		Hmac    string `json:"hmac"`
	}
)

// GetVersion returns the resource version.
func (s *Signature) GetVersion() string { return s.Version }

// GetKind returns the resource kind.
func (s *Signature) GetKind() string { return s.Kind }

// Validate returns an error if the signature is invalid.
func (s Signature) Validate() error {
	if s.Hmac == "" {
		return errors.New("yaml: invalid signature. missing hash")
	}
	return nil
}
