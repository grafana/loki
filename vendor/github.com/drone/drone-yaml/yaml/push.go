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
	// Push configures a Docker push.
	Push struct {
		Image string `json:"image,omitempty"`
	}

	// push is a tempoary type used to unmarshal
	// the Push struct when long format is used.
	push struct {
		Image string `json:"image,omitempty"`
	}
)

// UnmarshalYAML implements yaml unmarshalling.
func (p *Push) UnmarshalYAML(unmarshal func(interface{}) error) error {
	d := new(push)
	err := unmarshal(&d.Image)
	if err != nil {
		err = unmarshal(d)
	}
	p.Image = d.Image
	return err
}
