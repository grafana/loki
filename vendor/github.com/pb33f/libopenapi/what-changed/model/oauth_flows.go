// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

// OAuthFlowsChanges represents changes found between two OpenAPI OAuthFlows objects.
type OAuthFlowsChanges struct {
	*PropertyChanges
	ImplicitChanges          *OAuthFlowChanges `json:"implicit,omitempty" yaml:"implicit,omitempty"`
	PasswordChanges          *OAuthFlowChanges `json:"password,omitempty" yaml:"password,omitempty"`
	ClientCredentialsChanges *OAuthFlowChanges `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCodeChanges *OAuthFlowChanges `json:"authCode,omitempty" yaml:"authCode,omitempty"`
	DeviceChanges            *OAuthFlowChanges `json:"device,omitempty" yaml:"device,omitempty"` // OpenAPI 3.2+ device flow
	ExtensionChanges         *ExtensionChanges `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between OAuthFlows objects
func (o *OAuthFlowsChanges) GetAllChanges() []*Change {
	if o == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, o.Changes...)
	if o.ImplicitChanges != nil {
		changes = append(changes, o.ImplicitChanges.GetAllChanges()...)
	}
	if o.PasswordChanges != nil {
		changes = append(changes, o.PasswordChanges.GetAllChanges()...)
	}
	if o.ClientCredentialsChanges != nil {
		changes = append(changes, o.ClientCredentialsChanges.GetAllChanges()...)
	}
	if o.AuthorizationCodeChanges != nil {
		changes = append(changes, o.AuthorizationCodeChanges.GetAllChanges()...)
	}
	if o.DeviceChanges != nil {
		changes = append(changes, o.DeviceChanges.GetAllChanges()...)
	}
	if o.ExtensionChanges != nil {
		changes = append(changes, o.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the number of changes made between two OAuthFlows instances.
func (o *OAuthFlowsChanges) TotalChanges() int {
	if o == nil {
		return 0
	}
	c := o.PropertyChanges.TotalChanges()
	if o.ImplicitChanges != nil {
		c += o.ImplicitChanges.TotalChanges()
	}
	if o.PasswordChanges != nil {
		c += o.PasswordChanges.TotalChanges()
	}
	if o.ClientCredentialsChanges != nil {
		c += o.ClientCredentialsChanges.TotalChanges()
	}
	if o.AuthorizationCodeChanges != nil {
		c += o.AuthorizationCodeChanges.TotalChanges()
	}
	if o.DeviceChanges != nil {
		c += o.DeviceChanges.TotalChanges()
	}
	if o.ExtensionChanges != nil {
		c += o.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the number of breaking changes made between two OAuthFlows objects.
func (o *OAuthFlowsChanges) TotalBreakingChanges() int {
	c := o.PropertyChanges.TotalBreakingChanges()
	if o.ImplicitChanges != nil {
		c += o.ImplicitChanges.TotalBreakingChanges()
	}
	if o.PasswordChanges != nil {
		c += o.PasswordChanges.TotalBreakingChanges()
	}
	if o.ClientCredentialsChanges != nil {
		c += o.ClientCredentialsChanges.TotalBreakingChanges()
	}
	if o.AuthorizationCodeChanges != nil {
		c += o.AuthorizationCodeChanges.TotalBreakingChanges()
	}
	if o.DeviceChanges != nil {
		c += o.DeviceChanges.TotalBreakingChanges()
	}
	return c
}

// CompareOAuthFlows compares a left and right OAuthFlows object. If changes are found a pointer to *OAuthFlowsChanges
// is returned, otherwise nil is returned.
func CompareOAuthFlows(l, r *v3.OAuthFlows) *OAuthFlowsChanges {
	if low.AreEqual(l, r) {
		return nil
	}

	oa := new(OAuthFlowsChanges)
	var changes []*Change

	// client credentials
	if !l.ClientCredentials.IsEmpty() && !r.ClientCredentials.IsEmpty() {
		oa.ClientCredentialsChanges = CompareOAuthFlow(l.ClientCredentials.Value, r.ClientCredentials.Value)
	}
	if !l.ClientCredentials.IsEmpty() && r.ClientCredentials.IsEmpty() {
		CreateChange(&changes, ObjectRemoved, v3.ClientCredentialsLabel,
			l.ClientCredentials.ValueNode, nil, BreakingRemoved(CompOAuthFlows, PropClientCredentials),
			l.ClientCredentials.Value, nil)
	}
	if l.ClientCredentials.IsEmpty() && !r.ClientCredentials.IsEmpty() {
		CreateChange(&changes, ObjectAdded, v3.ClientCredentialsLabel,
			nil, r.ClientCredentials.ValueNode, BreakingAdded(CompOAuthFlows, PropClientCredentials),
			nil, r.ClientCredentials.Value)
	}

	// implicit
	if !l.Implicit.IsEmpty() && !r.Implicit.IsEmpty() {
		oa.ImplicitChanges = CompareOAuthFlow(l.Implicit.Value, r.Implicit.Value)
	}
	if !l.Implicit.IsEmpty() && r.Implicit.IsEmpty() {
		CreateChange(&changes, ObjectRemoved, v3.ImplicitLabel,
			l.Implicit.ValueNode, nil, BreakingRemoved(CompOAuthFlows, PropImplicit),
			l.Implicit.Value, nil)
	}
	if l.Implicit.IsEmpty() && !r.Implicit.IsEmpty() {
		CreateChange(&changes, ObjectAdded, v3.ImplicitLabel,
			nil, r.Implicit.ValueNode, BreakingAdded(CompOAuthFlows, PropImplicit),
			nil, r.Implicit.Value)
	}

	// password
	if !l.Password.IsEmpty() && !r.Password.IsEmpty() {
		oa.PasswordChanges = CompareOAuthFlow(l.Password.Value, r.Password.Value)
	}
	if !l.Password.IsEmpty() && r.Password.IsEmpty() {
		CreateChange(&changes, ObjectRemoved, v3.PasswordLabel,
			l.Password.ValueNode, nil, BreakingRemoved(CompOAuthFlows, PropPassword),
			l.Password.Value, nil)
	}
	if l.Password.IsEmpty() && !r.Password.IsEmpty() {
		CreateChange(&changes, ObjectAdded, v3.PasswordLabel,
			nil, r.Password.ValueNode, BreakingAdded(CompOAuthFlows, PropPassword),
			nil, r.Password.Value)
	}

	// auth code
	if !l.AuthorizationCode.IsEmpty() && !r.AuthorizationCode.IsEmpty() {
		oa.AuthorizationCodeChanges = CompareOAuthFlow(l.AuthorizationCode.Value, r.AuthorizationCode.Value)
	}
	if !l.AuthorizationCode.IsEmpty() && r.AuthorizationCode.IsEmpty() {
		CreateChange(&changes, ObjectRemoved, v3.AuthorizationCodeLabel,
			l.AuthorizationCode.ValueNode, nil, BreakingRemoved(CompOAuthFlows, PropAuthorizationCode),
			l.AuthorizationCode.Value, nil)
	}
	if l.AuthorizationCode.IsEmpty() && !r.AuthorizationCode.IsEmpty() {
		CreateChange(&changes, ObjectAdded, v3.AuthorizationCodeLabel,
			nil, r.AuthorizationCode.ValueNode, BreakingAdded(CompOAuthFlows, PropAuthorizationCode),
			nil, r.AuthorizationCode.Value)
	}

	// device flow (OpenAPI 3.2+)
	if !l.Device.IsEmpty() && !r.Device.IsEmpty() {
		oa.DeviceChanges = CompareOAuthFlow(l.Device.Value, r.Device.Value)
	}
	if !l.Device.IsEmpty() && r.Device.IsEmpty() {
		CreateChange(&changes, ObjectRemoved, v3.DeviceLabel,
			l.Device.ValueNode, nil, BreakingRemoved(CompOAuthFlows, PropDevice),
			l.Device.Value, nil)
	}
	if l.Device.IsEmpty() && !r.Device.IsEmpty() {
		CreateChange(&changes, ObjectAdded, v3.DeviceLabel,
			nil, r.Device.ValueNode, BreakingAdded(CompOAuthFlows, PropDevice),
			nil, r.Device.Value)
	}

	oa.ExtensionChanges = CompareExtensions(l.Extensions, r.Extensions)
	oa.PropertyChanges = NewPropertyChanges(changes)
	return oa
}

// OAuthFlowChanges represents an OpenAPI OAuthFlow object.
type OAuthFlowChanges struct {
	*PropertyChanges
	ExtensionChanges *ExtensionChanges `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between OAuthFlow objects
func (o *OAuthFlowChanges) GetAllChanges() []*Change {
	if o == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, o.Changes...)
	if o.ExtensionChanges != nil {
		changes = append(changes, o.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total number of changes made between two OAuthFlow objects
func (o *OAuthFlowChanges) TotalChanges() int {
	if o == nil {
		return 0
	}
	c := o.PropertyChanges.TotalChanges()
	if o.ExtensionChanges != nil {
		c += o.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of breaking changes made between two OAuthFlow objects
func (o *OAuthFlowChanges) TotalBreakingChanges() int {
	return o.PropertyChanges.TotalBreakingChanges()
}

// CompareOAuthFlow checks a left and a right OAuthFlow object for changes. If found, returns a pointer to
// an OAuthFlowChanges instance, or nil if nothing is found.
func CompareOAuthFlow(l, r *v3.OAuthFlow) *OAuthFlowChanges {
	if low.AreEqual(l, r) {
		return nil
	}

	var changes []*Change
	props := make([]*PropertyCheck, 0, 3)

	props = append(props,
		NewPropertyCheck(CompOAuthFlow, PropAuthorizationURL,
			l.AuthorizationUrl.ValueNode, r.AuthorizationUrl.ValueNode,
			v3.AuthorizationUrlLabel, &changes, l, r),
		NewPropertyCheck(CompOAuthFlow, PropTokenURL,
			l.TokenUrl.ValueNode, r.TokenUrl.ValueNode,
			v3.TokenUrlLabel, &changes, l, r),
		NewPropertyCheck(CompOAuthFlow, PropRefreshURL,
			l.RefreshUrl.ValueNode, r.RefreshUrl.ValueNode,
			v3.RefreshUrlLabel, &changes, l, r),
	)

	CheckProperties(props)

	for k, v := range l.Scopes.Value.FromOldest() {
		if r != nil && r.FindScope(k.Value) == nil {
			CreateChange(&changes, ObjectRemoved, v3.Scopes, v.ValueNode, nil, BreakingRemoved(CompOAuthFlow, PropScopes), k.Value, nil)
			continue
		}
		if r != nil && r.FindScope(k.Value) != nil {
			if v.Value != r.FindScope(k.Value).Value {
				CreateChange(&changes, Modified, v3.Scopes,
					v.ValueNode, r.FindScope(k.Value).ValueNode, BreakingModified(CompOAuthFlow, PropScopes),
					v.Value, r.FindScope(k.Value).Value)
			}
		}
	}
	for k, v := range r.Scopes.Value.FromOldest() {
		if l != nil && l.FindScope(k.Value) == nil {
			CreateChange(&changes, ObjectAdded, v3.Scopes, nil, v.ValueNode, BreakingAdded(CompOAuthFlow, PropScopes), nil, k.Value)
		}
	}
	oa := new(OAuthFlowChanges)
	oa.PropertyChanges = NewPropertyChanges(changes)
	oa.ExtensionChanges = CompareExtensions(l.Extensions, r.Extensions)
	return oa
}
