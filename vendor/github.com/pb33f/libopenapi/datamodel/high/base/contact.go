// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Contact represents a high-level representation of the Contact definitions found at
//
//	v2 - https://swagger.io/specification/v2/#contactObject
//	v3 - https://spec.openapis.org/oas/v3.1.0#contact-object
type Contact struct {
	Name       string                              `json:"name,omitempty" yaml:"name,omitempty"`
	URL        string                              `json:"url,omitempty" yaml:"url,omitempty"`
	Email      string                              `json:"email,omitempty" yaml:"email,omitempty"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *low.Contact                        `json:"-" yaml:"-"` // low-level representation
}

// NewContact will create a new Contact instance using a low-level Contact
func NewContact(contact *low.Contact) *Contact {
	c := new(Contact)
	c.low = contact
	c.URL = contact.URL.Value
	c.Name = contact.Name.Value
	c.Email = contact.Email.Value
	c.Extensions = high.ExtractExtensions(contact.Extensions)
	return c
}

// GoLow returns the low level Contact object used to create the high-level one.
func (c *Contact) GoLow() *low.Contact {
	return c.low
}

// GoLowUntyped will return the low-level Contact instance that was used to create the high-level one, with no type
func (c *Contact) GoLowUntyped() any {
	return c.low
}

func (c *Contact) Render() ([]byte, error) {
	return yaml.Marshal(c)
}

func (c *Contact) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(c, c.low)
	return nb.Render(), nil
}
