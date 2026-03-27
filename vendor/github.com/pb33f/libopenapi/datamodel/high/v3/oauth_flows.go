// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// OAuthFlows represents a high-level OpenAPI 3+ OAuthFlows object that is backed by a low-level one.
//   - https://spec.openapis.org/oas/v3.1.0#oauth-flows-object
type OAuthFlows struct {
	Implicit          *OAuthFlow                          `json:"implicit,omitempty" yaml:"implicit,omitempty"`
	Password          *OAuthFlow                          `json:"password,omitempty" yaml:"password,omitempty"`
	ClientCredentials *OAuthFlow                          `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCode *OAuthFlow                          `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty"`
	Device            *OAuthFlow                          `json:"device,omitempty" yaml:"device,omitempty"` // OpenAPI 3.2+ device flow
	Extensions        *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low               *low.OAuthFlows
}

// NewOAuthFlows creates a new high-level OAuthFlows instance from a low-level one.
func NewOAuthFlows(flows *low.OAuthFlows) *OAuthFlows {
	o := new(OAuthFlows)
	o.low = flows
	if !flows.Implicit.IsEmpty() {
		o.Implicit = NewOAuthFlow(flows.Implicit.Value)
	}
	if !flows.Password.IsEmpty() {
		o.Password = NewOAuthFlow(flows.Password.Value)
	}
	if !flows.ClientCredentials.IsEmpty() {
		o.ClientCredentials = NewOAuthFlow(flows.ClientCredentials.Value)
	}
	if !flows.AuthorizationCode.IsEmpty() {
		o.AuthorizationCode = NewOAuthFlow(flows.AuthorizationCode.Value)
	}
	if !flows.Device.IsEmpty() {
		o.Device = NewOAuthFlow(flows.Device.Value)
	}
	o.Extensions = high.ExtractExtensions(flows.Extensions)
	return o
}

// GoLow returns the low-level OAuthFlows instance used to create the high-level one.
func (o *OAuthFlows) GoLow() *low.OAuthFlows {
	return o.low
}

// GoLowUntyped will return the low-level OAuthFlows instance that was used to create the high-level one, with no type
func (o *OAuthFlows) GoLowUntyped() any {
	return o.low
}

// Render will return a YAML representation of the OAuthFlows object as a byte slice.
func (o *OAuthFlows) Render() ([]byte, error) {
	return yaml.Marshal(o)
}

// MarshalYAML will create a ready to render YAML representation of the OAuthFlows object.
func (o *OAuthFlows) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(o, o.low)
	return nb.Render(), nil
}
