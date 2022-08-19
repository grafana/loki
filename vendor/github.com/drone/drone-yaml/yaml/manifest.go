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

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/buildkite/yaml"
)

// Resource enums.
const (
	KindCron      = "cron"
	KindPipeline  = "pipeline"
	KindRegistry  = "registry"
	KindSecret    = "secret"
	KindSignature = "signature"
)

type (
	// Manifest is a collection of Drone resources.
	Manifest struct {
		Resources []Resource
	}

	// Resource represents a Drone resource.
	Resource interface {
		// GetVersion returns the resource version.
		GetVersion() string

		// GetKind returns the resource kind.
		GetKind() string
	}

	// RawResource is a raw encoded resource with the
	// resource kind and type extracted.
	RawResource struct {
		Version string
		Kind    string
		Type    string
		Data    []byte `yaml:"-"`
	}

	resource struct {
		Version string
		Kind    string `json:"kind"`
		Type    string `json:"type"`
	}
)

// UnmarshalJSON implement the json.Unmarshaler.
func (m *Manifest) UnmarshalJSON(b []byte) error {
	messages := []json.RawMessage{}
	err := json.Unmarshal(b, &messages)
	if err != nil {
		return err
	}
	for _, message := range messages {
		res := new(resource)
		err := json.Unmarshal(message, res)
		if err != nil {
			return err
		}
		var obj Resource
		switch res.Kind {
		case "cron":
			obj = new(Cron)
		case "secret":
			obj = new(Secret)
		case "signature":
			obj = new(Signature)
		case "registry":
			obj = new(Registry)
		default:
			obj = new(Pipeline)
		}
		err = json.Unmarshal(message, obj)
		if err != nil {
			return err
		}
		m.Resources = append(m.Resources, obj)
	}
	return nil
}

// MarshalJSON implement the json.Marshaler.
func (m *Manifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.Resources)
}

// MarshalYAML is not implemented and returns an error. This is
// because the Manifest is a representation of multiple yaml
// documents, and MarshalYAML would otherwise attempt to marshal
// as a single Yaml document. Use the Encode method instead.
func (m *Manifest) MarshalYAML() (interface{}, error) {
	return nil, errors.New("yaml: marshal not implemented")
}

// Encode encodes the manifest in Yaml format.
func (m *Manifest) Encode() ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := yaml.NewEncoder(buf)
	for _, res := range m.Resources {
		if err := enc.Encode(res); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}
