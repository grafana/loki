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
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/buildkite/yaml"
)

var errorMissingKind = errors.New("yaml: missing kind attribute")

// Parse parses the configuration from io.Reader r.
func Parse(r io.Reader) (*Manifest, error) {
	resources, err := ParseRaw(r)
	if err != nil {
		return nil, err
	}
	manifest := new(Manifest)
	for _, raw := range resources {
		if raw == nil {
			continue
		}
		resource, err := parseRaw(raw)
		if err != nil {
			return nil, err
		}
		if resource.GetKind() == "" {
			return nil, errorMissingKind
		}
		manifest.Resources = append(
			manifest.Resources,
			resource,
		)
	}
	return manifest, nil
}

// ParseBytes parses the configuration from bytes b.
func ParseBytes(b []byte) (*Manifest, error) {
	return Parse(
		bytes.NewBuffer(b),
	)
}

// ParseString parses the configuration from string s.
func ParseString(s string) (*Manifest, error) {
	return ParseBytes(
		[]byte(s),
	)
}

// ParseFile parses the configuration from path p.
func ParseFile(p string) (*Manifest, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

func parseRaw(r *RawResource) (Resource, error) {
	var obj Resource
	switch r.Kind {
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
	err := yaml.Unmarshal(r.Data, obj)
	return obj, err
}

// ParseRaw parses the multi-document yaml from the
// io.Reader and returns a slice of raw resources.
func ParseRaw(r io.Reader) ([]*RawResource, error) {
	const newline = '\n'
	var resources []*RawResource
	var resource *RawResource

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if isSeparator(line) {
			resource = nil
		}
		if resource == nil {
			resource = &RawResource{}
			resources = append(resources, resource)
		}
		if isSeparator(line) {
			continue
		}
		if isTerminator(line) {
			break
		}
		if scanner.Err() == io.EOF {
			break
		}
		resource.Data = append(
			resource.Data,
			line...,
		)
		resource.Data = append(
			resource.Data,
			newline,
		)
	}
	for _, resource := range resources {
		err := yaml.Unmarshal(resource.Data, resource)
		if err != nil {
			return nil, err
		}
	}
	return resources, nil
}

// ParseRawString parses the multi-document yaml from s
// and returns a slice of raw resources.
func ParseRawString(s string) ([]*RawResource, error) {
	return ParseRaw(
		strings.NewReader(s),
	)
}

// ParseRawBytes parses the multi-document yaml from b
// and returns a slice of raw resources.
func ParseRawBytes(b []byte) ([]*RawResource, error) {
	return ParseRaw(
		bytes.NewReader(b),
	)
}

// ParseRawFile parses the multi-document yaml from path p
// and returns a slice of raw resources.
func ParseRawFile(p string) ([]*RawResource, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseRaw(f)
}

func isSeparator(s string) bool {
	return strings.HasPrefix(s, "---")
}

func isTerminator(s string) bool {
	return strings.HasPrefix(s, "...")
}
