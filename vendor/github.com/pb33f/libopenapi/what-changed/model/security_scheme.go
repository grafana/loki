// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// SecuritySchemeChanges represents changes made between Swagger or OpenAPI SecurityScheme Objects.
type SecuritySchemeChanges struct {
	*PropertyChanges
	ExtensionChanges *ExtensionChanges `json:"extensions,omitempty" yaml:"extensions,omitempty"`

	// OpenAPI Version
	OAuthFlowChanges *OAuthFlowsChanges `json:"oAuthFlow,omitempty" yaml:"oAuthFlow,omitempty"`

	// Swagger Version
	ScopesChanges *ScopesChanges `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// GetAllChanges returns a slice of all changes made between SecurityRequirement objects
func (ss *SecuritySchemeChanges) GetAllChanges() []*Change {
	if ss == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, ss.Changes...)
	if ss.OAuthFlowChanges != nil {
		changes = append(changes, ss.OAuthFlowChanges.GetAllChanges()...)
	}
	if ss.ScopesChanges != nil {
		changes = append(changes, ss.ScopesChanges.GetAllChanges()...)
	}
	if ss.ExtensionChanges != nil {
		changes = append(changes, ss.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges represents total changes found between two Swagger or OpenAPI SecurityScheme instances.
func (ss *SecuritySchemeChanges) TotalChanges() int {
	if ss == nil {
		return 0
	}
	c := ss.PropertyChanges.TotalChanges()
	if ss.OAuthFlowChanges != nil {
		c += ss.OAuthFlowChanges.TotalChanges()
	}
	if ss.ScopesChanges != nil {
		c += ss.ScopesChanges.TotalChanges()
	}
	if ss.ExtensionChanges != nil {
		c += ss.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns total number of breaking changes between two SecurityScheme Objects.
func (ss *SecuritySchemeChanges) TotalBreakingChanges() int {
	c := ss.PropertyChanges.TotalBreakingChanges()
	if ss.OAuthFlowChanges != nil {
		c += ss.OAuthFlowChanges.TotalBreakingChanges()
	}
	if ss.ScopesChanges != nil {
		c += ss.ScopesChanges.TotalBreakingChanges()
	}
	return c
}

// CompareSecuritySchemesV2 is a Swagger type safe proxy for CompareSecuritySchemes
func CompareSecuritySchemesV2(l, r *v2.SecurityScheme) *SecuritySchemeChanges {
	return CompareSecuritySchemes(l, r)
}

// CompareSecuritySchemesV3 is an OpenAPI type safe proxt for CompareSecuritySchemes
func CompareSecuritySchemesV3(l, r *v3.SecurityScheme) *SecuritySchemeChanges {
	return CompareSecuritySchemes(l, r)
}

// CompareSecuritySchemes compares left and right Swagger or OpenAPI Security Scheme objects for changes.
// If anything is found, returns a pointer to *SecuritySchemeChanges or nil if nothing is found.
func CompareSecuritySchemes(l, r any) *SecuritySchemeChanges {
	var props []*PropertyCheck
	var changes []*Change

	sc := new(SecuritySchemeChanges)
	if reflect.TypeOf(&v2.SecurityScheme{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v2.SecurityScheme{}) == reflect.TypeOf(r) {

		lSS := l.(*v2.SecurityScheme)
		rSS := r.(*v2.SecurityScheme)

		if low.AreEqual(lSS, rSS) {
			return nil
		}
		addPropertyCheck(&props, lSS.Type.ValueNode, rSS.Type.ValueNode,
			lSS.Type.Value, rSS.Type.Value, &changes, v3.TypeLabel, true, CompSecurityScheme, PropType)

		addPropertyCheck(&props, lSS.Description.ValueNode, rSS.Description.ValueNode,
			lSS.Description.Value, rSS.Description.Value, &changes, v3.DescriptionLabel, false, CompSecurityScheme, PropDescription)

		addPropertyCheck(&props, lSS.Name.ValueNode, rSS.Name.ValueNode,
			lSS.Name.Value, rSS.Name.Value, &changes, v3.NameLabel, true, CompSecurityScheme, PropName)

		addPropertyCheck(&props, lSS.In.ValueNode, rSS.In.ValueNode,
			lSS.In.Value, rSS.In.Value, &changes, v3.InLabel, true, CompSecurityScheme, PropIn)

		addPropertyCheck(&props, lSS.Flow.ValueNode, rSS.Flow.ValueNode,
			lSS.Flow.Value, rSS.Flow.Value, &changes, v3.FlowLabel, true, CompSecurityScheme, PropFlow)

		addPropertyCheck(&props, lSS.AuthorizationUrl.ValueNode, rSS.AuthorizationUrl.ValueNode,
			lSS.AuthorizationUrl.Value, rSS.AuthorizationUrl.Value, &changes, v3.AuthorizationUrlLabel, true, CompSecurityScheme, PropAuthorizationURL)

		addPropertyCheck(&props, lSS.TokenUrl.ValueNode, rSS.TokenUrl.ValueNode,
			lSS.TokenUrl.Value, rSS.TokenUrl.Value, &changes, v3.TokenUrlLabel, true, CompSecurityScheme, PropTokenURL)

		if !lSS.Scopes.IsEmpty() && !rSS.Scopes.IsEmpty() {
			if !low.AreEqual(lSS.Scopes.Value, rSS.Scopes.Value) {
				sc.ScopesChanges = CompareScopes(lSS.Scopes.Value, rSS.Scopes.Value)
			}
		}
		if lSS.Scopes.IsEmpty() && !rSS.Scopes.IsEmpty() {
			CreateChange(&changes, ObjectAdded, v3.ScopesLabel,
				nil, rSS.Scopes.ValueNode, BreakingAdded(CompSecurityScheme, PropScopes), nil, rSS.Scopes.Value)
		}
		if !lSS.Scopes.IsEmpty() && rSS.Scopes.IsEmpty() {
			CreateChange(&changes, ObjectRemoved, v3.ScopesLabel,
				lSS.Scopes.ValueNode, nil, BreakingRemoved(CompSecurityScheme, PropScopes), lSS.Scopes.Value, nil)
		}

		sc.ExtensionChanges = CompareExtensions(lSS.Extensions, rSS.Extensions)
	}

	if reflect.TypeOf(&v3.SecurityScheme{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v3.SecurityScheme{}) == reflect.TypeOf(r) {

		lSS := l.(*v3.SecurityScheme)
		rSS := r.(*v3.SecurityScheme)

		if low.AreEqual(lSS, rSS) {
			return nil
		}
		addPropertyCheck(&props, lSS.Type.ValueNode, rSS.Type.ValueNode,
			lSS.Type.Value, rSS.Type.Value, &changes, v3.TypeLabel,
			BreakingModified(CompSecurityScheme, PropType), CompSecurityScheme, PropType)

		addPropertyCheck(&props, lSS.Description.ValueNode, rSS.Description.ValueNode,
			lSS.Description.Value, rSS.Description.Value, &changes, v3.DescriptionLabel,
			BreakingModified(CompSecurityScheme, PropDescription), CompSecurityScheme, PropDescription)

		addPropertyCheck(&props, lSS.Name.ValueNode, rSS.Name.ValueNode,
			lSS.Name.Value, rSS.Name.Value, &changes, v3.NameLabel,
			BreakingModified(CompSecurityScheme, PropName), CompSecurityScheme, PropName)

		addPropertyCheck(&props, lSS.In.ValueNode, rSS.In.ValueNode,
			lSS.In.Value, rSS.In.Value, &changes, v3.InLabel,
			BreakingModified(CompSecurityScheme, PropIn), CompSecurityScheme, PropIn)

		addPropertyCheck(&props, lSS.Scheme.ValueNode, rSS.Scheme.ValueNode,
			lSS.Scheme.Value, rSS.Scheme.Value, &changes, v3.SchemeLabel,
			BreakingModified(CompSecurityScheme, PropScheme), CompSecurityScheme, PropScheme)

		addPropertyCheck(&props, lSS.BearerFormat.ValueNode, rSS.BearerFormat.ValueNode,
			lSS.BearerFormat.Value, rSS.BearerFormat.Value, &changes, v3.SchemeLabel,
			BreakingModified(CompSecurityScheme, PropBearerFormat), CompSecurityScheme, PropBearerFormat)

		addPropertyCheck(&props, lSS.OpenIdConnectUrl.ValueNode, rSS.OpenIdConnectUrl.ValueNode,
			lSS.OpenIdConnectUrl.Value, rSS.OpenIdConnectUrl.Value, &changes, v3.OpenIdConnectUrlLabel,
			BreakingModified(CompSecurityScheme, PropOpenIDConnectURL), CompSecurityScheme, PropOpenIDConnectURL)

		// OpenAPI 3.2+ fields
		addPropertyCheck(&props, lSS.OAuth2MetadataUrl.ValueNode, rSS.OAuth2MetadataUrl.ValueNode,
			lSS.OAuth2MetadataUrl.Value, rSS.OAuth2MetadataUrl.Value, &changes, v3.OAuth2MetadataUrlLabel,
			BreakingModified(CompSecurityScheme, PropOAuth2MetadataUrl), CompSecurityScheme, PropOAuth2MetadataUrl)

		addPropertyCheck(&props, lSS.Deprecated.ValueNode, rSS.Deprecated.ValueNode,
			lSS.Deprecated.Value, rSS.Deprecated.Value, &changes, v3.DeprecatedLabel,
			BreakingModified(CompSecurityScheme, PropDeprecated), CompSecurityScheme, PropDeprecated)

		if !lSS.Flows.IsEmpty() && !rSS.Flows.IsEmpty() {
			if !low.AreEqual(lSS.Flows.Value, rSS.Flows.Value) {
				sc.OAuthFlowChanges = CompareOAuthFlows(lSS.Flows.Value, rSS.Flows.Value)
			}
		}
		if lSS.Flows.IsEmpty() && !rSS.Flows.IsEmpty() {
			CreateChange(&changes, ObjectAdded, v3.FlowsLabel,
				nil, rSS.Flows.ValueNode, BreakingAdded(CompSecurityScheme, PropFlows), nil, rSS.Flows.Value)
		}
		if !lSS.Flows.IsEmpty() && rSS.Flows.IsEmpty() {
			CreateChange(&changes, ObjectRemoved, v3.FlowsLabel,
				lSS.Flows.ValueNode, nil, BreakingRemoved(CompSecurityScheme, PropFlows), lSS.Flows.Value, nil)
		}
		sc.ExtensionChanges = CompareExtensions(lSS.Extensions, rSS.Extensions)
	}
	CheckProperties(props)
	sc.PropertyChanges = NewPropertyChanges(changes)
	return sc
}
