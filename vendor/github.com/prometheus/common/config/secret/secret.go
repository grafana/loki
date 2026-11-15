// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package secret provides the Secret type used in Prometheus configuration.
//
// It is split out of package config so that libraries can use Secret without
// pulling in the dependencies of the HTTP client configuration. config.Secret
// is an alias of this type, so the two can be used interchangeably.
package secret

import (
	"encoding/json"

	"github.com/prometheus/common/config/internal/secretvalue"
)

const secretToken = "<secret>"

// Secret special type for storing secrets.
//
// Secret values are redacted when marshalled, unless config.MarshalSecretValue
// is set to true.
type Secret string

// MarshalYAML implements the yaml.Marshaler interface for Secrets.
func (s Secret) MarshalYAML() (any, error) {
	if *secretvalue.Marshal {
		return string(s), nil
	}
	if s != "" {
		return secretToken, nil
	}
	return nil, nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface for Secrets.
func (s *Secret) UnmarshalYAML(unmarshal func(any) error) error {
	type plain Secret
	return unmarshal((*plain)(s))
}

// MarshalJSON implements the json.Marshaler interface for Secret.
func (s Secret) MarshalJSON() ([]byte, error) {
	if *secretvalue.Marshal {
		return json.Marshal(string(s))
	}
	if len(s) == 0 {
		return json.Marshal("")
	}
	return json.Marshal(secretToken)
}
