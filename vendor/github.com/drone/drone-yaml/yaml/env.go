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

type (
	// Variable represents an environment variable that
	// can be defined as a string literal or as a reference
	// to a secret.
	Variable struct {
		Value  string `json:"value,omitempty"`
		Secret string `json:"from_secret,omitempty" yaml:"from_secret"`
	}

	// variable is a tempoary type used to unmarshal
	// variables with references to secrets.
	variable struct {
		Value  string
		Secret string `yaml:"from_secret"`
	}
)

// UnmarshalYAML implements yaml unmarshalling.
func (v *Variable) UnmarshalYAML(unmarshal func(interface{}) error) error {
	d := new(variable)
	err := unmarshal(&d.Value)
	if err != nil {
		err = unmarshal(d)
	}
	v.Value = d.Value
	v.Secret = d.Secret
	return err
}

// MarshalYAML implements yaml marshalling.
func (v *Variable) MarshalYAML() (interface{}, error) {
	if v.Secret != "" {
		m := map[string]interface{}{}
		m["from_secret"] = v.Secret
		return m, nil
	}
	if v.Value != "" {
		return v.Value, nil
	}
	return nil, nil
}
