// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Info represents a high-level Info object as defined by both OpenAPI 2 and OpenAPI 3.
//
// The object provides metadata about the API. The metadata MAY be used by the clients if needed, and MAY be presented
// in editing or documentation generation tools for convenience.
//
//	v2 - https://swagger.io/specification/v2/#infoObject
//	v3 - https://spec.openapis.org/oas/v3.1.0#info-object
type Info struct {
	Summary        string                              `json:"summary,omitempty" yaml:"summary,omitempty"`
	Title          string                              `json:"title,omitempty" yaml:"title,omitempty"`
	Description    string                              `json:"description,omitempty" yaml:"description,omitempty"`
	TermsOfService string                              `json:"termsOfService,omitempty" yaml:"termsOfService,omitempty"`
	Contact        *Contact                            `json:"contact,omitempty" yaml:"contact,omitempty"`
	License        *License                            `json:"license,omitempty" yaml:"license,omitempty"`
	Version        string                              `json:"version,omitempty" yaml:"version,omitempty"`
	Extensions     *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low            *low.Info
}

// NewInfo will create a new high-level Info instance from a low-level one.
func NewInfo(info *low.Info) *Info {
	i := new(Info)
	i.low = info
	if !info.Title.IsEmpty() {
		i.Title = info.Title.Value
	}
	if !info.Summary.IsEmpty() {
		i.Summary = info.Summary.Value
	}
	if !info.Description.IsEmpty() {
		i.Description = info.Description.Value
	}
	if !info.TermsOfService.IsEmpty() {
		i.TermsOfService = info.TermsOfService.Value
	}
	if !info.Contact.IsEmpty() {
		i.Contact = NewContact(info.Contact.Value)
	}
	if !info.License.IsEmpty() {
		i.License = NewLicense(info.License.Value)
	}
	if !info.Version.IsEmpty() {
		i.Version = info.Version.Value
	}
	if orderedmap.Len(info.Extensions) > 0 {
		i.Extensions = high.ExtractExtensions(info.Extensions)
	}
	return i
}

// GoLow will return the low-level Info instance that was used to create the high-level one.
func (i *Info) GoLow() *low.Info {
	return i.low
}

// GoLowUntyped will return the low-level Info instance that was used to create the high-level one, with no type
func (i *Info) GoLowUntyped() any {
	return i.low
}

// Render will return a YAML representation of the Info object as a byte slice.
func (i *Info) Render() ([]byte, error) {
	return yaml.Marshal(i)
}

// MarshalYAML will create a ready to render YAML representation of the Info object.
func (i *Info) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(i, i.low)
	return nb.Render(), nil
}
