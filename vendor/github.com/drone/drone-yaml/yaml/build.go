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
	// Build configures a Docker build.
	Build struct {
		Args       map[string]string `json:"args,omitempty"`
		CacheFrom  []string          `json:"cache_from,omitempty" yaml:"cache_from"`
		Context    string            `json:"context,omitempty"`
		Dockerfile string            `json:"dockerfile,omitempty"`
		Image      string            `json:"image,omitempty"`
		Labels     map[string]string `json:"labels,omitempty"`
	}

	// build is a tempoary type used to unmarshal
	// the Build struct when long format is used.
	build struct {
		Args       map[string]string
		CacheFrom  []string `yaml:"cache_from"`
		Context    string
		Dockerfile string
		Image      string
		Labels     map[string]string
	}
)

// UnmarshalYAML implements yaml unmarshalling.
func (b *Build) UnmarshalYAML(unmarshal func(interface{}) error) error {
	d := new(build)
	err := unmarshal(&d.Image)
	if err != nil {
		err = unmarshal(d)
	}
	b.Args = d.Args
	b.CacheFrom = d.CacheFrom
	b.Context = d.Context
	b.Dockerfile = d.Dockerfile
	b.Labels = d.Labels
	b.Image = d.Image
	return err
}
