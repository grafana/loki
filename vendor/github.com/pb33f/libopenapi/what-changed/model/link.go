// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

// LinkChanges represent changes made between two OpenAPI Link Objects.
type LinkChanges struct {
	*PropertyChanges
	ExtensionChanges *ExtensionChanges `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	ServerChanges    *ServerChanges    `json:"server,omitempty" yaml:"server,omitempty"`
}

// GetAllChanges returns a slice of all changes made between Link objects
func (l *LinkChanges) GetAllChanges() []*Change {
	if l == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, l.Changes...)
	if l.ServerChanges != nil {
		changes = append(changes, l.ServerChanges.GetAllChanges()...)
	}
	if l.ExtensionChanges != nil {
		changes = append(changes, l.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total changes made between OpenAPI Link objects
func (l *LinkChanges) TotalChanges() int {
	if l == nil {
		return 0
	}
	c := l.PropertyChanges.TotalChanges()
	if l.ExtensionChanges != nil {
		c += l.ExtensionChanges.TotalChanges()
	}
	if l.ServerChanges != nil {
		c += l.ServerChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the number of breaking changes made between two OpenAPI Link Objects
func (l *LinkChanges) TotalBreakingChanges() int {
	c := l.PropertyChanges.TotalBreakingChanges()
	if l.ServerChanges != nil {
		c += l.ServerChanges.TotalBreakingChanges()
	}
	return c
}

// CompareLinks checks a left and right OpenAPI Link for any changes. If they are found, returns a pointer to
// LinkChanges, and returns nil if nothing is found.
func CompareLinks(l, r *v3.Link) *LinkChanges {
	if low.AreEqual(l, r) {
		return nil
	}

	var changes []*Change
	props := make([]*PropertyCheck, 0, 4)

	props = append(props,
		NewPropertyCheck(CompLink, PropOperationRef,
			l.OperationRef.ValueNode, r.OperationRef.ValueNode,
			v3.OperationRefLabel, &changes, l, r),
		NewPropertyCheck(CompLink, PropOperationID,
			l.OperationId.ValueNode, r.OperationId.ValueNode,
			v3.OperationIdLabel, &changes, l, r),
		NewPropertyCheck(CompLink, PropRequestBody,
			l.RequestBody.ValueNode, r.RequestBody.ValueNode,
			v3.RequestBodyLabel, &changes, l, r),
		NewPropertyCheck(CompLink, PropDescription,
			l.Description.ValueNode, r.Description.ValueNode,
			v3.DescriptionLabel, &changes, l, r),
	)

	CheckProperties(props)
	lc := new(LinkChanges)
	lc.ExtensionChanges = CompareExtensions(l.Extensions, r.Extensions)

	// server
	if !l.Server.IsEmpty() && !r.Server.IsEmpty() {
		if !low.AreEqual(l.Server.Value, r.Server.Value) {
			lc.ServerChanges = CompareServers(l.Server.Value, r.Server.Value)
		}
	}
	if !l.Server.IsEmpty() && r.Server.IsEmpty() {
		CreateChange(&changes, PropertyRemoved, v3.ServerLabel,
			l.Server.ValueNode, nil, BreakingRemoved(CompLink, PropServer),
			l.Server.Value, nil)
	}
	if l.Server.IsEmpty() && !r.Server.IsEmpty() {
		CreateChange(&changes, PropertyAdded, v3.ServerLabel,
			nil, r.Server.ValueNode, BreakingAdded(CompLink, PropServer),
			nil, r.Server.Value)
	}

	// parameters
	lValues := make(map[string]low.ValueReference[string])
	rValues := make(map[string]low.ValueReference[string])
	for k, v := range l.Parameters.Value.FromOldest() {
		lValues[k.Value] = v
	}
	for k, v := range r.Parameters.Value.FromOldest() {
		rValues[k.Value] = v
	}
	for k := range lValues {
		if _, ok := rValues[k]; !ok {
			CreateChange(&changes, ObjectRemoved, v3.ParametersLabel,
				lValues[k].ValueNode, nil, BreakingRemoved(CompLink, PropParameters),
				k, nil)
			continue
		}
		if lValues[k].Value != rValues[k].Value {
			CreateChange(&changes, Modified, v3.ParametersLabel,
				lValues[k].ValueNode, rValues[k].ValueNode, BreakingModified(CompLink, PropParameters),
				k, k)
		}

	}
	for k := range rValues {
		if _, ok := lValues[k]; !ok {
			CreateChange(&changes, ObjectAdded, v3.ParametersLabel,
				nil, rValues[k].ValueNode, BreakingAdded(CompLink, PropParameters),
				nil, k)
		}
	}

	lc.PropertyChanges = NewPropertyChanges(changes)
	return lc
}
