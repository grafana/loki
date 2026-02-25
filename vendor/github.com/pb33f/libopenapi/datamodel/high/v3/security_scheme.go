// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"

	"github.com/pb33f/libopenapi/datamodel/high"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// buildLowSecurityScheme builds a low-level SecurityScheme from a resolved YAML node.
func buildLowSecurityScheme(node *yaml.Node, idx *index.SpecIndex) (*low.SecurityScheme, error) {
	var ss low.SecurityScheme
	lowmodel.BuildModel(node, &ss)
	ss.Build(context.Background(), nil, node, idx)
	return &ss, nil
}

// SecurityScheme represents a high-level OpenAPI 3+ SecurityScheme object that is backed by a low-level one.
//
// Defines a security scheme that can be used by the operations.
//
// Supported schemes are HTTP authentication, an API key (either as a header, a cookie parameter or as a query parameter),
// mutual TLS (use of a client certificate), OAuth2's common flows (implicit, password, client credentials and
// authorization code) as defined in RFC6749 (https://www.rfc-editor.org/rfc/rfc6749), and OpenID Connect Discovery.
// Please note that as of 2020, the implicit  flow is about to be deprecated by OAuth 2.0 Security Best Current Practice.
// Recommended for most use case is Authorization Code Grant flow with PKCE.
//   - https://spec.openapis.org/oas/v3.1.0#security-scheme-object
type SecurityScheme struct {
	Reference         string                              `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Type              string                              `json:"type,omitempty" yaml:"type,omitempty"`
	Description       string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Name              string                              `json:"name,omitempty" yaml:"name,omitempty"`
	In                string                              `json:"in,omitempty" yaml:"in,omitempty"`
	Scheme            string                              `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	BearerFormat      string                              `json:"bearerFormat,omitempty" yaml:"bearerFormat,omitempty"`
	Flows             *OAuthFlows                         `json:"flows,omitempty" yaml:"flows,omitempty"`
	OpenIdConnectUrl  string                              `json:"openIdConnectUrl,omitempty" yaml:"openIdConnectUrl,omitempty"`
	OAuth2MetadataUrl string                              `json:"oauth2MetadataUrl,omitempty" yaml:"oauth2MetadataUrl,omitempty"` // OpenAPI 3.2+ OAuth2 metadata URL
	Deprecated        bool                                `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`               // OpenAPI 3.2+ deprecated flag
	Extensions        *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low               *low.SecurityScheme
}

// NewSecurityScheme creates a new high-level SecurityScheme from a low-level one.
func NewSecurityScheme(ss *low.SecurityScheme) *SecurityScheme {
	s := new(SecurityScheme)
	s.low = ss
	s.Type = ss.Type.Value
	s.Description = ss.Description.Value
	s.Name = ss.Name.Value
	s.Scheme = ss.Scheme.Value
	s.In = ss.In.Value
	s.BearerFormat = ss.BearerFormat.Value
	s.OpenIdConnectUrl = ss.OpenIdConnectUrl.Value
	s.OAuth2MetadataUrl = ss.OAuth2MetadataUrl.Value
	s.Deprecated = ss.Deprecated.Value
	s.Extensions = high.ExtractExtensions(ss.Extensions)
	if !ss.Flows.IsEmpty() {
		s.Flows = NewOAuthFlows(ss.Flows.Value)
	}
	return s
}

// GoLow returns the low-level SecurityScheme that was used to create the high-level one.
func (s *SecurityScheme) GoLow() *low.SecurityScheme {
	return s.low
}

// GoLowUntyped will return the low-level SecurityScheme instance that was used to create the high-level one, with no type
func (s *SecurityScheme) GoLowUntyped() any {
	return s.low
}

// IsReference returns true if this SecurityScheme is a reference to another SecurityScheme definition.
func (s *SecurityScheme) IsReference() bool {
	return s.Reference != ""
}

// GetReference returns the reference string if this is a reference SecurityScheme.
func (s *SecurityScheme) GetReference() string {
	return s.Reference
}

// Render will return a YAML representation of the SecurityScheme object as a byte slice.
func (s *SecurityScheme) Render() ([]byte, error) {
	return yaml.Marshal(s)
}

// MarshalYAML will create a ready to render YAML representation of the SecurityScheme object.
func (s *SecurityScheme) MarshalYAML() (interface{}, error) {
	// Handle reference-only security scheme
	if s.Reference != "" {
		return utils.CreateRefNode(s.Reference), nil
	}
	nb := high.NewNodeBuilder(s, s.low)
	return nb.Render(), nil
}

// MarshalYAMLInline will create a ready to render YAML representation of the SecurityScheme object,
// with all references resolved inline.
func (s *SecurityScheme) MarshalYAMLInline() (interface{}, error) {
	// reference-only objects render as $ref nodes
	if s.Reference != "" {
		return utils.CreateRefNode(s.Reference), nil
	}

	// resolve external reference if present
	if s.low != nil {
		// buildLowSecurityScheme never returns an error, so we can ignore it
		rendered, _ := high.RenderExternalRef(s.low, buildLowSecurityScheme, NewSecurityScheme)
		if rendered != nil {
			return rendered, nil
		}
	}

	return high.RenderInline(s, s.low)
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the SecurityScheme object,
// resolving any references inline where possible. Uses the provided context for cycle detection.
// The ctx parameter should be *base.InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (s *SecurityScheme) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	if s.Reference != "" {
		return utils.CreateRefNode(s.Reference), nil
	}

	// resolve external reference if present
	if s.low != nil {
		// buildLowSecurityScheme never returns an error, so we can ignore it
		rendered, _ := high.RenderExternalRefWithContext(s.low, buildLowSecurityScheme, NewSecurityScheme, ctx)
		if rendered != nil {
			return rendered, nil
		}
	}

	return high.RenderInlineWithContext(s, s.low, ctx)
}

// CreateSecuritySchemeRef creates a SecurityScheme that renders as a $ref to another security scheme definition.
// This is useful when building OpenAPI specs programmatically and you want to reference
// a security scheme defined in components/securitySchemes rather than inlining the full definition.
//
// Example:
//
//	ss := v3.CreateSecuritySchemeRef("#/components/securitySchemes/BearerAuth")
//
// Renders as:
//
//	$ref: '#/components/securitySchemes/BearerAuth'
func CreateSecuritySchemeRef(ref string) *SecurityScheme {
	return &SecurityScheme{Reference: ref}
}
