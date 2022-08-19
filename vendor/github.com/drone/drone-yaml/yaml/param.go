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
	// Parameter represents an configuration parameter that
	// can be defined as a literal or as a reference
	// to a secret.
	Parameter struct {
		Value  interface{} `json:"value,omitempty"`
		Secret string      `json:"from_secret,omitempty" yaml:"from_secret"`
	}

	// parameter is a tempoary type used to unmarshal
	// parameters with references to secrets.
	parameter struct {
		Secret string `yaml:"from_secret"`
	}
)

// UnmarshalYAML implements yaml unmarshalling.
func (p *Parameter) UnmarshalYAML(unmarshal func(interface{}) error) error {
	d := new(parameter)
	err := unmarshal(d)
	if err == nil && d.Secret != "" {
		p.Secret = d.Secret
		return nil
	}
	var i interface{}
	err = unmarshal(&i)
	p.Value = i
	return err
}

// MarshalYAML implements yaml marshalling.
func (p *Parameter) MarshalYAML() (interface{}, error) {
	if p.Secret != "" {
		m := map[string]interface{}{}
		m["from_secret"] = p.Secret
		return m, nil
	}
	if p.Value != "" {
		return p.Value, nil
	}
	return nil, nil
}
